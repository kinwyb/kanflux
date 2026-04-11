package session

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/kinwyb/kanflux/memoria"
	"github.com/kinwyb/kanflux/memoria/types"
)

// Manager 会话管理器
type Manager struct {
	sessions  map[string]*Session
	mu        sync.RWMutex
	baseDir   string
	memoria   *memoria.Memoria // 替代 ConversationHistory
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

// SetMemoria 设置 Memoria（替代 SetEmbedder）
func (m *Manager) SetMemoria(mem *memoria.Memoria) {
	m.memoria = mem
}

// GetMemoria 获取 Memoria 实例
func (m *Manager) GetMemoria() *memoria.Memoria {
	return m.memoria
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

	// 异步处理聊天历史到 Memoria（不阻塞保存）
	if m.memoria != nil {
		go func() {
			// 格式化聊天内容
			content := formatSessionContent(session)
			userCtx := &types.DefaultUserIdentity{UserID: "default"}

			// 调用 Memoria 处理聊天记录
			if _, err := m.memoria.ProcessChat(ctx, session.Key, content, userCtx); err != nil {
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

// formatSessionContent 格式化会话内容为字符串
func formatSessionContent(session *Session) string {
	var content strings.Builder
	for _, msg := range session.Messages {
		switch msg.Role {
		case "user":
			content.WriteString(fmt.Sprintf("Q: %s\n\n", extractMessageText(msg)))
		case "assistant":
			content.WriteString(fmt.Sprintf("A: %s\n\n", extractMessageText(msg)))
		}
	}
	return content.String()
}

// extractMessageText 从消息中提取文本内容
func extractMessageText(msg adk.Message) string {
	if msg.Content != "" {
		return msg.Content
	}
	var texts []string
	for _, part := range msg.MultiContent {
		if part.Type == "text" && part.Text != "" {
			texts = append(texts, part.Text)
		}
	}
	return strings.Join(texts, "\n")
}
