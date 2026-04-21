package ws

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/kinwyb/kanflux/bus"
	"github.com/kinwyb/kanflux/config"
	"github.com/kinwyb/kanflux/gateway/handler"
	"github.com/kinwyb/kanflux/gateway/types"
	"github.com/kinwyb/kanflux/scheduler"
	"github.com/kinwyb/kanflux/session"
)

// ServerConfig WebSocket 服务器配置
type ServerConfig struct {
	Enabled      bool   `json:"enabled"`       // 是否启用
	Port         int    `json:"port"`          // WebSocket 端口，默认 8765
	Host         string `json:"host"`          // 主机地址，默认 localhost
	Path         string `json:"path"`          // WebSocket 路径，默认 /ws
	AuthToken    string `json:"auth_token"`    // 认证 token（可选）
	ReadTimeout  int    `json:"read_timeout"`  // 读超时（秒），默认 60
	WriteTimeout int    `json:"write_timeout"` // 写超时（秒），默认 60
}

// SetDefaults 设置默认值
func (c *ServerConfig) SetDefaults() {
	if c.Port == 0 {
		c.Port = 8765
	}
	if c.Host == "" {
		c.Host = "localhost"
	}
	if c.Path == "" {
		c.Path = "/ws"
	}
	if c.ReadTimeout == 0 {
		c.ReadTimeout = 60
	}
	if c.WriteTimeout == 0 {
		c.WriteTimeout = 60
	}
}

// URL 返回 WebSocket URL
func (c *ServerConfig) URL() string {
	return fmt.Sprintf("ws://%s:%d%s", c.Host, c.Port, c.Path)
}

// Server WebSocket 服务器
type Server struct {
	config        *ServerConfig
	bus           *bus.MessageBus
	sessionMgr    *session.Manager
	taskScheduler *scheduler.Scheduler // 定时任务调度器
	configMgr     *config.Manager      // 配置管理器
	logger        *slog.Logger

	upgrader    websocket.Upgrader
	connections map[string]*Connection
	connMu      sync.RWMutex

	// 订阅管理
	subscriptions map[string]*SubscriptionInfo
	subMu         sync.RWMutex

	// bus 订阅
	outSub  *bus.OutboundSubscription
	chatSub *bus.ChatEventSubscription
	logSub  *bus.LogEventSubscription

	httpServer *http.Server

	ctx    context.Context
	cancel context.CancelFunc

	// shutdown callback
	shutdownCallback func()
	shutdownMu       sync.Mutex

	// 命令处理器
	commandHandlers map[string]handler.Handler
}

// SubscriptionInfo 订阅信息
type SubscriptionInfo struct {
	Channels map[string]bool // 订阅的 channel 类型
	ChatIDs  map[string]bool // 订阅的 chatID
}

// NewServer 创建 WebSocket 服务器
func NewServer(bus *bus.MessageBus, cfg *ServerConfig, sessionMgr *session.Manager) *Server {
	if cfg == nil {
		cfg = &ServerConfig{Enabled: true}
	}
	cfg.SetDefaults()

	ctx, cancel := context.WithCancel(context.Background())

	return &Server{
		config:     cfg,
		bus:        bus,
		sessionMgr: sessionMgr,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 4096,
			CheckOrigin: func(r *http.Request) bool {
				// 本地服务，允许所有 origin
				return true
			},
		},
		connections:     make(map[string]*Connection),
		subscriptions:   make(map[string]*SubscriptionInfo),
		commandHandlers: make(map[string]handler.Handler),
		logger:          slog.Default().With("component", "ws-server"),
		ctx:             ctx,
		cancel:          cancel,
	}
}

// RegisterCommandHandler 注册命令处理器
func (s *Server) RegisterCommandHandler(action string, h handler.Handler) {
	s.commandHandlers[action] = h
}

// Start 启动 WebSocket 服务器
func (s *Server) Start(ctx context.Context) error {
	if !s.config.Enabled {
		s.logger.Info("WebSocket server is disabled")
		return nil
	}

	// 订阅 MessageBus 事件
	s.subscribeBusEvents(ctx)

	// 启动 HTTP 服务器
	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)

	mux := http.NewServeMux()
	mux.HandleFunc(s.config.Path, s.handleWebSocket)

	s.httpServer = &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  time.Duration(s.config.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(s.config.WriteTimeout) * time.Second,
	}

	s.logger.Info("WebSocket server starting", "url", s.config.URL())

	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Error("WebSocket server error", "error", err)
		}
	}()

	return nil
}

