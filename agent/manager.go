package agent

import (
	"context"
	"errors"
	"fmt"
	"github.com/kinwyb/kanflux/agent/tools"
	"github.com/kinwyb/kanflux/bus"
	"github.com/kinwyb/kanflux/session"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

// ResumeParamConverter 恢复参数转换函数类型
// 将入站消息转换成指定类型的恢复参数
type ResumeParamConverter func(msg *bus.InboundMessage, interruptInfo *InterruptInfo) (any, error)

// Manager 管理多个 Agent 实例
type Manager struct {
	agents           map[string]*Agent // agentID -> Agent
	defaultAgent     *Agent            // 默认 Agent
	bus              *bus.MessageBus
	sessionMgr       *session.Manager
	mu               sync.RWMutex
	resumeConverters map[reflect.Type]ResumeParamConverter // 按恢复参数类型注册转换器
	converterMu      sync.RWMutex
}

// NewManager 创建 Agent 管理器
func NewManager(msgBus *bus.MessageBus, sessionMgr *session.Manager) *Manager {
	ret := &Manager{
		agents:           make(map[string]*Agent),
		bus:              msgBus,
		sessionMgr:       sessionMgr,
		resumeConverters: make(map[reflect.Type]ResumeParamConverter),
	}

	// 注册 ApprovalResult 类型的转换器
	RegisterResumeConverterForType[*tools.ApprovalResult](
		ret,
		func(msg *bus.InboundMessage, interruptInfo *InterruptInfo) (*tools.ApprovalResult, error) {
			// 根据消息内容解析用户意图
			content := strings.ToLower(strings.TrimSpace(msg.Content))
			approved := content == "y" || content == "yes" || content == "同意"

			var disapproveReason *string
			if !approved && content != "" {
				disapproveReason = new(content)
			}
			return &tools.ApprovalResult{
				Approved:         approved,
				DisapproveReason: disapproveReason,
			}, nil
		},
	)
	return ret
}

// log 发布日志到总线
func (m *Manager) log(ctx context.Context, level, source, message string) {
	event := &bus.LogEvent{
		Level:     level,
		Source:    source,
		Message:   message,
		Timestamp: time.Now(),
	}
	_ = m.bus.PublishLogEvent(ctx, event)
}

// RegisterAgent 注册 Agent
func (m *Manager) RegisterAgent(agentID string, agent *Agent, isDefault bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.agents[agentID] = agent
	if isDefault {
		m.defaultAgent = agent
	}
}

// GetAgent 获取 Agent
func (m *Manager) GetAgent(agentID string) (*Agent, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	agent, ok := m.agents[agentID]
	return agent, ok
}

// GetDefaultAgent 获取默认 Agent
func (m *Manager) GetDefaultAgent() *Agent {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.defaultAgent
}

// RegisterResumeConverter 注册恢复参数转换器
// paramType: 期望的恢复参数类型
// converter: 转换函数，将入站消息转换为恢复参数
func (m *Manager) RegisterResumeConverter(paramType reflect.Type, converter ResumeParamConverter) {
	m.converterMu.Lock()
	defer m.converterMu.Unlock()
	m.resumeConverters[paramType] = converter
}

// RegisterResumeConverterForType 注册恢复参数转换器（泛型版本）
// T: 恢复参数类型
func RegisterResumeConverterForType[T any](m *Manager, converter func(msg *bus.InboundMessage, interruptInfo *InterruptInfo) (T, error)) {
	m.RegisterResumeConverter(reflect.TypeOf(new(T)).Elem(), func(msg *bus.InboundMessage, interruptInfo *InterruptInfo) (any, error) {
		return converter(msg, interruptInfo)
	})
}

// GetResumeConverter 获取指定类型的转换器
func (m *Manager) GetResumeConverter(paramType reflect.Type) (ResumeParamConverter, bool) {
	m.converterMu.RLock()
	defer m.converterMu.RUnlock()
	converter, ok := m.resumeConverters[paramType]
	return converter, ok
}

// convertResumeParam 将入站消息转换为恢复参数
func (m *Manager) convertResumeParam(msg *bus.InboundMessage, interruptInfo *InterruptInfo) (any, error) {
	if interruptInfo == nil || interruptInfo.ResumeParamType == nil {
		// 没有指定恢复参数类型，返回 nil
		return nil, nil
	}

	converter, ok := m.GetResumeConverter(interruptInfo.ResumeParamType)
	if !ok {
		// 没有注册转换器，返回 nil（让调用方使用默认逻辑）
		return nil, fmt.Errorf("no converter registered for type %v", interruptInfo.ResumeParamType)
	}

	return converter(msg, interruptInfo)
}

// ListAgents 列出所有 Agent ID
func (m *Manager) ListAgents() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ids := make([]string, 0, len(m.agents))
	for id := range m.agents {
		ids = append(ids, id)
	}
	return ids
}

