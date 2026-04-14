package ws

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// ClientConfig WebSocket 客户端配置
type ClientConfig struct {
	URL              string        // WebSocket URL，如 ws://localhost:8765/ws
	AuthToken        string        // 认证 token（可选）
	Reconnect        bool          // 是否自动重连
	MaxReconnect     int           // 最大重连次数，默认 10
	ReconnectInterval time.Duration // 重连间隔，默认 1秒
	HeartbeatInterval time.Duration // 心跳间隔，默认 30秒
}

// SetDefaults 设置默认值
func (c *ClientConfig) SetDefaults() {
	if c.MaxReconnect == 0 {
		c.MaxReconnect = 10
	}
	if c.ReconnectInterval == 0 {
		c.ReconnectInterval = 1 * time.Second
	}
	if c.HeartbeatInterval == 0 {
		c.HeartbeatInterval = 30 * time.Second
	}
}

// Client WebSocket 客户端
type Client struct {
	config    *ClientConfig
	wsConn    *websocket.Conn

	// 事件回调
	onOutbound   func(*OutboundPayload)
	onChatEvent  func(*ChatEventPayload)
	onLogEvent   func(*LogEventPayload)
	onConnect    func()
	onDisconnect func(error)

	// 接收通道
	outboundChan  chan *OutboundPayload
	chatEventChan chan *ChatEventPayload
	logEventChan  chan *LogEventPayload

	// 发送通道
	sendChan chan []byte

	mu       sync.Mutex
	connected bool

	logger   *slog.Logger

	ctx      context.Context
	cancel   context.CancelFunc

	// 重连计数
	reconnectCount int
}

// NewClient 创建客户端
func NewClient(cfg *ClientConfig) *Client {
	if cfg == nil {
		cfg = &ClientConfig{}
	}
	cfg.SetDefaults()

	ctx, cancel := context.WithCancel(context.Background())

	return &Client{
		config:        cfg,
		outboundChan:  make(chan *OutboundPayload, 100),
		chatEventChan: make(chan *ChatEventPayload, 100),
		logEventChan:  make(chan *LogEventPayload, 100),
		sendChan:      make(chan []byte, 100),
		logger:        slog.Default().With("component", "ws-client"),
		ctx:           ctx,
		cancel:        cancel,
	}
}

// Connect 连接服务器
func (c *Client) Connect(ctx context.Context) error {
	// 设置请求头（认证）
	header := http.Header{}
	if c.config.AuthToken != "" {
		header.Set("Authorization", "Bearer "+c.config.AuthToken)
	}

	wsConn, _, err := websocket.DefaultDialer.Dial(c.config.URL, header)
	if err != nil {
		return err
	}

	c.wsConn = wsConn
	c.mu.Lock()
	c.connected = true
	c.mu.Unlock()

	// 启动处理循环
	go c.readLoop(ctx)
	go c.sendLoop(ctx)
	go c.heartbeatLoop(ctx)

	if c.onConnect != nil {
		c.onConnect()
	}

	c.logger.Info("WebSocket connected", "url", c.config.URL)

	return nil
}

// ConnectWithReconnect 连接服务器（带重连）
func (c *Client) ConnectWithReconnect(ctx context.Context) error {
	err := c.Connect(ctx)
	if err != nil && c.config.Reconnect {
		go c.reconnectLoop(ctx)
	}
	return err
}

// reconnectLoop 重连循环
func (c *Client) reconnectLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(c.config.ReconnectInterval):
			if c.reconnectCount >= c.config.MaxReconnect {
				c.logger.Warn("Max reconnect attempts reached")
				return
			}

			c.reconnectCount++
			c.logger.Info("Attempting reconnect", "attempt", c.reconnectCount)

			err := c.Connect(ctx)
			if err == nil {
				c.reconnectCount = 0
				return // 连接成功，退出重连循环
			}

			c.logger.Debug("Reconnect failed", "error", err)
		}
	}
}

// readLoop 读循环
func (c *Client) readLoop(ctx context.Context) {
	defer c.handleDisconnect(nil)

	for {
		select {
		case <-ctx.Done():
			return
		default:
			_, message, err := c.wsConn.ReadMessage()
			if err != nil {
				c.handleDisconnect(err)
				return
			}

			c.handleMessage(message)
		}
	}
}

// sendLoop 发送循环
func (c *Client) sendLoop(ctx context.Context) {
	defer c.Close()

	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-c.sendChan:
			if !ok {
				return
			}

			c.mu.Lock()
			if c.wsConn == nil {
				c.mu.Unlock()
				return
			}
			err := c.wsConn.WriteMessage(websocket.TextMessage, msg)
			c.mu.Unlock()

			if err != nil {
				c.logger.Debug("Write error", "error", err)
				c.handleDisconnect(err)
				return
			}
		}
	}
}

// heartbeatLoop 心跳循环
func (c *Client) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(c.config.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.SendHeartbeat()
		}
	}
}

