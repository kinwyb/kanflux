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
	MsgTypeInbound     MessageType = "inbound"     // 入站消息
	MsgTypeSubscribe   MessageType = "subscribe"   // 订阅请求
	MsgTypeHeartbeat   MessageType = "heartbeat"   // 心跳
	MsgTypeControl     MessageType = "control"     // 控制消息（如 shutdown）
	MsgTypeSessionList MessageType = "session_list" // 获取 session 列表
	MsgTypeSessionGet  MessageType = "session_get"  // 获取 session 内容
	// 定时任务管理
	MsgTypeTaskList    MessageType = "task_list"    // 获取任务列表
	MsgTypeTaskAdd     MessageType = "task_add"     // 添加任务
	MsgTypeTaskUpdate  MessageType = "task_update"  // 更新任务
	MsgTypeTaskRemove  MessageType = "task_remove"  // 删除任务
	MsgTypeTaskTrigger MessageType = "task_trigger" // 触发任务
	MsgTypeTaskStatus  MessageType = "task_status"  // 获取任务状态
	// 配置管理
	MsgTypeConfigGet    MessageType = "config_get"    // 获取配置
	MsgTypeConfigUpdate MessageType = "config_update" // 更新配置

	// 服务端 -> 客户端
	MsgTypeOutbound        MessageType = "outbound"        // 出站消息
	MsgTypeChatEvent       MessageType = "chat_event"      // 聊天事件（流式）
	MsgTypeLogEvent        MessageType = "log_event"       // 日志事件
	MsgTypeHeartbeatAck    MessageType = "heartbeat_ack"   // 心跳响应
	MsgTypeControlAck      MessageType = "control_ack"     // 控制消息响应
	MsgTypeError           MessageType = "error"           // 错误消息
	MsgTypeSessionListAck  MessageType = "session_list_ack" // session 列表响应
	MsgTypeSessionGetAck   MessageType = "session_get_ack"  // session 内容响应
	// 定时任务响应
	MsgTypeTaskListAck    MessageType = "task_list_ack"    // 任务列表响应
	MsgTypeTaskAddAck     MessageType = "task_add_ack"     // 添加任务响应
	MsgTypeTaskUpdateAck  MessageType = "task_update_ack"  // 更新任务响应
	MsgTypeTaskRemoveAck  MessageType = "task_remove_ack"  // 删除任务响应
	MsgTypeTaskTriggerAck MessageType = "task_trigger_ack" // 触发任务响应
	MsgTypeTaskStatusAck  MessageType = "task_status_ack"  // 任务状态响应
	// 配置管理响应
	MsgTypeConfigGetAck    MessageType = "config_get_ack"    // 获取配置响应
	MsgTypeConfigUpdateAck MessageType = "config_update_ack" // 更新配置响应
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
	ID            string                 `json:"id"`
	Channel       string                 `json:"channel"`
	AccountID     string                 `json:"account_id"`
	SenderID      string                 `json:"sender_id"`
	ChatID        string                 `json:"chat_id"`
	Content       string                 `json:"content"`
	StreamingMode string                 `json:"streaming_mode,omitempty"` // 流式输出模式：delta/accumulate
	Media         []MediaPayload         `json:"media,omitempty"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
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
	ReplyTo   string                 `json:"reply_to,omitempty"` // 关联的入站消息ID，与 OutboundMessage.ReplyTo 一致
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
	ID        string `json:"id"`
	Level     string `json:"level"` // debug, info, warn, error
	Message   string `json:"message"`
	Source    string `json:"source"`
	Timestamp int64  `json:"timestamp"` // Unix 时间戳（毫秒）
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

// SessionListPayload session 列表请求 payload
type SessionListPayload struct {
	DateStart string `json:"date_start,omitempty"` // 开始日期 YYYY-MM-DD
	DateEnd   string `json:"date_end,omitempty"`   // 结束日期 YYYY-MM-DD
}

// SessionMetaPayload session 元数据 payload（用于列表返回）
type SessionMetaPayload struct {
	Key          string                 `json:"key"`
	CreatedAt    string                 `json:"created_at"`
	UpdatedAt    string                 `json:"updated_at"`
	MessageCount int                    `json:"message_count"`
	InstrCount   int                    `json:"instruction_count"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

