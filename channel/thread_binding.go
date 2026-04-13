package channel

import (
	"fmt"
	"sync"

	"github.com/kinwyb/kanflux/bus"
)

// ThreadBinding 会话到通道的绑定
type ThreadBinding struct {
	SessionKey  string                 // Channel:AccountID:ChatID
	TargetChan  string                 // 目标通道名称
	AgentName   string                 // 可选：指定 agent
	Priority    int                    // 优先级（越高越优先）
	Metadata    map[string]interface{} // 附加元数据
}

// BindingOption 绑定配置选项
type BindingOption func(*ThreadBinding)

// WithAgent 设置指定 agent
func WithAgent(agentName string) BindingOption {
	return func(b *ThreadBinding) {
		b.AgentName = agentName
	}
}

// WithPriority 设置优先级
func WithPriority(priority int) BindingOption {
	return func(b *ThreadBinding) {
		b.Priority = priority
	}
}

// WithMetadata 设置元数据
func WithMetadata(metadata map[string]interface{}) BindingOption {
	return func(b *ThreadBinding) {
		b.Metadata = metadata
	}
}

// ThreadBindingService 管理会话绑定
type ThreadBindingService struct {
	bindings map[string]*ThreadBinding // sessionKey -> binding
	mu       sync.RWMutex
}

// NewThreadBindingService 创建绑定服务
func NewThreadBindingService() *ThreadBindingService {
	return &ThreadBindingService{
		bindings: make(map[string]*ThreadBinding),
	}
}

// Bind 绑定会话到通道
func (s *ThreadBindingService) Bind(sessionKey, targetChan string, opts ...BindingOption) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	binding := &ThreadBinding{
		SessionKey: sessionKey,
		TargetChan: targetChan,
		Priority:   0,
		Metadata:   make(map[string]interface{}),
	}

	// 应用选项
	for _, opt := range opts {
		opt(binding)
	}

	s.bindings[sessionKey] = binding
	return nil
}

// Unbind 解除绑定
func (s *ThreadBindingService) Unbind(sessionKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.bindings[sessionKey]; !ok {
		return fmt.Errorf("binding not found: %s", sessionKey)
	}

	delete(s.bindings, sessionKey)
	return nil
}

// GetBinding 获取绑定
func (s *ThreadBindingService) GetBinding(sessionKey string) (*ThreadBinding, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	binding, ok := s.bindings[sessionKey]
	return binding, ok
}

// GetChannelForSession 获取会话对应的目标通道
func (s *ThreadBindingService) GetChannelForSession(sessionKey string) string {
	binding, ok := s.GetBinding(sessionKey)
	if ok {
		return binding.TargetChan
	}
	return ""
}

// GetAgentForSession 获取会话对应的 agent
func (s *ThreadBindingService) GetAgentForSession(sessionKey string) string {
	binding, ok := s.GetBinding(sessionKey)
	if ok && binding.AgentName != "" {
		return binding.AgentName
	}
	return ""
}

// ResolveChannel 根据消息确定目标通道
// 优先级：显式绑定 > 消息的 Channel 字段
func (s *ThreadBindingService) ResolveChannel(msg *bus.OutboundMessage) string {
	// 构建 session key (OutboundMessage 没有 AccountID，使用 Channel:ChatID)
	sessionKey := fmt.Sprintf("%s:%s", msg.Channel, msg.ChatID)

	// 检查是否有绑定
	if binding, ok := s.GetBinding(sessionKey); ok {
		return binding.TargetChan
	}

	// 默认使用消息中的 Channel
	return msg.Channel
}

// ResolveAgent 根据消息确定目标 agent
func (s *ThreadBindingService) ResolveAgent(msg *bus.OutboundMessage) string {
	sessionKey := fmt.Sprintf("%s:%s", msg.Channel, msg.ChatID)

	if binding, ok := s.GetBinding(sessionKey); ok && binding.AgentName != "" {
		return binding.AgentName
	}
	return ""
}

// ListBindings 列出所有绑定
func (s *ThreadBindingService) ListBindings() []*ThreadBinding {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*ThreadBinding, 0, len(s.bindings))
	for _, binding := range s.bindings {
		result = append(result, binding)
	}
	return result
}

// Clear 清除所有绑定
func (s *ThreadBindingService) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bindings = make(map[string]*ThreadBinding)
}

// BindingCount 获取绑定数量
func (s *ThreadBindingService) BindingCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.bindings)
}