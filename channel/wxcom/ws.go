package wxcom

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// WsManager WebSocket连接管理器
// 负责维护与企业微信的WebSocket长连接，包括心跳、重连、认证、串行回复队列等
type WsManager struct {
	config *WxComConfig
	logger *slog.Logger

	// WebSocket连接
	conn        *websocket.Conn
	connMu      sync.Mutex
	isConnected bool
	isManualClose bool

	// 认证状态
	isAuthenticated bool

	// 心跳
	heartbeatCancel context.CancelFunc
	missedPongCount int

	// 重连
	reconnectAttempts int

	// 回复队列 (reqID -> queue)
	replyQueues   map[string][]*replyQueueItem
	replyQueuesMu sync.Mutex
	processingQueuesMu sync.Mutex
	processingQueues   map[string]bool // 正在处理的reqID集合

	// 待处理回执
	pendingAcks   map[string]*pendingAck
	pendingAcksMu sync.Mutex

	// 回调
	onConnected    func()
	onAuthenticated func()
	onDisconnected func(reason string)
	onReconnecting func(attempt int)
	onError        func(error)
	onMessage      func(frame *WsFrame)
	onEvent        func(frame *WsFrame)

	// 接收循环
	receiveCancel context.CancelFunc
	receiveCtx    context.Context

	// 停止信号
	stopChan chan struct{}
}

// replyQueueItem 回复队列项
type replyQueueItem struct {
	frame  *WsFrame
	result chan *WsFrame
	err    chan error
}

// pendingAck 待处理回执
type pendingAck struct {
	result    chan *WsFrame
	err       chan error
	timeout   *time.Timer
	reqID     string
}

// NewWsManager 创建WebSocket管理器
func NewWsManager(config *WxComConfig, logger *slog.Logger) *WsManager {
	return &WsManager{
		config:           config,
		logger:           logger,
		replyQueues:      make(map[string][]*replyQueueItem),
		pendingAcks:      make(map[string]*pendingAck),
		processingQueues: make(map[string]bool),
		stopChan:         make(chan struct{}),
	}
}

// SetCredentials 设置认证凭证 (已通过config设置)
func (m *WsManager) SetCredentials(botID, secret string) {
	m.config.BotID = botID
	m.config.Secret = secret
}

// Connect 建立WebSocket连接
func (m *WsManager) Connect(ctx context.Context) error {
	m.isManualClose = false

	// 清理旧连接
	m.cleanup()

	m.logger.Info("Connecting to WebSocket", "url", m.config.WSURL)

	dialer := websocket.DefaultDialer
	conn, _, err := dialer.DialContext(ctx, m.config.WSURL, nil)
	if err != nil {
		m.logger.Error("Failed to create WebSocket connection", "error", err)
		if m.onError != nil {
			m.onError(err)
		}
		// 安排重连
		go m.scheduleReconnect(ctx)
		return err
	}

	m.connMu.Lock()
	m.conn = conn
	m.isConnected = true
	m.connMu.Unlock()

	m.reconnectAttempts = 0
	m.missedPongCount = 0

	m.logger.Info("WebSocket connection established, sending auth...")

	// 连接建立回调
	if m.onConnected != nil {
		m.onConnected()
	}

	// 发送认证帧
	if err := m.sendAuth(ctx); err != nil {
		m.logger.Error("Failed to send auth frame", "error", err)
		return err
	}

	// 启动消息接收循环
	m.receiveCtx, m.receiveCancel = context.WithCancel(context.Background())
	go m.receiveLoop(m.receiveCtx)

	return nil
}

// cleanup 清理WebSocket连接
func (m *WsManager) cleanup() {
	// 停止接收循环
	if m.receiveCancel != nil {
		m.receiveCancel()
		m.receiveCancel = nil
	}

	// 停止心跳
	m.stopHeartbeat()

	// 关闭连接
	m.connMu.Lock()
	if m.conn != nil {
		// 使用CloseMessage发送关闭帧
		err := m.conn.WriteMessage(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, "Manual disconnect"),
		)
		if err != nil {
			m.conn.Close()
		}
		m.conn = nil
	}
	m.isConnected = false
	m.isAuthenticated = false
	m.connMu.Unlock()

	// 清理待处理消息
	m.clearPendingMessages("connection closed")
}

// sendAuth 发送认证帧
func (m *WsManager) sendAuth(ctx context.Context) error {
	reqID := generateReqID(WsCmdSubscribe)
	frame := &WsFrame{
		Cmd: WsCmdSubscribe,
		Headers: map[string]string{
			"req_id": reqID,
		},
		Body: map[string]interface{}{
			"bot_id": m.config.BotID,
			"secret": m.config.Secret,
		},
	}

	return m.send(frame)
}