// Start 启动消息处理器
func (m *Manager) Start(ctx context.Context) error {
	// 启动消息处理器
	go m.processMessages(ctx)
	return nil
}

// Stop 停止所有 Agent
func (m *Manager) Stop() error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for id, agent := range m.agents {
		if err := agent.Stop(); err != nil {
			m.log(context.Background(), bus.LogLevelError, "manager", fmt.Sprintf("Failed to stop agent %s: %v", id, err))
		}
	}
	return nil
}

// processMessages 处理入站消息
func (m *Manager) processMessages(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			msg, err := m.bus.ConsumeInbound(ctx)
			if err != nil {
				if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
					continue
				}
				continue
			}

			if err = m.RouteInbound(ctx, msg); err != nil {
				m.log(ctx, bus.LogLevelError, "manager", fmt.Sprintf("Failed to route message: %v", err))
			}
		}
	}
}

// RouteInbound 路由入站消息到对应的 Agent
func (m *Manager) RouteInbound(ctx context.Context, msg *bus.InboundMessage) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// 使用默认 Agent
	agent := m.defaultAgent
	if agent == nil {
		return fmt.Errorf("no agent available")
	}

	// 处理消息
	return m.handleInboundMessage(ctx, msg, agent)
}

// handleInboundMessage 处理入站消息
func (m *Manager) handleInboundMessage(ctx context.Context, msg *bus.InboundMessage, agent *Agent) error {
	// 生成会话键
	sessionKey := fmt.Sprintf("%s:%s:%s", msg.Channel, msg.AccountID, msg.ChatID)
	if msg.ChatID == "default" || msg.ChatID == "" {
		sessionKey = fmt.Sprintf("%s:%s:%d", msg.Channel, msg.AccountID, msg.Timestamp.Unix())
	}

	sess, err := m.sessionMgr.GetOrCreate(sessionKey)
	if err != nil {
		return err
	}

	// 注册回调，通过闭包携带 msg 信息
	eventSeq := 0
	cbID := agent.RegisterCallback(func(event *Event) {
		if event == nil {
			return
		}
		eventSeq++
		m.handleAgentEvent(ctx, msg, event, eventSeq)
	})

	var responses []adk.Message
	var historyLen int // 记录历史消息数量，用于区分新增消息

	// 加载历史消息并添加当前消息
	maxHistory := 100 // 默认值
	history := sess.GetHistorySafe(maxHistory)
	historyLen = len(history)
	m.log(ctx, bus.LogLevelDebug, "manager", fmt.Sprintf("History loaded: %d messages", historyLen))

	// 获取中断信息
	interruptInfo := agent.GetInterruptInfo(sessionKey)

	if interruptInfo != nil {

		// 尝试使用注册的转换器将消息转换为恢复参数
		resumeParams, convertErr := m.convertResumeParam(msg, interruptInfo)

		if convertErr == nil && resumeParams != nil {
			// 使用转换后的参数恢复
			m.log(ctx, bus.LogLevelInfo, "manager", "Resuming with converted params")
			responses, err = agent.Resume(ctx, sessionKey, resumeParams)
		} else if convertErr != nil {
			// 有转换器但转换失败，使用默认逻辑
			m.log(ctx, bus.LogLevelDebug, "manager", fmt.Sprintf("Resume param conversion failed: %v", convertErr))
			responses, err = agent.Resume(ctx, sessionKey, msg.Content)
		} else {
			// 没有注册转换器，使用默认逻辑
			responses, err = agent.Resume(ctx, sessionKey, msg.Content)
		}
		if err != nil {
			m.log(ctx, bus.LogLevelError, "manager", fmt.Sprintf("Failed to resume session: %v", err))
		}

	} else {
		// 正常处理新消息

		// 将用户消息添加到 session（支持媒体内容）
		userMsg := buildUserMessage(msg.Content, msg.Media)
		newMessages := append(history, userMsg)

		// 使用 Agent 处理消息
		responses, err = agent.Prompt(ctx, newMessages, sessionKey)

	}

	// 注销回调
	agent.UnregisterCallback(cbID)

	if err != nil {
		if info, ok := compose.IsInterruptRerunError(err); ok {
			// 发布到总线
			outbound := &bus.OutboundMessage{
				Channel:   msg.Channel,
				ChatID:    msg.ChatID,
				Content:   info.(string),
				ReplyTo:   msg.ID,
				Timestamp: time.Now(),
			}
			if err = m.bus.PublishOutbound(ctx, outbound); err != nil {
				return err
			}
			return nil
		}
		m.publishChatEvent(ctx, msg.Channel, msg.ChatID, bus.ChatEventStateError, err.Error(), eventSeq)
		return err
	}

	if len(responses) == 0 {
		return nil
	}

	// 将新增的消息存入 session（排除历史消息）
	// responses 包含历史消息 + 新消息，需要只存储新消息
	if len(responses) > historyLen { // +1 是因为添加了用户消息
		newResponses := responses[historyLen:]
		for _, resp := range newResponses {
			sess.AddMessage(resp)
		}
	}

	// 保存 session
	m.sessionMgr.Save(sess)

	response := responses[len(responses)-1]

	// 发布聊天事件结束
	m.publishChatEvent(ctx, msg.Channel, msg.ChatID, bus.ChatEventStateFinal, response.Content, eventSeq)

	// 提取响应中的媒体内容
	responseMedia := extractMediaFromMessage(response)

	// 发布到总线
	outbound := &bus.OutboundMessage{
		Channel:   msg.Channel,
		ChatID:    msg.ChatID,
		Content:   response.Content,
		Media:     responseMedia,
		ReplyTo:   msg.ID,
		Timestamp: time.Now(),
	}

	if err = m.bus.PublishOutbound(ctx, outbound); err != nil {
		return err
	}

	return nil
}

