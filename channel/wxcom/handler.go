package wxcom

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/kinwyb/kanflux/bus"
)

// MessageHandler 消息处理器
// 负责解析WebSocket帧并转换为bus消息格式
type MessageHandler struct {
	logger *slog.Logger
}

// NewMessageHandler 创建消息处理器
func NewMessageHandler(logger *slog.Logger) *MessageHandler {
	return &MessageHandler{
		logger: logger,
	}
}

// ParseInboundMessage 将WebSocket帧解析为入站消息
func (h *MessageHandler) ParseInboundMessage(frame *WsFrame) (*WsMessage, error) {
	body := frame.Body
	if body == nil {
		return nil, fmt.Errorf("frame body is nil")
	}

	msgType, ok := body["msgtype"].(string)
	if !ok {
		return nil, fmt.Errorf("msgtype not found in body")
	}

	// 获取基本信息
	var userID, chatID string
	if event, ok := body["event"].(map[string]interface{}); ok {
		userID = getString(event, "userid")
		chatID = getString(event, "chatid")
	} else {
		// 从消息体获取
		chatID = getString(body, "chatid")
	}

	msg := &WsMessage{
		Frame:   frame,
		MsgType: msgType,
		ChatID:  chatID,
		UserID:  userID,
		MsgTime: time.Now(),
	}

	// 根据消息类型解析内容
	switch msgType {
	case MsgTypeText:
		if text, ok := body[MsgTypeText].(map[string]interface{}); ok {
			msg.Content = getString(text, "content")
		}

	case MsgTypeImage:
		if image, ok := body[MsgTypeImage].(map[string]interface{}); ok {
			msg.MediaURL = getString(image, "url")
			msg.MediaKey = getString(image, "aeskey")
		}

	case MsgTypeMixed:
		if mixed, ok := body[MsgTypeMixed].(map[string]interface{}); ok {
			items, _ := mixed["msg_item"].([]interface{})
			// 解析第一个文本项作为内容
			for _, item := range items {
				if itemMap, ok := item.(map[string]interface{}); ok {
					if getString(itemMap, "msgtype") == MsgTypeText {
						if text, ok := itemMap[MsgTypeText].(map[string]interface{}); ok {
							msg.Content = getString(text, "content")
							break
						}
					}
				}
			}
		}

	case MsgTypeVoice:
		if voice, ok := body[MsgTypeVoice].(map[string]interface{}); ok {
			msg.Content = getString(voice, "content") // 语音转文本内容
		}

	case MsgTypeFile:
		if file, ok := body[MsgTypeFile].(map[string]interface{}); ok {
			msg.MediaURL = getString(file, "url")
			msg.MediaKey = getString(file, "aeskey")
		}
	}

	return msg, nil
}

// ParseEvent 将WebSocket帧解析为事件
func (h *MessageHandler) ParseEvent(frame *WsFrame) (*WsEvent, error) {
	body := frame.Body
	if body == nil {
		return nil, fmt.Errorf("frame body is nil")
	}

	eventData, ok := body["event"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("event not found in body")
	}

	eventType := getString(eventData, "eventtype")

	event := &WsEvent{
		Frame:     frame,
		EventType: eventType,
		UserID:    getString(eventData, "userid"),
		ChatID:    getString(eventData, "chatid"),
		EventKey:  getString(eventData, "event_key"),
		TaskID:    getString(eventData, "task_id"),
		EventTime: time.Now(),
	}

	return event, nil
}

// ConvertToInbound 将WsMessage转换为bus.InboundMessage
func (h *MessageHandler) ConvertToInbound(msg *WsMessage, channelName, accountID string) *bus.InboundMessage {
	inbound := &bus.InboundMessage{
		Channel:   channelName,
		AccountID: accountID,
		SenderID:  msg.UserID,
		ChatID:    msg.ChatID,
		Content:   msg.Content,
		Timestamp: msg.MsgTime,
		Metadata:  make(map[string]interface{}),
	}

	// 设置消息类型
	inbound.Metadata["wx_msgtype"] = msg.MsgType

	// 处理媒体
	if msg.MediaURL != "" {
		media := bus.Media{
			URL:    msg.MediaURL,
			Metadata: make(map[string]interface{}),
		}
		if msg.MediaKey != "" {
			media.Metadata["aeskey"] = msg.MediaKey
		}

		switch msg.MsgType {
		case MsgTypeImage:
			media.Type = "image"
		case MsgTypeFile:
			media.Type = "document"
		}

		inbound.Media = []bus.Media{media}
	}

	// 保存原始req_id
	if msg.Frame.Headers != nil {
		inbound.Metadata["req_id"] = msg.Frame.Headers["req_id"]
	}

	return inbound
}

