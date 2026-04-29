// Package wxcom provides enterprise WeChat (企业微信) intelligent robot channel implementation.
// Based on WebSocket long connection for message sending/receiving, streaming replies, template cards, etc.
package wxcom

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

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

	// logger
	logger *slog.Logger

	// 运行状态
	running   bool
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

	// 转换为bus消息，使用 channel 自己的名称（支持多账号场景）
	inbound := c.handler.ConvertToInbound(msg, c.Name(), c.config.BotID)

	// 下载并解密媒体文件
	if len(inbound.Media) > 0 {
		c.processMedia(context.Background(), inbound)
	}

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
			ID:            frame.Headers["req_id"],
			Channel:       c.Name(),
			AccountID:     c.config.BotID,
			SenderID:      event.UserID,
			ChatID:        event.UserID,
			Content:       "你好", // 空内容表示进入会话事件
			StreamingMode: bus.StreamingModeAccumulate,
			Timestamp:     event.EventTime,
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
			ID:            frame.Headers["req_id"],
			Channel:       c.Name(),
			AccountID:     c.config.BotID,
			SenderID:      event.UserID,
			ChatID:        event.ChatID,
			Content:       fmt.Sprintf("模板卡片事件: %s", event.EventKey),
			StreamingMode: bus.StreamingModeAccumulate,
			Timestamp:     event.EventTime,
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
			ID:            frame.Headers["req_id"],
			Channel:       c.Name(),
			AccountID:     c.config.BotID,
			SenderID:      event.UserID,
			ChatID:        event.ChatID,
			Content:       "用户反馈事件",
			StreamingMode: bus.StreamingModeAccumulate,
			Timestamp:     event.EventTime,
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

	// 从metadata获取req_id，同时用作streamID
	reqID := msg.ReplyTo
	if msg.Metadata != nil {
		if id, ok := msg.Metadata["req_id"].(string); ok && id != "" {
			reqID = id
		}
	}

	// 如果没有req_id，生成一个用于主动发送
	if reqID == "" {
		reqID = generateReqID(WsCmdSendMsg)
	}

	// req_id 作为 streamID
	streamID := reqID

	// 构建回复消息
	body := c.handler.ConvertOutboundToReply(msg, "", streamID, true)

	// 如果是主动发送（无原始req_id），需要包装成发送消息格式
	if msg.Metadata == nil || msg.Metadata["req_id"] == nil {
		body = c.handler.BuildSendMessage(msg.ChatID, body)
	}

	// 发送回复
	_, err := c.wsManager.SendReply(ctx, reqID, body, WsCmdResponse)
	if err != nil {
		c.logger.Error("Failed to send message", "error", err)
		return err
	}

	return nil
}

// SendStream 发送流式消息
func (c *WxComChannel) SendStream(ctx context.Context, msg *bus.OutboundMessage) error {
	if !c.IsRunning() {
		return nil
	}

	// 从metadata获取req_id，同时用作streamID
	reqID := msg.ReplyTo
	if msg.Metadata != nil {
		if id, ok := msg.Metadata["req_id"].(string); ok && id != "" {
			reqID = id
		}
	}

	// 如果没有req_id，生成一个（主动发送场景）
	if reqID == "" {
		reqID = generateReqID(WsCmdSendMsg)
	}

	if msg.Error != "" {
		c.logger.Error("Stream error", "error", msg.Error)
		return fmt.Errorf("stream error: %s", msg.Error)
	}

	// req_id 作为 streamID
	streamID := reqID

	// 发送流式消息
	finish := msg.IsFinal
	body := c.handler.BuildStreamReply(streamID, msg.Content, finish, nil, nil)

	_, err := c.wsManager.SendReply(ctx, reqID, body, WsCmdResponse)
	if err != nil {
		c.logger.Error("Failed to send stream message", "error", err)
		return err
	}

	return nil
}

// HandleChatEvent 处理聊天事件（仅状态通知）
func (c *WxComChannel) HandleChatEvent(ctx context.Context, event *bus.ChatEvent) error {
	if !c.IsRunning() {
		return nil
	}

	// ChatEvent 现在只做状态通知，不携带内容
	// 内容通过 OutboundMessage 发送，由 Send/SendStream 处理
	switch event.State {
	case bus.ChatEventStateStart:
		// 开始处理（可选：显示 loading 状态）
		c.logger.Debug("Chat started", "chat_id", event.ChatID)

	case bus.ChatEventStateTool:
		// 工具调用通知（可选：显示工具调用状态）
		if event.ToolInfo != nil {
			if event.ToolInfo.IsStart {
				c.logger.Info("Tool started", "tool_name", event.ToolInfo.Name, "chat_id", event.ChatID)
			} else {
				c.logger.Info("Tool completed", "chat_id", event.ChatID)
			}
		}

	case bus.ChatEventStateComplete:
		// 处理完成
		c.logger.Debug("Chat completed", "chat_id", event.ChatID)

	case bus.ChatEventStateError:
		// 错误
		c.logger.Error("Chat error", "error", event.Error, "chat_id", event.ChatID)

	case bus.ChatEventStateInterrupt:
		// 中断（等待用户确认）
		c.logger.Debug("Chat interrupted", "chat_id", event.ChatID)
	}

	return nil
}

