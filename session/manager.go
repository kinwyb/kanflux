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
	metaCache  map[string]*SessionMeta // 轻量元数据缓存
	dataCache  map[string]*SessionData // 重数据缓存（可选）
	mu         sync.RWMutex
	baseDir    string
	dateIndex  map[string]string // session key -> date folder (内存缓存)
}

// NewManager 创建会话管理器
func NewManager(baseDir string) (*Manager, error) {
	baseDir = filepath.Join(baseDir, ".kanflux", "sessions")
	// 确保目录存在
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, err
	}
	return &Manager{
		metaCache:  make(map[string]*SessionMeta),
		dataCache:  make(map[string]*SessionData),
		baseDir:    baseDir,
		dateIndex:  make(map[string]string),
	}, nil
}

// GetOrCreate 获取或创建会话
func (m *Manager) GetOrCreate(key string) (*Session, error) {
	// 先检查元数据缓存（用读锁）
	m.mu.RLock()
	if meta, ok := m.metaCache[key]; ok {
		m.mu.RUnlock()
		// 返回 Session，数据懒加载
		return m.newSessionWithLoader(meta), nil
	}
	m.mu.RUnlock()

	// 尝试从磁盘加载元数据（不持有锁）
	meta, err := m.loadMetaOnly(key)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		// 文件不存在，创建新会话元数据
		meta = &SessionMeta{
			Key:          key,
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
			Metadata:     make(map[string]interface{}),
			MessageCount: 0,
			InstrCount:   0,
		}
	}

	// 添加到元数据缓存（用写锁）
	m.mu.Lock()
	// 双重检查，防止并发时重复添加
	if existing, ok := m.metaCache[key]; ok {
		m.mu.Unlock()
		return m.newSessionWithLoader(existing), nil
	}
	m.metaCache[key] = meta
	m.mu.Unlock()

	return m.newSessionWithLoader(meta), nil
}

// newSessionWithLoader 创建 Session 并设置懒加载函数
func (m *Manager) newSessionWithLoader(meta *SessionMeta) *Session {
	sess := &Session{
		meta: meta,
		data: nil,
	}
	// 设置懒加载函数
	sess.SetLoader(func() error {
		return m.loadFullData(sess)
	})
	return sess
}

// Save 保存会话
func (m *Manager) Save(session *Session) error {
	return m.save(session)
}

// save 保存会话到磁盘
func (m *Manager) save(session *Session) error {
	// 确保数据已加载
	if err := session.ensureDataLoaded(); err != nil {
		return err
	}

	meta := session.GetMeta()
	data := session.data

	data.mu.RLock()
	defer data.mu.RUnlock()

	// 更新计数
	meta.MessageCount = len(data.Messages)
	meta.InstrCount = len(data.Instructions)

	// 确定文件路径（使用session的CreatedAt日期）
	dateFolder := meta.CreatedAt.Format("2006-01-02")
	filePath := m.sessionPath(meta.Key, dateFolder)

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

	// 写入元数据行（包含计数）
	encoder := json.NewEncoder(file)
	metadata := map[string]interface{}{
		"_type":           "metadata",
		"key":             meta.Key,
		"created_at":      meta.CreatedAt,
		"updated_at":      meta.UpdatedAt,
		"metadata":        meta.Metadata,
		"message_count":   meta.MessageCount,
		"instruction_count": meta.InstrCount,
	}
	if err := encoder.Encode(metadata); err != nil {
		return err
	}

	// 写入 instructions（在 metadata 之后）
	for _, instr := range data.Instructions {
		if err := encoder.Encode(instr); err != nil {
			return err
		}
	}

	// 写入消息
	for _, msg := range data.Messages {
		if err := encoder.Encode(msg); err != nil {
			return err
		}
	}

	// 原子性重命名
	if err := os.Rename(tmpPath, filePath); err != nil {
		return err
	}

	// 更新日期索引缓存和元数据缓存
	m.mu.Lock()
	m.dateIndex[meta.Key] = dateFolder
	m.metaCache[meta.Key] = meta
	m.mu.Unlock()

	return nil
}

