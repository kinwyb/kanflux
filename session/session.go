package session

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

const InterruptRole = schema.RoleType("interrupt")

// SessionMeta 轻量元数据，用于快速查询
type SessionMeta struct {
	Key          string                 `json:"key"`
	CreatedAt    time.Time              `json:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
	MessageCount int                    `json:"message_count"`     // 消息数量
	InstrCount   int                    `json:"instruction_count"` // 指令数量
}

// SessionData 重数据，懒加载
type SessionData struct {
	Instructions []InstructionEntry `json:"instructions,omitempty"`
	Messages     []adk.Message      `json:"messages"`
	mu           sync.RWMutex       // 保护 Instructions 和 Messages
}

// InstructionEntry 记录一次 agent 执行的 instruction
type InstructionEntry struct {
	Type        string    `json:"_type"`        // 固定为 "instruction"，用于 JSONL 解析
	AgentName   string    `json:"agent_name"`   // 执行的 agent 名称
	Content     string    `json:"content"`      // instruction 内容（已替换占位符）
	Timestamp   time.Time `json:"timestamp"`    // 记录时间
	ContentHash string    `json:"content_hash"` // 内容哈希，用于去重
}

// Session 会话，组合 meta 和 data
type Session struct {
	meta   *SessionMeta // 始终存在
	data   *SessionData // 懒加载，nil 表示未加载
	loader func() error // 加载函数（由 Manager 提供）
	mu     sync.RWMutex // 保护 loaded 状态和 data
}

// SetLoader 设置懒加载函数（由 Manager 调用）
func (s *Session) SetLoader(loader func() error) {
	s.mu.Lock()
	s.loader = loader
	s.mu.Unlock()
}

// IsDataLoaded 检查数据是否已加载
func (s *Session) IsDataLoaded() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data != nil
}

// ensureDataLoaded 确保数据已加载（懒加载触发）
func (s *Session) ensureDataLoaded() error {
	// 快速检查（读锁）
	s.mu.RLock()
	if s.data != nil {
		s.mu.RUnlock()
		return nil
	}
	// 检查是否有 loader
	hasLoader := s.loader != nil
	s.mu.RUnlock()

	// 如果没有 loader，直接初始化空数据
	if !hasLoader {
		s.mu.Lock()
		if s.data == nil {
			s.data = &SessionData{
				Instructions: []InstructionEntry{},
				Messages:     []adk.Message{},
			}
		}
		s.mu.Unlock()
		return nil
	}

	// 调用加载函数（不持有锁，避免死锁）
	if err := s.loader(); err != nil {
		return err
	}

	// 双重检查：loader 应该已经设置了 data，如果没有则初始化
	s.mu.Lock()
	if s.data == nil {
		s.data = &SessionData{
			Instructions: []InstructionEntry{},
			Messages:     []adk.Message{},
		}
	}
	s.mu.Unlock()

	return nil
}

// GetMeta 获取元数据（快速，不加载数据）
func (s *Session) GetMeta() *SessionMeta {
	return s.meta
}

// GetKey 获取会话键
func (s *Session) GetKey() string {
	return s.meta.Key
}

// GetCreatedAt 获取创建时间
func (s *Session) GetCreatedAt() time.Time {
	return s.meta.CreatedAt
}

// GetUpdatedAt 获取更新时间
func (s *Session) GetUpdatedAt() time.Time {
	return s.meta.UpdatedAt
}

// GetMetadata 获取元数据 map
func (s *Session) GetMetadata() map[string]interface{} {
	return s.meta.Metadata
}

// AddInstruction 添加 instruction 记录（带去重）
// 返回值：true 表示添加成功，false 表示重复未添加
func (s *Session) AddInstruction(entry InstructionEntry) bool {
	if err := s.ensureDataLoaded(); err != nil {
		return false
	}

	s.data.mu.Lock()
	defer s.data.mu.Unlock()

	// 计算 content hash（如果未提供）
	if entry.ContentHash == "" {
		entry.ContentHash = computeHash(entry.Content)
	}

	// 去重检查：比较哈希值
	for _, existing := range s.data.Instructions {
		if existing.ContentHash == entry.ContentHash {
			return false // 重复，不添加
		}
	}

	// 设置时间戳（如果未提供）
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}

	// 设置 Type
	entry.Type = "instruction"

	s.data.Instructions = append(s.data.Instructions, entry)
	s.meta.InstrCount = len(s.data.Instructions)
	s.meta.UpdatedAt = time.Now()
	return true
}

// GetInstructions 获取所有 instruction 记录
func (s *Session) GetInstructions() []InstructionEntry {
	if err := s.ensureDataLoaded(); err != nil {
		return nil
	}

	s.data.mu.RLock()
	defer s.data.mu.RUnlock()

	result := make([]InstructionEntry, len(s.data.Instructions))
	copy(result, s.data.Instructions)
	return result
}

// computeHash 计算 SHA256 哈希
func computeHash(content string) string {
	h := sha256.Sum256([]byte(content))
	return hex.EncodeToString(h[:])
}

// AddMessage 添加消息
func (s *Session) AddMessage(msg adk.Message) {
	if err := s.ensureDataLoaded(); err != nil {
		return
	}

	s.data.mu.Lock()
	defer s.data.mu.Unlock()

	s.data.Messages = append(s.data.Messages, msg)
	s.meta.MessageCount = len(s.data.Messages)
	s.meta.UpdatedAt = time.Now()
}

// GetHistory 获取历史消息
func (s *Session) GetHistory(maxMessages int) []adk.Message {
	if err := s.ensureDataLoaded(); err != nil {
		return nil
	}

	s.data.mu.RLock()
	defer s.data.mu.RUnlock()

	if maxMessages <= 0 || maxMessages >= len(s.data.Messages) {
		// 返回所有消息的副本
		result := make([]adk.Message, len(s.data.Messages))
		copy(result, s.data.Messages)
		return result
	}

	// 返回最近的消息
	start := len(s.data.Messages) - maxMessages
	result := make([]adk.Message, maxMessages)
	copy(result, s.data.Messages[start:])
	return result
}

// GetHistorySafe 获取历史消息，确保不会在工具调用中间截断
// 这样可以保证消息的完整性和顺序
func (s *Session) GetHistorySafe(maxMessages int) []adk.Message {
	if err := s.ensureDataLoaded(); err != nil {
		return nil
	}

	s.data.mu.RLock()
	defer s.data.mu.RUnlock()

	if maxMessages <= 0 || maxMessages >= len(s.data.Messages) {
		// 返回所有消息的副本
		result := make([]adk.Message, len(s.data.Messages))
		copy(result, s.data.Messages)
		return result
	}

	messages := s.data.Messages

	// 使用两指针法，找到一组完整的消息
	// 从 maxMessages 开始，向前扩展到包含完整的工具调用组
	startIdx := len(messages) - maxMessages

	// 确保不会截断工具调用
	// 从 startIdx 向前扫描，如果遇到 tool 消息，需要把对应的 assistant 消息也包含进来
	for startIdx > 0 {
		msg := messages[startIdx]

		// 如果当前消息是 tool，需要找到对应的 assistant
		if msg.Role == schema.Tool {
			// 向前查找包含这个 tool_call_id 的 assistant 消息
			found := false
			for j := startIdx - 1; j >= 0; j-- {
				if messages[j].Role == schema.Assistant && len(messages[j].ToolCalls) > 0 {
					// 检查是否包含这个 tool 的 tool_call_id
					for _, tc := range messages[j].ToolCalls {
						if tc.ID == msg.ToolCallID {
							// 找到了，将 startIdx 移到这个 assistant
							startIdx = j
							found = true
							break
						}
					}
					if found {
						break
					}
				}
			}
			// 如果没找到对应的 assistant，跳过这个 tool 消息，向后移动 startIdx
			if !found {
				startIdx++
				break
			}
		} else {
			// 不是 tool 消息，检查是否是 assistant 且后面还有 tool 消息没包含进来
			if msg.Role == schema.Assistant && len(msg.ToolCalls) > 0 {
				// 检查这个 assistant 的 tool_calls 是否都在当前范围内
				allToolsIncluded := true
				for _, tc := range msg.ToolCalls {
					// 在 startIdx 之后查找对应的 tool 消息
					found := false
					for k := startIdx + 1; k < len(messages); k++ {
						if messages[k].Role == schema.Tool && messages[k].ToolCallID == tc.ID {
							found = true
							break
						}
					}
					if !found {
						// 这个 tool_call 在范围内没有对应的 tool 消息，需要向前扩展
						allToolsIncluded = false
						break
					}
				}
				if !allToolsIncluded {
					// 需要向前扩展以包含所有相关的 tool 消息
					startIdx--
					continue
				}
			}
			// 消息完整，可以停止
			break
		}
	}

	// 返回从 startIdx 到末尾的消息
	result := make([]adk.Message, len(messages)-startIdx)
	copy(result, messages[startIdx:])
	return result
}

// GetHistoryLen 获取历史消息数量（快速，不加载数据）
func (s *Session) GetHistoryLen() int {
	return s.meta.MessageCount
}

// Clear 清空消息和 instructions
func (s *Session) Clear() {
	if err := s.ensureDataLoaded(); err != nil {
		return
	}

	s.data.mu.Lock()
	defer s.data.mu.Unlock()

	s.data.Messages = []adk.Message{}
	s.data.Instructions = []InstructionEntry{}
	s.meta.MessageCount = 0
	s.meta.InstrCount = 0
	s.meta.UpdatedAt = time.Now()
}

// IsInterrupted 是不是一个中断消息,role中断消息角色
func (s *Session) IsInterrupted() bool {
	if err := s.ensureDataLoaded(); err != nil {
		return false
	}

	s.data.mu.Lock()
	defer s.data.mu.Unlock()

	if len(s.data.Messages) == 0 {
		return false
	}
	lastMessage := s.data.Messages[len(s.data.Messages)-1]
	if lastMessage.Role == InterruptRole {
		s.data.Messages = s.data.Messages[:len(s.data.Messages)-1]
		s.meta.MessageCount = len(s.data.Messages)
		s.meta.UpdatedAt = time.Now()
		return true
	}
	return false
}
