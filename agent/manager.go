package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/kinwyb/kanflux/agent/tools"
	"github.com/kinwyb/kanflux/bus"
	"github.com/kinwyb/kanflux/config"
	"github.com/kinwyb/kanflux/memoria"
	"github.com/kinwyb/kanflux/providers"
	"github.com/kinwyb/kanflux/session"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
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
	commands         map[string]CommandHandler // 命令注册表
	commandsMu       sync.RWMutex
}

// NewManager 创建 Agent 管理器
func NewManager(msgBus *bus.MessageBus, sessionMgr *session.Manager) *Manager {
	ret := &Manager{
		agents:           make(map[string]*Agent),
		bus:              msgBus,
		sessionMgr:       sessionMgr,
		resumeConverters: make(map[reflect.Type]ResumeParamConverter),
		commands:         make(map[string]CommandHandler),
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

	// 注册默认命令
	ret.registerDefaultCommands()

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

// RegisterAgentsFromConfig 根据配置自动注册所有 agent
// 处理子 agent 依赖关系，确保子 agent 先于父 agent 创建
func (m *Manager) RegisterAgentsFromConfig(ctx context.Context, cfg *config.Config, providerFactory func(ctx context.Context, apiBaseURL, model, apiKey string) (model.ToolCallingChatModel, error)) error {
	if cfg == nil || len(cfg.Agents) == 0 {
		return errors.New("no agents in config")
	}

	// 解析所有 agent 配置
	resolvedConfigs := make(map[string]*config.ResolvedAgentConfig)
	for _, agentCfg := range cfg.Agents {
		resolved, err := cfg.ResolveAgentConfig(agentCfg.Name)
		if err != nil {
			return fmt.Errorf("failed to resolve agent '%s': %w", agentCfg.Name, err)
		}
		resolvedConfigs[resolved.Name] = resolved
	}

	// 构建依赖图并拓扑排序
	agentNames := cfg.GetAllAgentNames()
	sortedNames, err := m.topologicalSortAgents(agentNames, resolvedConfigs)
	if err != nil {
		return fmt.Errorf("failed to sort agents by dependencies: %w", err)
	}

	// 按顺序创建和注册 agent
	createdAgents := make(map[string]*Agent)
	defaultAgentName := cfg.GetDefaultAgentName()

	for _, name := range sortedNames {
		resolved := resolvedConfigs[name]

		// 创建 LLM
		var llm model.ToolCallingChatModel
		var err error
		if providerFactory != nil {
			llm, err = providerFactory(ctx, resolved.APIBaseURL, resolved.Model, resolved.APIKey)
		} else {
			llm, err = providers.NewOpenAI(ctx, resolved.APIBaseURL, resolved.Model, resolved.APIKey)
		}
		if err != nil {
			return fmt.Errorf("failed to create LLM for agent '%s': %w", name, err)
		}

		// 创建工具注册器
		toolRegistry := tools.NewRegistry()

		// 获取已创建的子 agent
		var subAgents []*Agent
		for _, subName := range resolved.SubAgents {
			if subAg, ok := createdAgents[subName]; ok {
				subAgents = append(subAgents, subAg)
			}
		}

		// 获取默认 skills 目录
		skillDirs := config.GetDefaultSkillDirs(resolved.Workspace)

		// 创建 Memoria 统一记忆系统（替代 RAG Manager + History）
		var memInstance *memoria.Memoria
		if len(resolved.KnowledgePaths) > 0 || resolved.EmbeddingModel != "" {
			m.log(ctx, bus.LogLevelInfo, "manager", fmt.Sprintf("Creating Memoria for agent '%s'...", name))

			mem, err := m.createMemoria(ctx, resolved)
			if err != nil {
				m.log(ctx, bus.LogLevelWarn, "manager", fmt.Sprintf("Failed to create Memoria for agent '%s': %v", name, err))
			} else {
				memInstance = mem
				// 异步启动（InitialScan=true 时会自动扫描知识库和聊天记录）
				go func() {
					initCtx := context.Background()
					if err := memInstance.Start(initCtx); err != nil {
						slog.Warn("Memoria start failed", "agent", name, "error", err)
					}
				}()
			}
		}

		// 创建 Agent 配置
		agentConfig := &Config{
			Name:           resolved.Name,
			Type:           resolved.Type,
			Description:    resolved.Description,
			LLM:            llm,
			Workspace:      resolved.Workspace,
			MaxIteration:   resolved.MaxIteration,
			ToolRegister:   toolRegistry,
			SkillDirs:      skillDirs,
			SubAgents:      subAgents,
			SubAgentNames:  resolved.SubAgents,
			Streaming:      resolved.Streaming,
			Tools:          resolved.Tools,
			ToolsApproval:  resolved.ToolsApproval,
			Memoria:        memInstance,
			SessionManager: m.sessionMgr,
		}

		// 创建 Agent
		ag, err := NewAgent(ctx, agentConfig)
		if err != nil {
			return fmt.Errorf("failed to create agent '%s': %w", name, err)
		}

		createdAgents[name] = ag

		// 注册到 Manager
		isDefault := name == defaultAgentName
		m.RegisterAgent(name, ag, isDefault)

		m.log(ctx, bus.LogLevelInfo, "manager", fmt.Sprintf("Agent '%s' registered (default=%v)", name, isDefault))
	}

	return nil
}

// topologicalSortAgents 拓扑排序 agent，确保子 agent 在父 agent 之前
func (m *Manager) topologicalSortAgents(agentNames []string, resolvedConfigs map[string]*config.ResolvedAgentConfig) ([]string, error) {
	// 构建依赖图
	graph := make(map[string][]string) // agent -> dependencies (sub_agents)
	inDegree := make(map[string]int)

	for _, name := range agentNames {
		inDegree[name] = 0
		graph[name] = []string{}
	}

	// 子 agent 是父 agent 的依赖（子 agent 必须先创建）
	for _, name := range agentNames {
		resolved := resolvedConfigs[name]
		for _, subName := range resolved.SubAgents {
			// subName 是 name 的依赖，name 依赖 subName
			if _, exists := resolvedConfigs[subName]; exists {
				graph[subName] = append(graph[subName], name)
				inDegree[name]++
			}
		}
	}

	// Kahn 算法拓扑排序
	queue := []string{}
	for _, name := range agentNames {
		if inDegree[name] == 0 {
			queue = append(queue, name)
		}
	}

	result := []string{}
	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]
		result = append(result, curr)

		for _, neighbor := range graph[curr] {
			inDegree[neighbor]--
			if inDegree[neighbor] == 0 {
				queue = append(queue, neighbor)
			}
		}
	}

	// 检查是否有环
	if len(result) != len(agentNames) {
		return nil, errors.New("circular dependency detected in agent sub_agents")
	}

	return result, nil
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

// GetSessionManager 获取会话管理器
func (m *Manager) GetSessionManager() *session.Manager {
	return m.sessionMgr
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

// RegisterCommand 注册命令处理器
// cmd: 命令名称（不含斜杠，如 "help"）
// handler: 命令处理器
func (m *Manager) RegisterCommand(cmd string, handler CommandHandler) {
	m.commandsMu.Lock()
	defer m.commandsMu.Unlock()
	m.commands[cmd] = handler
}

// RegisterCommandFunc 注册命令处理函数
func (m *Manager) RegisterCommandFunc(cmd string, handler CommandHandlerFunc) {
	m.RegisterCommand(cmd, handler)
}

// GetCommandHandler 获取命令处理器
func (m *Manager) GetCommandHandler(cmd string) (CommandHandler, bool) {
	m.commandsMu.RLock()
	defer m.commandsMu.RUnlock()
	handler, ok := m.commands[cmd]
	return handler, ok
}

// ListCommands 列出所有注册的命令
func (m *Manager) ListCommands() []string {
	m.commandsMu.RLock()
	defer m.commandsMu.RUnlock()
	cmds := make([]string, 0, len(m.commands))
	for cmd := range m.commands {
		cmds = append(cmds, "/"+cmd)
	}
	return cmds
}

// registerDefaultCommands 注册默认命令
func (m *Manager) registerDefaultCommands() {
	// /help - 显示帮助信息
	m.RegisterCommandFunc("help", func(ctx context.Context, cmd string, args []string, msg *bus.InboundMessage) *CommandResult {
		helpText := `可用命令:
  /help          - 显示帮助信息
  /agents        - 列出所有 Agent
  /sessions      - 列出所有会话
  /commands      - 列出所有可用命令
  /new           - 开始新会话
  /clear         - 清空当前会话历史
  /status        - 显示系统状态

其他消息将由 AI Agent 处理。`
		return &CommandResult{
			Content:     helpText,
			SkipAgent:   true,
			ShouldReply: true,
		}
	})

	// /agents - 列出所有 Agent
	m.RegisterCommandFunc("agents", func(ctx context.Context, cmd string, args []string, msg *bus.InboundMessage) *CommandResult {
		result, _ := m.listAgentsCommand()
		return &CommandResult{
			Content:     result,
			SkipAgent:   true,
			ShouldReply: true,
		}
	})

	// /sessions - 列出所有会话
	m.RegisterCommandFunc("sessions", func(ctx context.Context, cmd string, args []string, msg *bus.InboundMessage) *CommandResult {
		result, _ := m.listSessionsCommand()
		return &CommandResult{
			Content:     result,
			SkipAgent:   true,
			ShouldReply: true,
		}
	})

	// /commands - 列出所有命令
	m.RegisterCommandFunc("commands", func(ctx context.Context, cmd string, args []string, msg *bus.InboundMessage) *CommandResult {
		cmds := m.ListCommands()
		var sb strings.Builder
		sb.WriteString("可用命令:\n")
		for _, c := range cmds {
			sb.WriteString(fmt.Sprintf("  %s\n", c))
		}
		return &CommandResult{
			Content:     sb.String(),
			SkipAgent:   true,
			ShouldReply: true,
		}
	})

	// /new - 开始新会话
	m.RegisterCommandFunc("new", func(ctx context.Context, cmd string, args []string, msg *bus.InboundMessage) *CommandResult {
		// 生成新的 chatID
		newChatID := fmt.Sprintf("NEW_%d", time.Now().UnixNano())
		return &CommandResult{
			Content:     fmt.Sprintf("已开始新会话: %s", newChatID),
			SkipAgent:   true,
			ShouldReply: true,
			Metadata: map[string]interface{}{
				MetadataKeyNewChatID: newChatID,
			},
		}
	})

	// /clear - 清空当前会话历史
	m.RegisterCommandFunc("clear", func(ctx context.Context, cmd string, args []string, msg *bus.InboundMessage) *CommandResult {
		sessionKey := fmt.Sprintf("%s:%s:%s", msg.Channel, msg.AccountID, msg.ChatID)
		sess, err := m.sessionMgr.GetOrCreate(sessionKey)
		if err != nil {
			return &CommandResult{
				Content:     fmt.Sprintf("获取会话失败: %v", err),
				SkipAgent:   true,
				ShouldReply: true,
				Error:       err,
			}
		}
		sess.Clear()
		m.sessionMgr.Save(sess)
		return &CommandResult{
			Content:     "已清空当前会话历史",
			SkipAgent:   true,
			ShouldReply: true,
		}
	})

	// /status - 显示系统状态
	m.RegisterCommandFunc("status", func(ctx context.Context, cmd string, args []string, msg *bus.InboundMessage) *CommandResult {
		var sb strings.Builder
		sb.WriteString("系统状态:\n")
		sb.WriteString(fmt.Sprintf("  Agents: %d\n", len(m.agents)))
		sessions, _ := m.sessionMgr.List()
		sb.WriteString(fmt.Sprintf("  Sessions: %d\n", len(sessions)))
		sb.WriteString(fmt.Sprintf("  Commands: %d\n", len(m.commands)))
		return &CommandResult{
			Content:     sb.String(),
			SkipAgent:   true,
			ShouldReply: true,
		}
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
	// 检查是否是命令
	cmd, args, isCommand := ParseCommand(msg.Content)
	if isCommand {
		// 查找命令处理器
		handler, ok := m.GetCommandHandler(cmd)
		if ok {
			// 处理命令
			result := handler.Handle(ctx, cmd, args, msg)

			// 如果有错误，发布错误事件
			if result.Error != nil {
				m.publishChatEvent(ctx, msg.Channel, msg.ChatID, "command", bus.ChatEventStateError, result.Error.Error(), 0)
			}

			// 如果需要回复，发布到 outbound
			if result.ShouldReply && result.Content != "" {
				// 合并命令元数据到 outbound 消息
				outboundMeta := make(map[string]interface{})
				if msg.Metadata != nil {
					for k, v := range msg.Metadata {
						outboundMeta[k] = v
					}
				}
				if result.Metadata != nil {
					for k, v := range result.Metadata {
						outboundMeta[k] = v
					}
				}

				outbound := &bus.OutboundMessage{
					Channel:   msg.Channel,
					ChatID:    msg.ChatID,
					Content:   result.Content,
					Media:     result.Media,
					ReplyTo:   msg.ID,
					Timestamp: time.Now(),
					Metadata:  outboundMeta,
				}
				if err := m.bus.PublishOutbound(ctx, outbound); err != nil {
					return err
				}
			}

			// 如果跳过 agent，直接返回
			if result.SkipAgent {
				return result.Error
			}
		}
	}

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
	// 获取 agent 名称
	agentName := agent.cfg.Name

	// 生成会话键
	sessionKey := fmt.Sprintf("%s:%s:%s", msg.Channel, msg.AccountID, msg.ChatID)
	if msg.ChatID == "default" || msg.ChatID == "" {
		sessionKey = fmt.Sprintf("%s:%s:%d", msg.Channel, msg.AccountID, msg.Timestamp.Unix())
	}

	sess, err := m.sessionMgr.GetOrCreate(sessionKey)
	if err != nil {
		return err
	}

	// 注册回调，通过闭包携带 msg 和 agentName 信息
	eventSeq := 0
	cbID := agent.RegisterCallback(func(event *Event) {
		if event == nil {
			return
		}
		eventSeq++
		m.handleAgentEvent(ctx, msg, agentName, event, eventSeq)
	})

	// 注销回调
	defer agent.UnregisterCallback(cbID)

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

		// 用户习惯
		userPreferences := ""
		if agent.cfg != nil && agent.cfg.Memoria != nil {
			userPreferences = agent.cfg.Memoria.GetL1Summary(msg.AccountID)
		}

		// 将用户消息添加到 session（支持媒体内容）
		userMsg := buildUserMessage(msg.Content, msg.Media)
		newMessages := append(history, userMsg)

		// 使用 Agent 处理消息
		responses, err = agent.Prompt(ctx, newMessages, sessionKey, userPreferences)

	}

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

		// 发布错误事件
		m.publishChatEvent(ctx, msg.Channel, msg.ChatID, agentName, bus.ChatEventStateError, err.Error(), eventSeq)
		// 同时发布 OutboundMessage，确保调用方能收到响应不会卡住
		outbound := &bus.OutboundMessage{
			Channel:   msg.Channel,
			ChatID:    msg.ChatID,
			Content:   "",
			ReplyTo:   msg.ID,
			Timestamp: time.Now(),
			Metadata: map[string]interface{}{
				"error": err.Error(),
			},
		}
		if pubErr := m.bus.PublishOutbound(ctx, outbound); pubErr != nil {
			m.log(ctx, bus.LogLevelError, "manager", fmt.Sprintf("Failed to publish error outbound: %v", pubErr))
		}
		return err
	}

	if len(responses) == 0 {
		// 没有响应，发布空响应确保调用方不会卡住
		outbound := &bus.OutboundMessage{
			Channel:   msg.Channel,
			ChatID:    msg.ChatID,
			Content:   "",
			ReplyTo:   msg.ID,
			Timestamp: time.Now(),
			Metadata:  msg.Metadata,
		}
		if pubErr := m.bus.PublishOutbound(ctx, outbound); pubErr != nil {
			m.log(ctx, bus.LogLevelError, "manager", fmt.Sprintf("Failed to publish empty outbound: %v", pubErr))
		}
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
	m.publishChatEvent(ctx, msg.Channel, msg.ChatID, agentName, bus.ChatEventStateFinal, response.Content, eventSeq)

	// 提取响应中的媒体内容
	responseMedia := extractMediaFromMessage(response)

	// 发布到总线
	outbound := &bus.OutboundMessage{
		Channel:          msg.Channel,
		ChatID:           msg.ChatID,
		Content:          response.Content,
		ReasoningContent: response.ReasoningContent, // 提取思考内容
		Media:            responseMedia,
		ReplyTo:          msg.ID,
		Timestamp:        time.Now(),
		Metadata:         msg.Metadata,
	}

	if err = m.bus.PublishOutbound(ctx, outbound); err != nil {
		return err
	}

	return nil
}

// handleAgentEvent 处理 Agent 事件，转发到消息总线
func (m *Manager) handleAgentEvent(ctx context.Context, msg *bus.InboundMessage, agentName string, event *Event, seq int) {
	// 构建基础 metadata，包含入站消息的 req_id 用于回复透传
	baseMetadata := make(map[string]interface{})
	if msg.Metadata != nil {
		// 复制入站消息的 metadata，特别是 req_id 用于 wxcom channel 回复
		for k, v := range msg.Metadata {
			baseMetadata[k] = v
		}
	}

	switch event.Type {
	case EventMessageStart:
		//m.publishChatEvent(ctx, msg.Channel, msg.ChatID, agentName, bus.ChatEventStateDelta, "", seq, baseMetadata)
	case EventMessageUpdate:
		if event.Message != nil {
			if event.Message.ReasoningContent != "" {
				m.publishChatEvent(ctx, msg.Channel, msg.ChatID, agentName, bus.ChatEventStateThinking, event.Message.ReasoningContent, seq, baseMetadata)
			} else {
				m.publishChatEvent(ctx, msg.Channel, msg.ChatID, agentName, bus.ChatEventStateDelta, event.Message.Content, seq, baseMetadata)
			}
		}
	case EventMessageEnd:
		if event.Message != nil {
			m.publishChatEvent(ctx, msg.Channel, msg.ChatID, agentName, bus.ChatEventStateFinal, event.Message.Content, seq, baseMetadata)
		}
	case EventToolStart:
		if event.Message != nil && len(event.Message.ToolCalls) > 0 {
			toolInfo := map[string]interface{}{
				"tool_name": event.Message.ToolCalls[0].Function.Name,
				"tool_id":   event.Message.ToolCalls[0].ID,
				"args":      event.Message.ToolCalls[0].Function.Arguments,
			}
			// 合并 baseMetadata 和 toolInfo
			mergedMeta := make(map[string]interface{})
			for k, v := range baseMetadata {
				mergedMeta[k] = v
			}
			mergedMeta["tool_info"] = toolInfo
			m.publishChatEvent(ctx, msg.Channel, msg.ChatID, agentName, bus.ChatEventStateTool, fmt.Sprintf("%v", toolInfo), seq, mergedMeta)
		}
	case EventToolEnd:
		if event.Message != nil {
			toolInfo := map[string]interface{}{
				"tool_result": event.Message.Content,
			}
			// 合并 baseMetadata 和 toolInfo
			mergedMeta := make(map[string]interface{})
			for k, v := range baseMetadata {
				mergedMeta[k] = v
			}
			mergedMeta["tool_info"] = toolInfo
			m.publishChatEvent(ctx, msg.Channel, msg.ChatID, agentName, bus.ChatEventStateTool, fmt.Sprintf("%v", toolInfo), seq, mergedMeta)
		}
	case EventInterrupt:
		// 合并 baseMetadata 和 event.Metadata
		mergedMeta := make(map[string]interface{})
		for k, v := range baseMetadata {
			mergedMeta[k] = v
		}
		if event.Metadata != nil {
			for k, v := range event.Metadata {
				mergedMeta[k] = v
			}
		}
		m.publishChatEvent(ctx, msg.Channel, msg.ChatID, agentName, bus.ChatEventStateInterrupt, event.Message.Content, seq, mergedMeta)
	}
}

// publishChatEvent 发布聊天事件到总线
func (m *Manager) publishChatEvent(ctx context.Context, channel, chatID, agentName, state string, content string, seq int, metadata ...interface{}) {
	var meta interface{}
	if len(metadata) > 0 {
		meta = metadata[0]
	}
	event := &bus.ChatEvent{
		Channel:   channel,
		ChatID:    chatID,
		AgentName: agentName,
		State:     state,
		Content:   content,
		Seq:       seq,
		Timestamp: time.Now(),
		Metadata:  meta,
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

// HandleCommand 处理命令（独立调用，不经过消息总线）
func (m *Manager) HandleCommand(ctx context.Context, content string) (string, error) {
	cmd, args, isCommand := ParseCommand(content)
	if !isCommand {
		// 不是命令，使用 agent 处理
		agent := m.GetDefaultAgent()
		if agent == nil {
			return "", fmt.Errorf("no agent available")
		}
		resp, err := agent.Prompt(ctx, []adk.Message{schema.UserMessage(content)}, "", "")
		if err != nil {
			return "", err
		}
		return resp[len(resp)-1].Content, nil
	}

	handler, ok := m.GetCommandHandler(cmd)
	if !ok {
		return "", fmt.Errorf("unknown command: /%s", cmd)
	}

	result := handler.Handle(ctx, cmd, args, nil)
	return result.Content, result.Error
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

// createMemoria 创建 Memoria 统一记忆系统
func (m *Manager) createMemoria(ctx context.Context, resolved *config.ResolvedAgentConfig) (*memoria.Memoria, error) {
	m.log(ctx, bus.LogLevelInfo, "manager", "[Memoria] Creating instance...")

	// 创建 Memoria 配置
	memConfig := memoria.DefaultConfig()
	memConfig.Workspace = resolved.Workspace
	memConfig.KnowledgePaths = resolved.KnowledgePaths
	memConfig.InitialScan = true // Start 时自动扫描知识库和聊天记录

	// 设置 Embedding 配置（用于 L3 语义搜索）
	if resolved.EmbeddingModel != "" {
		memConfig.Embedding = &memoria.EmbeddingConfig{
			Provider:   resolved.EmbeddingProvider,
			Model:      resolved.EmbeddingModel,
			APIKey:     resolved.EmbeddingAPIKey,
			APIBaseURL: resolved.EmbeddingAPIBaseURL,
		}
	}

	// 创建 Memoria 实例
	mem, err := memoria.New(memConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create Memoria: %w", err)
	}

	// 设置 ChatModel（用于处理聊天记录和文件内容）
	if resolved.Model != "" {
		llm, err := providers.NewOpenAI(ctx, resolved.APIBaseURL, resolved.Model, resolved.APIKey)
		if err != nil {
			m.log(ctx, bus.LogLevelWarn, "manager", fmt.Sprintf("[Memoria] Failed to create LLM: %v", err))
		} else {
			// 使用适配器将 Eino ChatModel 转换为 memoria ChatModel
			mem.SetChatModel(memoria.NewEinoChatModelAdapter(llm))
		}
	}

	m.log(ctx, bus.LogLevelInfo, "manager", "[Memoria] Instance created successfully")
	return mem, nil
}
