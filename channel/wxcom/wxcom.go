// Package wxcom provides enterprise WeChat (企业微信) intelligent robot channel implementation.
// Based on WebSocket long connection for message sending/receiving, streaming replies, template cards, etc.
package wxcom

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/kinwyb/kanflux/bus"
	"github.com/kinwyb/kanflux/channel"
)

// WxComChannel 企业微信智能机器人Channel实现
type WxComChannel struct {
	*channel.ChannelBase

	// WebSocket管理器
	wsManager *WsManager

	// 消息处理器
	handler *MessageHandler

	// 配置
	config *WxComConfig

	// 流式消息管理
	streamIDs   map[string]string // chatID -> streamID
	streamIDsMu sync.Mutex

	// logger
	logger *slog.Logger

	// 运行状态
	running bool
	runningMu sync.Mutex
}

// NewWxComChannel 创建企业微信Channel
func NewWxComChannel(msgBus *bus.MessageBus, cfg *WxComConfig) (*WxComChannel, error) {
	return NewWxComChannelWithAccount(msgBus, cfg, "")
}

// NewWxComChannelWithAccount 创建企业微信Channel（带账号标识）
// accountID 用于多账号场景，生成唯一 channel 名称：wxCom:accountID
func NewWxComChannelWithAccount(msgBus *bus.MessageBus, cfg *WxComConfig, accountID string) (*WxComChannel, error) {
	// 设置默认值
	cfg.SetDefaults()

	// 验证配置
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	// 生成唯一的 channel 名称
	channelName := bus.ChannelWxCom
	if accountID != "" {
		channelName = bus.ChannelWxCom + ":" + accountID
	}

	logger := slog.Default().With("channel", channelName, "bot_id", cfg.BotID)

	// 创建基础配置
	baseConfig := channel.BaseChannelConfig{
		Enabled:    cfg.Enabled,
		AccountID:  cfg.BotID,
		Name:       channelName,
		AllowedIDs: nil, // 企业微信通过后台配置权限
	}

	// 创建基础实现
	base := channel.NewChannelBase(channelName, cfg.BotID, baseConfig, msgBus)

	// 创建WebSocket管理器
	wsManager := NewWsManager(cfg, logger)

	// 创建消息处理器
	handler := NewMessageHandler(logger)

	return &WxComChannel{
		ChannelBase: base,
		wsManager:   wsManager,
		handler:     handler,
		config:      cfg,
		streamIDs:   make(map[string]string),
		logger:      logger,
	}, nil
}

// Start 启动Channel
func (c *WxComChannel) Start(ctx context.Context) error {
	if !c.config.Enabled {
		c.logger.Info("WxCom channel is disabled")
		return nil
	}

	c.runningMu.Lock()
	if c.running {
		c.runningMu.Unlock()
		return nil
	}
	c.running = true
	c.runningMu.Unlock()

	c.logger.Info("Starting WxCom channel")

	// 设置WebSocket回调
	c.setupCallbacks()

	// 建立WebSocket连接
	if err := c.wsManager.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect WebSocket: %w", err)
	}

	// 调用基础实现的Start
	c.ChannelBase.Start(ctx)

	c.logger.Info("WxCom channel started")

	return nil
}

// setupCallbacks 设置WebSocket回调
func (c *WxComChannel) setupCallbacks() {
	c.wsManager.SetOnConnected(func() {
		c.logger.Info("WebSocket connected")
	})

	c.wsManager.SetOnAuthenticated(func() {
		c.logger.Info("WebSocket authenticated")
	})

	c.wsManager.SetOnDisconnected(func(reason string) {
		c.logger.Warn("WebSocket disconnected", "reason", reason)
	})

	c.wsManager.SetOnReconnecting(func(attempt int) {
		c.logger.Info("WebSocket reconnecting", "attempt", attempt)
	})

	c.wsManager.SetOnError(func(err error) {
		c.logger.Error("WebSocket error", "error", err)
	})

	// 消息回调
	c.wsManager.SetOnMessage(func(frame *WsFrame) {
		c.handleMessage(frame)
	})

	// 事件回调
	c.wsManager.SetOnEvent(func(frame *WsFrame) {
		c.handleEvent(frame)
	})
}

