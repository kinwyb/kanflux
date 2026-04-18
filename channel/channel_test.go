package channel

import (
	"context"
	"testing"

	"github.com/kinwyb/kanflux/bus"
)

func TestChannelBase(t *testing.T) {
	bus := bus.NewMessageBus(10)

	config := BaseChannelConfig{
		Enabled:    true,
		AccountID:  "test",
		Name:       "TestChannel",
		AllowedIDs: nil,
	}

	base := NewChannelBase("test", "test", config, bus)

	// Test Name
	if base.Name() != "test" {
		t.Errorf("Expected name 'test', got '%s'", base.Name())
	}

	// Test AccountID
	if base.AccountID() != "test" {
		t.Errorf("Expected accountID 'test', got '%s'", base.AccountID())
	}

	// Test IsAllowed (no restriction)
	if !base.IsAllowed("anyone") {
		t.Error("Expected IsAllowed to return true for anyone")
	}

	// Test IsAllowed with restriction
	config.AllowedIDs = []string{"user1", "user2"}
	base2 := NewChannelBase("test", "test", config, bus)
	if !base2.IsAllowed("user1") {
		t.Error("Expected IsAllowed to return true for user1")
	}
	if base2.IsAllowed("user3") {
		t.Error("Expected IsAllowed to return false for user3")
	}

	// Test Start/Stop
	ctx := context.Background()
	if err := base.Start(ctx); err != nil {
		t.Errorf("Start failed: %v", err)
	}
	if !base.IsRunning() {
		t.Error("Expected IsRunning to return true after Start")
	}
	if err := base.Stop(); err != nil {
		t.Errorf("Stop failed: %v", err)
	}
	if base.IsRunning() {
		t.Error("Expected IsRunning to return false after Stop")
	}
}

func TestManager(t *testing.T) {
	bus := bus.NewMessageBus(10)
	manager := NewManager(bus)

	// Test initial state
	if manager.ChannelCount() != 0 {
		t.Errorf("Expected 0 channels, got %d", manager.ChannelCount())
	}

	// Create and register a mock channel
	mockChannel := &MockChannel{
		name:      "mock",
		accountID: "default",
		running:   false,
	}

	if err := manager.Register(mockChannel); err != nil {
		t.Errorf("Register failed: %v", err)
	}

	if manager.ChannelCount() != 1 {
		t.Errorf("Expected 1 channel, got %d", manager.ChannelCount())
	}

	// Test duplicate registration
	if err := manager.Register(mockChannel); err == nil {
		t.Error("Expected error for duplicate registration")
	}

	// Test Get
	ch, ok := manager.Get("mock")
	if !ok {
		t.Error("Expected to find channel 'mock'")
	}
	if ch.Name() != "mock" {
		t.Errorf("Expected name 'mock', got '%s'", ch.Name())
	}

	// Test List
	names := manager.List()
	if len(names) != 1 {
		t.Errorf("Expected 1 name, got %d", len(names))
	}
	if names[0] != "mock" {
		t.Errorf("Expected name 'mock', got '%s'", names[0])
	}

	// Test Unregister
	if err := manager.Unregister("mock"); err != nil {
		t.Errorf("Unregister failed: %v", err)
	}
	if manager.ChannelCount() != 0 {
		t.Errorf("Expected 0 channels after unregister, got %d", manager.ChannelCount())
	}

	// Test unregister non-existent
	if err := manager.Unregister("nonexistent"); err == nil {
		t.Error("Expected error for unregistering non-existent channel")
	}
}

func TestThreadBindingService(t *testing.T) {
	service := NewThreadBindingService()

	// Test Bind
	if err := service.Bind("tui:chat1", "telegram"); err != nil {
		t.Errorf("Bind failed: %v", err)
	}

	if service.BindingCount() != 1 {
		t.Errorf("Expected 1 binding, got %d", service.BindingCount())
	}

	// Test GetBinding
	binding, ok := service.GetBinding("tui:chat1")
	if !ok {
		t.Error("Expected to find binding")
	}
	if binding.TargetChan != "telegram" {
		t.Errorf("Expected target channel 'telegram', got '%s'", binding.TargetChan)
	}

	// Test GetChannelForSession
	targetChan := service.GetChannelForSession("tui:chat1")
	if targetChan != "telegram" {
		t.Errorf("Expected 'telegram', got '%s'", targetChan)
	}

	// Test Bind with options
	if err := service.Bind("tui:chat2", "slack", WithAgent("work-agent"), WithPriority(10)); err != nil {
		t.Errorf("Bind with options failed: %v", err)
	}

	binding2, ok := service.GetBinding("tui:chat2")
	if !ok {
		t.Error("Expected to find binding2")
	}
	if binding2.AgentName != "work-agent" {
		t.Errorf("Expected agent 'work-agent', got '%s'", binding2.AgentName)
	}
	if binding2.Priority != 10 {
		t.Errorf("Expected priority 10, got %d", binding2.Priority)
	}

	// Test Unbind
	if err := service.Unbind("tui:chat1"); err != nil {
		t.Errorf("Unbind failed: %v", err)
	}
	if service.BindingCount() != 1 {
		t.Errorf("Expected 1 binding after unbind, got %d", service.BindingCount())
	}

	// Test unbind non-existent
	if err := service.Unbind("nonexistent"); err == nil {
		t.Error("Expected error for unbinding non-existent binding")
	}

	// Test ResolveChannel
	msg := &bus.OutboundMessage{
		Channel: "tui",
		ChatID:  "chat2",
	}
	targetChan = service.ResolveChannel(msg)
	if targetChan != "slack" {
		t.Errorf("Expected 'slack', got '%s'", targetChan)
	}

	// Test ResolveChannel for non-bound session
	msg2 := &bus.OutboundMessage{
		Channel: "tui",
		ChatID:  "chat3",
	}
	targetChan = service.ResolveChannel(msg2)
	if targetChan != "tui" {
		t.Errorf("Expected 'tui' for non-bound session, got '%s'", targetChan)
	}

	// Test Clear
	service.Clear()
	if service.BindingCount() != 0 {
		t.Errorf("Expected 0 bindings after clear, got %d", service.BindingCount())
	}
}

// MockChannel for testing
type MockChannel struct {
	name      string
	accountID string
	running   bool
}

func (m *MockChannel) Name() string        { return m.name }
func (m *MockChannel) AccountID() string   { return m.accountID }
func (m *MockChannel) Start(ctx context.Context) error {
	m.running = true
	return nil
}
func (m *MockChannel) Stop() error {
	m.running = false
	return nil
}
func (m *MockChannel) IsRunning() bool { return m.running }
func (m *MockChannel) Send(ctx context.Context, msg *bus.OutboundMessage) error { return nil }
func (m *MockChannel) SendStream(ctx context.Context, msg *bus.OutboundMessage) error {
	return nil
}
func (m *MockChannel) HandleChatEvent(ctx context.Context, event *bus.ChatEvent) error { return nil }
func (m *MockChannel) IsAllowed(senderID string) bool { return true }
func (m *MockChannel) HandleRequest(ctx context.Context, request *bus.OutboundMessage) (*bus.OutboundMessage, error) {
	return nil, nil
}