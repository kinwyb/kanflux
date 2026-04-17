package scheduler

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// JSONTaskStore JSON 文件存储实现
type JSONTaskStore struct {
	filePath string
	mu       sync.RWMutex
}

// NewJSONTaskStore 创建 JSON 文件存储
func NewJSONTaskStore(filePath string) *JSONTaskStore {
	// 确保目录存在
	EnsureStateDir(filePath)

	return &JSONTaskStore{
		filePath: filePath,
	}
}

// Load 加载所有任务状态
func (s *JSONTaskStore) Load() (map[string]*TaskState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			// 文件不存在，返回空状态
			return make(map[string]*TaskState), nil
		}
		return nil, err
	}

	var states map[string]*TaskState
	if err := json.Unmarshal(data, &states); err != nil {
		return nil, err
	}

	return states, nil
}

// Save 保存单个任务状态
func (s *JSONTaskStore) Save(state *TaskState) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	states, err := s.loadWithoutLock()
	if err != nil {
		return err
	}

	states[state.TaskID] = state

	return s.saveAllWithoutLock(states)
}

// SaveAll 保存所有任务状态
func (s *JSONTaskStore) SaveAll(states map[string]*TaskState) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.saveAllWithoutLock(states)
}

// Close 关闭存储（无操作）
func (s *JSONTaskStore) Close() error {
	return nil
}

// loadWithoutLock 不加锁加载（内部使用）
func (s *JSONTaskStore) loadWithoutLock() (map[string]*TaskState, error) {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]*TaskState), nil
		}
		return nil, err
	}

	var states map[string]*TaskState
	if err := json.Unmarshal(data, &states); err != nil {
		return nil, err
	}

	return states, nil
}

// saveAllWithoutLock 不加锁保存（内部使用）
func (s *JSONTaskStore) saveAllWithoutLock(states map[string]*TaskState) error {
	// 确保目录存在
	dir := filepath.Dir(s.filePath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}

	data, err := json.MarshalIndent(states, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.filePath, data, 0644)
}