// BuildStreamReply 构建流式回复消息体
func (h *MessageHandler) BuildStreamReply(streamID, content string, finish bool, msgItem []MixedItem, feedback *StreamFeedback) map[string]interface{} {
	stream := map[string]interface{}{
		"id":      streamID,
		"content": content,
		"finish":  finish,
	}

	if finish && len(msgItem) > 0 {
		items := make([]map[string]interface{}, len(msgItem))
		for i, item := range msgItem {
			itemMap := map[string]interface{}{
				"msgtype": item.MsgType,
			}
			if item.Text != nil {
				itemMap[MsgTypeText] = map[string]interface{}{
					"content": item.Text.Content,
				}
			}
			if item.Image != nil {
				itemMap[MsgTypeImage] = map[string]interface{}{
					"url":    item.Image.URL,
					"aeskey": item.Image.AesKey,
				}
			}
			items[i] = itemMap
		}
		stream["msg_item"] = items
	}

	if feedback != nil {
		stream["feedback"] = map[string]interface{}{
			"button_desc": feedback.ButtonDesc,
		}
	}

	return map[string]interface{}{
		"msgtype": MsgTypeStream,
		"stream":  stream,
	}
}

// BuildMarkdownReply 构建Markdown回复消息体
func (h *MessageHandler) BuildMarkdownReply(content string) map[string]interface{} {
	return map[string]interface{}{
		"msgtype": MsgTypeMarkdown,
		"markdown": map[string]interface{}{
			"content": content,
		},
	}
}

// BuildTextReply 构建文本回复消息体
func (h *MessageHandler) BuildTextReply(content string) map[string]interface{} {
	return map[string]interface{}{
		"msgtype": MsgTypeText,
		MsgTypeText: map[string]interface{}{
			"content": content,
		},
	}
}

// BuildSendMessage 构建主动发送消息体
func (h *MessageHandler) BuildSendMessage(chatID string, body map[string]interface{}) map[string]interface{} {
	result := map[string]interface{}{
		"chatid": chatID,
	}
	for k, v := range body {
		result[k] = v
	}
	return result
}

// ConvertOutboundToReply 将bus.OutboundMessage转换为回复消息
func (h *MessageHandler) ConvertOutboundToReply(outbound *bus.OutboundMessage, reqID, streamID string, finish bool) map[string]interface{} {
	content := outbound.Content

	// 如果有思考内容，优先发送思考内容
	if outbound.ReasoningContent != "" {
		// TODO: 支持thinking类型消息
		// 目前直接拼接
		if content != "" {
			content = outbound.ReasoningContent + "\n\n" + content
		} else {
			content = outbound.ReasoningContent
		}
	}

	// 使用流式回复格式
	return h.BuildStreamReply(streamID, content, finish, nil, nil)
}

// getString 从map中获取字符串值
func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// ParseBodyFromJSON 从JSON字符串解析消息体
func ParseBodyFromJSON(jsonStr string) (map[string]interface{}, error) {
	var body map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &body); err != nil {
		return nil, err
	}
	return body, nil
}

// MarshalFrame 将帧序列化为JSON
func MarshalFrame(frame *WsFrame) ([]byte, error) {
	return json.Marshal(frame)
}

// BuildTemplateCardReply 构建模板卡片回复消息体
func (h *MessageHandler) BuildTemplateCardReply(card *TemplateCard, feedback *CardFeedback) map[string]interface{} {
	cardMap := h.cardToMap(card)
	if feedback != nil {
		cardMap["feedback"] = map[string]interface{}{
			"button_desc": feedback.ButtonDesc,
		}
	}

	return map[string]interface{}{
		"msgtype":       MsgTypeTemplateCard,
		"template_card": cardMap,
	}
}

