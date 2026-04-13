package wxcom

import (
	"testing"

	"github.com/kinwyb/kanflux/bus"
)

func TestWxComConfigDefaults(t *testing.T) {
	cfg := &WxComConfig{
		BotID: "test_bot",
		Secret: "test_secret",
	}

	cfg.SetDefaults()

	if cfg.WSURL != DefaultWSURL {
		t.Errorf("Expected WSURL %s, got %s", DefaultWSURL, cfg.WSURL)
	}

	if cfg.HeartbeatInterval != DefaultHeartbeatInterval {
		t.Errorf("Expected HeartbeatInterval %d, got %d", DefaultHeartbeatInterval, cfg.HeartbeatInterval)
	}

	if cfg.ReconnectInterval != DefaultReconnectInterval {
		t.Errorf("Expected ReconnectInterval %d, got %d", DefaultReconnectInterval, cfg.ReconnectInterval)
	}

	if cfg.MaxReconnect != DefaultMaxReconnect {
		t.Errorf("Expected MaxReconnect %d, got %d", DefaultMaxReconnect, cfg.MaxReconnect)
	}

	if cfg.RequestTimeout != DefaultRequestTimeout {
		t.Errorf("Expected RequestTimeout %d, got %d", DefaultRequestTimeout, cfg.RequestTimeout)
	}
}

