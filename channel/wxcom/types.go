// Package wxcom provides enterprise WeChat (企业微信) intelligent robot channel implementation.
// Based on WebSocket long connection for message sending/receiving, streaming replies, template cards, etc.
package wxcom

import (
	"time"
)

// WebSocket 命令常量
const (
	WsCmdSubscribe       = "aibot_subscribe"            // 认证订阅
	WsCmdHeartbeat       = "ping"                       // 心跳
	WsCmdResponse        = "aibot_respond_msg"          // 回复消息
	WsCmdResponseWelcome = "aibot_respond_welcome_msg"  // 回复欢迎语
	WsCmdResponseUpdate  = "aibot_respond_update_msg"   // 更新模板卡片
	WsCmdSendMsg         = "aibot_send_msg"             // 主动发送消息
	WsCmdCallback        = "aibot_msg_callback"         // 消息推送回调
	WsCmdEventCallback   = "aibot_event_callback"       // 事件推送回调
)

// 消息类型常量
const (
	MsgTypeText         = "text"         // 文本消息
	MsgTypeImage        = "image"        // 图片消息
	MsgTypeMixed        = "mixed"        // 图文混排消息
	MsgTypeVoice        = "voice"        // 语音消息
	MsgTypeFile         = "file"         // 文件消息
	MsgTypeStream       = "stream"       // 流式消息
	MsgTypeMarkdown     = "markdown"     // Markdown消息
	MsgTypeTemplateCard = "template_card" // 模板卡片消息
)

// 事件类型常量
const (
	EventTypeEnterChat        = "enter_chat"         // 进入会话事件
	EventTypeTemplateCardEvent = "template_card_event" // 模板卡片事件
	EventTypeFeedbackEvent    = "feedback_event"    // 用户反馈事件
)

// 默认 WebSocket URL
const DefaultWSURL = "wss://openws.work.weixin.qq.com"

// 默认配置值
const (
	DefaultHeartbeatInterval = 30000 // 心跳间隔(ms)
	DefaultReconnectInterval = 1000  // 重连基础延迟(ms)
	DefaultMaxReconnect      = 10    // 最大重连次数
	DefaultRequestTimeout    = 10000 // 请求超时(ms)
	DefaultMaxMissedPong     = 2     // 最大未收到pong次数
	DefaultReconnectMaxDelay = 30000 // 重连最大延迟(ms)
	DefaultReplyAckTimeout   = 5     // 回复回执超时(s)
)

// WxComConfig 企业微信机器人配置
type WxComConfig struct {
	Enabled           bool   `mapstructure:"enabled" json:"enabled"`
	BotID             string `mapstructure:"bot_id" json:"bot_id"`               // 机器人ID
	Secret            string `mapstructure:"secret" json:"secret"`               // 机器人Secret
	WSURL             string `mapstructure:"ws_url" json:"ws_url"`               // 自定义WS地址 (可选)
	HeartbeatInterval int    `mapstructure:"heartbeat_interval" json:"heartbeat_interval"` // 心跳间隔(ms)
	ReconnectInterval int    `mapstructure:"reconnect_interval" json:"reconnect_interval"` // 重连基础延迟(ms)
	MaxReconnect      int    `mapstructure:"max_reconnect" json:"max_reconnect"` // 最大重连次数, -1表示无限
	RequestTimeout    int    `mapstructure:"request_timeout" json:"request_timeout"` // 请求超时(ms)
}

// SetDefaults 设置默认值
func (c *WxComConfig) SetDefaults() {
	if c.WSURL == "" {
		c.WSURL = DefaultWSURL
	}
	if c.HeartbeatInterval <= 0 {
		c.HeartbeatInterval = DefaultHeartbeatInterval
	}
	if c.ReconnectInterval <= 0 {
		c.ReconnectInterval = DefaultReconnectInterval
	}
	if c.MaxReconnect == 0 {
		c.MaxReconnect = DefaultMaxReconnect
	}
	if c.RequestTimeout <= 0 {
		c.RequestTimeout = DefaultRequestTimeout
	}
}

// Validate 验证配置
func (c *WxComConfig) Validate() error {
	if c.BotID == "" {
		return ErrBotIDRequired
	}
	if c.Secret == "" {
		return ErrSecretRequired
	}
	return nil
}

// WsFrame WebSocket帧结构
type WsFrame struct {
	Cmd     string                 `json:"cmd,omitempty"`
	Headers map[string]string      `json:"headers"`
	Body    map[string]interface{} `json:"body,omitempty"`
	ErrCode int                    `json:"errcode,omitempty"`
	ErrMsg  string                 `json:"errmsg,omitempty"`
}

// WsHeaders WebSocket帧headers (用于reply等方法参数)
type WsHeaders map[string]string

// TextMessage 文本消息
type TextMessage struct {
	Content string `json:"content"` // 消息内容
}

// ImageMessage 图片消息
type ImageMessage struct {
	URL    string `json:"url"`     // 图片下载地址
	AesKey string `json:"aeskey"`  // AES解密密钥 (Base64编码)
}

