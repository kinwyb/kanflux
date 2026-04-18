package scheduler

import (
	"fmt"
	"time"
)

// Accessor 定义 Scheduler 访问接口，供外部工具使用
// Scheduler 默认实现此接口
type Accessor interface {
	// AddTask 添加新任务
	AddTask(config *TaskConfig) error
	// RemoveTask 移除任务
	RemoveTask(taskID string) error
	// UpdateTask 更新任务配置
	UpdateTask(config *TaskConfig) error
	// TriggerTask 手动触发任务
	TriggerTask(taskID string) error
	// GetTaskStatus 获取任务状态
	GetTaskStatus(taskID string) (*TaskState, error)
	// ListTaskDetails 列出所有任务的详细信息
	ListTaskDetails() []TaskDetail
	// GetTaskCount 获取任务数量
	GetTaskCount() int
}

// TaskInfo 任务信息摘要（用于工具输出）
type TaskInfo struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Enabled     bool      `json:"enabled"`
	Cron        string    `json:"cron"`
	Channel     string    `json:"channel"`
	ChatID      string    `json:"chat_id"`
	AgentName   string    `json:"agent_name"`
	Prompt      string    `json:"prompt"`
	NextRun     time.Time `json:"next_run"`
	LastRun     time.Time `json:"last_run"`
	IsRunning   bool      `json:"is_running"`
	SuccessCount int      `json:"success_count"`
	FailCount   int       `json:"fail_count"`
	LastResult  string    `json:"last_result"`
	LastError   string    `json:"last_error"`
}

// GetTaskInfoList 获取任务信息列表（用于工具输出）
func (s *Scheduler) GetTaskInfoList() []TaskInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]TaskInfo, 0, len(s.tasks))
	for _, task := range s.tasks {
		info := TaskInfo{
			ID:          task.GetID(),
			Name:        task.GetName(),
			Description: task.config.Description,
			Enabled:     task.IsEnabled(),
			Cron:        task.config.Schedule.Cron,
			Channel:     task.config.Target.Channel,
			ChatID:      task.config.Target.ChatID,
			AgentName:   task.config.Target.AgentName,
			Prompt:      task.config.Content.Prompt,
			NextRun:     task.nextRun,
			LastRun:     task.lastRun,
			IsRunning:   task.running,
		}

		if state, ok := s.taskStates[task.GetID()]; ok {
			info.SuccessCount = state.SuccessCount
			info.FailCount = state.FailCount
			info.LastResult = state.LastResult
			info.LastError = state.LastError
		}

		result = append(result, info)
	}
	return result
}

// GetTaskInfo 获取单个任务信息
func (s *Scheduler) GetTaskInfo(taskID string) (*TaskInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	task, exists := s.tasks[taskID]
	if !exists {
		return nil, fmtTaskNotFound(taskID)
	}

	info := &TaskInfo{
		ID:          task.GetID(),
		Name:        task.GetName(),
		Description: task.config.Description,
		Enabled:     task.IsEnabled(),
		Cron:        task.config.Schedule.Cron,
		Channel:     task.config.Target.Channel,
		ChatID:      task.config.Target.ChatID,
		AgentName:   task.config.Target.AgentName,
		Prompt:      task.config.Content.Prompt,
		NextRun:     task.nextRun,
		LastRun:     task.lastRun,
		IsRunning:   task.running,
	}

	if state, ok := s.taskStates[taskID]; ok {
		info.SuccessCount = state.SuccessCount
		info.FailCount = state.FailCount
		info.LastResult = state.LastResult
		info.LastError = state.LastError
	}

	return info, nil
}

// fmtTaskNotFound 返回任务不存在错误
func fmtTaskNotFound(taskID string) error {
	return fmtErr("task '%s' not found", taskID)
}

// fmtErr 格式化错误
func fmtErr(format string, args ...interface{}) error {
	return fmt.Errorf(format, args...)
}