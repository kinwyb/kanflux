// Package ws provides WebSocket server and client for kanflux.
// WebSocket service acts as a message hub between TUI/Web clients and the backend services.
package ws

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
// 对应 bus.InboundMessage
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
// 对应 bus.OutboundMessage
type OutboundPayload struct {
	ID               string                 `json:"id"`
	Channel          string                 `json:"channel"`
	ChatID           string                 `json:"chat_id"`
	Content          string                 `json:"content"`
	ReasoningContent string                 `json:"reasoning_content,omitempty"`
	Media            []MediaPayload         `json:"media,omitempty"`
	ReplyTo          string                 `json:"reply_to,omitempty"`
	Metadata         map[string]interface{} `json:"metadata,omitempty"`
}

// ChatEventPayload 聊天事件 payload
// 对应 bus.ChatEvent
type ChatEventPayload struct {
	ID        string                 `json:"id"`
	Channel   string                 `json:"channel"`
	ChatID    string                 `json:"chat_id"`
	RunID     string                 `json:"run_id,omitempty"`
	Seq       int                    `json:"seq"`
	AgentName string                 `json:"agent_name"`
	State     string                 `json:"state"` // delta, thinking, tool, final, error, interrupt
	Content   string                 `json:"content"`
	Message   string                 `json:"message,omitempty"`
	Error     string                 `json:"error,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// LogEventPayload 日志事件 payload
// 对应 bus.LogEvent
type LogEventPayload struct {
	ID      string `json:"id"`
	Level   string `json:"level"` // debug, info, warn, error
	Message string `json:"message"`
	Source  string `json:"source"`
}

// SubscribePayload 订阅请求 payload
type SubscribePayload struct {
	Channels []string `json:"channels,omitempty"` // 订阅的 channel 类型，空表示订阅所有
	ChatIDs  []string `json:"chat_ids,omitempty"` // 订阅的 chatID，空表示订阅所有
}

// MediaPayload 媒体 payload
// 对应 bus.Media
type MediaPayload struct {
	Type     string                 `json:"type"`               // image, video, audio, document
	URL      string                 `json:"url,omitempty"`      // 文件URL
	Base64   string                 `json:"base64,omitempty"`   // Base64编码内容
	MimeType string                 `json:"mimetype"`           // MIME类型
	Metadata map[string]interface{} `json:"metadata,omitempty"` // 额外元数据
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
	Action string                 `json:"action"` // shutdown, status, etc.
	Data   map[string]interface{} `json:"data,omitempty"`
}

// 控制消息 Action 常量
const (
	ControlActionShutdown = "shutdown" // 关闭服务
	ControlActionStatus   = "status"   // 获取状态
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

// ConvertInboundToPayload 将 bus.InboundMessage 转换为 InboundPayload
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

// ConvertOutboundToPayload 将 bus.OutboundMessage 转换为 OutboundPayload
func ConvertOutboundToPayload(msg *OutboundMessage) *OutboundPayload {
	return &OutboundPayload{
		ID:               msg.ID,
		Channel:          msg.Channel,
		ChatID:           msg.ChatID,
		Content:          msg.Content,
		ReasoningContent: msg.ReasoningContent,
		Media:            convertMediaToPayload(msg.Media),
		ReplyTo:          msg.ReplyTo,
		Metadata:         msg.Metadata,
	}
}

// ConvertChatEventToPayload 将 bus.ChatEvent 转换为 ChatEventPayload
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
		Content:   event.Content,
		Message:   event.Message,
		Error:     event.Error,
		Metadata:  metadata,
	}
}

// ConvertLogEventToPayload 将 bus.LogEvent 转换为 LogEventPayload
func ConvertLogEventToPayload(event *LogEvent) *LogEventPayload {
	return &LogEventPayload{
		ID:      event.ID,
		Level:   event.Level,
		Message: event.Message,
		Source:  event.Source,
	}
}

// ConvertPayloadToInbound 将 InboundPayload 转换为 bus.InboundMessage
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

// ConvertPayloadToOutbound 将 OutboundPayload 转换为 bus.OutboundMessage
func ConvertPayloadToOutbound(p *OutboundPayload) *OutboundMessage {
	return &OutboundMessage{
		ID:               p.ID,
		Channel:          p.Channel,
		ChatID:           p.ChatID,
		Content:          p.Content,
		ReasoningContent: p.ReasoningContent,
		Media:            convertPayloadToMedia(p.Media),
		ReplyTo:          p.ReplyTo,
		Metadata:         p.Metadata,
		Timestamp:        time.Now(),
	}
}

// ConvertPayloadToChatEvent 将 ChatEventPayload 转换为 bus.ChatEvent
func ConvertPayloadToChatEvent(p *ChatEventPayload) *ChatEvent {
	return &ChatEvent{
		ID:        p.ID,
		Channel:   p.Channel,
		ChatID:    p.ChatID,
		RunID:     p.RunID,
		Seq:       p.Seq,
		AgentName: p.AgentName,
		State:     p.State,
		Content:   p.Content,
		Message:   p.Message,
		Error:     p.Error,
		Timestamp: time.Now(),
		Metadata:  p.Metadata,
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

// 以下是从 bus/events.go 复制的类型定义，用于避免循环导入

// InboundMessage 入站消息（本地定义，避免导入 bus）
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

// OutboundMessage 出站消息（本地定义，避免导入 bus）
type OutboundMessage struct {
	ID               string
	Channel          string
	ChatID           string
	Content          string
	ReasoningContent string
	Media            []Media
	ReplyTo          string
	Metadata         map[string]interface{}
	Timestamp        time.Time
}

// ChatEvent 聊天事件（本地定义，避免导入 bus）
type ChatEvent struct {
	ID        string
	Channel   string
	ChatID    string
	RunID     string
	Seq       int
	AgentName string
	State     string
	Content   string
	Message   string
	Error     string
	Timestamp time.Time
	Metadata  interface{}
}

// LogEvent 日志事件（本地定义，避免导入 bus）
type LogEvent struct {
	ID      string
	Level   string
	Message string
	Source  string
}

// Media 媒体文件（本地定义，避免导入 bus）
type Media struct {
	Type     string
	URL      string
	Base64   string
	MimeType string
	Metadata map[string]interface{}
}