// Stop 停止 WebSocket 服务器
func (s *Server) Stop() error {
	s.cancel()

	// 取消 bus 订阅
	if s.outSub != nil {
		s.outSub.Unsubscribe()
	}
	if s.chatSub != nil {
		s.chatSub.Unsubscribe()
	}
	if s.logSub != nil {
		s.logSub.Unsubscribe()
	}

	// 获取连接列表副本，避免在持有锁时调用 Close 导致死锁
	s.connMu.Lock()
	conns := make([]*Connection, 0, len(s.connections))
	for _, conn := range s.connections {
		conns = append(conns, conn)
	}
	s.connections = make(map[string]*Connection)
	s.connMu.Unlock()

	// 释放锁后再关闭连接
	for _, conn := range conns {
		conn.Close()
	}

	// 关闭 HTTP 服务器
	if s.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return s.httpServer.Shutdown(ctx)
	}

	return nil
}

// IsRunning 检查服务器是否运行
func (s *Server) IsRunning() bool {
	return s.httpServer != nil
}

// GetConfig 获取配置
func (s *Server) GetConfig() *ServerConfig {
	return s.config
}

// SetShutdownCallback 设置关闭回调函数
func (s *Server) SetShutdownCallback(callback func()) {
	s.shutdownMu.Lock()
	s.shutdownCallback = callback
	s.shutdownMu.Unlock()
}

// TriggerShutdown 触发关闭
func (s *Server) TriggerShutdown() {
	s.shutdownMu.Lock()
	callback := s.shutdownCallback
	s.shutdownMu.Unlock()

	if callback != nil {
		go callback()
	} else {
		// 如果没有设置回调，直接取消 context
		s.cancel()
	}
}

// GetStatus 获取服务状态
func (s *Server) GetStatus() map[string]interface{} {
	return map[string]interface{}{
		"running":          s.IsRunning(),
		"connection_count": s.ConnectionCount(),
		"url":              s.config.URL(),
	}
}

// handleWebSocket 处理 WebSocket 连接请求
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// 认证检查（可选）
	if s.config.AuthToken != "" {
		token := r.Header.Get("Authorization")
		if token != "Bearer "+s.config.AuthToken {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
	}

	// 升级为 WebSocket
	wsConn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Error("WebSocket upgrade failed", "error", err)
		return
	}

	// 创建连接对象
	conn := NewConnection(wsConn, s)

	// 注册连接
	s.connMu.Lock()
	s.connections[conn.ID()] = conn
	s.connMu.Unlock()

	// 初始化订阅信息（默认订阅所有）
	s.subMu.Lock()
	s.subscriptions[conn.ID()] = &SubscriptionInfo{
		Channels: make(map[string]bool),
		ChatIDs:  make(map[string]bool),
	}
	s.subMu.Unlock()

	s.logger.Info("WebSocket connection established", "conn_id", conn.ID())

	// 启动连接处理
	go conn.Handle(s.ctx)
}

// subscribeBusEvents 订阅 MessageBus 事件
func (s *Server) subscribeBusEvents(ctx context.Context) {
	// 订阅出站消息（订阅所有）
	s.outSub = s.bus.SubscribeOutboundFiltered(nil)

	// 订阅聊天事件（订阅所有）
	s.chatSub = s.bus.SubscribeChatEventFiltered(nil)

	// 订阅日志事件
	s.logSub = s.bus.SubscribeLogEvent()

	go s.dispatchOutbound(ctx)
	go s.dispatchChatEvents(ctx)
	go s.dispatchLogEvents(ctx)
}

// dispatchOutbound 分发出站消息到 WebSocket 客户端
func (s *Server) dispatchOutbound(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-s.outSub.Channel:
			if !ok {
				return
			}

			// 广播到订阅了该 channel/chatID 的连接
			s.broadcastOutbound(msg)
		}
	}
}

// dispatchChatEvents 分发聊天事件到 WebSocket 客户端
func (s *Server) dispatchChatEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-s.chatSub.Channel:
			if !ok {
				return
			}

			// 广播到订阅了该 channel/chatID 的连接
			s.broadcastChatEvent(event)
		}
	}
}