// IsAllowed 检查发送者是否允许
func (c *WxComChannel) IsAllowed(senderID string) bool {
	// 企业微信通过后台配置权限，这里始终返回true
	return c.config.Enabled
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

// HandleRequest 处理请求消息（如 send_file）
func (c *WxComChannel) HandleRequest(ctx context.Context, request *bus.OutboundMessage) (*bus.OutboundMessage, error) {
	// 根据请求类型处理
	switch request.RequestType {
	case bus.RequestTypeSendFile:
		return c.handleSendFileRequest(ctx, request)
	default:
		// 不支持的请求类型，返回 nil 表示不处理
		return nil, nil
	}
}

// handleSendFileRequest 处理发送文件请求
func (c *WxComChannel) handleSendFileRequest(ctx context.Context, request *bus.OutboundMessage) (*bus.OutboundMessage, error) {
	if !c.IsRunning() {
		return &bus.OutboundMessage{
			Error:  ErrNotConnected.Error(),
			Result: "发送失败：通道未连接",
		}, nil
	}

	// 从 request.Media 获取文件路径
	if len(request.Media) == 0 {
		return &bus.OutboundMessage{
			Error:  "no file provided",
			Result: "发送失败：未提供文件",
		}, nil
	}

	filePath := request.Media[0].URL
	caption := ""
	if request.Media[0].Metadata != nil {
		if cap, ok := request.Media[0].Metadata["caption"].(string); ok {
			caption = cap
		}
	}

	// TODO: 实现企业微信发送文件 API
	// 当前返回模拟响应，后续需要实现微信发送文件逻辑
	c.logger.Info("Send file request received", "file_path", filePath, "chat_id", request.ChatID, "caption", caption)

	return &bus.OutboundMessage{
		Content: "✅ 文件发送请求已接收",
		Result:  fmt.Sprintf("文件发送成功（模拟）：%s", filePath),
	}, nil
}

// processMedia 下载并解密媒体文件，填充 Media 字段
func (c *WxComChannel) processMedia(ctx context.Context, inbound *bus.InboundMessage) {
	// 倒序遍历以支持安全删除 document 项
	for i := len(inbound.Media) - 1; i >= 0; i-- {
		media := &inbound.Media[i]
		switch media.Type {
		case "image", "audio":
			data, filename, err := c.DownloadImage(ctx, media)
			if err != nil {
				c.logger.Warn("Failed to download media", "type", media.Type, "error", err)
				continue
			}
			media.Base64 = base64.StdEncoding.EncodeToString(data)
			media.URL = "" // 清空 URL，避免 OpenAI 接口优先使用已失效的临时链接
			media.MimeType = DetectMimeType(data)
			if media.Metadata == nil {
				media.Metadata = make(map[string]interface{})
			}
			media.Metadata["filename"] = filename

		case "document":
			aesKey := ""
			if media.Metadata != nil {
				if key, ok := media.Metadata["aeskey"].(string); ok {
					aesKey = key
				}
			}
			data, filename, err := c.DownloadFile(ctx, media.URL, aesKey)
			if err != nil {
				c.logger.Warn("Failed to download document", "error", err)
				continue
			}
			text := extractText(data, filename)
			if inbound.Content != "" {
				inbound.Content = inbound.Content + fmt.Sprintf("\n\n[文件: %s]\n%s", filename, text)
			} else {
				inbound.Content = fmt.Sprintf("[文件: %s]\n%s", filename, text)
			}
			// 文档已提取为文本，移除 Media 避免 LLM 收到不支持的 file_url type
			inbound.Media = append(inbound.Media[:i], inbound.Media[i+1:]...)
		}
	}
}

// 常见纯文本扩展名
var textExtensions = map[string]bool{
	".txt":  true,
	".csv":  true,
	".json": true,
	".md":   true,
	".xml":  true,
	".yaml": true,
	".yml":  true,
	".log":  true,
	".ini":  true,
	".conf": true,
	".cfg":  true,
	".env":  true,
	".sh":   true,
	".html": true,
	".htm":  true,
	".sql":  true,
	".js":   true,
	".ts":   true,
	".py":   true,
	".go":   true,
	".java": true,
	".c":    true,
	".h":    true,
	".css":  true,
}

var xmlTagRegex = regexp.MustCompile(`<[^>]+>`)

// extractText 从文件数据中提取文本内容
func extractText(data []byte, filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))

	// 纯文本文件：直接返回
	if textExtensions[ext] {
		if t := strings.TrimSpace(string(data)); t != "" {
			return t
		}
		return "(空文件)"
	}

	// ZIP 格式：docx / xlsx / pptx
	if ext == ".zip" || ext == ".docx" || ext == ".xlsx" || ext == ".pptx" {
		return extractZipText(data, ext, filename)
	}

	// PDF 和其他二进制文件：返回描述
	return fmt.Sprintf("(无法提取文本，%d 字节)", len(data))
}

