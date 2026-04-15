package bus

import (
	"time"
)

// Channel 常量定义
const (
	ChannelTUI      = "tui"
	ChannelSystem   = "system"
	ChannelTelegram = "telegram"
	ChannelWhatsApp = "whatsapp"
	ChannelFeishu   = "feishu"
	ChannelCLI      = "cli"
	ChannelDiscord  = "discord"
	ChannelSlack    = "slack"
	ChannelWxCom    = "wxCom"
)

// StreamingMode 常量定义（流式输出模式）
const (
	StreamingModeDelta      = "delta"      // 增量模式：Content 返回增量内容
	StreamingModeAccumulate = "accumulate" // 累积模式：Content 返回累积后的完整内容
)

// InboundMessage 入站消息
type InboundMessage struct {
	ID            string                 `json:"id"`
	Channel       string                 `json:"channel"`        // 使用 Channel* 常量 (ChannelTelegram, ChannelWhatsApp, ChannelFeishu, ChannelCLI, ChannelTUI, ChannelSystem)
	AccountID     string                 `json:"account_id"`     // 账号ID（用于多账号场景）
	SenderID      string                 `json:"sender_id"`      // 发送者ID
	ChatID        string                 `json:"chat_id"`        // 聊天ID
	Content       string                 `json:"content"`        // 消息内容
	Media         []Media                `json:"media"`          // 媒体文件
	StreamingMode string                 `json:"streaming_mode"` // 流式输出模式："delta" 增量, "accumulate" 累积
	Metadata      map[string]interface{} `json:"metadata"`       // 元数据
	Timestamp     time.Time              `json:"timestamp"`
}

// Media 媒体文件
type Media struct {
	Type     string                 `json:"type"`               // image, video, audio, document
	URL      string                 `json:"url"`                // 文件URL
	Base64   string                 `json:"base64"`             // Base64编码内容
	MimeType string                 `json:"mimetype"`           // MIME类型
	Metadata map[string]interface{} `json:"metadata,omitempty"` // 额外元数据（如加密参数等）
}

// SessionKey 返回会话键
func (m *InboundMessage) SessionKey() string {
	return m.Channel + ":" + m.ChatID
}

// OutboundMessage 出站消息
type OutboundMessage struct {
	ID               string                 `json:"id"`
	Channel          string                 `json:"channel"`           // 使用 Channel* 常量
	ChatID           string                 `json:"chat_id"`           // 聊天ID
	Content          string                 `json:"content"`           // 消息内容（根据 StreamingMode 决定是增量还是累积）
	ReasoningContent string                 `json:"reasoning_content"` // 思考/推理内容
	IsStreaming      bool                   `json:"is_streaming"`      // 是否流式发送
	IsThinking       bool                   `json:"is_thinking"`       // 是否为思考内容（流式时使用）
	IsFinal          bool                   `json:"is_final"`          // 是否最终消息（流式时使用）
	ChunkIndex       int                    `json:"chunk_index"`       // chunk序号（流式时使用）
	Media            []Media                `json:"media"`             // 媒体文件
	ReplyTo          string                 `json:"reply_to"`          // 回复的消息ID
	Error            string                 `json:"error,omitempty"`   // 错误信息
	Metadata         map[string]interface{} `json:"metadata"`          // 元数据
	Timestamp        time.Time              `json:"timestamp"`
}

// SystemMessage 系统消息（用于子代理结果通知）
type SystemMessage struct {
	InboundMessage
	TaskID    string `json:"task_id"`    // 任务ID
	TaskLabel string `json:"task_label"` // 任务标签
	Status    string `json:"status"`     // completed, failed
}

// IsSystemMessage 判断是否为系统消息
func (m *InboundMessage) IsSystemMessage() bool {
	return m.Channel == ChannelSystem
}

// ChatEvent 聊天事件（用于状态通知）
type ChatEvent struct {
	ID          string          `json:"id"`
	Channel     string          `json:"channel"`
	ChatID      string          `json:"chat_id"`
	RunID       string          `json:"run_id"`
	Seq         int             `json:"seq"`
	AgentName   string          `json:"agent_name"` // Agent 名称
	State       string          `json:"state"`      // 状态类型：start/tool/complete/error/interrupt
	Error       string          `json:"error,omitempty"`
	ToolInfo    *ToolEventInfo  `json:"tool_info,omitempty"`    // 工具信息（tool 状态）
	Timestamp   time.Time       `json:"timestamp"`
	Metadata    interface{}     `json:"metadata,omitempty"`
}

// ChatEvent states（简化为状态通知）
const (
	ChatEventStateStart     = "start"     // 开始处理（UI 显示 loading）
	ChatEventStateTool      = "tool"      // 工具调用通知
	ChatEventStateComplete  = "complete"  // 处理完成（UI 隐藏 loading）
	ChatEventStateError     = "error"     // 错误通知
	ChatEventStateInterrupt = "interrupt" // 中断通知
)

// ToolEventInfo 工具事件信息
type ToolEventInfo struct {
	Name      string `json:"name"`               // 工具名称
	ID        string `json:"id"`                 // 工具调用ID
	Arguments string `json:"arguments,omitempty"` // 工具参数（开始时）
	Result    string `json:"result,omitempty"`    // 工具结果（结束时）
	IsStart   bool   `json:"is_start"`           // true=开始, false=结束
}

// LogEvent 日志事件（用于系统日志输出）
type LogEvent struct {
	ID        string    `json:"id"`
	Level     string    `json:"level"` // "debug", "info", "warn", "error"
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
	Source    string    `json:"source"` // 来源组件
}

// LogEvent levels
const (
	LogLevelDebug = "debug"
	LogLevelInfo  = "info"
	LogLevelWarn  = "warn"
	LogLevelError = "error"
)

// 中断类型常量
const (
	InterruptTypeYesNo  = "yes_no" // 是/否类型
	InterruptTypeSelect = "select" // 列表选择类型
)

// InterruptTypeProvider 定义获取中断类型的接口
// 中断信息类型可以实现此接口来声明中断类型
type InterruptTypeProvider interface {
	InterruptType() string
}