// handleMessage 处理消息回调
func (c *WxComChannel) handleMessage(frame *WsFrame) {
	msg, err := c.handler.ParseInboundMessage(frame)
	if err != nil {
		c.logger.Error("Failed to parse inbound message", "error", err)
		return
	}

	// 转换为bus消息
	inbound := c.handler.ConvertToInbound(msg, bus.ChannelWxCom, c.config.BotID)

	// 发布入站消息
	if err := c.PublishInbound(context.Background(), inbound); err != nil {
		c.logger.Error("Failed to publish inbound message", "error", err)
	}
}

// handleEvent 处理事件回调
func (c *WxComChannel) handleEvent(frame *WsFrame) {
	event, err := c.handler.ParseEvent(frame)
	if err != nil {
		c.logger.Error("Failed to parse event", "error", err)
		return
	}

	c.logger.Debug("Received event", "event_type", event.EventType, "user_id", event.UserID)

	// 处理进入会话事件
	if event.EventType == EventTypeEnterChat {
		// 发送欢迎语 (可选)
		// 需要在5秒内回复，这里不自动回复，由Agent处理
		inbound := &bus.InboundMessage{
			Channel:   bus.ChannelWxCom,
			AccountID: c.config.BotID,
			SenderID:  event.UserID,
			ChatID:    event.UserID, // 单聊场景，chatID = userID
			Content:   "", // 空内容表示进入会话事件
			Timestamp: event.EventTime,
			Metadata: map[string]interface{}{
				"event_type": EventTypeEnterChat,
				"req_id":     frame.Headers["req_id"],
			},
		}
		c.PublishInbound(context.Background(), inbound)
	}

	// 处理模板卡片事件
	if event.EventType == EventTypeTemplateCardEvent {
		inbound := &bus.InboundMessage{
			Channel:   bus.ChannelWxCom,
			AccountID: c.config.BotID,
			SenderID:  event.UserID,
			ChatID:    event.ChatID,
			Content:   fmt.Sprintf("模板卡片事件: %s", event.EventKey),
			Timestamp: event.EventTime,
			Metadata: map[string]interface{}{
				"event_type": EventTypeTemplateCardEvent,
				"event_key":  event.EventKey,
				"task_id":    event.TaskID,
				"req_id":     frame.Headers["req_id"],
			},
		}
		c.PublishInbound(context.Background(), inbound)
	}

	// 处理反馈事件
	if event.EventType == EventTypeFeedbackEvent {
		inbound := &bus.InboundMessage{
			Channel:   bus.ChannelWxCom,
			AccountID: c.config.BotID,
			SenderID:  event.UserID,
			ChatID:    event.ChatID,
			Content:   "用户反馈事件",
			Timestamp: event.EventTime,
			Metadata: map[string]interface{}{
				"event_type": EventTypeFeedbackEvent,
				"req_id":     frame.Headers["req_id"],
			},
		}
		c.PublishInbound(context.Background(), inbound)
	}
}

// Stop 停止Channel
func (c *WxComChannel) Stop() error {
	c.runningMu.Lock()
	if !c.running {
		c.runningMu.Unlock()
		return nil
	}
	c.running = false
	c.runningMu.Unlock()

	c.logger.Info("Stopping WxCom channel")

	// 断开WebSocket连接
	c.wsManager.Disconnect()

	// 调用基础实现的Stop
	c.ChannelBase.Stop()

	c.logger.Info("WxCom channel stopped")

	return nil
}

// IsRunning 是否运行中
func (c *WxComChannel) IsRunning() bool {
	c.runningMu.Lock()
	defer c.runningMu.Unlock()
	return c.running && c.wsManager.IsConnected()
}

// Send 发送完整消息
func (c *WxComChannel) Send(ctx context.Context, msg *bus.OutboundMessage) error {
	if !c.IsRunning() {
		return nil
	}

	// 获取或创建streamID
	streamID := c.getOrCreateStreamID(msg.ChatID)

	// 构建回复消息
	body := c.handler.ConvertOutboundToReply(msg, "", streamID, true)

	// 从metadata获取req_id (如果是从入站消息回复)
	reqID := ""
	if msg.Metadata != nil {
		if id, ok := msg.Metadata["req_id"].(string); ok && id != "" {
			reqID = id
		}
	}

	// 如果没有req_id，生成一个用于主动发送
	if reqID == "" {
		reqID = generateReqID(WsCmdSendMsg)
		body = c.handler.BuildSendMessage(msg.ChatID, body)
	}

	// 发送回复
	_, err := c.wsManager.SendReply(ctx, reqID, body, WsCmdResponse)
	if err != nil {
		c.logger.Error("Failed to send message", "error", err)
		return err
	}

	// 清理streamID
	c.clearStreamID(msg.ChatID)

	return nil
}

