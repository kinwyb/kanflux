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

// InstructionEntry 记录一次 agent 执行的 instruction
type InstructionEntry struct {
	Type        string    `json:"_type"`        // 固定为 "instruction"，用于 JSONL 解析
	AgentName   string    `json:"agent_name"`   // 执行的 agent 名称
	Content     string    `json:"content"`      // instruction 内容（已替换占位符）
	Timestamp   time.Time `json:"timestamp"`    // 记录时间
	ContentHash string    `json:"content_hash"` // 内容哈希，用于去重
}

// Session 会话
type Session struct {
	Key          string                 `json:"key"`
	Instructions []InstructionEntry     `json:"instructions,omitempty"` // instruction 记录
	Messages     []adk.Message          `json:"messages"`
	CreatedAt    time.Time              `json:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
	mu           sync.RWMutex
}

// AddInstruction 添加 instruction 记录（带去重）
// 返回值：true 表示添加成功，false 表示重复未添加
func (s *Session) AddInstruction(entry InstructionEntry) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 计算 content hash（如果未提供）
	if entry.ContentHash == "" {
		entry.ContentHash = computeHash(entry.Content)
	}

	// 去重检查：比较哈希值
	for _, existing := range s.Instructions {
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

	s.Instructions = append(s.Instructions, entry)
	s.UpdatedAt = time.Now()
	return true
}

// GetInstructions 获取所有 instruction 记录
func (s *Session) GetInstructions() []InstructionEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]InstructionEntry, len(s.Instructions))
	copy(result, s.Instructions)
	return result
}

// computeHash 计算 SHA256 哈希
func computeHash(content string) string {
	h := sha256.Sum256([]byte(content))
	return hex.EncodeToString(h[:])
}

// AddMessage 添加消息
func (s *Session) AddMessage(msg adk.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Messages = append(s.Messages, msg)
	s.UpdatedAt = time.Now()
}

// GetHistory 获取历史消息
func (s *Session) GetHistory(maxMessages int) []adk.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if maxMessages <= 0 || maxMessages >= len(s.Messages) {
		// 返回所有消息的副本
		result := make([]adk.Message, len(s.Messages))
		copy(result, s.Messages)
		return result
	}

	// 返回最近的消息
	start := len(s.Messages) - maxMessages
	result := make([]adk.Message, maxMessages)
	copy(result, s.Messages[start:])
	return result
}

// GetHistorySafe 获取历史消息，确保不会在工具调用中间截断
// 这样可以保证消息的完整性和顺序
func (s *Session) GetHistorySafe(maxMessages int) []adk.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if maxMessages <= 0 || maxMessages >= len(s.Messages) {
		// 返回所有消息的副本
		result := make([]adk.Message, len(s.Messages))
		copy(result, s.Messages)
		return result
	}

	messages := s.Messages

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

// GetHistoryLen 获取历史消息数量
func (s *Session) GetHistoryLen() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.Messages)
}

// Clear 清空消息和 instructions
func (s *Session) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Messages = []adk.Message{}
	s.Instructions = []InstructionEntry{}
	s.UpdatedAt = time.Now()
}

// IsInterrupted 是不是一个中断消息,role中断消息角色
func (s *Session) IsInterrupted() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.Messages) == 0 {
		return false
	}
	lastMessage := s.Messages[len(s.Messages)-1]
	if lastMessage.Role == InterruptRole {
		s.Messages = s.Messages[:len(s.Messages)-1]
		s.UpdatedAt = time.Now()
		return true
	}
	return false
}
