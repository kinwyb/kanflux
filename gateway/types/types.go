// Package types provides WebSocket message types for the gateway.
package types

import (
	"encoding/json"
	"time"
)

// MessageType WebSocket 消息类型
type MessageType string

const (
	// 客户端 -> 服务端
	MsgTypeInbound   MessageType = "inbound"   // 入站消息
	MsgTypeSubscribe MessageType = "subscribe" // 订阅请求
	MsgTypeHeartbeat MessageType = "heartbeat" // 心跳
	MsgTypeControl   MessageType = "control"   // 控制消息（如 shutdown）

	// 服务端 -> 客户端
	MsgTypeOutbound     MessageType = "outbound"     // 出站消息
	MsgTypeChatEvent    MessageType = "chat_event"   // 聊天事件（流式）
	MsgTypeLogEvent     MessageType = "log_event"    // 日志事件
	MsgTypeHeartbeatAck MessageType = "heartbeat_ack" // 心跳响应
	MsgTypeControlAck   MessageType = "control_ack"   // 控制消息响应
	MsgTypeError        MessageType = "error"        // 错误消息
)

// WSMessage WebSocket 消息封装
type WSMessage struct {
	Type      MessageType     `json:"type"`
	ID        string          `json:"id"`        // 消息唯一ID
	Timestamp int64           `json:"timestamp"` // Unix 时间戳（毫秒）
	Payload   json.RawMessage `json:"payload"`   // 原始 payload，按类型解析
}

