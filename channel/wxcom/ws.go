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
	conn          *websocket.Conn
	connMu        sync.Mutex
	isConnected   bool
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

	// 正在处理的 reqID（防止重复通知 worker）
	processingReqIDs map[string]bool
	processingMu     sync.Mutex

	// Worker pool 控制
	replyNotifyCh     chan string // 无缓冲，阻塞发送保证可靠
	replyWorkerCtx    context.Context
	replyWorkerCancel context.CancelFunc

	// 待处理回执
	pendingAcks   map[string]*pendingAck
	pendingAcksMu sync.Mutex

	// 回调
	onConnected     func()
	onAuthenticated func()
	onDisconnected  func(reason string)
	onReconnecting  func(attempt int)
	onError         func(error)
	onMessage       func(frame *WsFrame)
	onEvent         func(frame *WsFrame)

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
	result  chan *WsFrame
	err     chan error
	timeout *time.Timer
	reqID   string
}

// NewWsManager 创建WebSocket管理器
func NewWsManager(config *WxComConfig, logger *slog.Logger) *WsManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &WsManager{
		config:            config,
		logger:            logger,
		replyQueues:       make(map[string][]*replyQueueItem),
		processingReqIDs:  make(map[string]bool),
		pendingAcks:       make(map[string]*pendingAck),
		replyNotifyCh:     make(chan string), // 无缓冲，阻塞发送
		replyWorkerCtx:    ctx,
		replyWorkerCancel: cancel,
		stopChan:          make(chan struct{}),
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

	// 停止回复 workers
	if m.replyWorkerCancel != nil {
		m.replyWorkerCancel()
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

	// 收到任何帧都重置心跳计数，证明连接活跃
	m.missedPongCount = 0

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
	if reqID != "" && len(reqID) > len(WsCmdSubscribe) && reqID[:len(WsCmdSubscribe)] == WsCmdSubscribe {
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

		// 启动回复 workers（默认 3 个）
		m.startReplyWorkers(3)

		if m.onAuthenticated != nil {
			m.onAuthenticated()
		}
		return
	}

	// 心跳响应
	if reqID != "" && len(reqID) > len(WsCmdHeartbeat) && reqID[:len(WsCmdHeartbeat)] == WsCmdHeartbeat {
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

// SendReply 通过WebSocket通道发送回复消息
// 同一个reqID的消息会被放入队列中串行发送，不同reqID由不同worker并行处理
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

	// 加入队列
	m.replyQueuesMu.Lock()
	queue := m.replyQueues[reqID]
	if len(queue) >= 100 {
		m.replyQueuesMu.Unlock()
		return nil, ErrQueueFull
	}
	m.replyQueues[reqID] = append(queue, item)
	m.replyQueuesMu.Unlock()

	// 检查是否正在处理中
	m.processingMu.Lock()
	isProcessing := m.processingReqIDs[reqID]
	if !isProcessing {
		m.processingReqIDs[reqID] = true // 标记正在处理
	}
	m.processingMu.Unlock()

	// 只有不在处理中才通知 worker（阻塞发送保证可靠）
	if !isProcessing {
		m.replyNotifyCh <- reqID
	}

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

// SendCommand 发送 WebSocket 命令并等待响应
// 用于上传素材等非回复类命令，不走 reply worker 队列
func (m *WsManager) SendCommand(ctx context.Context, cmd string, body map[string]interface{}, timeout time.Duration) (*WsFrame, error) {
	reqID := generateReqID(cmd)
	frame := &WsFrame{
		Cmd: cmd,
		Headers: map[string]string{
			"req_id": reqID,
		},
		Body: body,
	}

	// 发送帧
	if err := m.send(frame); err != nil {
		return nil, err
	}

	// 创建 ack channel
	ackResultCh := make(chan *WsFrame, 1)
	ackErrCh := make(chan error, 1)

	ack := &pendingAck{
		result: ackResultCh,
		err:    ackErrCh,
		reqID:  reqID,
	}
	ack.timeout = time.AfterFunc(timeout, func() {
		m.onReplyAckTimeout(reqID, ack)
	})

	m.pendingAcksMu.Lock()
	m.pendingAcks[reqID] = ack
	m.pendingAcksMu.Unlock()

	m.logger.Debug("Command sent", "cmd", cmd, "req_id", reqID)

	select {
	case result := <-ackResultCh:
		return result, nil
	case err := <-ackErrCh:
		return nil, err
	case <-ctx.Done():
		// 清理 ack
		m.pendingAcksMu.Lock()
		delete(m.pendingAcks, reqID)
		m.pendingAcksMu.Unlock()
		return nil, ctx.Err()
	}
}

// startReplyWorkers 启动多个回复 worker
func (m *WsManager) startReplyWorkers(count int) {
	// 创建新的 worker context（每次连接成功时重新创建）
	m.replyWorkerCtx, m.replyWorkerCancel = context.WithCancel(context.Background())

	for i := 0; i < count; i++ {
		go m.replyWorkerLoop(i)
	}
	m.logger.Info("Reply workers started", "count", count)
}

// replyWorkerLoop worker 处理循环
func (m *WsManager) replyWorkerLoop(workerID int) {
	for {
		select {
		case <-m.replyWorkerCtx.Done():
			m.logger.Debug("Reply worker stopped", "worker_id", workerID)
			return
		case reqID := <-m.replyNotifyCh:
			m.logger.Debug("Worker received task", "worker_id", workerID, "req_id", reqID)
			m.processReplyQueueForReqID(reqID)
			// 处理完后清除标记
			m.processingMu.Lock()
			delete(m.processingReqIDs, reqID)
			m.processingMu.Unlock()
		}
	}
}

// processReplyQueueForReqID 处理指定 reqID 的队列直到空
func (m *WsManager) processReplyQueueForReqID(reqID string) {
	for {
		m.replyQueuesMu.Lock()
		queue := m.replyQueues[reqID]
		if len(queue) == 0 {
			delete(m.replyQueues, reqID)
			m.replyQueuesMu.Unlock()
			return // 队列空，结束处理
		}

		item := queue[0]
		m.replyQueuesMu.Unlock()

		// 发送并等待回执
		result, err := m.sendAndWaitAck(item.frame, reqID)

		// 移除已处理的消息
		m.replyQueuesMu.Lock()
		if q := m.replyQueues[reqID]; len(q) > 0 {
			m.replyQueues[reqID] = q[1:]
		}
		m.replyQueuesMu.Unlock()

		// 返回结果给调用者
		if err != nil {
			item.err <- err
		} else {
			item.result <- result
		}
	}
}

// sendAndWaitAck 发送帧并等待回执
func (m *WsManager) sendAndWaitAck(frame *WsFrame, reqID string) (*WsFrame, error) {
	if err := m.send(frame); err != nil {
		return nil, err
	}

	m.logger.Debug("Reply message sent", "req_id", reqID)

	// 创建 ack channel
	ackResultCh := make(chan *WsFrame, 1)
	ackErrCh := make(chan error, 1)

	ack := &pendingAck{
		result: ackResultCh,
		err:    ackErrCh,
		reqID:  reqID,
	}
	ack.timeout = time.AfterFunc(time.Duration(DefaultReplyAckTimeout)*time.Second, func() {
		m.onReplyAckTimeout(reqID, ack)
	})

	m.pendingAcksMu.Lock()
	m.pendingAcks[reqID] = ack
	m.pendingAcksMu.Unlock()

	select {
	case result := <-ackResultCh:
		return result, nil
	case err := <-ackErrCh:
		return nil, err
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
		ack.err <- fmt.Errorf("%s, reply cancelled", reason)
		delete(m.pendingAcks, reqID)
	}
	m.pendingAcksMu.Unlock()

	m.replyQueuesMu.Lock()
	for reqID, queue := range m.replyQueues {
		for _, item := range queue {
			item.err <- fmt.Errorf("%s, reply for req_id %s cancelled", reason, reqID)
		}
		delete(m.replyQueues, reqID)
	}
	m.replyQueuesMu.Unlock()

	m.processingMu.Lock()
	m.processingReqIDs = make(map[string]bool)
	m.processingMu.Unlock()
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