// dispatchLogEvents 分发日志事件到 WebSocket 客户端
func (s *Server) dispatchLogEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-s.logSub.Channel:
			if !ok {
				return
			}

			// 广播到所有连接
			s.broadcastLogEvent(event)
		}
	}
}

// broadcastOutbound 广播出站消息
func (s *Server) broadcastOutbound(msg *bus.OutboundMessage) {
	payload := ConvertOutboundToPayload(&OutboundMessage{
		ID:               msg.ID,
		Channel:          msg.Channel,
		ChatID:           msg.ChatID,
		Content:          msg.Content,
		ReasoningContent: msg.ReasoningContent,
		Media:            convertBusMediaToPayload(msg.Media),
		ReplyTo:          msg.ReplyTo,
		IsStreaming:      msg.IsStreaming,
		IsThinking:       msg.IsThinking,
		IsFinal:          msg.IsFinal,
		ChunkIndex:       msg.ChunkIndex,
		Error:            msg.Error,
		Metadata:         msg.Metadata,
	})

	wsMsg, err := NewWSMessage(MsgTypeOutbound, uuid.New().String(), payload)
	if err != nil {
		s.logger.Error("Failed to create outbound message", "error", err)
		return
	}

	msgBytes, err := wsMsg.Marshal()
	if err != nil {
		s.logger.Error("Failed to marshal outbound message", "error", err)
		return
	}

	// 广播到订阅了该 channel/chatID 的连接
	s.broadcastToSubscribers(msg.Channel, msg.ChatID, msgBytes)
}

// ConvertToolEventInfo 将 bus.ToolEventInfo 转换为 ws.ToolEventInfo
func ConvertToolEventInfo(info *bus.ToolEventInfo) *ToolEventInfo {
	if info == nil {
		return nil
	}
	return &ToolEventInfo{
		Name:      info.Name,
		ID:        info.ID,
		Arguments: info.Arguments,
		Result:    info.Result,
		IsStart:   info.IsStart,
	}
}

// broadcastChatEvent 广播聊天事件
func (s *Server) broadcastChatEvent(event *bus.ChatEvent) {
	metadata := make(map[string]interface{})
	if event.Metadata != nil {
		if m, ok := event.Metadata.(map[string]interface{}); ok {
			metadata = m
		}
	}

	payload := ConvertChatEventToPayload(&ChatEvent{
		ID:        event.ID,
		Channel:   event.Channel,
		ChatID:    event.ChatID,
		ReplyTo:   event.ReplyTo,
		Seq:       event.Seq,
		AgentName: event.AgentName,
		State:     event.State,
		Error:     event.Error,
		ToolInfo:  ConvertToolEventInfo(event.ToolInfo),
		Metadata:  metadata,
	})

	wsMsg, err := NewWSMessage(MsgTypeChatEvent, uuid.New().String(), payload)
	if err != nil {
		s.logger.Error("Failed to create chat event message", "error", err)
		return
	}

	msgBytes, err := wsMsg.Marshal()
	if err != nil {
		s.logger.Error("Failed to marshal chat event message", "error", err)
		return
	}

	// 广播到订阅了该 channel/chatID 的连接
	s.broadcastToSubscribers(event.Channel, event.ChatID, msgBytes)
}

// broadcastLogEvent 广播日志事件
func (s *Server) broadcastLogEvent(event *bus.LogEvent) {
	payload := ConvertLogEventToPayload(&LogEvent{
		ID:        event.ID,
		Level:     event.Level,
		Message:   event.Message,
		Source:    event.Source,
		Timestamp: event.Timestamp,
	})

	wsMsg, err := NewWSMessage(MsgTypeLogEvent, uuid.New().String(), payload)
	if err != nil {
		s.logger.Error("Failed to create log event message", "error", err)
		return
	}

	msgBytes, err := wsMsg.Marshal()
	if err != nil {
		s.logger.Error("Failed to marshal log event message", "error", err)
		return
	}

	// 广播到所有连接
	s.broadcastToAll(msgBytes)
}

// broadcastToSubscribers 广播到订阅了特定 channel/chatID 的连接
func (s *Server) broadcastToSubscribers(channel, chatID string, msgBytes []byte) {
	s.connMu.RLock()
	defer s.connMu.RUnlock()

	for connID, conn := range s.connections {
		if s.matchSubscription(connID, channel, chatID) {
			conn.Send(msgBytes)
		}
	}
}

