package channel

import (
	"context"
	"sync"

	"github.com/kinwyb/kanflux/bus"
)

// Channel 通道接口
type Channel interface {
	// Name 返回通道名称标识 (如 "tui", "telegram")
	Name() string

	// AccountID 返回账号ID（多账号场景）
	AccountID() string

	// Start 启动通道
	Start(ctx context.Context) error

	// Stop 停止通道
	Stop() error

	// IsRunning 是否运行中
	IsRunning() bool

	// Send 发送完整消息（非流式或流式最终消息）
	Send(ctx context.Context, msg *bus.OutboundMessage) error

	// SendStream 发送流式增量消息
	// msg 包含 ChatID、Content、IsStreaming、IsThinking、IsFinal 等信息
	SendStream(ctx context.Context, msg *bus.OutboundMessage) error

	// HandleChatEvent 处理聊天事件 (start, tool, complete, error, interrupt 等)
	HandleChatEvent(ctx context.Context, event *bus.ChatEvent) error

	// IsAllowed 检查发送者是否允许
	IsAllowed(senderID string) bool

	// HandleRequest 处理请求消息（如 send_file）
	// 返回响应消息，nil 表示不处理该请求类型
	HandleRequest(ctx context.Context, request *bus.OutboundMessage) (*bus.OutboundMessage, error)
}

// BaseChannelConfig 通道基础配置
type BaseChannelConfig struct {
	Enabled    bool     `mapstructure:"enabled" json:"enabled"`
	AccountID  string   `mapstructure:"account_id" json:"account_id"` // 账号ID
	Name       string   `mapstructure:"name" json:"name"`             // 账号显示名称
	AllowedIDs []string `mapstructure:"allowed_ids" json:"allowed_ids"`
}

// ChannelBase 通道基础实现（嵌入模式）
type ChannelBase struct {
	name      string
	accountID string
	config    BaseChannelConfig
	bus       *bus.MessageBus
	running   bool
	stopChan  chan struct{}
	mu        sync.RWMutex
}

// NewChannelBase 创建通道基础实现
func NewChannelBase(name, accountID string, config BaseChannelConfig, bus *bus.MessageBus) *ChannelBase {
	return &ChannelBase{
		name:      name,
		accountID: accountID,
		config:    config,
		bus:       bus,
		running:   false,
		stopChan:  make(chan struct{}),
	}
}

// Name 返回通道名称
func (c *ChannelBase) Name() string {
	return c.name
}

// AccountID 返回通道账号ID
func (c *ChannelBase) AccountID() string {
	return c.accountID
}

// Start 启动通道
func (c *ChannelBase) Start(ctx context.Context) error {
	if !c.config.Enabled {
		return nil
	}

	c.mu.Lock()
	c.running = true
	c.mu.Unlock()
	return nil
}

// Stop 停止通道
func (c *ChannelBase) Stop() error {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return nil
	}
	c.running = false
	c.mu.Unlock()

	close(c.stopChan)
	return nil
}

// IsRunning 检查是否运行中
func (c *ChannelBase) IsRunning() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.running
}

// IsAllowed 检查发送者是否允许
func (c *ChannelBase) IsAllowed(senderID string) bool {
	if !c.config.Enabled {
		return false
	}

	// 如果没有限制列表，允许所有
	if len(c.config.AllowedIDs) == 0 {
		return true
	}

	// 检查是否在允许列表中
	for _, id := range c.config.AllowedIDs {
		if id == senderID {
			return true
		}
	}

	return false
}

// PublishInbound 发布入站消息
func (c *ChannelBase) PublishInbound(ctx context.Context, msg *bus.InboundMessage) error {
	msg.Channel = c.name
	msg.AccountID = c.accountID
	return c.bus.PublishInbound(ctx, msg)
}

// WaitForStop 等待停止信号
func (c *ChannelBase) WaitForStop() <-chan struct{} {
	return c.stopChan
}

// GetBus 获取消息总线
func (c *ChannelBase) GetBus() *bus.MessageBus {
	return c.bus
}

// GetConfig 获取配置
func (c *ChannelBase) GetConfig() BaseChannelConfig {
	return c.config
}

// SendStream 发送流式消息 (默认实现：直接调用 Send)
func (c *ChannelBase) SendStream(ctx context.Context, msg *bus.OutboundMessage) error {
	// 默认实现：直接使用 Send 发送（适用于不支持流式的 Channel）
	return c.Send(ctx, msg)
}

// HandleChatEvent 处理聊天事件 (默认空实现，具体 channel 可覆盖)
func (c *ChannelBase) HandleChatEvent(ctx context.Context, event *bus.ChatEvent) error {
	// 默认实现不做任何处理，由具体 channel 实现
	return nil
}

// Send 发送消息 (默认空实现，具体 channel 必须覆盖)
func (c *ChannelBase) Send(ctx context.Context, msg *bus.OutboundMessage) error {
	// 默认实现不做任何处理，由具体 channel 实现
	return nil
}

// HandleRequest 处理请求消息 (默认不处理)
func (c *ChannelBase) HandleRequest(ctx context.Context, request *bus.OutboundMessage) (*bus.OutboundMessage, error) {
	// 默认实现不处理任何请求，由具体 channel 根据请求类型实现
	return nil, nil
}