// receiveLoop 消息接收循环
func (m *WsManager) receiveLoop(ctx context.Context) {
	defer m.cleanup()

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.stopChan:
			return
		default:
		}

		m.connMu.Lock()
		conn := m.conn
		m.connMu.Unlock()

		if conn == nil {
			return
		}

		_, rawMessage, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				m.logger.Warn("WebSocket connection closed", "error", err)
			}
			m.stopHeartbeat()
			m.clearPendingMessages(fmt.Sprintf("WebSocket connection closed (%v)", err))
			if m.onDisconnected != nil {
				m.onDisconnected(err.Error())
			}
			if !m.isManualClose {
				go m.scheduleReconnect(context.Background())
			}
			return
		}

		var frame WsFrame
		if err := json.Unmarshal(rawMessage, &frame); err != nil {
			m.logger.Error("Failed to parse WebSocket message", "error", err)
			continue
		}

		m.handleFrame(&frame)
	}
}

// handleFrame 处理收到的帧数据
func (m *WsManager) handleFrame(frame *WsFrame) {
	cmd := frame.Cmd

	// 消息推送
	if cmd == WsCmdCallback {
		m.logger.Debug("Received push message")
		if m.onMessage != nil {
			m.onMessage(frame)
		}
		return
	}

	// 事件推送
	if cmd == WsCmdEventCallback {
		m.logger.Debug("Received event callback")
		if m.onEvent != nil {
			m.onEvent(frame)
		}
		return
	}

	// 无cmd的帧：认证响应、心跳响应或回复消息回执
	reqID := frame.Headers["req_id"]

	// 检查是否是回复消息的回执
	m.pendingAcksMu.Lock()
	if ack, ok := m.pendingAcks[reqID]; ok {
		m.handleReplyAck(reqID, frame, ack)
		m.pendingAcksMu.Unlock()
		return
	}
	m.pendingAcksMu.Unlock()

	// 认证响应
	if reqID != "" && reqID[:len(WsCmdSubscribe)] == WsCmdSubscribe {
		if frame.ErrCode != 0 {
			m.logger.Error("Authentication failed", "errcode", frame.ErrCode, "errmsg", frame.ErrMsg)
			if m.onError != nil {
				m.onError(fmt.Errorf("authentication failed: %s (code: %d)", frame.ErrMsg, frame.ErrCode))
			}
			return
		}
		m.logger.Info("Authentication successful")
		m.connMu.Lock()
		m.isAuthenticated = true
		m.connMu.Unlock()
		m.startHeartbeat()
		if m.onAuthenticated != nil {
			m.onAuthenticated()
		}
		return
	}

	// 心跳响应
	if reqID != "" && reqID[:len(WsCmdHeartbeat)] == WsCmdHeartbeat {
		if frame.ErrCode != 0 {
			m.logger.Warn("Heartbeat ack error", "errcode", frame.ErrCode, "errmsg", frame.ErrMsg)
			return
		}
		m.missedPongCount = 0
		m.logger.Debug("Received heartbeat ack")
		return
	}

	// 未知帧类型
	m.logger.Warn("Received unknown frame", "cmd", cmd, "req_id", reqID)
	if m.onMessage != nil {
		m.onMessage(frame)
	}
}

// startHeartbeat 启动心跳
func (m *WsManager) startHeartbeat() {
	m.stopHeartbeat()

	ctx, cancel := context.WithCancel(context.Background())
	m.heartbeatCancel = cancel

	go m.heartbeatLoop(ctx)
	m.logger.Debug("Heartbeat timer started", "interval", m.config.HeartbeatInterval)
}

// stopHeartbeat 停止心跳
func (m *WsManager) stopHeartbeat() {
	if m.heartbeatCancel != nil {
		m.heartbeatCancel()
		m.heartbeatCancel = nil
		m.logger.Debug("Heartbeat timer stopped")
	}
}

// heartbeatLoop 心跳循环
func (m *WsManager) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(m.config.HeartbeatInterval) * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.sendHeartbeat()
		}
	}
}

// sendHeartbeat 发送心跳
func (m *WsManager) sendHeartbeat() {
	// 检查连续未收到pong的次数
	if m.missedPongCount >= DefaultMaxMissedPong {
		m.logger.Warn("No heartbeat ack received for consecutive pings, connection considered dead")
		m.stopHeartbeat()
		// 强制关闭底层连接
		m.connMu.Lock()
		if m.conn != nil {
			m.conn.Close()
			m.conn = nil
		}
		m.isConnected = false
		m.connMu.Unlock()
		return
	}

	m.missedPongCount++

	reqID := generateReqID(WsCmdHeartbeat)
	frame := &WsFrame{
		Cmd: WsCmdHeartbeat,
		Headers: map[string]string{
			"req_id": reqID,
		},
	}

	if err := m.send(frame); err != nil {
		m.logger.Error("Failed to send heartbeat", "error", err)
	}
}