// broadcastToAll 广播到所有连接
func (s *Server) broadcastToAll(msgBytes []byte) {
	s.connMu.RLock()
	defer s.connMu.RUnlock()

	for _, conn := range s.connections {
		conn.Send(msgBytes)
	}
}

// matchSubscription 检查消息是否匹配连接的订阅
func (s *Server) matchSubscription(connID, channel, chatID string) bool {
	s.subMu.RLock()
	sub, ok := s.subscriptions[connID]
	s.subMu.RUnlock()

	if !ok {
		return false
	}

	// 空订阅表示订阅所有
	if len(sub.Channels) == 0 && len(sub.ChatIDs) == 0 {
		return true
	}

	// 检查 channel 和 chatID
	channelMatch := len(sub.Channels) == 0 || sub.Channels[channel]
	chatIDMatch := len(sub.ChatIDs) == 0 || sub.ChatIDs[chatID]

	return channelMatch && chatIDMatch
}

// PublishInbound 发布入站消息到 MessageBus
func (s *Server) PublishInbound(ctx context.Context, msg *InboundMessage) error {
	// 转换为 bus.InboundMessage
	busMsg := &bus.InboundMessage{
		ID:            msg.ID,
		Channel:       msg.Channel,
		AccountID:     msg.AccountID,
		SenderID:      msg.SenderID,
		ChatID:        msg.ChatID,
		Content:       msg.Content,
		StreamingMode: msg.StreamingMode,
		Media:         convertPayloadToBusMedia(msg.Media),
		Metadata:      msg.Metadata,
		Timestamp:     msg.Timestamp,
	}
	return s.bus.PublishInbound(ctx, busMsg)
}

// UpdateSubscription 更新连接的订阅
func (s *Server) UpdateSubscription(connID string, channels, chatIDs []string) {
	s.subMu.Lock()
	sub, ok := s.subscriptions[connID]
	if !ok {
		sub = &SubscriptionInfo{
			Channels: make(map[string]bool),
			ChatIDs:  make(map[string]bool),
		}
		s.subscriptions[connID] = sub
	}
	s.subMu.Unlock()

	// 更新订阅
	for _, ch := range channels {
		sub.Channels[ch] = true
	}
	for _, chatID := range chatIDs {
		sub.ChatIDs[chatID] = true
	}

	s.logger.Debug("Subscription updated", "conn_id", connID, "channels", channels, "chat_ids", chatIDs)
}

// RemoveConnection 移除连接
func (s *Server) RemoveConnection(connID string) {
	s.connMu.Lock()
	delete(s.connections, connID)
	s.connMu.Unlock()

	s.subMu.Lock()
	delete(s.subscriptions, connID)
	s.subMu.Unlock()

	s.logger.Info("WebSocket connection removed", "conn_id", connID)
}

// ConnectionCount 获取连接数量
func (s *Server) ConnectionCount() int {
	s.connMu.RLock()
	defer s.connMu.RUnlock()
	return len(s.connections)
}

// GetCommandHandler 获取命令处理器
func (s *Server) GetCommandHandler(action string) handler.Handler {
	// 检查自定义命令处理器
	if h, ok := s.commandHandlers[action]; ok {
		return h
	}
	// 返回默认 handler registry 中的处理器
	return handler.Get(types.MsgTypeControl)
}

// Context returns the server's context (implements handler.Server interface)
func (s *Server) Context() context.Context {
	return s.ctx
}

// GetSessionManager returns the session manager (implements handler.Server interface)
func (s *Server) GetSessionManager() *session.Manager {
	return s.sessionMgr
}

// SetTaskScheduler 设置定时任务调度器
func (s *Server) SetTaskScheduler(ts *scheduler.Scheduler) {
	s.taskScheduler = ts
}

// GetTaskScheduler 获取定时任务调度器
func (s *Server) GetTaskScheduler() *scheduler.Scheduler {
	return s.taskScheduler
}

// SetConfigManager 设置配置管理器
func (s *Server) SetConfigManager(cm *config.Manager) {
	s.configMgr = cm
}

