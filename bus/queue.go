package bus

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

// MessageBus 消息总线
type MessageBus struct {
	inbound       chan *InboundMessage
	outbound      chan *OutboundMessage
	chatEvents    chan *ChatEvent
	logEvents     chan *LogEvent
	outSubs       map[string]chan *OutboundMessage
	chatSubs      map[string]chan *ChatEvent
	logSubs       map[string]chan *LogEvent
	outSubsMu     sync.RWMutex
	chatSubsMu    sync.RWMutex
	logSubsMu     sync.RWMutex
	mu            sync.RWMutex
	closed        bool
	fanoutStopped bool
}

// NewMessageBus 创建消息总线
func NewMessageBus(bufferSize int) *MessageBus {
	b := &MessageBus{
		inbound:    make(chan *InboundMessage, bufferSize),
		outbound:   make(chan *OutboundMessage, bufferSize),
		chatEvents: make(chan *ChatEvent, bufferSize),
		logEvents:  make(chan *LogEvent, bufferSize),
		outSubs:    make(map[string]chan *OutboundMessage),
		chatSubs:   make(map[string]chan *ChatEvent),
		logSubs:    make(map[string]chan *LogEvent),
		closed:     false,
	}
	// 启动广播 goroutine
	go b.fanoutMessages()
	go b.fanoutChatEvents()
	go b.fanoutLogEvents()
	return b
}