// scheduleReconnect 安排重连
func (m *WsManager) scheduleReconnect(ctx context.Context) {
	if m.isManualClose {
		return
	}

	if m.config.MaxReconnect != -1 && m.reconnectAttempts >= m.config.MaxReconnect {
		m.logger.Error("Max reconnect attempts reached", "attempts", m.reconnectAttempts)
		if m.onError != nil {
			m.onError(ErrMaxReconnect)
		}
		return
	}

	m.reconnectAttempts++
	// 指数退避：1s, 2s, 4s, 8s ... 上限30s
	delay := min(
		m.config.ReconnectInterval*(1<<(m.reconnectAttempts-1)),
		DefaultReconnectMaxDelay,
	)

	m.logger.Info("Reconnecting...", "attempt", m.reconnectAttempts, "delay_ms", delay)
	if m.onReconnecting != nil {
		m.onReconnecting(m.reconnectAttempts)
	}

	time.Sleep(time.Duration(delay) * time.Millisecond)
	if m.isManualClose {
		return
	}

	m.Connect(ctx)
}

// send 发送数据帧
func (m *WsManager) send(frame *WsFrame) error {
	m.connMu.Lock()
	defer m.connMu.Unlock()

	if m.conn == nil || !m.isConnected {
		return ErrNotConnected
	}

	data, err := json.Marshal(frame)
	if err != nil {
		return err
	}

	return m.conn.WriteMessage(websocket.TextMessage, data)
}