// MixedItem 图文混排项
type MixedItem struct {
	MsgType string      `json:"msgtype"`
	Text    *TextMessage `json:"text,omitempty"`
	Image   *ImageMessage `json:"image,omitempty"`
}

// MixedMessage 图文混排消息
type MixedMessage struct {
	MsgItem []MixedItem `json:"msg_item"` // 消息项列表
}

// VoiceMessage 语音消息
type VoiceMessage struct {
	Content string `json:"content"` // 语音转文本内容
}

// FileMessage 文件消息
type FileMessage struct {
	URL    string `json:"url"`    // 文件下载地址
	AesKey string `json:"aeskey"` // AES解密密钥 (Base64编码)
}

// StreamMessage 流式消息
type StreamMessage struct {
	ID       string       `json:"id"`                // 流式消息ID
	Content  string       `json:"content"`           // 内容 (支持Markdown)
	Finish   bool         `json:"finish"`            // 是否结束流
	MsgItem  []MixedItem  `json:"msg_item,omitempty"` // 图文混排项 (仅finish=true时有效)
	Feedback *StreamFeedback `json:"feedback,omitempty"` // 反馈信息
}

// StreamFeedback 流式消息反馈信息
type StreamFeedback struct {
	ButtonDesc string `json:"button_desc,omitempty"` // 按钮描述
}

// MarkdownMessage Markdown消息
type MarkdownMessage struct {
	Content string `json:"content"` // Markdown内容
}

// TemplateCard 模板卡片
type TemplateCard struct {
	CardType  string                 `json:"card_type"`
	Source    *CardSource            `json:"source,omitempty"`
	MainTitle *CardMainTitle         `json:"main_title,omitempty"`
	TaskID    string                 `json:"task_id,omitempty"`
	// 其他字段根据卡片类型不同而不同，使用map存储
	Extra     map[string]interface{} `json:"-"`
}

// CardSource 卡片来源
type CardSource struct {
	IconURL string `json:"icon_url,omitempty"`
	Desc    string `json:"desc,omitempty"`
}

// CardMainTitle 卡片主标题
type CardMainTitle struct {
	Title string `json:"title,omitempty"`
	Desc  string `json:"desc,omitempty"`
}

// MessageBody 消息体
type MessageBody struct {
	MsgType      string          `json:"msgtype"`
	Text         *TextMessage    `json:"text,omitempty"`
	Image        *ImageMessage   `json:"image,omitempty"`
	Mixed        *MixedMessage   `json:"mixed,omitempty"`
	Voice        *VoiceMessage   `json:"voice,omitempty"`
	File         *FileMessage    `json:"file,omitempty"`
	Stream       *StreamMessage  `json:"stream,omitempty"`
	Markdown     *MarkdownMessage `json:"markdown,omitempty"`
	TemplateCard *TemplateCard   `json:"template_card,omitempty"`
	ChatID       string          `json:"chatid,omitempty"` // 主动发送时使用
}

// EventBody 事件体
type EventBody struct {
	MsgType string     `json:"msgtype"`
	Event   *EventData `json:"event"`
}

// EventData 事件数据
type EventData struct {
	EventType string `json:"eventtype"` // 事件类型
	EventKey  string `json:"event_key,omitempty"` // 事件key
	UserID    string `json:"userid,omitempty"` // 用户ID
	ChatID    string `json:"chatid,omitempty"` // 会话ID
	TaskID    string `json:"task_id,omitempty"` // 任务ID (模板卡片事件)
	// 其他字段
	Extra     map[string]interface{} `json:"-"`
}

// WsMessage WebSocket消息 (入站消息回调)
type WsMessage struct {
	Frame    *WsFrame
	MsgType  string
	ChatID   string
	UserID   string
	Content  string
	MediaURL string
	MediaKey string
	MsgTime  time.Time
}

// WsEvent WebSocket事件 (入站事件回调)
type WsEvent struct {
	Frame     *WsFrame
	EventType string
	UserID    string
	ChatID    string
	EventKey  string
	TaskID    string
	EventTime time.Time
}

// ReplyRequest 回复请求
type ReplyRequest struct {
	ReqID   string
	ChatID  string
	Body    *MessageBody
	StreamID string // 流式消息ID (用于流式回复)
}

// Error 定义
var (
	ErrBotIDRequired    = &WxComError{Code: 1001, Message: "bot_id is required"}
	ErrSecretRequired   = &WxComError{Code: 1002, Message: "secret is required"}
	ErrNotConnected     = &WxComError{Code: 1003, Message: "WebSocket not connected"}
	ErrAuthFailed       = &WxComError{Code: 1004, Message: "authentication failed"}
	ErrReplyTimeout     = &WxComError{Code: 1005, Message: "reply ack timeout"}
	ErrQueueFull        = &WxComError{Code: 1006, Message: "reply queue is full"}
	ErrMaxReconnect     = &WxComError{Code: 1007, Message: "max reconnect attempts exceeded"}
)

// WxComError 企业微信错误
type WxComError struct {
	Code    int
	Message string
}

func (e *WxComError) Error() string {
	return e.Message
}