// GetConfigManager 获取配置管理器
func (s *Server) GetConfigManager() *config.Manager {
	return s.configMgr
}

// HandleTaskList 处理任务列表请求
func (s *Server) HandleTaskList(connID string, msgID string) error {
	if s.taskScheduler == nil {
		return s.sendTaskError(connID, msgID, MsgTypeTaskListAck, "task scheduler not available")
	}

	details := s.taskScheduler.ListTaskDetails()
	tasks := make([]*TaskDetailPayload, 0, len(details))
	for _, detail := range details {
		tasks = append(tasks, convertTaskDetailToPayload(detail))
	}

	ack := &TaskListAckPayload{
		Success: true,
		Tasks:   tasks,
	}

	return s.sendTaskResponse(connID, msgID, MsgTypeTaskListAck, ack)
}

// HandleTaskAdd 处理添加任务请求
func (s *Server) HandleTaskAdd(connID string, msgID string, payload *TaskAddPayload) error {
	if s.taskScheduler == nil {
		return s.sendTaskError(connID, msgID, MsgTypeTaskAddAck, "task scheduler not available")
	}

	taskConfig := &config.TaskConfig{
		ID:          payload.ID,
		Name:        payload.Name,
		Description: payload.Description,
		Enabled:     payload.Enabled,
		Schedule:    config.ScheduleConfig{Cron: payload.Schedule.Cron},
		Target: config.TargetConfig{
			Channel:   payload.Target.Channel,
			AccountID: payload.Target.AccountID,
			ChatID:    payload.Target.ChatID,
			AgentName: payload.Target.AgentName,
		},
		Content: config.ContentConfig{Prompt: payload.Content.Prompt},
	}

	if err := s.taskScheduler.AddTask(taskConfig); err != nil {
		return s.sendTaskError(connID, msgID, MsgTypeTaskAddAck, err.Error())
	}

	ack := &TaskAddAckPayload{
		Success: true,
		ID:      payload.ID,
	}

	return s.sendTaskResponse(connID, msgID, MsgTypeTaskAddAck, ack)
}

// HandleTaskUpdate 处理更新任务请求
func (s *Server) HandleTaskUpdate(connID string, msgID string, payload *TaskUpdatePayload) error {
	if s.taskScheduler == nil {
		return s.sendTaskError(connID, msgID, MsgTypeTaskUpdateAck, "task scheduler not available")
	}

	// 获取现有任务配置
	taskConfigs := s.taskScheduler.ListTasks()
	var existing *config.TaskConfig
	for _, tc := range taskConfigs {
		if tc.ID == payload.ID {
			existing = tc
			break
		}
	}

	if existing == nil {
		return s.sendTaskError(connID, msgID, MsgTypeTaskUpdateAck, fmt.Sprintf("task '%s' not found", payload.ID))
	}

	// 更新配置（只更新提供的字段）
	if payload.Name != "" {
		existing.Name = payload.Name
	}
	if payload.Description != "" {
		existing.Description = payload.Description
	}
	if payload.Schedule != nil {
		existing.Schedule.Cron = payload.Schedule.Cron
	}
	if payload.Target != nil {
		existing.Target.Channel = payload.Target.Channel
		existing.Target.AccountID = payload.Target.AccountID
		existing.Target.ChatID = payload.Target.ChatID
		existing.Target.AgentName = payload.Target.AgentName
	}
	if payload.Content != nil {
		existing.Content.Prompt = payload.Content.Prompt
	}

	if err := s.taskScheduler.UpdateTask(existing); err != nil {
		return s.sendTaskError(connID, msgID, MsgTypeTaskUpdateAck, err.Error())
	}

	ack := &TaskUpdateAckPayload{
		Success: true,
		ID:      payload.ID,
	}

	return s.sendTaskResponse(connID, msgID, MsgTypeTaskUpdateAck, ack)
}

// HandleTaskRemove 处理删除任务请求
func (s *Server) HandleTaskRemove(connID string, msgID string, payload *TaskRemovePayload) error {
	if s.taskScheduler == nil {
		return s.sendTaskError(connID, msgID, MsgTypeTaskRemoveAck, "task scheduler not available")
	}

	if err := s.taskScheduler.RemoveTask(payload.ID); err != nil {
		return s.sendTaskError(connID, msgID, MsgTypeTaskRemoveAck, err.Error())
	}

	ack := &TaskRemoveAckPayload{
		Success: true,
		ID:      payload.ID,
	}

	return s.sendTaskResponse(connID, msgID, MsgTypeTaskRemoveAck, ack)
}

