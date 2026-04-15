package channel

import (
	"context"
	"sync"

	"github.com/kinwyb/kanflux/bus"
)

// TUIModelInterface TUI Model 接口（由 cli/tui/model.go 实现）
type TUIModelInterface interface {
	// ReceiveOutbound 接收出站消息
	ReceiveOutbound(msg *bus.OutboundMessage)
	// ReceiveChatEvent 接收聊天事件
	ReceiveChatEvent(event *bus.ChatEvent)
	// GetChatID 获取当前 chatID
	GetChatID() string
	// SetChatID 设置 chatID
	SetChatID(chatID string)
}

// TUIChannel TUI 通道实现
type TUIChannel struct {
	*ChannelBase

	// TUI Model 接口
	model TUIModelInterface

	// 内部 channel 用于传递消息给 model
	outboundCh chan *bus.OutboundMessage
	eventCh    chan *bus.ChatEvent

	// 配置
	config *TUIConfig

	mu sync.Mutex
}

// TUIConfig TUI 通道配置
type TUIConfig struct {
	Enabled bool
}

// NewTUIChannel 创建 TUI 通道
func NewTUIChannel(msgBus *bus.MessageBus, model TUIModelInterface, cfg *TUIConfig) (*TUIChannel, error) {
	baseConfig := BaseChannelConfig{
		Enabled:    cfg.Enabled,
		AccountID:  "tui",
		Name:       "TUI",
		AllowedIDs: nil, // TUI 允许所有
	}

	base := NewChannelBase(bus.ChannelTUI, "tui", baseConfig, msgBus)

	return &TUIChannel{
		ChannelBase: base,
		model:       model,
		outboundCh:  make(chan *bus.OutboundMessage, 100),
		eventCh:     make(chan *bus.ChatEvent, 100),
		config:      cfg,
	}, nil
}

// Start 启动 TUI 通道
func (t *TUIChannel) Start(ctx context.Context) error {
	t.ChannelBase.Start(ctx)

	// 启动事件转发 goroutine
	go t.forwardEvents(ctx)

	return nil
}

// Stop 停止 TUI 通道
func (t *TUIChannel) Stop() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	// 关闭内部 channel
	close(t.outboundCh)
	close(t.eventCh)

	return t.ChannelBase.Stop()
}

// Send 发送消息到 TUI model
func (t *TUIChannel) Send(ctx context.Context, msg *bus.OutboundMessage) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.IsRunning() {
		return nil
	}

	select {
	case t.outboundCh <- msg:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		// channel 满，丢弃消息
		return nil
	}
}

// HandleChatEvent 处理聊天事件
func (t *TUIChannel) HandleChatEvent(ctx context.Context, event *bus.ChatEvent) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.IsRunning() {
		return nil
	}

	// 只处理当前 chatID 的事件
	if event.ChatID != t.model.GetChatID() {
		return nil
	}

	select {
	case t.eventCh <- event:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		// channel 满，丢弃事件
		return nil
	}
}

// SendStream 发送流式消息（TUI 使用增量内容）
func (t *TUIChannel) SendStream(ctx context.Context, chatID string, stream <-chan *bus.StreamMessage) error {
	for {
		select {
		case msg, ok := <-stream:
			if !ok {
				return nil
			}

			// 转换为 OutboundMessage 发送给 model
			outbound := &bus.OutboundMessage{
				Channel:    bus.ChannelTUI,
				ChatID:     chatID,
				Content:    msg.Content, // delta 模式下是增量内容
				IsStreaming: msg.IsFinal == false,
				IsThinking: msg.IsThinking,
				IsFinal:    msg.IsFinal,
				ChunkIndex: msg.ChunkIndex,
				Error:      msg.Error,
			}

			return t.Send(ctx, outbound)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// forwardEvents 将消息转发给 BubbleTea model
func (t *TUIChannel) forwardEvents(ctx context.Context) {
	for {
		select {
		case msg, ok := <-t.outboundCh:
			if !ok {
				return
			}
			t.model.ReceiveOutbound(msg)
		case event, ok := <-t.eventCh:
			if !ok {
				return
			}
			t.model.ReceiveChatEvent(event)
		case <-ctx.Done():
			return
		case <-t.WaitForStop():
			return
		}
	}
}

// PublishInbound 发布入站消息（用户输入）
func (t *TUIChannel) PublishInbound(ctx context.Context, content string, chatID string) error {
	msg := &bus.InboundMessage{
		Channel:       bus.ChannelTUI,
		AccountID:     "tui",
		SenderID:      "",
		ChatID:        chatID,
		Content:       content,
		StreamingMode: bus.StreamingModeDelta, // TUI 需要增量内容实时显示
	}

	return t.ChannelBase.PublishInbound(ctx, msg)
}

// IsAllowed TUI 总是允许所有发送者
func (t *TUIChannel) IsAllowed(senderID string) bool {
	return true
}