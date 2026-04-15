package bus

import (
	"testing"
)

func TestChatEventTransformer_DeltaMode(t *testing.T) {
	transformer := NewChatEventTransformer(TransformerModeDelta)

	event := &ChatEvent{
		ID:      "test-1",
		Channel: ChannelWxCom,
		ChatID:  "chat-123",
		State:   ChatEventStateDelta,
		Content: "增量内容",
	}

	// 增量模式应该返回原始事件
	result := transformer.Transform(event)

	if result.Content != "增量内容" {
		t.Errorf("Delta mode should keep original content, got: %s", result.Content)
	}
	if result != event {
		t.Error("Delta mode should return original event")
	}
}

func TestChatEventTransformer_AccumulateMode(t *testing.T) {
	transformer := NewChatEventTransformer(TransformerModeAccumulate)

	// 模拟连续的 delta 事件
	events := []struct {
		state   string
		content string
	}{
		{ChatEventStateDelta, "你"},
		{ChatEventStateDelta, "好"},
		{ChatEventStateDelta, "，"},
		{ChatEventStateDelta, "世"},
		{ChatEventStateDelta, "界"},
	}

	expectedContents := []string{"你", "你好", "你好，", "你好，世", "你好，世界"}

	for i, e := range events {
		event := &ChatEvent{
			ID:      "test-" + e.content,
			Channel: ChannelWxCom,
			ChatID:  "chat-123",
			State:   e.state,
			Content: e.content,
		}

		result := transformer.Transform(event)

		if result.Content != expectedContents[i] {
			t.Errorf("Accumulate mode: expected %s, got %s", expectedContents[i], result.Content)
		}
	}

	// Final 事件
	finalEvent := &ChatEvent{
		ID:      "test-final",
		Channel: ChannelWxCom,
		ChatID:  "chat-123",
		State:   ChatEventStateFinal,
		Message: "你好，世界！",
	}

	result := transformer.Transform(finalEvent)
	if result.Message != "你好，世界！" {
		t.Errorf("Final should use Message field, got: %s", result.Message)
	}

	// 验证累积器已清理
	transformer.mu.Lock()
	if len(transformer.accumulators) != 0 {
		t.Error("Accumulator should be cleared after Final")
	}
	transformer.mu.Unlock()
}

func TestChatEventTransformer_AccumulateThinking(t *testing.T) {
	transformer := NewChatEventTransformer(TransformerModeAccumulate)

	// 思考内容单独累积
	thinkingEvents := []struct {
		state   string
		content string
	}{
		{ChatEventStateThinking, "让我"},
		{ChatEventStateThinking, "思考"},
		{ChatEventStateThinking, "一下"},
	}

	expectedThinking := []string{"让我", "让我思考", "让我思考一下"}

	for i, e := range thinkingEvents {
		event := &ChatEvent{
			ID:      "thinking-" + e.content,
			Channel: ChannelWxCom,
			ChatID:  "chat-123",
			State:   e.state,
			Content: e.content,
		}

		result := transformer.Transform(event)

		if result.Content != expectedThinking[i] {
			t.Errorf("Thinking accumulate: expected %s, got %s", expectedThinking[i], result.Content)
		}
	}
}

func TestChatEventTransformer_ClearOnError(t *testing.T) {
	transformer := NewChatEventTransformer(TransformerModeAccumulate)

	// 先累积一些内容
	deltaEvent := &ChatEvent{
		ChatID: "chat-123",
		State:  ChatEventStateDelta,
		Content: "测试",
	}
	transformer.Transform(deltaEvent)

	// 验证有累积器
	transformer.mu.Lock()
	if len(transformer.accumulators) == 0 {
		t.Error("Should have accumulator after delta")
	}
	transformer.mu.Unlock()

	// Error 事件应该清理累积器
	errorEvent := &ChatEvent{
		ChatID: "chat-123",
		State:  ChatEventStateError,
		Error:  "some error",
	}
	transformer.Transform(errorEvent)

	transformer.mu.Lock()
	if len(transformer.accumulators) != 0 {
		t.Error("Accumulator should be cleared after Error")
	}
	transformer.mu.Unlock()
}

func TestChatEventTransformer_MultipleChatIDs(t *testing.T) {
	transformer := NewChatEventTransformer(TransformerModeAccumulate)

	// 两个不同的 chatID
	chat1Event := &ChatEvent{
		ChatID: "chat-1",
		State:  ChatEventStateDelta,
		Content: "A",
	}
	chat2Event := &ChatEvent{
		ChatID: "chat-2",
		State:  ChatEventStateDelta,
		Content: "B",
	}

	result1 := transformer.Transform(chat1Event)
	result2 := transformer.Transform(chat2Event)

	if result1.Content != "A" {
		t.Errorf("Chat-1: expected A, got %s", result1.Content)
	}
	if result2.Content != "B" {
		t.Errorf("Chat-2: expected B, got %s", result2.Content)
	}

	// 继续累积
	chat1Event2 := &ChatEvent{
		ChatID: "chat-1",
		State:  ChatEventStateDelta,
		Content: "A2",
	}
	chat2Event2 := &ChatEvent{
		ChatID: "chat-2",
		State:  ChatEventStateDelta,
		Content: "B2",
	}

	result1 = transformer.Transform(chat1Event2)
	result2 = transformer.Transform(chat2Event2)

	if result1.Content != "AA2" {
		t.Errorf("Chat-1: expected AA2, got %s", result1.Content)
	}
	if result2.Content != "BB2" {
		t.Errorf("Chat-2: expected BB2, got %s", result2.Content)
	}
}

func TestStreamAccumulator(t *testing.T) {
	acc := NewStreamAccumulator()

	// 测试 Delta 累积
	if acc.AccumulateDelta("你") != "你" {
		t.Error("First delta should return '你'")
	}
	if acc.AccumulateDelta("好") != "你好" {
		t.Error("Second delta should return '你好'")
	}

	// 测试 Thinking 累积（独立）
	if acc.AccumulateThinking("思考") != "思考" {
		t.Error("First thinking should return '思考'")
	}

	// 测试 GetContent 和 GetThinking
	if acc.GetContent() != "你好" {
		t.Errorf("GetContent should be '你好', got: %s", acc.GetContent())
	}
	if acc.GetThinking() != "思考" {
		t.Errorf("GetThinking should be '思考', got: %s", acc.GetThinking())
	}

	// 测试 Reset
	acc.Reset()
	if acc.GetContent() != "" {
		t.Error("Content should be empty after Reset")
	}
	if acc.GetThinking() != "" {
		t.Error("Thinking should be empty after Reset")
	}
}