// handleMessage 处理消息
func (c *Client) handleMessage(rawMsg []byte) {
	wsMsg, err := ParseWSMessage(rawMsg)
	if err != nil {
		c.logger.Error("Parse message error", "error", err)
		return
	}

	switch wsMsg.Type {
	case MsgTypeOutbound:
		var payload OutboundPayload
		if err := wsMsg.ParsePayload(&payload); err == nil {
			if c.onOutbound != nil {
				c.onOutbound(&payload)
			}
			select {
			case c.outboundChan <- &payload:
			default:
			}
		}

	case MsgTypeChatEvent:
		var payload ChatEventPayload
		if err := wsMsg.ParsePayload(&payload); err == nil {
			if c.onChatEvent != nil {
				c.onChatEvent(&payload)
			}
			select {
			case c.chatEventChan <- &payload:
			default:
			}
		}

	case MsgTypeLogEvent:
		var payload LogEventPayload
		if err := wsMsg.ParsePayload(&payload); err == nil {
			if c.onLogEvent != nil {
				c.onLogEvent(&payload)
			}
			select {
			case c.logEventChan <- &payload:
			default:
			}
		}

	case MsgTypeHeartbeatAck:
		// 心跳响应，忽略

	case MsgTypeError:
		var payload ErrorPayload
		if err := wsMsg.ParsePayload(&payload); err == nil {
			c.logger.Error("Server error", "message", payload.Message)
		}
	}
}

// handleDisconnect 处理断开连接
func (c *Client) handleDisconnect(err error) {
	c.mu.Lock()
	if !c.connected {
		c.mu.Unlock()
		return
	}
	c.connected = false
	c.mu.Unlock()

	if c.onDisconnect != nil {
		c.onDisconnect(err)
	}

	c.logger.Warn("Disconnected", "error", err)

	// 如果需要重连
	if c.config.Reconnect && err != nil {
		go c.reconnectLoop(c.ctx)
	}
}

// SendInbound 发送入站消息
func (c *Client) SendInbound(ctx context.Context, channel, accountID, senderID, chatID, content string, media []MediaPayload, metadata map[string]interface{}) error {
	payload := InboundPayload{
		Channel:   channel,
		AccountID: accountID,
		SenderID:  senderID,
		ChatID:    chatID,
		Content:   content,
		Media:     media,
		Metadata:  metadata,
	}

	wsMsg, err := NewWSMessage(MsgTypeInbound, uuid.New().String(), payload)
	if err != nil {
		return err
	}

	msgBytes, err := wsMsg.Marshal()
	if err != nil {
		return err
	}

	select {
	case c.sendChan <- msgBytes:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		return errors.New("send channel full")
	}
}

// Subscribe 订阅特定的 channels/chatIDs
func (c *Client) Subscribe(channels, chatIDs []string) error {
	payload := SubscribePayload{
		Channels: channels,
		ChatIDs:  chatIDs,
	}

	wsMsg, err := NewWSMessage(MsgTypeSubscribe, uuid.New().String(), payload)
	if err != nil {
		return err
	}

	msgBytes, err := wsMsg.Marshal()
	if err != nil {
		return err
	}

	select {
	case c.sendChan <- msgBytes:
		return nil
	default:
		return errors.New("send channel full")
	}
}

// SendHeartbeat 发送心跳
func (c *Client) SendHeartbeat() error {
	payload := HeartbeatPayload{
		Timestamp: time.Now().UnixMilli(),
	}

	wsMsg, err := NewWSMessage(MsgTypeHeartbeat, uuid.New().String(), payload)
	if err != nil {
		return err
	}

	msgBytes, err := wsMsg.Marshal()
	if err != nil {
		return err
	}

	select {
	case c.sendChan <- msgBytes:
		return nil
	default:
		return errors.New("send channel full")
	}
}

// OutboundChan 获取出站消息通道
func (c *Client) OutboundChan() <-chan *OutboundPayload {
	return c.outboundChan
}

// ChatEventChan 获取聊天事件通道
func (c *Client) ChatEventChan() <-chan *ChatEventPayload {
	return c.chatEventChan
}

// LogEventChan 获取日志事件通道
func (c *Client) LogEventChan() <-chan *LogEventPayload {
	return c.logEventChan
}

// IsConnected 检查是否已连接
func (c *Client) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected
}

// SetOnOutbound 设置出站消息回调
func (c *Client) SetOnOutbound(callback func(*OutboundPayload)) {
	c.onOutbound = callback
}

// SetOnChatEvent 设置聊天事件回调
func (c *Client) SetOnChatEvent(callback func(*ChatEventPayload)) {
	c.onChatEvent = callback
}

// SetOnLogEvent 设置日志事件回调
func (c *Client) SetOnLogEvent(callback func(*LogEventPayload)) {
	c.onLogEvent = callback
}

// SetOnConnect 设置连接回调
func (c *Client) SetOnConnect(callback func()) {
	c.onConnect = callback
}

// SetOnDisconnect 设置断开回调
func (c *Client) SetOnDisconnect(callback func(error)) {
	c.onDisconnect = callback
}

// Close 关闭连接
func (c *Client) Close() error {
	c.cancel()

	c.mu.Lock()
	c.connected = false
	if c.wsConn != nil {
		c.wsConn.Close()
		c.wsConn = nil
	}
	c.mu.Unlock()

	// 关闭通道
	close(c.sendChan)
	close(c.outboundChan)
	close(c.chatEventChan)
	close(c.logEventChan)

	c.logger.Info("Client closed")

	return nil
}