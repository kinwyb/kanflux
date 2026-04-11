package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/adk"
)

// Manager 会话管理器
type Manager struct {
	sessions  map[string]*Session
	mu        sync.RWMutex
	baseDir   string
	dateIndex map[string]string // session key -> date folder (内存缓存)
}

// NewManager 创建会话管理器
func NewManager(baseDir string) (*Manager, error) {
	baseDir = filepath.Join(baseDir, ".kanflux", "sessions")
	// 确保目录存在
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, err
	}
	return &Manager{
		sessions:  make(map[string]*Session),
		baseDir:   baseDir,
		dateIndex: make(map[string]string),
	}, nil
}

// GetOrCreate 获取或创建会话
func (m *Manager) GetOrCreate(key string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 检查内存缓存
	if session, ok := m.sessions[key]; ok {
		return session, nil
	}

	// 尝试从磁盘加载
	session, err := m.load(key)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		// 文件不存在，创建新会话
		session = &Session{
			Key:       key,
			Messages:  []adk.Message{},
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
			Metadata:  make(map[string]interface{}),
		}
	}

	// 添加到缓存
	m.sessions[key] = session
	return session, nil
}

// Save 保存会话
func (m *Manager) Save(session *Session) error {
	return m.save(session)
}

// save 保存会话到磁盘
func (m *Manager) save(session *Session) error {
	session.mu.RLock()
	defer session.mu.RUnlock()

	// 确定文件路径（使用session的CreatedAt日期）
	dateFolder := session.CreatedAt.Format("2006-01-02")
	filePath := m.sessionPath(session.Key, dateFolder)

	// 确保日期目录存在
	dateDir := filepath.Dir(filePath)
	if err := os.MkdirAll(dateDir, 0755); err != nil {
		return err
	}

	// 创建临时文件
	tmpPath := filePath + ".tmp"
	file, err := os.Create(tmpPath)
	if err != nil {
		return err
	}
	defer file.Close()

	// 写入元数据行
	encoder := json.NewEncoder(file)
	metadata := map[string]interface{}{
		"_type":      "metadata",
		"created_at": session.CreatedAt,
		"updated_at": session.UpdatedAt,
		"metadata":   session.Metadata,
	}
	if err := encoder.Encode(metadata); err != nil {
		return err
	}

	// 写入消息
	for _, msg := range session.Messages {
		if err := encoder.Encode(msg); err != nil {
			return err
		}
	}

	// 原子性重命名
	if err := os.Rename(tmpPath, filePath); err != nil {
		return err
	}

	// 更新日期索引缓存
	m.mu.Lock()
	m.dateIndex[session.Key] = dateFolder
	m.mu.Unlock()

	return nil
}

// Delete 删除会话
func (m *Manager) Delete(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 从缓存中删除
	delete(m.sessions, key)

	// 从日期索引中删除
	dateFolder, hasIndex := m.dateIndex[key]
	delete(m.dateIndex, key)

	// 尝试删除文件（优先使用索引中的日期文件夹）
	var lastErr error
	if hasIndex && dateFolder != "" {
		filePath := m.sessionPath(key, dateFolder)
		if err := os.Remove(filePath); err != nil {
			lastErr = err
		} else {
			return nil
		}
	}

	// 如果索引中没有或删除失败，尝试在所有日期文件夹中查找
	entries, err := os.ReadDir(m.baseDir)
	if err != nil {
		if lastErr != nil {
			return lastErr
		}
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			// 尝试日期文件夹格式 (YYYY-MM-DD)
			filePath := m.sessionPath(key, entry.Name())
			if err := os.Remove(filePath); err == nil {
				return nil
			}
		}
	}

	// 最后尝试旧格式（根目录下的文件）
	filePath := m.sessionPath(key, "")
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		return err
	}

	if lastErr != nil {
		return lastErr
	}
	return nil
}