// SendStream 发送流式消息
func (c *WxComChannel) SendStream(ctx context.Context, chatID string, stream <-chan *bus.StreamMessage) error {
	if !c.IsRunning() {
		return nil
	}

	streamID := c.getOrCreateStreamID(chatID)
	reqID := "" // 需要从之前的入站消息获取

	for {
		select {
		case msg, ok := <-stream:
			if !ok {
				// 流结束，发送finish消息
				body := c.handler.BuildStreamReply(streamID, "", true, nil, nil)
				// 尝试发送finish
				c.wsManager.SendReply(ctx, reqID, body, WsCmdResponse)
				c.clearStreamID(chatID)
				return nil
			}

			if msg.Error != "" {
				c.logger.Error("Stream error", "error", msg.Error)
				return fmt.Errorf("stream error: %s", msg.Error)
			}

			// 发送流式消息
			finish := msg.IsFinal
			body := c.handler.BuildStreamReply(streamID, msg.Content, finish, nil, nil)

			if reqID != "" {
				_, err := c.wsManager.SendReply(ctx, reqID, body, WsCmdResponse)
				if err != nil {
					c.logger.Error("Failed to send stream message", "error", err)
					return err
				}
			}

			if finish {
				c.clearStreamID(chatID)
				return nil
			}

		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// HandleChatEvent 处理聊天事件
func (c *WxComChannel) HandleChatEvent(ctx context.Context, event *bus.ChatEvent) error {
	if !c.IsRunning() {
		return nil
	}

	// 只处理当前chatID的事件
	streamID := c.getOrCreateStreamID(event.ChatID)

	// 从metadata获取req_id
	reqID := ""
	if event.Metadata != nil {
		if m, ok := event.Metadata.(map[string]interface{}); ok {
			if id, ok := m["req_id"].(string); ok && id != "" {
				reqID = id
			}
		}
	}

	var body map[string]interface{}

	switch event.State {
	case bus.ChatEventStateDelta:
		// 增量文本
		body = c.handler.BuildStreamReply(streamID, event.Content, false, nil, nil)

	case bus.ChatEventStateThinking:
		// 思考过程 (企业微信不支持thinking类型，暂作为普通文本处理)
		body = c.handler.BuildStreamReply(streamID, event.Content, false, nil, nil)

	case bus.ChatEventStateFinal:
		// 最终完成
		body = c.handler.BuildStreamReply(streamID, event.Message, true, nil, nil)
		c.clearStreamID(event.ChatID)

	case bus.ChatEventStateError:
		// 错误
		c.logger.Error("Chat event error", "error", event.Error)
		return nil

	case bus.ChatEventStateTool:
		// 工具调用 (暂不处理)
		return nil

	default:
		return nil
	}

	if reqID != "" && body != nil {
		_, err := c.wsManager.SendReply(ctx, reqID, body, WsCmdResponse)
		if err != nil {
			c.logger.Error("Failed to handle chat event", "error", err)
			return err
		}
	}

	return nil
}

// IsAllowed 检查发送者是否允许
func (c *WxComChannel) IsAllowed(senderID string) bool {
	// 企业微信通过后台配置权限，这里始终返回true
	return c.config.Enabled
}

// getOrCreateStreamID 获取或创建流式消息ID
func (c *WxComChannel) getOrCreateStreamID(chatID string) string {
	c.streamIDsMu.Lock()
	defer c.streamIDsMu.Unlock()

	if id, ok := c.streamIDs[chatID]; ok {
		return id
	}

	// 创建新的streamID
	id := fmt.Sprintf("stream_%s", uuid.New().String()[:8])
	c.streamIDs[chatID] = id
	return id
}

// clearStreamID 清理流式消息ID
func (c *WxComChannel) clearStreamID(chatID string) {
	c.streamIDsMu.Lock()
	delete(c.streamIDs, chatID)
	c.streamIDsMu.Unlock()
}

// ReplyWelcome 回复欢迎语 (需在收到enter_chat事件5秒内调用)
func (c *WxComChannel) ReplyWelcome(ctx context.Context, reqID string, content string) error {
	if !c.IsRunning() {
		return ErrNotConnected
	}

	body := c.handler.BuildTextReply(content)
	_, err := c.wsManager.SendReply(ctx, reqID, body, WsCmdResponseWelcome)
	return err
}

// ReplyMarkdown 回复Markdown消息
func (c *WxComChannel) ReplyMarkdown(ctx context.Context, reqID string, content string) error {
	if !c.IsRunning() {
		return ErrNotConnected
	}

	body := c.handler.BuildMarkdownReply(content)
	_, err := c.wsManager.SendReply(ctx, reqID, body, WsCmdResponse)
	return err
}

// SendMessage 主动发送消息
func (c *WxComChannel) SendMessage(ctx context.Context, chatID string, content string) error {
	if !c.IsRunning() {
		return ErrNotConnected
	}

	reqID := generateReqID(WsCmdSendMsg)
	body := c.handler.BuildSendMessage(chatID, c.handler.BuildMarkdownReply(content))
	_, err := c.wsManager.SendReply(ctx, reqID, body, WsCmdSendMsg)
	return err
}

// DownloadFile 下载并解密文件
// url: 文件下载地址
// aesKey: Base64编码的AES密钥 (来自消息中的 aeskey)
// 返回: 解密后的文件数据, 文件名, 错误
func (c *WxComChannel) DownloadFile(ctx context.Context, fileURL, aesKey string) ([]byte, string, error) {
	if !c.IsRunning() {
		return nil, "", ErrNotConnected
	}

	timeout := time.Duration(c.config.RequestTimeout) * time.Millisecond
	downloader := NewFileDownloader(timeout)

	return downloader.DownloadFile(ctx, fileURL, aesKey)
}

// DownloadImage 下载并解密图片
// 从 bus.Media 中获取 URL 和 aeskey
func (c *WxComChannel) DownloadImage(ctx context.Context, media *bus.Media) ([]byte, string, error) {
	if media == nil || media.URL == "" {
		return nil, "", fmt.Errorf("media URL is empty")
	}

	aesKey := ""
	if media.Metadata != nil {
		if key, ok := media.Metadata["aeskey"].(string); ok {
			aesKey = key
		}
	}

	return c.DownloadFile(ctx, media.URL, aesKey)
}

// ReplyTemplateCard 回复模板卡片消息
func (c *WxComChannel) ReplyTemplateCard(ctx context.Context, reqID string, card *TemplateCard, feedback *CardFeedback) error {
	if !c.IsRunning() {
		return ErrNotConnected
	}

	body := c.handler.BuildTemplateCardReply(card, feedback)
	_, err := c.wsManager.SendReply(ctx, reqID, body, WsCmdResponse)
	return err
}

// ReplyStreamWithCard 发送流式消息+模板卡片组合回复
func (c *WxComChannel) ReplyStreamWithCard(ctx context.Context, reqID, streamID, content string, finish bool,
	card *TemplateCard, cardFeedback *CardFeedback) error {
	if !c.IsRunning() {
		return ErrNotConnected
	}

	body := c.handler.BuildStreamWithCardReply(streamID, content, finish, nil, nil, card, cardFeedback)
	_, err := c.wsManager.SendReply(ctx, reqID, body, WsCmdResponse)
	return err
}

// UpdateTemplateCard 更新模板卡片 (需在收到template_card_event事件5秒内调用)
func (c *WxComChannel) UpdateTemplateCard(ctx context.Context, reqID string, card *TemplateCard, userIDs []string) error {
	if !c.IsRunning() {
		return ErrNotConnected
	}

	body := c.handler.BuildUpdateTemplateCard(card, userIDs)
	_, err := c.wsManager.SendReply(ctx, reqID, body, WsCmdResponseUpdate)
	return err
}

// SendTemplateCard 主动发送模板卡片消息
func (c *WxComChannel) SendTemplateCard(ctx context.Context, chatID string, card *TemplateCard) error {
	if !c.IsRunning() {
		return ErrNotConnected
	}

	reqID := generateReqID(WsCmdSendMsg)
	body := c.handler.BuildSendMessage(chatID, c.handler.BuildTemplateCardReply(card, nil))
	_, err := c.wsManager.SendReply(ctx, reqID, body, WsCmdSendMsg)
	return err
}