// InboundPayload 入站消息 payload
type InboundPayload struct {
	ID        string                 `json:"id"`
	Channel   string                 `json:"channel"`
	AccountID string                 `json:"account_id"`
	SenderID  string                 `json:"sender_id"`
	ChatID    string                 `json:"chat_id"`
	Content   string                 `json:"content"`
	Media     []MediaPayload         `json:"media,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// OutboundPayload 出站消息 payload
type OutboundPayload struct {
	ID               string                 `json:"id"`
	Channel          string                 `json:"channel"`
	ChatID           string                 `json:"chat_id"`
	Content          string                 `json:"content"`
	ReasoningContent string                 `json:"reasoning_content,omitempty"`
	Media            []MediaPayload         `json:"media,omitempty"`
	ReplyTo          string                 `json:"reply_to,omitempty"`
	IsStreaming      bool                   `json:"is_streaming"` // 是否流式发送
	IsThinking       bool                   `json:"is_thinking"`  // 是否为思考内容
	IsFinal          bool                   `json:"is_final"`     // 是否最终消息
	ChunkIndex       int                    `json:"chunk_index"`  // chunk序号
	Error            string                 `json:"error,omitempty"`
	Metadata         map[string]interface{} `json:"metadata,omitempty"`
}

// ChatEventPayload 聊天事件 payload
type ChatEventPayload struct {
	ID        string                 `json:"id"`
	Channel   string                 `json:"channel"`
	ChatID    string                 `json:"chat_id"`
	RunID     string                 `json:"run_id,omitempty"`
	Seq       int                    `json:"seq"`
	AgentName string                 `json:"agent_name"`
	State     string                 `json:"state"` // start, tool, complete, error, interrupt
	Error     string                 `json:"error,omitempty"`
	ToolInfo  *ToolEventInfoPayload  `json:"tool_info,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// ToolEventInfoPayload 工具事件信息 payload
type ToolEventInfoPayload struct {
	Name      string `json:"name"`
	ID        string `json:"id"`
	Arguments string `json:"arguments,omitempty"`
	Result    string `json:"result,omitempty"`
	IsStart   bool   `json:"is_start"`
}

// LogEventPayload 日志事件 payload
type LogEventPayload struct {
	ID      string `json:"id"`
	Level   string `json:"level"` // debug, info, warn, error
	Message string `json:"message"`
	Source  string `json:"source"`
}

// SubscribePayload 订阅请求 payload
type SubscribePayload struct {
	Channels []string `json:"channels,omitempty"` // 订阅的 channel 类型
	ChatIDs  []string `json:"chat_ids,omitempty"` // 订阅的 chatID
}

// MediaPayload 媒体 payload
type MediaPayload struct {
	Type     string                 `json:"type"`
	URL      string                 `json:"url,omitempty"`
	Base64   string                 `json:"base64,omitempty"`
	MimeType string                 `json:"mimetype"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// ErrorPayload 错误消息 payload
type ErrorPayload struct {
	Message string `json:"message"`
	Code    int    `json:"code,omitempty"`
}

// HeartbeatPayload 心跳 payload
type HeartbeatPayload struct {
	Timestamp int64 `json:"timestamp"`
}

// ControlPayload 控制消息 payload
type ControlPayload struct {
	Action string                 `json:"action"`
	Data   map[string]interface{} `json:"data,omitempty"`
}

// 控制消息 Action 常量
const (
	ControlActionShutdown = "shutdown"
	ControlActionStatus   = "status"
)

// ControlAckPayload 控制消息响应 payload
type ControlAckPayload struct {
	Action  string `json:"action"`
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}

// NewWSMessage 创建 WebSocket 消息
func NewWSMessage(typ MessageType, id string, payload interface{}) (*WSMessage, error) {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	return &WSMessage{
		Type:      typ,
		ID:        id,
		Timestamp: time.Now().UnixMilli(),
		Payload:   payloadBytes,
	}, nil
}

// ParsePayload 解析 payload
func (m *WSMessage) ParsePayload(target interface{}) error {
	return json.Unmarshal(m.Payload, target)
}

// Marshal 序列化消息
func (m *WSMessage) Marshal() ([]byte, error) {
	return json.Marshal(m)
}

// ParseWSMessage 解析 WebSocket 消息
func ParseWSMessage(data []byte) (*WSMessage, error) {
	var msg WSMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

// InboundMessage 入站消息（本地定义，避免循环导入 bus）
type InboundMessage struct {
	ID        string
	Channel   string
	AccountID string
	SenderID  string
	ChatID    string
	Content   string
	Media     []Media
	Metadata  map[string]interface{}
	Timestamp time.Time
}

// OutboundMessage 出站消息（本地定义，避免循环导入 bus）
type OutboundMessage struct {
	ID               string
	Channel          string
	ChatID           string
	Content          string
	ReasoningContent string
	Media            []Media
	ReplyTo          string
	IsStreaming      bool
	IsThinking       bool
	IsFinal          bool
	ChunkIndex       int
	Error            string
	Metadata         map[string]interface{}
	Timestamp        time.Time
}

// ChatEvent 聊天事件（本地定义，避免循环导入 bus）
type ChatEvent struct {
	ID        string
	Channel   string
	ChatID    string
	RunID     string
	Seq       int
	AgentName string
	State     string
	Error     string
	ToolInfo  *ToolEventInfo
	Timestamp time.Time
	Metadata  interface{}
}

// ToolEventInfo 工具事件信息
type ToolEventInfo struct {
	Name      string
	ID        string
	Arguments string
	Result    string
	IsStart   bool
}

// LogEvent 日志事件（本地定义，避免循环导入 bus）
type LogEvent struct {
	ID      string
	Level   string
	Message string
	Source  string
}

// Media 媒体文件（本地定义，避免循环导入 bus）
type Media struct {
	Type     string
	URL      string
	Base64   string
	MimeType string
	Metadata map[string]interface{}
}

// ConvertInboundToPayload 将 InboundMessage 转换为 InboundPayload
func ConvertInboundToPayload(msg *InboundMessage) *InboundPayload {
	return &InboundPayload{
		ID:        msg.ID,
		Channel:   msg.Channel,
		AccountID: msg.AccountID,
		SenderID:  msg.SenderID,
		ChatID:    msg.ChatID,
		Content:   msg.Content,
		Media:     convertMediaToPayload(msg.Media),
		Metadata:  msg.Metadata,
	}
}

// ConvertOutboundToPayload 将 OutboundMessage 转换为 OutboundPayload
func ConvertOutboundToPayload(msg *OutboundMessage) *OutboundPayload {
	return &OutboundPayload{
		ID:               msg.ID,
		Channel:          msg.Channel,
		ChatID:           msg.ChatID,
		Content:          msg.Content,
		ReasoningContent: msg.ReasoningContent,
		Media:            convertMediaToPayload(msg.Media),
		ReplyTo:          msg.ReplyTo,
		IsStreaming:      msg.IsStreaming,
		IsThinking:       msg.IsThinking,
		IsFinal:          msg.IsFinal,
		ChunkIndex:       msg.ChunkIndex,
		Error:            msg.Error,
		Metadata:         msg.Metadata,
	}
}

// ConvertChatEventToPayload 将 ChatEvent 转换为 ChatEventPayload
func ConvertChatEventToPayload(event *ChatEvent) *ChatEventPayload {
	metadata := make(map[string]interface{})
	if event.Metadata != nil {
		if m, ok := event.Metadata.(map[string]interface{}); ok {
			metadata = m
		}
	}

	return &ChatEventPayload{
		ID:        event.ID,
		Channel:   event.Channel,
		ChatID:    event.ChatID,
		RunID:     event.RunID,
		Seq:       event.Seq,
		AgentName: event.AgentName,
		State:     event.State,
		Error:     event.Error,
		ToolInfo:  ConvertToolEventInfoPayload(event.ToolInfo),
		Metadata:  metadata,
	}
}

// ConvertToolEventInfoPayload 将 ToolEventInfo 转换为 ToolEventInfoPayload
func ConvertToolEventInfoPayload(info *ToolEventInfo) *ToolEventInfoPayload {
	if info == nil {
		return nil
	}
	return &ToolEventInfoPayload{
		Name:      info.Name,
		ID:        info.ID,
		Arguments: info.Arguments,
		Result:    info.Result,
		IsStart:   info.IsStart,
	}
}

// ConvertLogEventToPayload 将 LogEvent 转换为 LogEventPayload
func ConvertLogEventToPayload(event *LogEvent) *LogEventPayload {
	return &LogEventPayload{
		ID:      event.ID,
		Level:   event.Level,
		Message: event.Message,
		Source:  event.Source,
	}
}

// ConvertPayloadToInbound 将 InboundPayload 转换为 InboundMessage
func ConvertPayloadToInbound(p *InboundPayload) *InboundMessage {
	return &InboundMessage{
		ID:        p.ID,
		Channel:   p.Channel,
		AccountID: p.AccountID,
		SenderID:  p.SenderID,
		ChatID:    p.ChatID,
		Content:   p.Content,
		Media:     convertPayloadToMedia(p.Media),
		Metadata:  p.Metadata,
		Timestamp: time.Now(),
	}
}

// ConvertPayloadToOutbound 将 OutboundPayload 转换为 OutboundMessage
func ConvertPayloadToOutbound(p *OutboundPayload) *OutboundMessage {
	return &OutboundMessage{
		ID:               p.ID,
		Channel:          p.Channel,
		ChatID:           p.ChatID,
		Content:          p.Content,
		ReasoningContent: p.ReasoningContent,
		Media:            convertPayloadToMedia(p.Media),
		ReplyTo:          p.ReplyTo,
		IsStreaming:      p.IsStreaming,
		IsThinking:       p.IsThinking,
		IsFinal:          p.IsFinal,
		ChunkIndex:       p.ChunkIndex,
		Error:            p.Error,
		Metadata:         p.Metadata,
		Timestamp:        time.Now(),
	}
}

// ConvertPayloadToChatEvent 将 ChatEventPayload 转换为 ChatEvent
func ConvertPayloadToChatEvent(p *ChatEventPayload) *ChatEvent {
	return &ChatEvent{
		ID:        p.ID,
		Channel:   p.Channel,
		ChatID:    p.ChatID,
		RunID:     p.RunID,
		Seq:       p.Seq,
		AgentName: p.AgentName,
		State:     p.State,
		Error:     p.Error,
		ToolInfo:  ConvertPayloadToToolEventInfo(p.ToolInfo),
		Timestamp: time.Now(),
		Metadata:  p.Metadata,
	}
}

// ConvertPayloadToToolEventInfo 将 ToolEventInfoPayload 转换为 ToolEventInfo
func ConvertPayloadToToolEventInfo(p *ToolEventInfoPayload) *ToolEventInfo {
	if p == nil {
		return nil
	}
	return &ToolEventInfo{
		Name:      p.Name,
		ID:        p.ID,
		Arguments: p.Arguments,
		Result:    p.Result,
		IsStart:   p.IsStart,
	}
}

// 内部转换函数

func convertMediaToPayload(media []Media) []MediaPayload {
	if media == nil {
		return nil
	}
	result := make([]MediaPayload, len(media))
	for i, m := range media {
		result[i] = MediaPayload{
			Type:     m.Type,
			URL:      m.URL,
			Base64:   m.Base64,
			MimeType: m.MimeType,
			Metadata: m.Metadata,
		}
	}
	return result
}

func convertPayloadToMedia(media []MediaPayload) []Media {
	if media == nil {
		return nil
	}
	result := make([]Media, len(media))
	for i, m := range media {
		result[i] = Media{
			Type:     m.Type,
			URL:      m.URL,
			Base64:   m.Base64,
			MimeType: m.MimeType,
			Metadata: m.Metadata,
		}
	}
	return result
}