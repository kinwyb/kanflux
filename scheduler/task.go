package scheduler

import "time"

// Task 运行时任务结构
type Task struct {
	config  *TaskConfig
	nextRun time.Time // 下次执行时间
	lastRun time.Time // 上次执行时间
	running bool      // 是否正在执行
}

// TaskState 任务执行状态（用于持久化）
type TaskState struct {
	TaskID       string    `json:"task_id"`
	LastRun      time.Time `json:"last_run"`
	LastResult   string    `json:"last_result"`
	LastError    string    `json:"last_error,omitempty"`
	SuccessCount int       `json:"success_count"`
	FailCount    int       `json:"fail_count"`
	NextRun      time.Time `json:"next_run"`
}

// newTask 创建新任务
func newTask(config *TaskConfig) *Task {
	return &Task{
		config:  config,
		running: false,
	}
}

// GetConfig 获取任务配置
func (t *Task) GetConfig() *TaskConfig {
	return t.config
}

// GetNextRun 获取下次执行时间
func (t *Task) GetNextRun() time.Time {
	return t.nextRun
}

// GetLastRun 获取上次执行时间
func (t *Task) GetLastRun() time.Time {
	return t.lastRun
}

// IsRunning 是否正在执行
func (t *Task) IsRunning() bool {
	return t.running
}

// IsEnabled 是否启用
func (t *Task) IsEnabled() bool {
	return t.config.Enabled
}

// GetID 获取任务ID
func (t *Task) GetID() string {
	return t.config.ID
}

// GetName 获取任务名称
func (t *Task) GetName() string {
	return t.config.Name
}