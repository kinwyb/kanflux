package scheduler

// TaskStore 任务状态存储接口
type TaskStore interface {
	// Load 加载所有任务状态
	Load() (map[string]*TaskState, error)
	// Save 保存单个任务状态
	Save(state *TaskState) error
	// SaveAll 保存所有任务状态
	SaveAll(states map[string]*TaskState) error
	// Close 关闭存储（可选实现）
	Close() error
}