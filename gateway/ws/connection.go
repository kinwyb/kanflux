package ws

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/kinwyb/kanflux/gateway/handler"
	"github.com/kinwyb/kanflux/gateway/types"
)

// Connection WebSocket 连接
type Connection struct {
	connID string
	wsConn *websocket.Conn
	server *Server

	sendChan chan []byte // 发送队列
	mu       sync.Mutex  // 写锁

	logger *slog.Logger

	closed  bool
	closeMu sync.Mutex
}

// NewConnection 创建连接
func NewConnection(wsConn *websocket.Conn, server *Server) *Connection {
	connID := uuid.New().String()
	return &Connection{
		connID:   connID,
		wsConn:   wsConn,
		server:   server,
		sendChan: make(chan []byte, 100),
		logger:   server.logger.With("conn_id", connID[:8]),
	}
}

// Handle 处理连接（启动读/写循环）
func (c *Connection) Handle(ctx context.Context) {
	// 启动发送协程
	go c.sendLoop()

	// 读循环
	c.readLoop()
}

// readLoop 读循环
func (c *Connection) readLoop() {
	defer c.Close()

	for {
		_, message, err := c.wsConn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				c.logger.Debug("Read error", "error", err)
			}
			return
		}

		// 处理消息
		c.handleMessage(message)
	}
}

// sendLoop 发送循环
func (c *Connection) sendLoop() {
	for {
		msg, ok := <-c.sendChan
		if !ok {
			// sendChan 关闭，退出
			return
		}

		c.mu.Lock()
		err := c.wsConn.WriteMessage(websocket.TextMessage, msg)
		c.mu.Unlock()

		if err != nil {
			c.logger.Debug("Write error", "error", err)
			c.Close()
			return
		}
	}
}

// handleMessage 处理接收的消息
func (c *Connection) handleMessage(rawMsg []byte) {
	wsMsg, err := types.ParseWSMessage(rawMsg)
	if err != nil {
		c.logger.Error("Parse message error", "error", err)
		handler.SendError(c, "Invalid message format")
		return
	}

	// 使用 handler registry 处理消息
	if err := handler.Handle(c.server.ctx, c, wsMsg); err != nil {
		if _, ok := err.(handler.ErrNoHandler); ok {
			handler.SendError(c, "Unknown message type: "+string(wsMsg.Type))
		}
	}
}

// Send 发送消息（非阻塞） - implements handler.Conn interface
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
		c.logger.Warn("Send channel full, message dropped")
	}
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
	c.server.RemoveConnection(c.connID)

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

// SendMessage 发送任意类型的消息 - implements handler.Conn interface
func (c *Connection) SendMessage(typ types.MessageType, id string, payload interface{}) error {
	wsMsg, err := types.NewWSMessage(typ, id, payload)
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

// ID returns the connection ID - implements handler.Conn interface
func (c *Connection) ID() string {
	return c.connID
}

// GetServer returns the server interface - implements handler.Conn interface
func (c *Connection) GetServer() handler.Server {
	return c.server
}

// GetLogger returns the logger - implements handler.Conn interface
func (c *Connection) GetLogger() handler.Logger {
	return c.logger
}