func TestWxComConfigValidation(t *testing.T) {
	// 测试缺少 BotID
	cfg := &WxComConfig{
		Secret: "test_secret",
	}
	cfg.SetDefaults()

	if err := cfg.Validate(); err != ErrBotIDRequired {
		t.Errorf("Expected ErrBotIDRequired, got %v", err)
	}

	// 测试缺少 Secret
	cfg = &WxComConfig{
		BotID: "test_bot",
	}
	cfg.SetDefaults()

	if err := cfg.Validate(); err != ErrSecretRequired {
		t.Errorf("Expected ErrSecretRequired, got %v", err)
	}

	// 测试完整配置
	cfg = &WxComConfig{
		BotID: "test_bot",
		Secret: "test_secret",
	}
	cfg.SetDefaults()

	if err := cfg.Validate(); err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

func TestGenerateReqID(t *testing.T) {
	reqID := generateReqID(WsCmdSubscribe)

	if reqID == "" {
		t.Error("Expected non-empty req_id")
	}

	// 检查前缀
	if reqID[:len(WsCmdSubscribe)] != WsCmdSubscribe {
		t.Errorf("Expected prefix %s, got %s", WsCmdSubscribe, reqID[:len(WsCmdSubscribe)])
	}

	// 检查唯一性
	reqID2 := generateReqID(WsCmdSubscribe)
	if reqID == reqID2 {
		t.Error("Expected different req_ids")
	}
}

func TestMessageHandlerParseTextMessage(t *testing.T) {
	handler := NewMessageHandler(nil)

	frame := &WsFrame{
		Cmd: WsCmdCallback,
		Headers: map[string]string{
			"req_id": "test_req_id",
		},
		Body: map[string]interface{}{
			"msgtype": MsgTypeText,
			MsgTypeText: map[string]interface{}{
				"content": "Hello, World!",
			},
			"chatid":  "test_chat",
		},
	}

	msg, err := handler.ParseInboundMessage(frame)
	if err != nil {
		t.Fatalf("Failed to parse message: %v", err)
	}

	if msg.MsgType != MsgTypeText {
		t.Errorf("Expected MsgType %s, got %s", MsgTypeText, msg.MsgType)
	}

	if msg.Content != "Hello, World!" {
		t.Errorf("Expected Content 'Hello, World!', got '%s'", msg.Content)
	}

	if msg.ChatID != "test_chat" {
		t.Errorf("Expected ChatID 'test_chat', got '%s'", msg.ChatID)
	}
}

func TestMessageHandlerParseImageMessage(t *testing.T) {
	handler := NewMessageHandler(nil)

	frame := &WsFrame{
		Cmd: WsCmdCallback,
		Headers: map[string]string{
			"req_id": "test_req_id",
		},
		Body: map[string]interface{}{
			"msgtype": MsgTypeImage,
			MsgTypeImage: map[string]interface{}{
				"url":    "https://example.com/image.jpg",
				"aeskey": "test_aes_key",
			},
			"chatid": "test_chat",
		},
	}

	msg, err := handler.ParseInboundMessage(frame)
	if err != nil {
		t.Fatalf("Failed to parse message: %v", err)
	}

	if msg.MsgType != MsgTypeImage {
		t.Errorf("Expected MsgType %s, got %s", MsgTypeImage, msg.MsgType)
	}

	if msg.MediaURL != "https://example.com/image.jpg" {
		t.Errorf("Expected MediaURL 'https://example.com/image.jpg', got '%s'", msg.MediaURL)
	}

	if msg.MediaKey != "test_aes_key" {
		t.Errorf("Expected MediaKey 'test_aes_key', got '%s'", msg.MediaKey)
	}
}

func TestMessageHandlerParseEvent(t *testing.T) {
	handler := NewMessageHandler(nil)

	frame := &WsFrame{
		Cmd: WsCmdEventCallback,
		Headers: map[string]string{
			"req_id": "test_req_id",
		},
		Body: map[string]interface{}{
			"msgtype": "event",
			"event": map[string]interface{}{
				"eventtype": EventTypeEnterChat,
				"userid":    "test_user",
				"chatid":    "test_chat",
			},
		},
	}

	event, err := handler.ParseEvent(frame)
	if err != nil {
		t.Fatalf("Failed to parse event: %v", err)
	}

	if event.EventType != EventTypeEnterChat {
		t.Errorf("Expected EventType %s, got %s", EventTypeEnterChat, event.EventType)
	}

	if event.UserID != "test_user" {
		t.Errorf("Expected UserID 'test_user', got '%s'", event.UserID)
	}
}

func TestMessageHandlerConvertToInbound(t *testing.T) {
	handler := NewMessageHandler(nil)

	wsMsg := &WsMessage{
		Frame: &WsFrame{
			Headers: map[string]string{
				"req_id": "test_req_id",
			},
		},
		MsgType: MsgTypeText,
		ChatID:  "test_chat",
		UserID:  "test_user",
		Content: "Hello",
	}

	inbound := handler.ConvertToInbound(wsMsg, bus.ChannelWxCom, "test_bot")

	if inbound.Channel != bus.ChannelWxCom {
		t.Errorf("Expected Channel %s, got %s", bus.ChannelWxCom, inbound.Channel)
	}

	if inbound.AccountID != "test_bot" {
		t.Errorf("Expected AccountID 'test_bot', got '%s'", inbound.AccountID)
	}

	if inbound.SenderID != "test_user" {
		t.Errorf("Expected SenderID 'test_user', got '%s'", inbound.SenderID)
	}

	if inbound.ChatID != "test_chat" {
		t.Errorf("Expected ChatID 'test_chat', got '%s'", inbound.ChatID)
	}

	if inbound.Content != "Hello" {
		t.Errorf("Expected Content 'Hello', got '%s'", inbound.Content)
	}

	if inbound.Metadata["req_id"] != "test_req_id" {
		t.Errorf("Expected req_id in metadata")
	}
}

func TestMessageHandlerBuildStreamReply(t *testing.T) {
	handler := NewMessageHandler(nil)

	body := handler.BuildStreamReply("stream_123", "Hello", false, nil, nil)

	if body["msgtype"] != MsgTypeStream {
		t.Errorf("Expected msgtype %s, got %s", MsgTypeStream, body["msgtype"])
	}

	stream, ok := body["stream"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected stream map")
	}

	if stream["id"] != "stream_123" {
		t.Errorf("Expected stream id 'stream_123', got '%s'", stream["id"])
	}

	if stream["content"] != "Hello" {
		t.Errorf("Expected stream content 'Hello', got '%s'", stream["content"])
	}

	if stream["finish"] != false {
		t.Errorf("Expected stream finish false, got %v", stream["finish"])
	}
}

func TestMessageHandlerBuildMarkdownReply(t *testing.T) {
	handler := NewMessageHandler(nil)

	body := handler.BuildMarkdownReply("# Hello\nThis is **markdown**")

	if body["msgtype"] != MsgTypeMarkdown {
		t.Errorf("Expected msgtype %s, got %s", MsgTypeMarkdown, body["msgtype"])
	}

	markdown, ok := body["markdown"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected markdown map")
	}

	if markdown["content"] != "# Hello\nThis is **markdown**" {
		t.Errorf("Expected markdown content, got '%s'", markdown["content"])
	}
}

func TestWxComError(t *testing.T) {
	err := ErrBotIDRequired

	if err.Error() != "bot_id is required" {
		t.Errorf("Expected error message 'bot_id is required', got '%s'", err.Error())
	}

	if err.Code != 1001 {
		t.Errorf("Expected error code 1001, got %d", err.Code)
	}
}

func TestConstants(t *testing.T) {
	// 测试常量值
	if WsCmdSubscribe != "aibot_subscribe" {
		t.Errorf("Expected WsCmdSubscribe 'aibot_subscribe', got '%s'", WsCmdSubscribe)
	}

	if MsgTypeText != "text" {
		t.Errorf("Expected MsgTypeText 'text', got '%s'", MsgTypeText)
	}

	if EventTypeEnterChat != "enter_chat" {
		t.Errorf("Expected EventTypeEnterChat 'enter_chat', got '%s'", EventTypeEnterChat)
	}

	if DefaultWSURL != "wss://openws.work.weixin.qq.com" {
		t.Errorf("Expected DefaultWSURL 'wss://openws.work.weixin.qq.com', got '%s'", DefaultWSURL)
	}
}