// HandleTaskTrigger 处理触发任务请求
func (s *Server) HandleTaskTrigger(connID string, msgID string, payload *TaskTriggerPayload) error {
	if s.taskScheduler == nil {
		return s.sendTaskError(connID, msgID, MsgTypeTaskTriggerAck, "task scheduler not available")
	}

	if err := s.taskScheduler.TriggerTask(payload.ID); err != nil {
		return s.sendTaskError(connID, msgID, MsgTypeTaskTriggerAck, err.Error())
	}

	ack := &TaskTriggerAckPayload{
		Success: true,
		ID:      payload.ID,
	}

	return s.sendTaskResponse(connID, msgID, MsgTypeTaskTriggerAck, ack)
}

// HandleTaskStatus 处理获取任务状态请求
func (s *Server) HandleTaskStatus(connID string, msgID string, payload *TaskStatusPayload) error {
	if s.taskScheduler == nil {
		return s.sendTaskError(connID, msgID, MsgTypeTaskStatusAck, "task scheduler not available")
	}

	state, err := s.taskScheduler.GetTaskStatus(payload.ID)
	if err != nil {
		return s.sendTaskError(connID, msgID, MsgTypeTaskStatusAck, err.Error())
	}

	ack := &TaskStatusAckPayload{
		Success: true,
		ID:      payload.ID,
		State:   convertTaskStateToPayload(state),
	}

	return s.sendTaskResponse(connID, msgID, MsgTypeTaskStatusAck, ack)
}

// sendTaskResponse 发送任务响应
func (s *Server) sendTaskResponse(connID string, msgID string, msgType MessageType, payload interface{}) error {
	s.connMu.RLock()
	conn, ok := s.connections[connID]
	s.connMu.RUnlock()

	if !ok {
		return fmt.Errorf("connection %s not found", connID)
	}

	wsMsg, err := NewWSMessage(msgType, msgID, payload)
	if err != nil {
		return err
	}

	msgBytes, err := wsMsg.Marshal()
	if err != nil {
		return err
	}

	conn.Send(msgBytes)
	return nil
}

// sendTaskError 发送任务错误响应
func (s *Server) sendTaskError(connID string, msgID string, msgType MessageType, errMsg string) error {
	switch msgType {
	case MsgTypeTaskListAck:
		return s.sendTaskResponse(connID, msgID, msgType, &TaskListAckPayload{Success: false, Error: errMsg})
	case MsgTypeTaskAddAck:
		return s.sendTaskResponse(connID, msgID, msgType, &TaskAddAckPayload{Success: false, Error: errMsg})
	case MsgTypeTaskUpdateAck:
		return s.sendTaskResponse(connID, msgID, msgType, &TaskUpdateAckPayload{Success: false, Error: errMsg})
	case MsgTypeTaskRemoveAck:
		return s.sendTaskResponse(connID, msgID, msgType, &TaskRemoveAckPayload{Success: false, Error: errMsg})
	case MsgTypeTaskTriggerAck:
		return s.sendTaskResponse(connID, msgID, msgType, &TaskTriggerAckPayload{Success: false, Error: errMsg})
	case MsgTypeTaskStatusAck:
		return s.sendTaskResponse(connID, msgID, msgType, &TaskStatusAckPayload{Success: false, Error: errMsg})
	default:
		return s.sendTaskResponse(connID, msgID, MsgTypeError, &ErrorPayload{Message: errMsg})
	}
}

// 内部转换函数

func convertBusMediaToPayload(media []bus.Media) []Media {
	if media == nil {
		return nil
	}
	result := make([]Media, len(media))
	for i, m := range media {
		result[i] = Media{
			Type:     m.Type,
			URL:      m.URL,
			Base64:   m.Base64,
			MimeType: m.MimeType,
			Metadata: m.Metadata,
		}
	}
	return result
}

func convertPayloadToBusMedia(media []Media) []bus.Media {
	if media == nil {
		return nil
	}
	result := make([]bus.Media, len(media))
	for i, m := range media {
		result[i] = bus.Media{
			Type:     m.Type,
			URL:      m.URL,
			Base64:   m.Base64,
			MimeType: m.MimeType,
			Metadata: m.Metadata,
		}
	}
	return result
}