// BuildStreamWithCardReply 构建流式消息+模板卡片组合回复
func (h *MessageHandler) BuildStreamWithCardReply(streamID, content string, finish bool,
	msgItem []MixedItem, streamFeedback *StreamFeedback, card *TemplateCard, cardFeedback *CardFeedback) map[string]interface{} {

	stream := map[string]interface{}{
		"id":      streamID,
		"content": content,
		"finish":  finish,
	}

	if finish && len(msgItem) > 0 {
		items := make([]map[string]interface{}, len(msgItem))
		for i, item := range msgItem {
			itemMap := map[string]interface{}{
				"msgtype": item.MsgType,
			}
			if item.Text != nil {
				itemMap[MsgTypeText] = map[string]interface{}{
					"content": item.Text.Content,
				}
			}
			if item.Image != nil {
				itemMap[MsgTypeImage] = map[string]interface{}{
					"url":    item.Image.URL,
					"aeskey": item.Image.AesKey,
				}
			}
			items[i] = itemMap
		}
		stream["msg_item"] = items
	}

	if streamFeedback != nil {
		stream["feedback"] = map[string]interface{}{
			"button_desc": streamFeedback.ButtonDesc,
		}
	}

	body := map[string]interface{}{
		"msgtype": "stream_with_template_card",
		"stream":  stream,
	}

	if card != nil {
		cardMap := h.cardToMap(card)
		if cardFeedback != nil {
			cardMap["feedback"] = map[string]interface{}{
				"button_desc": cardFeedback.ButtonDesc,
			}
		}
		body["template_card"] = cardMap
	}

	return body
}

// BuildUpdateTemplateCard 构建更新模板卡片消息体
func (h *MessageHandler) BuildUpdateTemplateCard(card *TemplateCard, userIDs []string) map[string]interface{} {
	body := map[string]interface{}{
		"response_type": "update_template_card",
		"template_card": h.cardToMap(card),
	}

	if len(userIDs) > 0 {
		body["userids"] = userIDs
	}

	return body
}