// handleAgentEvent 处理 Agent 事件，转发到消息总线
func (m *Manager) handleAgentEvent(ctx context.Context, msg *bus.InboundMessage, event *Event, seq int) {
	switch event.Type {
	case EventMessageStart:
		//m.publishChatEvent(ctx, msg.Channel, msg.ChatID, bus.ChatEventStateDelta, "", seq)
	case EventMessageUpdate:
		if event.Message != nil {
			if event.Message.ReasoningContent != "" {
				m.publishChatEvent(ctx, msg.Channel, msg.ChatID, bus.ChatEventStateThinking, event.Message.ReasoningContent, seq)
			} else {
				m.publishChatEvent(ctx, msg.Channel, msg.ChatID, bus.ChatEventStateDelta, event.Message.Content, seq)
			}
		}
	case EventMessageEnd:
		if event.Message != nil {
			m.publishChatEvent(ctx, msg.Channel, msg.ChatID, bus.ChatEventStateFinal, event.Message.Content, seq)
		}
	case EventToolStart:
		if event.Message != nil && len(event.Message.ToolCalls) > 0 {
			toolInfo := map[string]interface{}{
				"tool_name": event.Message.ToolCalls[0].Function.Name,
				"tool_id":   event.Message.ToolCalls[0].ID,
				"args":      event.Message.ToolCalls[0].Function.Arguments,
			}
			m.publishChatEvent(ctx, msg.Channel, msg.ChatID, bus.ChatEventStateTool, fmt.Sprintf("%v", toolInfo), seq)
		}
	case EventToolEnd:
		if event.Message != nil {
			toolInfo := map[string]interface{}{
				"tool_result": event.Message.Content,
			}
			m.publishChatEvent(ctx, msg.Channel, msg.ChatID, bus.ChatEventStateTool, fmt.Sprintf("%v", toolInfo), seq)
		}
	case EventInterrupt:
		m.publishChatEvent(ctx, msg.Channel, msg.ChatID, bus.ChatEventStateInterrupt, event.Message.Content, seq)
	}
}