// extractZipText 从 ZIP 文件中提取文本
func extractZipText(data []byte, ext, filename string) string {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Sprintf("(ZIP 解析失败: %s)", err.Error())
	}

	switch ext {
	case ".docx":
		return extractDocxText(r)
	case ".xlsx":
		return extractXlsxText(r)
	case ".pptx":
		return "(PPTX 暂不支持文本提取)"
	default:
		return "(无法从 ZIP 中提取文本)"
	}
}

// extractDocxText 从 docx 中提取文本（读取 word/document.xml）
func extractDocxText(r *zip.Reader) string {
	for _, f := range r.File {
		if f.Name == "word/document.xml" || strings.HasSuffix(f.Name, "document.xml") {
			rc, err := f.Open()
			if err != nil {
				continue
			}
			xmlData, _ := io.ReadAll(rc)
			rc.Close()
			text := stripXMLTags(string(xmlData))
			if t := strings.TrimSpace(text); t != "" {
				return t
			}
		}
	}
	return "(docx 中未找到文本内容)"
}

// extractXlsxText 从 xlsx 中提取文本（读取 sharedStrings.xml 和 sheet*.xml）
func extractXlsxText(r *zip.Reader) string {
	// 先读取 shared strings
	sharedStrings := make(map[string]string)
	for _, f := range r.File {
		if strings.HasSuffix(f.Name, "sharedStrings.xml") {
			rc, err := f.Open()
			if err != nil {
				continue
			}
			xmlData, _ := io.ReadAll(rc)
			rc.Close()
			sharedStrings = parseSharedStrings(string(xmlData))
			break
		}
	}

	var result strings.Builder
	for _, f := range r.File {
		if strings.Contains(f.Name, "worksheets/sheet") && strings.HasSuffix(f.Name, ".xml") {
			rc, err := f.Open()
			if err != nil {
				continue
			}
			xmlData, _ := io.ReadAll(rc)
			rc.Close()
			cells := parseSheetCells(string(xmlData), sharedStrings)
			if len(cells) > 0 {
				if result.Len() > 0 {
					result.WriteString("\n")
				}
				result.WriteString(strings.Join(cells, "\t"))
			}
		}
	}
	if result.Len() > 0 {
		return result.String()
	}
	return "(xlsx 中未找到文本内容)"
}

// stripXMLTags 去除 XML 标签，保留文本
func stripXMLTags(xmlStr string) string {
	// 替换标签为空格
	text := xmlTagRegex.ReplaceAllString(xmlStr, " ")
	// 清理多余空白
	spaces := regexp.MustCompile(`\s+`)
	text = spaces.ReplaceAllString(text, " ")
	return strings.TrimSpace(text)
}

// parseSharedStrings 解析 xlsx shared strings
func parseSharedStrings(xmlStr string) map[string]string {
	result := make(map[string]string)
	// 简化的解析：提取 <t>...</t> 之间的内容
	re := regexp.MustCompile(`<t[^>]*>([^<]*)</t>`)
	matches := re.FindAllStringSubmatch(xmlStr, -1)
	for i, m := range matches {
		if len(m) > 1 {
			result[fmt.Sprintf("%d", i)] = m[1]
		}
	}
	return result
}

// parseSheetCells 解析 xlsx sheet 的单元格文本
func parseSheetCells(xmlStr string, sharedStrings map[string]string) []string {
	var cells []string
	// 简化解析：提取所有 <v>...</v> 和 <t>...</t> 之间的内容
	re := regexp.MustCompile(`<t r="[^"]*"[^>]*>([^<]*)</t>`)
	matches := re.FindAllStringSubmatch(xmlStr, -1)
	for _, m := range matches {
		if len(m) > 1 {
			cells = append(cells, m[1])
		}
	}
	// 如果没有 inline 字符串，尝试提取 value
	if len(cells) == 0 {
		re2 := regexp.MustCompile(`<v>([^<]*)</v>`)
		matches2 := re2.FindAllStringSubmatch(xmlStr, -1)
		for _, m := range matches2 {
			if len(m) > 1 {
				// 尝试作为 shared string 引用
				if text, ok := sharedStrings[m[1]]; ok {
					cells = append(cells, text)
				} else {
					cells = append(cells, m[1])
				}
			}
		}
	}
	return cells
}
