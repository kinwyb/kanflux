package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/kinwyb/kanflux/bus"
)

// Scheduler 定时任务调度器
type Scheduler struct {
	config     *SchedulerConfig
	tasks      map[string]*Task        // taskID -> Task
	taskStates map[string]*TaskState   // taskID -> TaskState
	bus        *bus.MessageBus         // 发布 InboundMessage
	store      TaskStore               // 任务状态持久化
	workspace  string                  // 工作目录

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	mu     sync.RWMutex
}

// NewScheduler 创建定时任务调度器
func NewScheduler(config *SchedulerConfig, bus *bus.MessageBus, workspace string) *Scheduler {
	s := &Scheduler{
		config:     config,
		tasks:      make(map[string]*Task),
		taskStates: make(map[string]*TaskState),
		bus:        bus,
		workspace:  workspace,
	}

	// 初始化任务
	for _, taskConfig := range config.Tasks {
		task := newTask(&taskConfig)
		s.tasks[taskConfig.ID] = task
	}

	// 设置默认值
	if s.config.CheckInterval == "" {
		s.config.CheckInterval = "1m"
	}

	// 初始化状态存储
	if config.PersistState {
		stateFile := config.StateFile
		if stateFile == "" {
			stateFile = ".kanflux/scheduler/state.json"
		}
		if !filepath.IsAbs(stateFile) {
			stateFile = filepath.Join(workspace, stateFile)
		}
		s.store = NewJSONTaskStore(stateFile)
	}

	return s
}

// Start 启动调度器
func (s *Scheduler) Start(ctx context.Context) error {
	s.ctx, s.cancel = context.WithCancel(ctx)

	// 加载任务状态
	if s.store != nil {
		states, err := s.store.Load()
		if err != nil {
			slog.Warn("Failed to load task states", "error", err)
		} else {
			for taskID, state := range states {
				s.taskStates[taskID] = state
			}
		}
	}

	// 计算每个任务的下次执行时间
	s.calculateAllNextRuns()

	// 启动调度循环
	s.wg.Add(1)
	go s.scheduleLoop()

	slog.Info("Task scheduler started", "tasks", len(s.tasks))
	return nil
}

// Stop 停止调度器
func (s *Scheduler) Stop() error {
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()

	// 保存任务状态
	if s.store != nil {
		if err := s.store.SaveAll(s.taskStates); err != nil {
			slog.Warn("Failed to save task states", "error", err)
		}
	}

	slog.Info("Task scheduler stopped")
	return nil
}

// scheduleLoop 调度循环
func (s *Scheduler) scheduleLoop() {
	defer s.wg.Done()

	// 解析检查间隔
	checkInterval, err := parseInterval(s.config.CheckInterval)
	if err != nil {
		checkInterval = 1 * time.Minute
		slog.Warn("Invalid check_interval, using default 1m", "error", err)
	}

	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.checkAndExecute()
		}
	}
}

// checkAndExecute 检查并执行到期任务
func (s *Scheduler) checkAndExecute() {
	now := time.Now()

	s.mu.RLock()
	tasksToRun := make([]*Task, 0)
	for _, task := range s.tasks {
		if task.IsEnabled() && !task.running && !task.nextRun.IsZero() && task.nextRun.Before(now) {
			tasksToRun = append(tasksToRun, task)
		}
	}
	s.mu.RUnlock()

	// 执行到期任务
	for _, task := range tasksToRun {
		go s.executeTask(task)
	}
}

// executeTask 执行单个任务
func (s *Scheduler) executeTask(task *Task) {
	// 标记运行中
	s.mu.Lock()
	task.running = true
	s.mu.Unlock()

	defer func() {
		// 标记运行完成，计算下次执行时间
		s.mu.Lock()
		task.running = false
		task.lastRun = time.Now()
		s.calculateNextRun(task)
		s.mu.Unlock()
	}()

	ctx := s.ctx
	taskID := task.GetID()
	target := task.config.Target
	content := task.config.Content

	slog.Info("Executing scheduled task",
		"task_id", taskID,
		"name", task.GetName(),
		"target_channel", target.Channel,
		"target_chat", target.ChatID)

	// 发布 InboundMessage 到 MessageBus，走统一的 AgentManager.RouteInbound 流程
	inbound := &bus.InboundMessage{
		ID:            fmt.Sprintf("task_%s_%d", taskID, time.Now().UnixNano()),
		Channel:       target.Channel,
		AccountID:     target.AccountID,
		SenderID:      "scheduler",
		ChatID:        target.ChatID,
		Content:       content.Prompt,
		StreamingMode: "accumulate",
		Timestamp:     time.Now(),
		Metadata: map[string]interface{}{
			"scheduled_task_id":   taskID,
			"scheduled_task_name": task.GetName(),
			"target_agent":        target.AgentName,
		},
	}

	// 发布到 MessageBus
	if err := s.bus.PublishInbound(ctx, inbound); err != nil {
		slog.Error("Failed to publish scheduled task", "task_id", taskID, "error", err)
		s.updateTaskState(taskID, "", err)
		return
	}

	slog.Info("Scheduled task published to MessageBus", "task_id", taskID)
	s.updateTaskState(taskID, "triggered", nil)
}