// cardToMap 将TemplateCard转换为map
func (h *MessageHandler) cardToMap(card *TemplateCard) map[string]interface{} {
	if card == nil {
		return nil
	}

	cardMap := map[string]interface{}{
		"card_type": card.CardType,
	}

	if card.Source != nil {
		cardMap["source"] = map[string]interface{}{
			"icon_url": card.Source.IconURL,
			"desc":     card.Source.Desc,
		}
	}

	if card.MainTitle != nil {
		cardMap["main_title"] = map[string]interface{}{
			"title": card.MainTitle.Title,
			"desc":  card.MainTitle.Desc,
		}
	}

	if card.SubTitle != nil {
		cardMap["sub_title"] = map[string]interface{}{
			"title": card.SubTitle.Title,
			"desc":  card.SubTitle.Desc,
		}
	}

	if card.EmphasisTitle != nil {
		cardMap["emphasis_title"] = map[string]interface{}{
			"title": card.EmphasisTitle.Title,
			"desc":  card.EmphasisTitle.Desc,
		}
	}

	if card.TaskID != "" {
		cardMap["task_id"] = card.TaskID
	}

	if card.CardAction != nil {
		action := map[string]interface{}{}
		if card.CardAction.Type > 0 {
			action["type"] = card.CardAction.Type
		}
		if card.CardAction.URL != "" {
			action["url"] = card.CardAction.URL
		}
		if card.CardAction.AppID != "" {
			action["appid"] = card.CardAction.AppID
		}
		if card.CardAction.PagePath != "" {
			action["pagepath"] = card.CardAction.PagePath
		}
		cardMap["card_action"] = action
	}

	if card.ButtonSelection != nil {
		selection := map[string]interface{}{
			"question_key": card.ButtonSelection.QuestionKey,
			"title":        card.ButtonSelection.Title,
			"disable":      card.ButtonSelection.Disable,
			"selected_id":  card.ButtonSelection.SelectedID,
		}
		if len(card.ButtonSelection.OptionList) > 0 {
			options := make([]map[string]interface{}, len(card.ButtonSelection.OptionList))
			for i, opt := range card.ButtonSelection.OptionList {
				options[i] = map[string]interface{}{
					"id":      opt.ID,
					"text":    opt.Text,
					"disable": opt.Disable,
				}
			}
			selection["option_list"] = options
		}
		cardMap["button_selection"] = selection
	}

	if card.ButtonTextArea != nil {
		cardMap["button_textarea"] = map[string]interface{}{
			"question_key": card.ButtonTextArea.QuestionKey,
			"title":        card.ButtonTextArea.Title,
			"disable":      card.ButtonTextArea.Disable,
			"placeholder":  card.ButtonTextArea.Placeholder,
			"value":        card.ButtonTextArea.Value,
		}
	}

	if len(card.SelectList) > 0 {
		selectList := make([]map[string]interface{}, len(card.SelectList))
		for i, item := range card.SelectList {
			selectItem := map[string]interface{}{
				"question_key": item.QuestionKey,
				"title":        item.Title,
				"disable":      item.Disable,
				"selected_id":  item.SelectedID,
			}
			if len(item.OptionList) > 0 {
				options := make([]map[string]interface{}, len(item.OptionList))
				for j, opt := range item.OptionList {
					options[j] = map[string]interface{}{
						"id":      opt.ID,
						"text":    opt.Text,
						"disable": opt.Disable,
					}
				}
				selectItem["option_list"] = options
			}
			selectList[i] = selectItem
		}
		cardMap["select_list"] = selectList
	}

	if card.SubmitButton != nil {
		cardMap["submit_button"] = map[string]interface{}{
			"text":    card.SubmitButton.Text,
			"key":     card.SubmitButton.Key,
			"disable": card.SubmitButton.Disable,
		}
	}

	if card.ImageTextArea != nil {
		cardMap["image_text_area"] = map[string]interface{}{
			"type":      card.ImageTextArea.Type,
			"url":       card.ImageTextArea.URL,
			"title":     card.ImageTextArea.Title,
			"desc":      card.ImageTextArea.Desc,
			"image_url": card.ImageTextArea.ImageURL,
		}
	}

	if len(card.VerticalContent) > 0 {
		content := make([]map[string]interface{}, len(card.VerticalContent))
		for i, item := range card.VerticalContent {
			content[i] = map[string]interface{}{
				"title": item.Title,
				"desc":  item.Desc,
			}
		}
		cardMap["vertical_content"] = content
	}

	return cardMap
}

// NewTextNoticeCard 创建文本通知卡片
func NewTextNoticeCard(title, desc string) *TemplateCard {
	return &TemplateCard{
		CardType: CardTypeTextNotice,
		MainTitle: &CardMainTitle{
			Title: title,
			Desc:  desc,
		},
	}
}

// NewButtonInteractionCard 创建按钮交互卡片
func NewButtonInteractionCard(title, desc string, buttons []CardButtonOption, taskID string) *TemplateCard {
	return &TemplateCard{
		CardType: CardTypeButtonInteraction,
		MainTitle: &CardMainTitle{
			Title: title,
			Desc:  desc,
		},
		ButtonSelection: &CardButtonSelection{
			OptionList: buttons,
		},
		TaskID: taskID,
	}
}

// NewVoteInteractionCard 创建投票选择卡片
func NewVoteInteractionCard(title string, options []CardSelectOption, taskID string) *TemplateCard {
	return &TemplateCard{
		CardType: CardTypeVoteInteraction,
		MainTitle: &CardMainTitle{
			Title: title,
		},
		SelectList: []CardSelectItem{
			{
				OptionList: options,
			},
		},
		TaskID: taskID,
	}
}

// NewMultipleInteractionCard 创建多项选择卡片
func NewMultipleInteractionCard(title string, selectItems []CardSelectItem, taskID string) *TemplateCard {
	return &TemplateCard{
		CardType: CardTypeMultipleInteraction,
		MainTitle: &CardMainTitle{
			Title: title,
		},
		SelectList:   selectItems,
		SubmitButton: &CardSubmitButton{Text: "提交"},
		TaskID:       taskID,
	}
}