// Delete 删除会话
func (m *Manager) Delete(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 从元数据缓存中删除
	delete(m.metaCache, key)

	// 从数据缓存中删除
	delete(m.dataCache, key)

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

// List 列出所有会话键
func (m *Manager) List() ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

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

// ListMeta 列出所有会话的元数据（快速查询）
func (m *Manager) ListMeta() ([]*SessionMeta, error) {
	keys, err := m.List()
	if err != nil {
		return nil, err
	}

	metas := make([]*SessionMeta, 0, len(keys))
	for _, key := range keys {
		meta, err := m.GetMeta(key)
		if err != nil {
			continue
		}
		metas = append(metas, meta)
	}

	return metas, nil
}

// GetMeta 获取会话元数据（快速，不加载消息）
func (m *Manager) GetMeta(key string) (*SessionMeta, error) {
	// 先检查缓存
	m.mu.RLock()
	if meta, ok := m.metaCache[key]; ok {
		m.mu.RUnlock()
		return meta, nil
	}
	m.mu.RUnlock()

	// 从磁盘加载元数据
	meta, err := m.loadMetaOnly(key)
	if err != nil {
		return nil, err
	}

	// 添加到缓存
	m.mu.Lock()
	m.metaCache[key] = meta
	m.mu.Unlock()

	return meta, nil
}

// GetMetaByDateRange 按日期范围获取会话元数据
func (m *Manager) GetMetaByDateRange(start, end time.Time) ([]*SessionMeta, error) {
	metas, err := m.ListMeta()
	if err != nil {
		return nil, err
	}

	result := make([]*SessionMeta, 0)
	for _, meta := range metas {
		if (meta.CreatedAt.After(start) || meta.CreatedAt.Equal(start)) &&
			(meta.CreatedAt.Before(end) || meta.CreatedAt.Equal(end)) {
			result = append(result, meta)
		}
	}

	return result, nil
}

// loadMetaOnly 只加载元数据（只读第一行）
func (m *Manager) loadMetaOnly(key string) (*SessionMeta, error) {
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

	// 只读第一行元数据
	decoder := json.NewDecoder(file)
	var raw map[string]interface{}
	if err := decoder.Decode(&raw); err != nil {
		return nil, err
	}

	// 解析元数据
	meta := &SessionMeta{
		Key:       key,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Metadata:  make(map[string]interface{}),
	}

	// 检查是否是 metadata 行
	msgType, ok := raw["_type"].(string)
	if ok && msgType == "metadata" {
		if createdAt, ok := raw["created_at"].(string); ok {
			meta.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		}
		if updatedAt, ok := raw["updated_at"].(string); ok {
			meta.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		}
		if metadata, ok := raw["metadata"].(map[string]interface{}); ok {
			meta.Metadata = metadata
		}
		if keyFromRaw, ok := raw["key"].(string); ok {
			meta.Key = keyFromRaw
		}
		// 读取计数（向后兼容，旧文件可能没有）
		if msgCount, ok := raw["message_count"].(float64); ok {
			meta.MessageCount = int(msgCount)
		}
		if instrCount, ok := raw["instruction_count"].(float64); ok {
			meta.InstrCount = int(instrCount)
		}
	}

	return meta, nil
}

// loadFullData 加载完整数据（用于懒加载）
func (m *Manager) loadFullData(session *Session) error {
	key := session.meta.Key

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
			// 文件不存在，创建空数据
			session.mu.Lock()
			session.data = &SessionData{
				Instructions: []InstructionEntry{},
				Messages:     []adk.Message{},
			}
			session.mu.Unlock()
			return nil
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
		// 文件不存在，创建空数据
		session.mu.Lock()
		session.data = &SessionData{
			Instructions: []InstructionEntry{},
			Messages:     []adk.Message{},
		}
		session.mu.Unlock()
		return nil
	}
	defer file.Close()

	// 创建数据
	data := &SessionData{
		Instructions: []InstructionEntry{},
		Messages:     []adk.Message{},
	}

	// 解析文件
	decoder := json.NewDecoder(file)
	for decoder.More() {
		var raw map[string]interface{}
		if err := decoder.Decode(&raw); err != nil {
			return err
		}

		// 检查行类型
		msgType, ok := raw["_type"].(string)
		if !ok {
			// 没有 _type 字段，可能是旧格式的消息行
			rawData, _ := json.Marshal(raw)
			var msg adk.Message
			if err := json.Unmarshal(rawData, &msg); err != nil {
				return err
			}
			data.Messages = append(data.Messages, msg)
			continue
		}

		switch msgType {
		case "metadata":
			// 跳过元数据行（已在 loadMetaOnly 中解析）
			// 但如果 meta 中没有计数，可以在这里计算
			if session.meta.MessageCount == 0 && session.meta.InstrCount == 0 {
				// 计数将在读取完成后更新
			}

		case "instruction":
			// 解析 instruction
			rawData, _ := json.Marshal(raw)
			var instr InstructionEntry
			if err := json.Unmarshal(rawData, &instr); err != nil {
				// 解析失败，跳过该行
				continue
			}
			data.Instructions = append(data.Instructions, instr)

		default:
			// 消息行
			rawData, _ := json.Marshal(raw)
			var msg adk.Message
			if err := json.Unmarshal(rawData, &msg); err != nil {
				return err
			}
			data.Messages = append(data.Messages, msg)
		}
	}

	// 更新计数（如果旧文件没有）
	session.meta.MessageCount = len(data.Messages)
	session.meta.InstrCount = len(data.Instructions)

	// 设置数据
	session.mu.Lock()
	session.data = data
	session.mu.Unlock()

	// 添加到数据缓存
	m.mu.Lock()
	m.dataCache[key] = data
	m.mu.Unlock()

	return nil
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