// publishChatEvent 发布聊天事件到总线
func (m *Manager) publishChatEvent(ctx context.Context, channel, chatID, state string, content string, seq int) {
	event := &bus.ChatEvent{
		Channel:   channel,
		ChatID:    chatID,
		State:     state,
		Content:   content,
		Seq:       seq,
		Timestamp: time.Now(),
	}

	_ = m.bus.PublishChatEvent(ctx, event)
}

// generateID 生成唯一ID
func generateID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// PublishToBus 发布消息到总线
func (m *Manager) PublishToBus(ctx context.Context, channel, chatID string, content string) error {
	outbound := &bus.OutboundMessage{
		Channel:   channel,
		ChatID:    chatID,
		Content:   content,
		Timestamp: time.Now(),
	}

	return m.bus.PublishOutbound(ctx, outbound)
}

// PublishSystemMessage 发布系统消息
func (m *Manager) PublishSystemMessage(ctx context.Context, content string, metadata map[string]interface{}) error {
	inbound := &bus.InboundMessage{
		ID:        generateID(),
		Channel:   bus.ChannelSystem,
		Content:   content,
		Timestamp: time.Now(),
		Metadata:  metadata,
	}

	return m.bus.PublishInbound(ctx, inbound)
}

// HandleCommand 处理命令
func (m *Manager) HandleCommand(ctx context.Context, cmd string, args []string) (string, error) {
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return "", fmt.Errorf("empty command")
	}

	command := parts[0]
	switch command {
	case "/agents":
		return m.listAgentsCommand()
	case "/sessions":
		return m.listSessionsCommand()
	default:
		// 使用默认 Agent 处理
		agent := m.GetDefaultAgent()
		if agent == nil {
			return "", fmt.Errorf("no agent available")
		}
		resp, err := agent.Prompt(ctx, []adk.Message{schema.UserMessage(cmd)}, "")
		if err != nil {
			return "", err
		}
		return resp[len(resp)-1].Content, nil
	}
}

// listAgentsCommand 列出所有 Agent
func (m *Manager) listAgentsCommand() (string, error) {
	agents := m.ListAgents()
	if len(agents) == 0 {
		return "No agents registered", nil
	}

	var sb strings.Builder
	sb.WriteString("Registered agents:\n")
	for _, id := range agents {
		sb.WriteString(fmt.Sprintf("  - %s\n", id))
	}
	return sb.String(), nil
}

// listSessionsCommand 列出所有会话
func (m *Manager) listSessionsCommand() (string, error) {
	sessions, err := m.sessionMgr.List()
	if err != nil {
		return "", err
	}

	if len(sessions) == 0 {
		return "No active sessions", nil
	}

	var sb strings.Builder
	sb.WriteString("Active sessions:\n")
	for _, key := range sessions {
		sb.WriteString(fmt.Sprintf("  - %s\n", key))
	}
	return sb.String(), nil
}

// GetToolsInfo 获取工具信息
func (m *Manager) GetToolsInfo() (map[string]interface{}, error) {
	result := make(map[string]interface{})
	result["manager"] = "AgentManager"
	result["agents"] = m.ListAgents()
	return result, nil
}