// SessionListAckPayload session 列表响应 payload
type SessionListAckPayload struct {
	Success bool                `json:"success"`
	Error   string              `json:"error,omitempty"`
	Sessions []*SessionMetaPayload `json:"sessions,omitempty"`
}

// SessionGetPayload session 内容请求 payload
type SessionGetPayload struct {
	Key string `json:"key"` // session key
}

// MessagePayload 消息 payload（用于 session 内容返回）
type MessagePayload struct {
	Role       string             `json:"role"`
	Content    string             `json:"content"`
	ToolCallID string             `json:"tool_call_id,omitempty"`
	Name       string             `json:"name,omitempty"`
	ToolCalls  []*ToolCallPayload `json:"tool_calls,omitempty"`
}

// ToolCallPayload 工具调用 payload
type ToolCallPayload struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function *ToolFunctionPayload `json:"function"`
}

// ToolFunctionPayload 工具函数 payload
type ToolFunctionPayload struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// InstructionPayload instruction payload（用于 session 内容返回）
type InstructionPayload struct {
	AgentName string `json:"agent_name"`
	Content   string `json:"content"`
	Timestamp string `json:"timestamp"`
}

// SessionGetAckPayload session 内容响应 payload
type SessionGetAckPayload struct {
	Success      bool                 `json:"success"`
	Error        string               `json:"error,omitempty"`
	Key          string               `json:"key,omitempty"`
	CreatedAt    string               `json:"created_at,omitempty"`
	UpdatedAt    string               `json:"updated_at,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
	Messages     []*MessagePayload    `json:"messages,omitempty"`
	Instructions []*InstructionPayload `json:"instructions,omitempty"`
}

// ========== 定时任务相关 Payload ==========

// TaskListPayload 任务列表请求 payload（空）
type TaskListPayload struct{}

// TaskAddPayload 添加任务请求 payload
type TaskAddPayload struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Enabled     bool           `json:"enabled"`
	Schedule    SchedulePayload `json:"schedule"`
	Target      TargetPayload   `json:"target"`
	Content     ContentPayload  `json:"content"`
}

// TaskUpdatePayload 更新任务请求 payload
type TaskUpdatePayload struct {
	ID          string          `json:"id"`
	Name        string          `json:"name,omitempty"`
	Description string          `json:"description,omitempty"`
	Enabled     bool            `json:"enabled,omitempty"`
	Schedule    *SchedulePayload `json:"schedule,omitempty"`
	Target      *TargetPayload   `json:"target,omitempty"`
	Content     *ContentPayload  `json:"content,omitempty"`
}

// TaskRemovePayload 删除任务请求 payload
type TaskRemovePayload struct {
	ID string `json:"id"`
}

// TaskTriggerPayload 触发任务请求 payload
type TaskTriggerPayload struct {
	ID string `json:"id"`
}

// TaskStatusPayload 获取任务状态请求 payload
type TaskStatusPayload struct {
	ID string `json:"id"`
}

// SchedulePayload 调度配置 payload
type SchedulePayload struct {
	Cron string `json:"cron"`
}

// TargetPayload 目标配置 payload
type TargetPayload struct {
	Channel   string `json:"channel"`
	AccountID string `json:"account_id,omitempty"`
	ChatID    string `json:"chat_id"`
	AgentName string `json:"agent_name,omitempty"`
}

// ContentPayload 内容配置 payload
type ContentPayload struct {
	Prompt string `json:"prompt"`
}

// TaskListAckPayload 任务列表响应 payload
type TaskListAckPayload struct {
	Success bool              `json:"success"`
	Error   string            `json:"error,omitempty"`
	Tasks   []*TaskDetailPayload `json:"tasks,omitempty"`
}

// TaskDetailPayload 任务详情 payload
type TaskDetailPayload struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Enabled     bool              `json:"enabled"`
	Schedule    SchedulePayload   `json:"schedule"`
	Target      TargetPayload     `json:"target"`
	Content     ContentPayload    `json:"content"`
	NextRun     int64             `json:"next_run,omitempty"`   // Unix 时间戳（毫秒）
	LastRun     int64             `json:"last_run,omitempty"`   // Unix 时间戳（毫秒）
	IsRunning   bool              `json:"is_running"`
	State       *TaskStatePayload `json:"state,omitempty"`
}

// TaskStatePayload 任务状态 payload
type TaskStatePayload struct {
	LastRun      int64  `json:"last_run,omitempty"`    // Unix 时间戳（毫秒）
	LastResult   string `json:"last_result,omitempty"`
	LastError    string `json:"last_error,omitempty"`
	SuccessCount int    `json:"success_count"`
	FailCount    int    `json:"fail_count"`
	NextRun      int64  `json:"next_run,omitempty"`    // Unix 时间戳（毫秒）
}

// TaskAddAckPayload 添加任务响应 payload
type TaskAddAckPayload struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
	ID      string `json:"id,omitempty"`
}

// TaskUpdateAckPayload 更新任务响应 payload
type TaskUpdateAckPayload struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
	ID      string `json:"id,omitempty"`
}

// TaskRemoveAckPayload 删除任务响应 payload
type TaskRemoveAckPayload struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
	ID      string `json:"id,omitempty"`
}

// TaskTriggerAckPayload 触发任务响应 payload
type TaskTriggerAckPayload struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
	ID      string `json:"id,omitempty"`
}

// TaskStatusAckPayload 任务状态响应 payload
type TaskStatusAckPayload struct {
	Success bool              `json:"success"`
	Error   string            `json:"error,omitempty"`
	ID      string            `json:"id,omitempty"`
	State   *TaskStatePayload `json:"state,omitempty"`
}

// ========== 配置管理相关 Payload ==========

// ConfigGetPayload 获取配置请求 payload（空）
type ConfigGetPayload struct{}

// ConfigUpdatePayload 更新配置请求 payload
type ConfigUpdatePayload struct {
	Config json.RawMessage `json:"config"` // 原始配置 JSON
}

// ConfigGetAckPayload 获取配置响应 payload
type ConfigGetAckPayload struct {
	Success bool            `json:"success"`
	Error   string          `json:"error,omitempty"`
	Config  json.RawMessage `json:"config,omitempty"` // 返回完整配置 JSON
}

// ConfigUpdateAckPayload 更新配置响应 payload
type ConfigUpdateAckPayload struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
	Message string `json:"message,omitempty"` // 成功/失败消息
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
	ID            string
	Channel       string
	AccountID     string
	SenderID      string
	ChatID        string
	Content       string
	StreamingMode string // 流式输出模式：delta/accumulate
	Media         []Media
	Metadata      map[string]interface{}
	Timestamp     time.Time
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
	ReplyTo   string // 关联的入站消息ID，与 OutboundMessage.ReplyTo 一致
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
	ID        string
	Level     string
	Message   string
	Source    string
	Timestamp time.Time
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
		ID:            msg.ID,
		Channel:       msg.Channel,
		AccountID:     msg.AccountID,
		SenderID:      msg.SenderID,
		ChatID:        msg.ChatID,
		Content:       msg.Content,
		StreamingMode: msg.StreamingMode,
		Media:         convertMediaToPayload(msg.Media),
		Metadata:      msg.Metadata,
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
		ReplyTo:   event.ReplyTo,
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
		ID:        event.ID,
		Level:     event.Level,
		Message:   event.Message,
		Source:    event.Source,
		Timestamp: event.Timestamp.UnixMilli(),
	}
}

// ConvertPayloadToInbound 将 InboundPayload 转换为 InboundMessage
func ConvertPayloadToInbound(p *InboundPayload) *InboundMessage {
	return &InboundMessage{
		ID:            p.ID,
		Channel:       p.Channel,
		AccountID:     p.AccountID,
		SenderID:      p.SenderID,
		ChatID:        p.ChatID,
		Content:       p.Content,
		StreamingMode: p.StreamingMode,
		Media:         convertPayloadToMedia(p.Media),
		Metadata:      p.Metadata,
		Timestamp:     time.Now(),
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
		ReplyTo:   p.ReplyTo,
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