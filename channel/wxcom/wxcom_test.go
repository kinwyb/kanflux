package wxcom

import (
	"context"
	"fmt"
	"testing"

	"github.com/kinwyb/kanflux/bus"
)

func TestWxComConfigDefaults(t *testing.T) {
	cfg := &WxComConfig{
		BotID:  "test_bot",
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
		BotID:  "test_bot",
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
			"chatid": "test_chat",
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

func TestCardTypeConstants(t *testing.T) {
	if CardTypeTextNotice != "text_notice" {
		t.Errorf("Expected CardTypeTextNotice 'text_notice', got '%s'", CardTypeTextNotice)
	}

	if CardTypeButtonInteraction != "button_interaction" {
		t.Errorf("Expected CardTypeButtonInteraction 'button_interaction', got '%s'", CardTypeButtonInteraction)
	}

	if CardTypeVoteInteraction != "vote_interaction" {
		t.Errorf("Expected CardTypeVoteInteraction 'vote_interaction', got '%s'", CardTypeVoteInteraction)
	}

	if CardTypeMultipleInteraction != "multiple_interaction" {
		t.Errorf("Expected CardTypeMultipleInteraction 'multiple_interaction', got '%s'", CardTypeMultipleInteraction)
	}
}

func TestNewTextNoticeCard(t *testing.T) {
	card := NewTextNoticeCard("标题", "描述内容")

	if card.CardType != CardTypeTextNotice {
		t.Errorf("Expected card type %s, got %s", CardTypeTextNotice, card.CardType)
	}

	if card.MainTitle.Title != "标题" {
		t.Errorf("Expected title '标题', got '%s'", card.MainTitle.Title)
	}

	if card.MainTitle.Desc != "描述内容" {
		t.Errorf("Expected desc '描述内容', got '%s'", card.MainTitle.Desc)
	}
}

func TestNewButtonInteractionCard(t *testing.T) {
	buttons := []CardButtonOption{
		{ID: "btn1", Text: "按钮1"},
		{ID: "btn2", Text: "按钮2"},
	}

	card := NewButtonInteractionCard("选择操作", "请选择", buttons, "task_123")

	if card.CardType != CardTypeButtonInteraction {
		t.Errorf("Expected card type %s, got %s", CardTypeButtonInteraction, card.CardType)
	}

	if len(card.ButtonSelection.OptionList) != 2 {
		t.Errorf("Expected 2 buttons, got %d", len(card.ButtonSelection.OptionList))
	}

	if card.TaskID != "task_123" {
		t.Errorf("Expected task_id 'task_123', got '%s'", card.TaskID)
	}
}

func TestNewVoteInteractionCard(t *testing.T) {
	options := []CardSelectOption{
		{ID: "opt1", Text: "选项1"},
		{ID: "opt2", Text: "选项2"},
	}

	card := NewVoteInteractionCard("投票标题", options, "vote_123")

	if card.CardType != CardTypeVoteInteraction {
		t.Errorf("Expected card type %s, got %s", CardTypeVoteInteraction, card.CardType)
	}

	if len(card.SelectList) != 1 {
		t.Errorf("Expected 1 select item, got %d", len(card.SelectList))
	}

	if card.TaskID != "vote_123" {
		t.Errorf("Expected task_id 'vote_123', got '%s'", card.TaskID)
	}
}

func TestMessageHandlerBuildTemplateCardReply(t *testing.T) {
	handler := NewMessageHandler(nil)

	card := NewTextNoticeCard("通知标题", "通知内容")
	feedback := &CardFeedback{ButtonDesc: "点击反馈"}

	body := handler.BuildTemplateCardReply(card, feedback)

	if body["msgtype"] != MsgTypeTemplateCard {
		t.Errorf("Expected msgtype %s, got %s", MsgTypeTemplateCard, body["msgtype"])
	}

	cardMap, ok := body["template_card"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected template_card map")
	}

	if cardMap["card_type"] != CardTypeTextNotice {
		t.Errorf("Expected card_type %s, got %s", CardTypeTextNotice, cardMap["card_type"])
	}

	feedbackMap, ok := cardMap["feedback"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected feedback map")
	}

	if feedbackMap["button_desc"] != "点击反馈" {
		t.Errorf("Expected button_desc '点击反馈', got '%s'", feedbackMap["button_desc"])
	}
}

func TestMessageHandlerBuildStreamWithCardReply(t *testing.T) {
	handler := NewMessageHandler(nil)

	card := NewTextNoticeCard("卡片标题", "卡片内容")
	cardFeedback := &CardFeedback{ButtonDesc: "卡片反馈"}

	body := handler.BuildStreamWithCardReply("stream_123", "流式内容", false, nil, nil, card, cardFeedback)

	if body["msgtype"] != "stream_with_template_card" {
		t.Errorf("Expected msgtype 'stream_with_template_card', got '%s'", body["msgtype"])
	}

	streamMap, ok := body["stream"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected stream map")
	}

	if streamMap["id"] != "stream_123" {
		t.Errorf("Expected stream id 'stream_123', got '%s'", streamMap["id"])
	}

	cardMap, ok := body["template_card"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected template_card map")
	}

	if cardMap["card_type"] != CardTypeTextNotice {
		t.Errorf("Expected card_type %s, got %s", CardTypeTextNotice, cardMap["card_type"])
	}
}

func TestMessageHandlerBuildUpdateTemplateCard(t *testing.T) {
	handler := NewMessageHandler(nil)

	card := NewTextNoticeCard("更新标题", "更新内容")
	card.TaskID = "task_update"

	body := handler.BuildUpdateTemplateCard(card, []string{"user1", "user2"})

	if body["response_type"] != "update_template_card" {
		t.Errorf("Expected response_type 'update_template_card', got '%s'", body["response_type"])
	}

	userIDs, ok := body["userids"].([]string)
	if !ok {
		t.Fatal("Expected userids slice")
	}

	if len(userIDs) != 2 {
		t.Errorf("Expected 2 userids, got %d", len(userIDs))
	}
}

func TestCardToMapWithAllFields(t *testing.T) {
	handler := NewMessageHandler(nil)

	card := &TemplateCard{
		CardType:  CardTypeMultipleInteraction,
		Source:    &CardSource{IconURL: "https://icon.url", Desc: "来源描述"},
		MainTitle: &CardMainTitle{Title: "主标题", Desc: "主标题描述"},
		SubTitle:  &CardSubTitle{Title: "副标题", Desc: "副标题描述"},
		TaskID:    "task_full",
		SelectList: []CardSelectItem{
			{
				QuestionKey: "q1",
				Title:       "问题1",
				OptionList: []CardSelectOption{
					{ID: "opt1", Text: "选项1"},
					{ID: "opt2", Text: "选项2"},
				},
			},
		},
		SubmitButton: &CardSubmitButton{Text: "提交", Key: "submit_key"},
	}

	cardMap := handler.cardToMap(card)

	if cardMap["card_type"] != CardTypeMultipleInteraction {
		t.Errorf("Expected card_type %s, got %s", CardTypeMultipleInteraction, cardMap["card_type"])
	}

	if cardMap["task_id"] != "task_full" {
		t.Errorf("Expected task_id 'task_full', got '%s'", cardMap["task_id"])
	}

	selectList, ok := cardMap["select_list"].([]map[string]interface{})
	if !ok {
		t.Fatal("Expected select_list")
	}

	if len(selectList) != 1 {
		t.Errorf("Expected 1 select item, got %d", len(selectList))
	}
}

func TestUploadCommandConstants(t *testing.T) {
	if WsCmdUploadMediaInit != "aibot_upload_media_init" {
		t.Errorf("Expected 'aibot_upload_media_init', got '%s'", WsCmdUploadMediaInit)
	}
	if WsCmdUploadMediaChunk != "aibot_upload_media_chunk" {
		t.Errorf("Expected 'aibot_upload_media_chunk', got '%s'", WsCmdUploadMediaChunk)
	}
	if WsCmdUploadMediaFinish != "aibot_upload_media_finish" {
		t.Errorf("Expected 'aibot_upload_media_finish', got '%s'", WsCmdUploadMediaFinish)
	}
}

// newTestChannelWithMockWS creates a WxComChannel with a mock WsManager for testing.
// connected controls whether the mock WsManager reports as connected.
func newTestChannelWithMockWS(t *testing.T, connected bool) *WxComChannel {
	t.Helper()
	handler := NewMessageHandler(nil)
	wsManager := &WsManager{
		isConnected:      connected,
		stopChan:         make(chan struct{}),
		replyNotifyCh:    make(chan string),
		replyQueues:      make(map[string][]*replyQueueItem),
		pendingAcks:      make(map[string]*pendingAck),
		processingReqIDs: make(map[string]bool),
	}
	return &WxComChannel{
		running:   connected,
		handler:   handler,
		wsManager: wsManager,
	}
}

func TestComputeMD5(t *testing.T) {
	// 测试已知 MD5 值
	hash := computeMD5([]byte("hello"))
	expected := "5d41402abc4b2a76b9719d911017c592"
	if fmt.Sprintf("%x", hash) != expected {
		t.Errorf("Expected MD5 '%s', got '%x'", expected, hash)
	}

	// 测试空数据
	emptyHash := computeMD5([]byte{})
	expectedEmpty := "d41d8cd98f00b204e9800998ecf8427e"
	if fmt.Sprintf("%x", emptyHash) != expectedEmpty {
		t.Errorf("Expected MD5 '%s', got '%x'", expectedEmpty, emptyHash)
	}
}

func TestGetStringFromMap(t *testing.T) {
	// 测试正常获取
	m := map[string]interface{}{"key": "value"}
	if v, ok := getStringFromMap(m, "key"); !ok || v != "value" {
		t.Errorf("Expected 'value', got '%v', ok=%v", v, ok)
	}

	// 测试不存在的 key
	if v, ok := getStringFromMap(m, "missing"); ok {
		t.Errorf("Expected false for missing key, got '%v'", v)
	}

	// 测试 nil map
	if v, ok := getStringFromMap(nil, "key"); ok {
		t.Errorf("Expected false for nil map, got '%v'", v)
	}

	// 测试非 string 类型值
	m2 := map[string]interface{}{"num": 123}
	if v, ok := getStringFromMap(m2, "num"); ok {
		t.Errorf("Expected false for non-string value, got '%v'", v)
	}
}

func TestUploadMediaSmallFile(t *testing.T) {
	//cfg := WxComConfig{
	//	Enabled: true,
	//	BotID:   "aibAnLYMK4TgdOkkhCCYZabAtOGE64ouXFV",
	//	Secret:  "krmWVv9crsg52IkvabpS0h8NQzV1X6Yk0yNIsiyeObX",
	//}
	//mbus := bus.NewMessageBus(10)
	//channel, err := NewWxComChannel(mbus, &cfg)
	//if err != nil {
	//	t.Fatal(err)
	//}
	//// 小于 5 字节应该报错
	//ctx := context.Background()
	//err = channel.Start(ctx)
	//if err != nil {
	//	t.Fatal(err)
	//}
	//meid, err := channel.UploadMedia(ctx, "file", "tiny.txt", []byte("abcdefghijklmnopqrstuvwxyz"))
	//if err != nil {
	//	t.Fatal("Expected error for file smaller than 5 bytes", err)
	//}
	//t.Log(meid)
	//reqID := "NMnZQimJSBem6Jc0_6KZOwAA"
	//err = channel.SendMedia(ctx, MsgTypeFile, reqID, "", meid)
	//if err != nil {
	//	t.Fatal(err)
	//}
	//channel.Stop()
}

func TestUploadMediaLargeFile(t *testing.T) {
	channel := newTestChannelWithMockWS(t, true)

	// 超过 100 分片（100 * 512KB + 1）
	largeData := make([]byte, 100*512*1024+1)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	ctx := context.Background()
	_, err := channel.UploadMedia(ctx, "file", "large.bin", largeData)
	if err == nil {
		t.Error("Expected error for file exceeding 100 chunks")
	}
}

func TestUploadMediaChunkSize(t *testing.T) {
	// 测试分片计算逻辑
	const chunkSize = 512 * 1024

	// 正好 1 个分片
	data1 := make([]byte, chunkSize)
	totalChunks1 := (len(data1) + chunkSize - 1) / chunkSize
	if totalChunks1 != 1 {
		t.Errorf("Expected 1 chunk for 512KB, got %d", totalChunks1)
	}

	// 正好 2 个分片
	data2 := make([]byte, chunkSize+1)
	totalChunks2 := (len(data2) + chunkSize - 1) / chunkSize
	if totalChunks2 != 2 {
		t.Errorf("Expected 2 chunks for 512KB+1, got %d", totalChunks2)
	}

	// 100 个分片（边界）
	data100 := make([]byte, 100*chunkSize)
	totalChunks100 := (len(data100) + chunkSize - 1) / chunkSize
	if totalChunks100 != 100 {
		t.Errorf("Expected 100 chunks for max size, got %d", totalChunks100)
	}

	// 超过 100 个分片
	data101 := make([]byte, 100*chunkSize+1)
	totalChunks101 := (len(data101) + chunkSize - 1) / chunkSize
	if totalChunks101 != 101 {
		t.Errorf("Expected 101 chunks, got %d", totalChunks101)
	}
}

func TestUploadMediaMediaTypes(t *testing.T) {
	// 测试合法的 media type 常量
	validTypes := []string{"file", "image", "voice", "video"}
	for _, mt := range validTypes {
		if mt == "" {
			t.Errorf("Media type '%s' should not be empty", mt)
		}
	}
}

func TestUploadMediaNotConnected(t *testing.T) {
	handler := NewMessageHandler(nil)
	channel := &WxComChannel{
		running: false,
		handler: handler,
	}

	ctx := context.Background()
	_, err := channel.UploadMedia(ctx, "file", "test.txt", []byte("hello"))
	if err != ErrNotConnected {
		t.Errorf("Expected ErrNotConnected, got %v", err)
	}
}
