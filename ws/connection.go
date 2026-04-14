package ws

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// Connection WebSocket 连接
type Connection struct {
	ID       string
	wsConn   *websocket.Conn
	server   *Server

	sendChan chan []byte // 发送队列
	mu       sync.Mutex  // 写锁

	logger *slog.Logger

	closed  bool
	closeMu sync.Mutex
}

// NewConnection 创建连接
func NewConnection(wsConn *websocket.Conn, server *Server) *Connection {
	return &Connection{
		ID:       uuid.New().String(),
		wsConn:   wsConn,
		server:   server,
		sendChan: make(chan []byte, 100),
		logger:   server.logger.With("conn_id", uuid.New().String()[:8]),
	}
}

// Handle 处理连接（启动读/写循环）
func (c *Connection) Handle(ctx context.Context) {
	// 启动发送协程
	go c.sendLoop(ctx)

	// 读循环
	c.readLoop(ctx)
}

// readLoop 读循环
func (c *Connection) readLoop(ctx context.Context) {
	defer c.Close()

	for {
		select {
		case <-ctx.Done():
			return
		default:
			_, message, err := c.wsConn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					c.logger.Debug("Read error", "error", err)
				}
				return
			}

			// 处理消息
			c.handleMessage(ctx, message)
		}
	}
}

// sendLoop 发送循环
func (c *Connection) sendLoop(ctx context.Context) {
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
			err := c.wsConn.WriteMessage(websocket.TextMessage, msg)
			c.mu.Unlock()

			if err != nil {
				c.logger.Debug("Write error", "error", err)
				return
			}
		}
	}
}

// handleMessage 处理接收的消息
func (c *Connection) handleMessage(ctx context.Context, rawMsg []byte) {
	wsMsg, err := ParseWSMessage(rawMsg)
	if err != nil {
		c.logger.Error("Parse message error", "error", err)
		c.sendError("Invalid message format")
		return
	}

	switch wsMsg.Type {
	case MsgTypeInbound:
		c.handleInbound(ctx, wsMsg)
	case MsgTypeSubscribe:
		c.handleSubscribe(wsMsg)
	case MsgTypeHeartbeat:
		c.handleHeartbeat(wsMsg)
	case MsgTypeControl:
		c.handleControl(wsMsg)
	default:
		c.sendError("Unknown message type: " + string(wsMsg.Type))
	}
}

// handleInbound 处理入站消息
func (c *Connection) handleInbound(ctx context.Context, wsMsg *WSMessage) {
	var payload InboundPayload
	if err := wsMsg.ParsePayload(&payload); err != nil {
		c.logger.Error("Parse inbound payload error", "error", err)
		c.sendError("Invalid inbound payload")
		return
	}

	// 转换为 InboundMessage
	inbound := ConvertPayloadToInbound(&payload)
	if inbound.ID == "" {
		inbound.ID = wsMsg.ID
	}

	// 发布到 MessageBus
	if err := c.server.PublishInbound(ctx, inbound); err != nil {
		c.logger.Error("Publish inbound error", "error", err)
		c.sendError("Failed to publish inbound: " + err.Error())
	}

	c.logger.Debug("Inbound message published", "channel", inbound.Channel, "chat_id", inbound.ChatID)
}

// handleSubscribe 处理订阅请求
func (c *Connection) handleSubscribe(wsMsg *WSMessage) {
	var payload SubscribePayload
	if err := wsMsg.ParsePayload(&payload); err != nil {
		c.logger.Error("Parse subscribe payload error", "error", err)
		c.sendError("Invalid subscribe payload")
		return
	}

	// 更新订阅
	c.server.UpdateSubscription(c.ID, payload.Channels, payload.ChatIDs)

	c.logger.Debug("Subscription updated", "channels", payload.Channels, "chat_ids", payload.ChatIDs)
}

// handleHeartbeat 处理心跳
func (c *Connection) handleHeartbeat(wsMsg *WSMessage) {
	var payload HeartbeatPayload
	if err := wsMsg.ParsePayload(&payload); err != nil {
		payload.Timestamp = time.Now().UnixMilli()
	}

	// 发送心跳响应
	ackPayload := HeartbeatPayload{
		Timestamp: time.Now().UnixMilli(),
	}

	ackMsg, err := NewWSMessage(MsgTypeHeartbeatAck, wsMsg.ID, ackPayload)
	if err != nil {
		return
	}

	msgBytes, err := ackMsg.Marshal()
	if err != nil {
		return
	}

	c.Send(msgBytes)
}