// SendReply 通过WebSocket通道发送回复消息 (串行队列版本)
// 同一个reqID的消息会被放入队列中串行发送
func (m *WsManager) SendReply(ctx context.Context, reqID string, body map[string]interface{}, cmd string) (*WsFrame, error) {
	frame := &WsFrame{
		Cmd: cmd,
		Headers: map[string]string{
			"req_id": reqID,
		},
		Body: body,
	}

	item := &replyQueueItem{
		frame:  frame,
		result: make(chan *WsFrame, 1),
		err:    make(chan error, 1),
	}

	m.replyQueuesMu.Lock()
	if m.replyQueues[reqID] == nil {
		m.replyQueues[reqID] = []*replyQueueItem{}
	}

	queue := m.replyQueues[reqID]

	// 防止队列无限增长
	if len(queue) >= 100 {
		m.replyQueuesMu.Unlock()
		return nil, ErrQueueFull
	}

	queue = append(queue, item)
	m.replyQueues[reqID] = queue

	// 如果队列中只有这一条，立即开始处理
	if len(queue) == 1 {
		m.processingQueuesMu.Lock()
		processing := m.processingQueues[reqID]
		m.processingQueuesMu.Unlock()
		if !processing {
			go m.processReplyQueue(ctx, reqID)
		}
	}
	m.replyQueuesMu.Unlock()

	// 等待结果
	select {
	case result := <-item.result:
		return result, nil
	case err := <-item.err:
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// processReplyQueue 处理指定reqID的回复队列
func (m *WsManager) processReplyQueue(ctx context.Context, reqID string) {
	m.processingQueuesMu.Lock()
	m.processingQueues[reqID] = true
	m.processingQueuesMu.Unlock()

	defer func() {
		m.processingQueuesMu.Lock()
		delete(m.processingQueues, reqID)
		m.processingQueuesMu.Unlock()
	}()

	for {
		m.replyQueuesMu.Lock()
		queue := m.replyQueues[reqID]
		if len(queue) == 0 {
			delete(m.replyQueues, reqID)
			m.replyQueuesMu.Unlock()
			break
		}

		item := queue[0]
		m.replyQueuesMu.Unlock()

		// 发送消息
		if err := m.send(item.frame); err != nil {
			m.logger.Error("Failed to send reply", "req_id", reqID, "error", err)
			m.replyQueuesMu.Lock()
			m.replyQueues[reqID] = m.replyQueues[reqID][1:]
			m.replyQueuesMu.Unlock()
			item.err <- err
			continue
		}

		m.logger.Debug("Reply message sent", "req_id", reqID)

		// 设置回执等待
		ack := &pendingAck{
			result:  item.result,
			err:     item.err,
			reqID:   reqID,
		}
		ack.timeout = time.AfterFunc(time.Duration(DefaultReplyAckTimeout)*time.Second, func() {
			m.onReplyAckTimeout(reqID, ack)
		})

		m.pendingAcksMu.Lock()
		m.pendingAcks[reqID] = ack
		m.pendingAcksMu.Unlock()

		// 等待回执结果
		select {
		case result := <-ack.result:
			// 成功收到回执
			m.replyQueuesMu.Lock()
			m.replyQueues[reqID] = m.replyQueues[reqID][1:]
			m.replyQueuesMu.Unlock()
			item.result <- result
		case err := <-ack.err:
			// 回执错误
			m.replyQueuesMu.Lock()
			m.replyQueues[reqID] = m.replyQueues[reqID][1:]
			m.replyQueuesMu.Unlock()
			item.err <- err
		case <-ctx.Done():
			return
		}
	}
}

// handleReplyAck 处理回复消息的回执
func (m *WsManager) handleReplyAck(reqID string, frame *WsFrame, ack *pendingAck) {
	// 取消超时
	if ack.timeout != nil {
		ack.timeout.Stop()
	}

	// 删除pending
	delete(m.pendingAcks, reqID)

	if frame.ErrCode != 0 {
		m.logger.Warn("Reply ack error", "req_id", reqID, "errcode", frame.ErrCode, "errmsg", frame.ErrMsg)
		ack.err <- fmt.Errorf("reply ack error: %s (code: %d)", frame.ErrMsg, frame.ErrCode)
	} else {
		m.logger.Debug("Reply ack received", "req_id", reqID)
		ack.result <- frame
	}
}

// onReplyAckTimeout 回复回执超时回调
func (m *WsManager) onReplyAckTimeout(reqID string, ack *pendingAck) {
	m.pendingAcksMu.Lock()
	delete(m.pendingAcks, reqID)
	m.pendingAcksMu.Unlock()

	m.logger.Warn("Reply ack timeout", "req_id", reqID, "timeout_sec", DefaultReplyAckTimeout)
	ack.err <- ErrReplyTimeout
}

// clearPendingMessages 清理所有待处理的消息和回执
func (m *WsManager) clearPendingMessages(reason string) {
	m.pendingAcksMu.Lock()
	for reqID, ack := range m.pendingAcks {
		if ack.timeout != nil {
			ack.timeout.Stop()
		}
		ack.err <- fmt.Errorf("%s, reply for req_id %s cancelled", reason, reqID)
	}
	m.pendingAcks = make(map[string]*pendingAck)
	m.pendingAcksMu.Unlock()

	m.replyQueuesMu.Lock()
	for reqID, queue := range m.replyQueues {
		for _, item := range queue {
			item.err <- fmt.Errorf("%s, reply for req_id %s cancelled", reason, reqID)
		}
	}
	m.replyQueues = make(map[string][]*replyQueueItem)
	m.replyQueuesMu.Unlock()
}

// Disconnect 断开连接
func (m *WsManager) Disconnect() {
	m.isManualClose = true
	m.stopHeartbeat()
	m.clearPendingMessages("Connection manually closed")

	close(m.stopChan)

	m.connMu.Lock()
	if m.conn != nil {
		// 发送关闭帧
		err := m.conn.WriteMessage(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, "Manual disconnect"),
		)
		if err != nil {
			m.logger.Debug("Failed to send close message", "error", err)
		}
		m.conn.Close()
		m.conn = nil
	}
	m.isConnected = false
	m.isAuthenticated = false
	m.connMu.Unlock()

	m.logger.Info("WebSocket connection manually closed")
}

// IsConnected 获取当前连接状态
func (m *WsManager) IsConnected() bool {
	m.connMu.Lock()
	defer m.connMu.Unlock()
	return m.isConnected
}

// IsAuthenticated 获取认证状态
func (m *WsManager) IsAuthenticated() bool {
	m.connMu.Lock()
	defer m.connMu.Unlock()
	return m.isAuthenticated
}

// SetOnConnected 设置连接建立回调
func (m *WsManager) SetOnConnected(f func()) {
	m.onConnected = f
}

// SetOnAuthenticated 设置认证成功回调
func (m *WsManager) SetOnAuthenticated(f func()) {
	m.onAuthenticated = f
}

// SetOnDisconnected 设置断开连接回调
func (m *WsManager) SetOnDisconnected(f func(reason string)) {
	m.onDisconnected = f
}

// SetOnReconnecting 设置重连回调
func (m *WsManager) SetOnReconnecting(f func(attempt int)) {
	m.onReconnecting = f
}

// SetOnError 设置错误回调
func (m *WsManager) SetOnError(f func(error)) {
	m.onError = f
}

// SetOnMessage 设置消息回调
func (m *WsManager) SetOnMessage(f func(frame *WsFrame)) {
	m.onMessage = f
}

// SetOnEvent 设置事件回调
func (m *WsManager) SetOnEvent(f func(frame *WsFrame)) {
	m.onEvent = f
}

// generateReqID 生成请求ID
func generateReqID(cmd string) string {
	return fmt.Sprintf("%s_%s", cmd, uuid.New().String()[:8])
}

// min 返回最小值
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}