// calculateAllNextRuns 计算所有任务的下次执行时间
func (s *Scheduler) calculateAllNextRuns() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, task := range s.tasks {
		s.calculateNextRun(task)
	}
}

// calculateNextRun 计算单个任务的下次执行时间
func (s *Scheduler) calculateNextRun(task *Task) {
	if task.config.Schedule.Cron == "" {
		task.nextRun = time.Time{}
		return
	}

	now := time.Now()
	// 如果有上次执行时间，从上次执行时间开始计算（避免重复执行）
	if !task.lastRun.IsZero() {
		now = task.lastRun
	}

	nextRun, err := parseCron(task.config.Schedule.Cron, now)
	if err != nil {
		slog.Error("Failed to parse cron expression",
			"task_id", task.GetID(),
			"cron", task.config.Schedule.Cron,
			"error", err)
		task.nextRun = time.Time{}
		return
	}

	task.nextRun = nextRun

	// 更新状态
	if state, ok := s.taskStates[task.GetID()]; ok {
		state.NextRun = nextRun
	}
}

// updateTaskState 更新任务状态
func (s *Scheduler) updateTaskState(taskID string, result string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, exists := s.taskStates[taskID]
	if !exists {
		state = &TaskState{
			TaskID: taskID,
		}
		s.taskStates[taskID] = state
	}

	state.LastRun = time.Now()
	state.LastResult = result
	if err != nil {
		state.LastError = err.Error()
		state.FailCount++
	} else {
		state.LastError = ""
		state.SuccessCount++
	}

	// 查找任务并更新 NextRun
	if task, ok := s.tasks[taskID]; ok {
		state.NextRun = task.nextRun
	}

	// 持久化单个任务状态
	if s.store != nil {
		if saveErr := s.store.Save(state); saveErr != nil {
			slog.Warn("Failed to save task state", "task_id", taskID, "error", saveErr)
		}
	}
}

// AddTask 添加新任务
func (s *Scheduler) AddTask(config *TaskConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.tasks[config.ID]; exists {
		return fmt.Errorf("task '%s' already exists", config.ID)
	}

	task := newTask(config)
	s.calculateNextRun(task)
	s.tasks[config.ID] = task

	slog.Info("Task added", "task_id", config.ID, "name", config.Name)
	return nil
}

// RemoveTask 移除任务
func (s *Scheduler) RemoveTask(taskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.tasks[taskID]; !exists {
		return fmt.Errorf("task '%s' not found", taskID)
	}

	delete(s.tasks, taskID)
	delete(s.taskStates, taskID)

	slog.Info("Task removed", "task_id", taskID)
	return nil
}

// UpdateTask 更新任务配置
func (s *Scheduler) UpdateTask(config *TaskConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, exists := s.tasks[config.ID]
	if !exists {
		return fmt.Errorf("task '%s' not found", config.ID)
	}

	task.config = config
	s.calculateNextRun(task)

	slog.Info("Task updated", "task_id", config.ID)
	return nil
}

// TriggerTask 手动触发任务
func (s *Scheduler) TriggerTask(taskID string) error {
	s.mu.RLock()
	task, exists := s.tasks[taskID]
	s.mu.RUnlock()

	if !exists {
		return fmt.Errorf("task '%s' not found", taskID)
	}

	go s.executeTask(task)
	return nil
}

// GetTaskStatus 获取任务状态
func (s *Scheduler) GetTaskStatus(taskID string) (*TaskState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	state, exists := s.taskStates[taskID]
	if !exists {
		// 如果任务存在但状态不存在，返回初始状态
		if task, ok := s.tasks[taskID]; ok {
			return &TaskState{
				TaskID:  taskID,
				NextRun: task.nextRun,
			}, nil
		}
		return nil, fmt.Errorf("task '%s' not found", taskID)
	}

	return state, nil
}

// ListTasks 列出所有任务配置
func (s *Scheduler) ListTasks() []*TaskConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*TaskConfig, 0, len(s.tasks))
	for _, task := range s.tasks {
		result = append(result, task.config)
	}
	return result
}

// ListTaskDetails 列出所有任务的详细信息（包含状态）
func (s *Scheduler) ListTaskDetails() []TaskDetail {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]TaskDetail, 0, len(s.tasks))
	for _, task := range s.tasks {
		detail := TaskDetail{
			Config:    task.config,
			NextRun:   task.nextRun,
			LastRun:   task.lastRun,
			IsRunning: task.running,
		}

		if state, ok := s.taskStates[task.GetID()]; ok {
			detail.State = state
		}

		result = append(result, detail)
	}
	return result
}

// TaskDetail 任务详细信息
type TaskDetail struct {
	Config    *TaskConfig
	State     *TaskState
	NextRun   time.Time
	LastRun   time.Time
	IsRunning bool
}

// GetTaskCount 获取任务数量
func (s *Scheduler) GetTaskCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.tasks)
}

// EnsureStateDir 确保状态文件目录存在
func EnsureStateDir(stateFile string) error {
	dir := filepath.Dir(stateFile)
	if dir != "" && dir != "." {
		return os.MkdirAll(dir, 0755)
	}
	return nil
}