// handleControl 处理控制消息
func (c *Connection) handleControl(wsMsg *WSMessage) {
	var payload ControlPayload
	if err := wsMsg.ParsePayload(&payload); err != nil {
		c.logger.Error("Parse control payload error", "error", err)
		c.sendControlAck(wsMsg.ID, payload.Action, false, "Invalid control payload")
		return
	}

	switch payload.Action {
	case ControlActionShutdown:
		c.logger.Info("Received shutdown control message")
		// 发送响应
		c.sendControlAck(wsMsg.ID, ControlActionShutdown, true, "Gateway shutting down...")
		// 触发服务关闭
		c.server.TriggerShutdown()
	case ControlActionStatus:
		// 发送状态响应
		status := c.server.GetStatus()
		c.sendControlAckWithData(wsMsg.ID, ControlActionStatus, true, "Gateway is running", status)
	default:
		c.sendControlAck(wsMsg.ID, payload.Action, false, "Unknown control action: "+payload.Action)
	}
}

// sendControlAck 发送控制消息响应
func (c *Connection) sendControlAck(id, action string, success bool, message string) {
	ackPayload := ControlAckPayload{
		Action:  action,
		Success: success,
		Message: message,
	}

	ackMsg, err := NewWSMessage(MsgTypeControlAck, id, ackPayload)
	if err != nil {
		return
	}

	msgBytes, err := ackMsg.Marshal()
	if err != nil {
		return
	}

	c.Send(msgBytes)
}

// sendControlAckWithData 发送带数据的控制消息响应
func (c *Connection) sendControlAckWithData(id, action string, success bool, message string, data map[string]interface{}) {
	ackPayload := ControlAckPayload{
		Action:  action,
		Success: success,
		Message: message,
	}
	// 使用 base payload 结构发送额外数据
	ackMsg, err := NewWSMessage(MsgTypeControlAck, id, ackPayload)
	if err != nil {
		return
	}

	msgBytes, err := ackMsg.Marshal()
	if err != nil {
		return
	}

	c.Send(msgBytes)
}

// Send 发送消息（非阻塞）
func (c *Connection) Send(msg []byte) {
	c.closeMu.Lock()
	if c.closed {
		c.closeMu.Unlock()
		return
	}
	c.closeMu.Unlock()

	select {
	case c.sendChan <- msg:
	default:
		// channel 满，丢弃消息
		c.logger.Warn("Send channel full, message dropped")
	}
}

// sendError 发送错误消息
func (c *Connection) sendError(message string) {
	errorPayload := ErrorPayload{
		Message: message,
	}

	wsMsg, err := NewWSMessage(MsgTypeError, uuid.New().String(), errorPayload)
	if err != nil {
		return
	}

	msgBytes, err := wsMsg.Marshal()
	if err != nil {
		return
	}

	c.Send(msgBytes)
}

// Close 关闭连接
func (c *Connection) Close() {
	c.closeMu.Lock()
	if c.closed {
		c.closeMu.Unlock()
		return
	}
	c.closed = true
	c.closeMu.Unlock()

	// 从服务器移除
	c.server.RemoveConnection(c.ID)

	// 关闭发送通道
	close(c.sendChan)

	// 关闭 WebSocket 连接
	c.wsConn.Close()

	c.logger.Info("Connection closed")
}

// IsClosed 检查连接是否已关闭
func (c *Connection) IsClosed() bool {
	c.closeMu.Lock()
	defer c.closeMu.Unlock()
	return c.closed
}

// SendMessage 发送任意类型的消息
func (c *Connection) SendMessage(typ MessageType, id string, payload interface{}) error {
	wsMsg, err := NewWSMessage(typ, id, payload)
	if err != nil {
		return err
	}

	msgBytes, err := wsMsg.Marshal()
	if err != nil {
		return err
	}

	c.Send(msgBytes)
	return nil
}

// SendJSON 直接发送 JSON 数据
func (c *Connection) SendJSON(data interface{}) error {
	msgBytes, err := json.Marshal(data)
	if err != nil {
		return err
	}

	c.Send(msgBytes)
	return nil
}