package bus

// StreamMessage represents a streaming message chunk for Channel.SendStream
type StreamMessage struct {
	ID         string                 `json:"id"`
	Channel    string                 `json:"channel"`
	ChatID     string                 `json:"chat_id"`
	Content    string                 `json:"content"`       // 消息内容（增量或累积，由 Manager 决定）
	ChunkIndex int                    `json:"chunk_index"`   // chunk序号
	IsThinking bool                   `json:"is_thinking"`   // 是否为思考内容
	IsFinal    bool                   `json:"is_final"`      // 是否最终消息
	IsComplete bool                   `json:"is_complete"`   // 是否完成（同 IsFinal）
	Error      string                 `json:"error,omitempty"`
	Metadata   map[string]interface{} `json:"metadata"`
}