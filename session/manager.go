package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/embedding"
)

// Manager 会话管理器
type Manager struct {
	sessions  map[string]*Session
	mu        sync.RWMutex
	baseDir   string
	history   *ConversationHistory
	embedder  embedding.Embedder
}

// NewManager 创建会话管理器
func NewManager(baseDir string) (*Manager, error) {
	baseDir = filepath.Join(baseDir, ".kanflux", "sessions")
	// 确保目录存在
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, err
	}
	return &Manager{
		sessions: make(map[string]*Session),
		baseDir:  baseDir,
	}, nil
}

// SetEmbedder 设置 embedder，用于历史对话检索
func (m *Manager) SetEmbedder(embedder embedding.Embedder) error {
	m.embedder = embedder
	// 初始化历史对话管理器
	history, err := NewConversationHistory(filepath.Dir(filepath.Dir(m.baseDir)), embedder)
	if err != nil {
		return err
	}
	m.history = history
	return nil
}

// GetHistory 获取历史对话管理器
func (m *Manager) GetHistory() *ConversationHistory {
	return m.history
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
	return m.SaveWithContext(context.Background(), session)
}

// SaveWithContext 保存会话（带上下文，用于长期记忆处理）
func (m *Manager) SaveWithContext(ctx context.Context, session *Session) error {
	session.mu.RLock()
	defer session.mu.RUnlock()

	// 确定文件路径
	filePath := m.sessionPath(session.Key)

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

	// 异步处理历史对话（不阻塞保存）
	if m.history != nil {
		go func() {
			// 创建 session 副本用于处理
			sessionCopy := &Session{
				Key:       session.Key,
				Messages:  make([]adk.Message, len(session.Messages)),
				CreatedAt: session.CreatedAt,
				UpdatedAt: session.UpdatedAt,
			}
			copy(sessionCopy.Messages, session.Messages)

			if err := m.history.ProcessSession(ctx, sessionCopy); err != nil {
				// 静默失败，不影响主流程
			}
		}()
	}

	return nil
}

// Delete 删除会话
func (m *Manager) Delete(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 从缓存中删除
	delete(m.sessions, key)

	// 删除文件
	filePath := m.sessionPath(key)
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		return err
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
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".jsonl" {
			key := strings.TrimSuffix(entry.Name(), ".jsonl")
			keys = append(keys, key)
		}
	}

	return keys, nil
}

// load 从磁盘加载会话
func (m *Manager) load(key string) (*Session, error) {
	filePath := m.sessionPath(key)

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

// sessionPath 获取会话文件路径
func (m *Manager) sessionPath(key string) string {
	// 将 key 中的特殊字符替换为下划线
	safeKey := strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|' {
			return '_'
		}
		return r
	}, key)

	return filepath.Join(m.baseDir, safeKey+".jsonl")
}
