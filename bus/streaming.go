package bus

import (
	"sync"
)

// StreamMessage represents a streaming message update
type StreamMessage struct {
	ID         string                 `json:"id"`
	Channel    string                 `json:"channel"`
	ChatID     string                 `json:"chat_id"`
	Content    string                 `json:"content"`
	ChunkIndex int                    `json:"chunk_index"`
	IsComplete bool                   `json:"is_complete"`
	IsThinking bool                   `json:"is_thinking"`
	IsFinal    bool                   `json:"is_final"`
	Metadata   map[string]interface{} `json:"metadata"`
	Error      string                 `json:"error,omitempty"`
}

// ChatEventTransformerMode 转换器模式
type ChatEventTransformerMode int

const (
	// TransformerModeDelta 增量模式：保持原始增量内容输出
	TransformerModeDelta ChatEventTransformerMode = iota
	// TransformerModeAccumulate 累积模式：输出累积后的完整内容
	TransformerModeAccumulate
)

// ChatEventTransformer 聊天事件转换器
// 用于将增量 ChatEvent 转换为累积后的完整内容，或保持增量输出
// 不同 channel 可根据需求选择不同模式
type ChatEventTransformer struct {
	mode         ChatEventTransformerMode
	accumulators map[string]*StreamAccumulator // chatID -> accumulator
	mu           sync.RWMutex
}

// NewChatEventTransformer 创建聊天事件转换器
// mode: TransformerModeDelta 保持增量输出，TransformerModeAccumulate 累积输出
func NewChatEventTransformer(mode ChatEventTransformerMode) *ChatEventTransformer {
	return &ChatEventTransformer{
		mode:         mode,
		accumulators: make(map[string]*StreamAccumulator),
	}
}

// Transform 转换 ChatEvent，返回处理后的事件
// 增量模式：返回原始事件（Content 保持增量）
// 累积模式：返回累积后的完整内容
func (t *ChatEventTransformer) Transform(event *ChatEvent) *ChatEvent {
	if t.mode == TransformerModeDelta {
		// 增量模式：直接返回原始事件
		return event
	}

	// 累积模式：处理累积逻辑
	return t.transformAccumulate(event)
}

// transformAccumulate 累积模式处理逻辑
func (t *ChatEventTransformer) transformAccumulate(event *ChatEvent) *ChatEvent {
	// 获取或创建累积器
	accumulator := t.getOrCreateAccumulator(event.ChatID)

	// 处理不同状态
	switch event.State {
	case ChatEventStateDelta:
		// 累积文本内容
		accumulatedContent := accumulator.AccumulateDelta(event.Content)
		// 返回新事件，Content 为累积后的完整内容
		return &ChatEvent{
			ID:        event.ID,
			Channel:   event.Channel,
			ChatID:    event.ChatID,
			RunID:     event.RunID,
			Seq:       event.Seq,
			AgentName: event.AgentName,
			State:     event.State,
			Content:   accumulatedContent, // 替换为累积后的完整内容
			Message:   event.Message,
			Error:     event.Error,
			Timestamp: event.Timestamp,
			Metadata:  event.Metadata,
		}

	case ChatEventStateThinking:
		// 累积思考内容
		accumulatedThinking := accumulator.AccumulateThinking(event.Content)
		return &ChatEvent{
			ID:        event.ID,
			Channel:   event.Channel,
			ChatID:    event.ChatID,
			RunID:     event.RunID,
			Seq:       event.Seq,
			AgentName: event.AgentName,
			State:     event.State,
			Content:   accumulatedThinking, // 替换为累积后的完整内容
			Message:   event.Message,
			Error:     event.Error,
			Timestamp: event.Timestamp,
			Metadata:  event.Metadata,
		}

	case ChatEventStateFinal:
		// 最终完成：重置累积器
		accumulator.Reset()
		t.removeAccumulator(event.ChatID)
		// Final 状态使用 event.Message（已经是完整内容）
		return event

	case ChatEventStateError, ChatEventStateInterrupt, ChatEventStateTool:
		// 这些状态清理累积器
		accumulator.Reset()
		t.removeAccumulator(event.ChatID)
		return event

	default:
		return event
	}
}

// getOrCreateAccumulator 获取或创建指定 chatID 的累积器
func (t *ChatEventTransformer) getOrCreateAccumulator(chatID string) *StreamAccumulator {
	t.mu.Lock()
	defer t.mu.Unlock()

	if acc, ok := t.accumulators[chatID]; ok {
		return acc
	}

	acc := NewStreamAccumulator()
	t.accumulators[chatID] = acc
	return acc
}

// removeAccumulator 移除指定 chatID 的累积器
func (t *ChatEventTransformer) removeAccumulator(chatID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.accumulators, chatID)
}

// Clear 清理所有累积器
func (t *ChatEventTransformer) Clear() {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, acc := range t.accumulators {
		acc.Reset()
	}
	t.accumulators = make(map[string]*StreamAccumulator)
}

// GetMode 获取当前模式
func (t *ChatEventTransformer) GetMode() ChatEventTransformerMode {
	return t.mode
}

// SetMode 设置模式（动态切换）
func (t *ChatEventTransformer) SetMode(mode ChatEventTransformerMode) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.mode = mode
	// 切换模式时清理累积器
	for _, acc := range t.accumulators {
		acc.Reset()
	}
	t.accumulators = make(map[string]*StreamAccumulator)
}

// StreamAccumulator 流式内容累积器
// 用于累积增量内容，返回完整内容
type StreamAccumulator struct {
	mu         sync.Mutex
	content    string
	thinking   string
	chunkIndex int
}

// NewStreamAccumulator 创建新的流式累积器
func NewStreamAccumulator() *StreamAccumulator {
	return &StreamAccumulator{}
}

// AccumulateDelta 累积增量文本内容，返回累积后的完整内容
func (a *StreamAccumulator) AccumulateDelta(delta string) string {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.content += delta
	a.chunkIndex++
	return a.content
}

// AccumulateThinking 累积思考内容，返回累积后的完整内容
func (a *StreamAccumulator) AccumulateThinking(delta string) string {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.thinking += delta
	a.chunkIndex++
	return a.thinking
}

// GetContent 获取当前累积的文本内容
func (a *StreamAccumulator) GetContent() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.content
}

// GetThinking 获取当前累积的思考内容
func (a *StreamAccumulator) GetThinking() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.thinking
}

// GetChunkIndex 获取当前 chunk 序号
func (a *StreamAccumulator) GetChunkIndex() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.chunkIndex
}

// Reset 重置累积器状态
func (a *StreamAccumulator) Reset() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.content = ""
	a.thinking = ""
	a.chunkIndex = 0
}