// 定时任务相关转换函数

func convertTaskDetailToPayload(detail scheduler.TaskDetail) *TaskDetailPayload {
	payload := &TaskDetailPayload{
		ID:          detail.Config.ID,
		Name:        detail.Config.Name,
		Description: detail.Config.Description,
		Enabled:     detail.Config.Enabled,
		Schedule:    SchedulePayload{Cron: detail.Config.Schedule.Cron},
		Target: TargetPayload{
			Channel:   detail.Config.Target.Channel,
			AccountID: detail.Config.Target.AccountID,
			ChatID:    detail.Config.Target.ChatID,
			AgentName: detail.Config.Target.AgentName,
		},
		Content:   ContentPayload{Prompt: detail.Config.Content.Prompt},
		NextRun:   detail.NextRun.UnixMilli(),
		LastRun:   detail.LastRun.UnixMilli(),
		IsRunning: detail.IsRunning,
	}

	if detail.State != nil {
		payload.State = convertTaskStateToPayload(detail.State)
	}

	return payload
}

func convertTaskStateToPayload(state *scheduler.TaskState) *TaskStatePayload {
	if state == nil {
		return nil
	}
	return &TaskStatePayload{
		LastRun:      state.LastRun.UnixMilli(),
		LastResult:   state.LastResult,
		LastError:    state.LastError,
		SuccessCount: state.SuccessCount,
		FailCount:    state.FailCount,
		NextRun:      state.NextRun.UnixMilli(),
	}
}

// ========== 配置管理相关方法 ==========

// HandleConfigGet 处理获取配置请求
func (s *Server) HandleConfigGet(connID string, msgID string) error {
	if s.configMgr == nil {
		return s.sendConfigError(connID, msgID, MsgTypeConfigGetAck, "config manager not available")
	}

	configJSON, err := s.configMgr.GetConfigJSON()
	if err != nil {
		return s.sendConfigError(connID, msgID, MsgTypeConfigGetAck, err.Error())
	}

	ack := &ConfigGetAckPayload{
		Success: true,
		Config:  configJSON,
	}

	return s.sendConfigResponse(connID, msgID, MsgTypeConfigGetAck, ack)
}

// HandleConfigUpdate 处理更新配置请求
func (s *Server) HandleConfigUpdate(connID string, msgID string, payload *ConfigUpdatePayload) error {
	if s.configMgr == nil {
		return s.sendConfigError(connID, msgID, MsgTypeConfigUpdateAck, "config manager not available")
	}

	if err := s.configMgr.UpdateConfig(payload.Config); err != nil {
		return s.sendConfigError(connID, msgID, MsgTypeConfigUpdateAck, err.Error())
	}

	ack := &ConfigUpdateAckPayload{
		Success: true,
		Message: "Configuration updated successfully",
	}

	return s.sendConfigResponse(connID, msgID, MsgTypeConfigUpdateAck, ack)
}

// sendConfigResponse 发送配置响应
func (s *Server) sendConfigResponse(connID string, msgID string, msgType MessageType, payload interface{}) error {
	s.connMu.RLock()
	conn, ok := s.connections[connID]
	s.connMu.RUnlock()

	if !ok {
		return fmt.Errorf("connection %s not found", connID)
	}

	wsMsg, err := NewWSMessage(msgType, msgID, payload)
	if err != nil {
		return err
	}

	msgBytes, err := wsMsg.Marshal()
	if err != nil {
		return err
	}

	conn.Send(msgBytes)
	return nil
}

// sendConfigError 发送配置错误响应
func (s *Server) sendConfigError(connID string, msgID string, msgType MessageType, errMsg string) error {
	switch msgType {
	case MsgTypeConfigGetAck:
		return s.sendConfigResponse(connID, msgID, msgType, &ConfigGetAckPayload{Success: false, Error: errMsg})
	case MsgTypeConfigUpdateAck:
		return s.sendConfigResponse(connID, msgID, msgType, &ConfigUpdateAckPayload{Success: false, Error: errMsg})
	default:
		return s.sendConfigResponse(connID, msgID, MsgTypeError, &ErrorPayload{Message: errMsg})
	}
}