// List 列出所有会话
func (m *Manager) List() ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// 读取目录
	entries, err := os.ReadDir(m.baseDir)
	if err != nil {
		return nil, err
	}

	// 提取会话键
	keys := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			// 日期文件夹，递归读取
			subEntries, err := os.ReadDir(filepath.Join(m.baseDir, entry.Name()))
			if err != nil {
				continue
			}
			for _, subEntry := range subEntries {
				if !subEntry.IsDir() && filepath.Ext(subEntry.Name()) == ".jsonl" {
					key := strings.TrimSuffix(subEntry.Name(), ".jsonl")
					keys = append(keys, key)
					// 更新日期索引缓存
					m.dateIndex[key] = entry.Name()
				}
			}
		} else if filepath.Ext(entry.Name()) == ".jsonl" {
			// 旧格式（根目录下的文件）
			key := strings.TrimSuffix(entry.Name(), ".jsonl")
			keys = append(keys, key)
		}
	}

	return keys, nil
}

// load 从磁盘加载会话
func (m *Manager) load(key string) (*Session, error) {
	// 首先尝试从日期索引缓存中获取路径
	m.mu.RLock()
	dateFolder, hasIndex := m.dateIndex[key]
	m.mu.RUnlock()

	var filePath string
	if hasIndex && dateFolder != "" {
		filePath = m.sessionPath(key, dateFolder)
	} else {
		// 尝试查找文件（支持日期文件夹和旧格式）
		var err error
		filePath, dateFolder, err = m.findSessionFile(key)
		if err != nil {
			return nil, err
		}
		// 更新索引缓存
		if dateFolder != "" {
			m.mu.Lock()
			m.dateIndex[key] = dateFolder
			m.mu.Unlock()
		}
	}

	// 打开文件
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// 创建会话
	session := &Session{
		Key:       key,
		Messages:  []adk.Message{},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Metadata:  make(map[string]interface{}),
	}

	// 解析文件
	decoder := json.NewDecoder(file)
	for decoder.More() {
		var raw map[string]interface{}
		if err := decoder.Decode(&raw); err != nil {
			return nil, err
		}

		// 检查是否为元数据行
		if msgType, ok := raw["_type"].(string); ok && msgType == "metadata" {
			if createdAt, ok := raw["created_at"].(string); ok {
				session.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
			}
			if updatedAt, ok := raw["updated_at"].(string); ok {
				session.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
			}
			if metadata, ok := raw["metadata"].(map[string]interface{}); ok {
				session.Metadata = metadata
			}
		} else {
			// 消息行
			data, _ := json.Marshal(raw)
			var msg adk.Message
			if err := json.Unmarshal(data, &msg); err != nil {
				return nil, err
			}
			session.Messages = append(session.Messages, msg)
		}
	}

	return session, nil
}

// findSessionFile 查找会话文件，返回文件路径和日期文件夹名
func (m *Manager) findSessionFile(key string) (filePath string, dateFolder string, err error) {
	safeKey := m.sanitizeKey(key)

	// 读取根目录
	entries, err := os.ReadDir(m.baseDir)
	if err != nil {
		return "", "", err
	}

	// 遍历目录，优先查找日期文件夹
	for _, entry := range entries {
		if entry.IsDir() {
			// 尝试日期文件夹格式 (YYYY-MM-DD)
			candidatePath := filepath.Join(m.baseDir, entry.Name(), safeKey+".jsonl")
			if _, err := os.Stat(candidatePath); err == nil {
				return candidatePath, entry.Name(), nil
			}
		}
	}

	// 尝试旧格式（根目录下的文件）
	oldPath := filepath.Join(m.baseDir, safeKey+".jsonl")
	if _, err := os.Stat(oldPath); err == nil {
		return oldPath, "", nil
	}

	return "", "", os.ErrNotExist
}

// sanitizeKey 清理key中的特殊字符
func (m *Manager) sanitizeKey(key string) string {
	return strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|' {
			return '_'
		}
		return r
	}, key)
}

// sessionPath 获取会话文件路径
// dateFolder 为日期文件夹名（格式：YYYY-MM-DD），为空时使用根目录（兼容旧格式）
func (m *Manager) sessionPath(key string, dateFolder string) string {
	safeKey := m.sanitizeKey(key)

	if dateFolder == "" {
		// 旧格式：根目录下
		return filepath.Join(m.baseDir, safeKey+".jsonl")
	}

	// 新格式：日期文件夹下
	return filepath.Join(m.baseDir, dateFolder, safeKey+".jsonl")
}