// PublishInbound 发布入站消息
func (b *MessageBus) PublishInbound(ctx context.Context, msg *InboundMessage) error {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.closed {
		return ErrBusClosed
	}

	// 设置ID和时间戳
	if msg.ID == "" {
		msg.ID = uuid.New().String()
	}
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now()
	}

	select {
	case b.inbound <- msg:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ConsumeInbound 消费入站消息
func (b *MessageBus) ConsumeInbound(ctx context.Context) (*InboundMessage, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.closed {
		return nil, ErrBusClosed
	}

	select {
	case msg := <-b.inbound:
		return msg, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// PublishOutbound 发布出站消息
func (b *MessageBus) PublishOutbound(ctx context.Context, msg *OutboundMessage) error {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.closed {
		return ErrBusClosed
	}

	// 设置ID和时间戳
	if msg.ID == "" {
		msg.ID = uuid.New().String()
	}
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now()
	}

	select {
	case b.outbound <- msg:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ConsumeOutbound 消费出站消息
// 使用订阅机制，确保消息能够被正确接收
func (b *MessageBus) ConsumeOutbound(ctx context.Context) (*OutboundMessage, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.closed {
		return nil, ErrBusClosed
	}

	// 创建临时订阅
	sub := b.SubscribeOutbound()
	defer sub.Unsubscribe()

	// 等待消息
	select {
	case msg := <-sub.Channel:
		return msg, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Close 关闭消息总线
func (b *MessageBus) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return nil
	}

	b.closed = true

	// 关闭所有订阅者的 channel
	b.outSubsMu.Lock()
	for _, ch := range b.outSubs {
		close(ch)
	}
	// 清空 map
	for k := range b.outSubs {
		delete(b.outSubs, k)
	}
	b.outSubsMu.Unlock()

	// 关闭聊天事件订阅者的 channel
	b.chatSubsMu.Lock()
	for _, ch := range b.chatSubs {
		close(ch)
	}
	for k := range b.chatSubs {
		delete(b.chatSubs, k)
	}
	b.chatSubsMu.Unlock()

	// 关闭日志事件订阅者的 channel
	b.logSubsMu.Lock()
	for _, ch := range b.logSubs {
		close(ch)
	}
	for k := range b.logSubs {
		delete(b.logSubs, k)
	}
	b.logSubsMu.Unlock()

	close(b.inbound)
	close(b.outbound)
	close(b.chatEvents)
	close(b.logEvents)

	return nil
}

// IsClosed 检查是否已关闭
func (b *MessageBus) IsClosed() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.closed
}

// InboundCount 获取入站消息数量
func (b *MessageBus) InboundCount() int {
	return len(b.inbound)
}

// OutboundCount 获取出站消息数量
func (b *MessageBus) OutboundCount() int {
	return len(b.outbound)
}

// OutboundSubscription 出站消息订阅
type OutboundSubscription struct {
	ID      string
	Channel <-chan *OutboundMessage
	bus     *MessageBus
}

// Unsubscribe 取消订阅
func (s *OutboundSubscription) Unsubscribe() {
	if s == nil || s.bus == nil {
		return
	}
	s.bus.UnsubscribeOutbound(s.ID)
}

// SubscribeOutbound 订阅出站消息（支持多个消费者）
// 使用内部订阅机制，每个订阅者有独立的 channel
// 返回一个 OutboundSubscription 对象，包含只读 channel 和取消订阅方法
func (b *MessageBus) SubscribeOutbound() *OutboundSubscription {
	b.outSubsMu.Lock()
	defer b.outSubsMu.Unlock()

	subID := uuid.New().String()
	ch := make(chan *OutboundMessage, 100) // 每个订阅者有独立的缓冲
	b.outSubs[subID] = ch

	return &OutboundSubscription{
		ID:      subID,
		Channel: ch,
		bus:     b,
	}
}

// UnsubscribeOutbound 取消订阅出站消息
func (b *MessageBus) UnsubscribeOutbound(subID string) {
	b.outSubsMu.Lock()
	defer b.outSubsMu.Unlock()

	ch, ok := b.outSubs[subID]
	if ok {
		delete(b.outSubs, subID)
		close(ch)
	}
}

// fanoutMessages 将 outbound channel 的消息分发给所有订阅者
// 这是唯一从 outbound channel 读取的地方
func (b *MessageBus) fanoutMessages() {
	for msg := range b.outbound {
		b.outSubsMu.RLock()
		subCount := len(b.outSubs)
		b.outSubsMu.RUnlock()

		if subCount == 0 {
			continue
		}

		// 转发到所有订阅者
		b.outSubsMu.RLock()
		for _, ch := range b.outSubs {
			// 非阻塞发送，避免一个慢订阅者阻塞其他订阅者
			select {
			case ch <- msg:
			default:
			}
		}
		b.outSubsMu.RUnlock()
	}

	b.mu.Lock()
	b.fanoutStopped = true
	b.mu.Unlock()
}

// OutboundChan 获取出站消息通道（已废弃）
// 此方法已废弃，请使用 SubscribeOutbound 代替
func (b *MessageBus) OutboundChan() <-chan *OutboundMessage {
	return b.outbound
}

// Errors
var (
	ErrBusClosed = &Error{Message: "message bus is closed"}
)

// Error 总线错误
type Error struct {
	Message string
}

func (e *Error) Error() string {
	return e.Message
}

// PublishChatEvent 发布聊天事件
func (b *MessageBus) PublishChatEvent(ctx context.Context, event *ChatEvent) error {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.closed {
		return ErrBusClosed
	}

	// 设置默认值
	if event.ID == "" {
		event.ID = uuid.New().String()
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	select {
	case b.chatEvents <- event:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		slog.Debug("Bus publish chat event full")
		return nil
	}
}

// ChatEventSubscription 聊天事件订阅
type ChatEventSubscription struct {
	ID      string
	Channel <-chan *ChatEvent
	bus     *MessageBus
}

// Unsubscribe 取消订阅
func (s *ChatEventSubscription) Unsubscribe() {
	if s == nil || s.bus == nil {
		return
	}
	s.bus.UnsubscribeChatEvent(s.ID)
}

// SubscribeChatEvent 订阅聊天事件
func (b *MessageBus) SubscribeChatEvent() *ChatEventSubscription {
	b.chatSubsMu.Lock()
	defer b.chatSubsMu.Unlock()

	subID := uuid.New().String()
	ch := make(chan *ChatEvent, 100)
	b.chatSubs[subID] = ch

	return &ChatEventSubscription{
		ID:      subID,
		Channel: ch,
		bus:     b,
	}
}

// UnsubscribeChatEvent 取消订阅聊天事件
func (b *MessageBus) UnsubscribeChatEvent(subID string) {
	b.chatSubsMu.Lock()
	defer b.chatSubsMu.Unlock()

	ch, ok := b.chatSubs[subID]
	if ok {
		delete(b.chatSubs, subID)
		close(ch)
	}
}

// fanoutChatEvents 将聊天事件分发给所有订阅者
func (b *MessageBus) fanoutChatEvents() {
	for event := range b.chatEvents {
		b.chatSubsMu.RLock()
		subCount := len(b.chatSubs)
		b.chatSubsMu.RUnlock()

		if subCount == 0 {
			continue
		}

		// 转发到所有订阅者
		b.chatSubsMu.RLock()
		for _, ch := range b.chatSubs {
			select {
			case ch <- event:
			default:
			}
		}
		b.chatSubsMu.RUnlock()
	}
}

// PublishLogEvent 发布日志事件
func (b *MessageBus) PublishLogEvent(ctx context.Context, event *LogEvent) error {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.closed {
		return ErrBusClosed
	}

	// 设置默认值
	if event.ID == "" {
		event.ID = uuid.New().String()
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	select {
	case b.logEvents <- event:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		slog.Debug("Bus event bus is full")
		return nil
	}
}

// LogEventSubscription 日志事件订阅
type LogEventSubscription struct {
	ID      string
	Channel <-chan *LogEvent
	bus     *MessageBus
}

// Unsubscribe 取消订阅
func (s *LogEventSubscription) Unsubscribe() {
	if s == nil || s.bus == nil {
		return
	}
	s.bus.UnsubscribeLogEvent(s.ID)
}

// SubscribeLogEvent 订阅日志事件
func (b *MessageBus) SubscribeLogEvent() *LogEventSubscription {
	b.logSubsMu.Lock()
	defer b.logSubsMu.Unlock()

	subID := uuid.New().String()
	ch := make(chan *LogEvent, 100)
	b.logSubs[subID] = ch

	return &LogEventSubscription{
		ID:      subID,
		Channel: ch,
		bus:     b,
	}
}

// UnsubscribeLogEvent 取消订阅日志事件
func (b *MessageBus) UnsubscribeLogEvent(subID string) {
	b.logSubsMu.Lock()
	defer b.logSubsMu.Unlock()

	ch, ok := b.logSubs[subID]
	if ok {
		delete(b.logSubs, subID)
		close(ch)
	}
}

// fanoutLogEvents 将日志事件分发给所有订阅者
func (b *MessageBus) fanoutLogEvents() {
	for event := range b.logEvents {
		b.logSubsMu.RLock()
		subCount := len(b.logSubs)
		b.logSubsMu.RUnlock()

		if subCount == 0 {
			continue
		}

		// 转发到所有订阅者
		b.logSubsMu.RLock()
		for _, ch := range b.logSubs {
			select {
			case ch <- event:
			default:
			}
		}
		b.logSubsMu.RUnlock()
	}
}
