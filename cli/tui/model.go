package tui

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/kinwyb/kanflux/agent"
	"github.com/kinwyb/kanflux/agent/tools"
	"github.com/kinwyb/kanflux/bus"
	"github.com/kinwyb/kanflux/config"
	"github.com/kinwyb/kanflux/providers"
	"github.com/kinwyb/kanflux/session"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"
)

// 状态定义
type SessionState int

const (
	StateIdle SessionState = iota
	StateProcessing
	StateWaitingApproval // 等待工具审批确认
)

// 消息类型
type (
	// AgentResponseMsg Agent响应消息
	AgentResponseMsg struct {
		Content          string
		ReasoningContent string // 思考/推理内容
		Error            error
		Metadata         map[string]interface{} // 元数据，可携带命令返回的信息如新 chatID
	}
	// StreamMsg 流式消息
	StreamMsg struct {
		Content    string
		IsFinal    bool
		IsThinking bool
	}
	// StatusMsg 状态更新消息
	StatusMsg struct {
		Status string
	}
	// LogMsg 日志消息
	LogMsg struct {
		Level   string
		Source  string
		Message string
	}
	// ChatEventMsg 聊天事件消息
	ChatEventMsg struct {
		AgentName string                 // Agent 名称
		State     string                 // delta, thinking, tool, final, error, interrupt
		Content   string                 // 事件内容
		Metadata  map[string]interface{} // 元数据
	}
)

// Model TUI模型
type Model struct {
	ctx        context.Context
	cfg        *Config
	manager    *agent.Manager
	bus        *bus.MessageBus
	sessionMgr *session.Manager
	chatID     string

	// UI组件
	input        textinput.Model
	chatViewport viewport.Model // 左侧主区域：对话内容
	logViewport  viewport.Model // 右侧小块：系统日志
	messages     []Message
	messageMu    sync.Mutex
	logs         []string // 日志信息
	logMu        sync.Mutex
	logSub       *bus.LogEventSubscription
	chatSub      *bus.ChatEventSubscription

	// 状态
	state  SessionState
	status string
	width  int
	height int
	ready  bool
	err    error

	// 当前对话
	currentUserMsg   string
	currentAIMsg     string
	currentThinking  string // 当前思考内容（流式）
	currentToolInfo  string // 当前工具调用信息
	currentAgentName string // 当前处理的 agent 名称
	isThinkingLogged bool   // 是否已记录思考日志
	isFinalLogged    bool   // 是否已记录完成日志

	// 中断确认
	interruptType    string // 当前中断类型 (yes_no, select)
	interruptContent string // 中断提示内容

	// 样式
	styles Styles
}

// Message 聊天消息
type Message struct {
	Role            string // "user" 或 "assistant"
	Content         string
	ThinkingContent string // 思考/推理内容
	Timestamp       time.Time
}

// NewModel 创建新模型
func NewModel(ctx context.Context, cfg *Config) (*Model, error) {
	var manager *agent.Manager
	var workspace string

	// 创建消息总线
	msgBus := bus.NewMessageBus(10)

	// 设置 slog 默认 logger 使用 bus handler
	bus.SetupDefaultLogger(msgBus, slog.LevelDebug, bus.ChannelTUI)

	if cfg.AppConfig != nil && len(cfg.AppConfig.Agents) > 0 {
		// 多 agent 模式：从配置文件自动注册所有 agent
		workspace = cfg.Workspace
		if workspace == "" {
			// 使用默认 agent 的 workspace
			defaultName := cfg.AppConfig.GetDefaultAgentName()
			if resolved, err := cfg.AppConfig.ResolveAgentConfig(defaultName); err == nil {
				workspace = resolved.Workspace
			}
		}

		// 创建会话管理器
		sessionMgr, err := session.NewManager(workspace)
		if err != nil {
			return nil, fmt.Errorf("failed to create session manager: %w", err)
		}

		// 创建 Manager
		manager = agent.NewManager(msgBus, sessionMgr)

		// 自动注册所有 agent
		if err := manager.RegisterAgentsFromConfig(ctx, cfg.AppConfig, nil); err != nil {
			return nil, fmt.Errorf("failed to register agents from config: %w", err)
		}

	} else {
		// 单 agent 模式：手动创建单个 agent
		workspace = cfg.Workspace
		if workspace == "" {
			workspace = "."
		}

		// 创建LLM
		llm, err := providers.NewOpenAI(ctx, cfg.APIBaseURL, cfg.Model, cfg.APIKey)
		if err != nil {
			return nil, fmt.Errorf("failed to create LLM: %w", err)
		}

		// 创建工具注册器
		toolRegistry := tools.NewRegistry()

		// 获取默认 skills 目录
		skillDirs := config.GetDefaultSkillDirs(workspace)

		// 创建Agent配置
		agentCfg := &agent.Config{
			LLM:          llm,
			Workspace:    workspace,
			MaxIteration: cfg.MaxIteration,
			ToolRegister: toolRegistry,
			SkillDirs:    skillDirs,
			Streaming:    true,
		}

		// 创建Agent
		ag, err := agent.NewAgent(ctx, agentCfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create agent: %w", err)
		}

		// 创建会话管理器
		sessionMgr, err := session.NewManager(workspace)
		if err != nil {
			return nil, fmt.Errorf("failed to create session manager: %w", err)
		}

		// 创建Agent管理器
		manager = agent.NewManager(msgBus, sessionMgr)
		manager.RegisterAgent("default", ag, true)
	}

	// 启动 Manager
	manager.Start(ctx)

	// 创建输入框
	ti := textinput.New()
	ti.Placeholder = "输入消息..."
	ti.Focus()
	ti.Width = 50

	// 创建样式
	styles := NewStyles()

	// 创建左侧主viewport（对话内容）
	chatVP := viewport.New(60, 20)
	chatVP.SetContent("")
	chatVP.Style = styles.ChatViewport

	// 创建右侧小块viewport（系统日志）
	logVP := viewport.New(20, 20)
	logVP.SetContent(" [系统日志]")
	logVP.Style = styles.LogViewport
	chatID := strings.ToUpper(uuid.NewString())
	return &Model{
		ctx:             ctx,
		cfg:             cfg,
		manager:         manager,
		bus:             msgBus,
		sessionMgr:      manager.GetSessionManager(),
		chatID:          chatID,
		input:           ti,
		chatViewport:    chatVP,
		logViewport:     logVP,
		messages:        make([]Message, 0),
		logs:            make([]string, 0),
		logSub:          msgBus.SubscribeLogEvent(),
		chatSub:         msgBus.SubscribeChatEventFiltered([]string{bus.ChannelTUI}),
		state:           StateIdle,
		status:          "就绪",
		styles:          styles,
		currentThinking: "",
		currentToolInfo: "",
	}, nil
}

// Init 初始化
func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		textinput.Blink,
		m.waitForResponse(),
		m.listenLogs(),
		m.listenChatEvents(),
	)
}

// listenLogs 监听日志事件
func (m *Model) listenLogs() tea.Cmd {
	return func() tea.Msg {
		for {
			select {
			case <-m.ctx.Done():
				return nil
			case event, ok := <-m.logSub.Channel:
				if !ok {
					return nil
				}
				return LogMsg{
					Level:   event.Level,
					Source:  event.Source,
					Message: event.Message,
				}
			}
		}
	}
}

// listenChatEvents 监听聊天事件
func (m *Model) listenChatEvents() tea.Cmd {
	return func() tea.Msg {
		for {
			select {
			case <-m.ctx.Done():
				return nil
			case event, ok := <-m.chatSub.Channel:
				if !ok {
					return nil
				}
				// 只处理当前 chatID 的事件
				if event.ChatID != m.chatID {
					continue
				}
				// 转换 Metadata
				var metadata map[string]interface{}
				if event.Metadata != nil {
					if md, ok := event.Metadata.(map[string]interface{}); ok {
						metadata = md
					}
				}
				return ChatEventMsg{
					AgentName: event.AgentName,
					State:     event.State,
					Content:   event.Content,
					Metadata:  metadata,
				}
			}
		}
	}
}

// Update 更新
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			// 如果在等待确认状态，ESC 取消确认
			if m.state == StateWaitingApproval {
				m.state = StateIdle
				m.status = "就绪"
				m.interruptType = ""
				m.interruptContent = ""
				m.updateViewports()
				return m, nil
			}
			return m, tea.Quit
		case tea.KeyEnter:
			if m.state == StateIdle && m.input.Value() != "" {
				return m, m.sendMessage()
			}
		case tea.KeyUp:
			m.logViewport.ScrollUp(1)
			m.chatViewport.ScrollUp(1)
		case tea.KeyDown:
			m.logViewport.ScrollDown(1)
			m.chatViewport.ScrollDown(1)
		case tea.KeyLeft:
			m.logViewport.ScrollLeft(1)
			m.chatViewport.ScrollLeft(1)
		case tea.KeyRight:
			m.logViewport.ScrollRight(1)
			m.chatViewport.ScrollRight(1)
		default:
			// 处理 Y/N 键确认
			if m.state == StateWaitingApproval && m.interruptType == bus.InterruptTypeYesNo {
				key := strings.ToLower(msg.String())
				if key == "y" || key == "n" {
					return m, m.sendApproval(key == "y")
				}
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		// 计算布局：左侧72%，右侧28%
		chatWidth := int(float64(msg.Width) * 0.72)
		logWidth := msg.Width - chatWidth - 3
		if logWidth < 25 {
			logWidth = 25
		}

		m.input.Width = chatWidth - 10

		// 调整viewport高度，留出输入框和状态栏空间
		vpHeight := msg.Height - 8
		if vpHeight < 5 {
			vpHeight = 5
		}

		m.chatViewport.Height = vpHeight
		m.chatViewport.Width = chatWidth

		m.logViewport.Height = vpHeight
		m.logViewport.Width = logWidth

		if !m.ready {
			m.ready = true
		}

	case AgentResponseMsg:
		// 如果当前处于等待确认状态，不要重置状态
		// 中断时 manager 会发送 OutboundMessage，但我们已经通过 ChatEventMsg 处理了
		if m.state == StateWaitingApproval {
			return m, m.waitForResponse()
		}
		m.state = StateIdle
		m.status = "就绪"
		// 使用流式累积的思考内容，或从响应中获取
		thinkingContent := m.currentThinking
		if thinkingContent == "" && msg.ReasoningContent != "" {
			thinkingContent = msg.ReasoningContent
		}
		m.currentThinking = ""
		m.currentToolInfo = ""

		// 检查错误（msg.Error 或 metadata 中的 error）
		var errMsg string
		if msg.Error != nil {
			errMsg = msg.Error.Error()
		} else if msg.Metadata != nil {
			if errStr, ok := msg.Metadata["error"].(string); ok && errStr != "" {
				errMsg = errStr
			}
		}

		if errMsg != "" {
			m.currentAIMsg = fmt.Sprintf("错误: %s", errMsg)
			m.addMessage("assistant", m.currentAIMsg, "")
			m.addLog("error", errMsg)
		} else {
			m.currentAIMsg = msg.Content
			m.addMessage("assistant", msg.Content, thinkingContent)
		}

		// 处理元数据，如新 chatID
		if msg.Metadata != nil {
			if newChatID, ok := msg.Metadata[agent.MetadataKeyNewChatID].(string); ok && newChatID != "" {
				m.chatID = newChatID
				m.addLog("info", fmt.Sprintf("切换到新会话: %s", newChatID))
			}
		}
		m.updateViewports()
		return m, m.waitForResponse()

	case StreamMsg:
		if msg.IsThinking {
			m.status = "思考中..."
		} else if msg.IsFinal {
			m.status = "完成"
		} else {
			m.status = "生成中..."
		}
		m.updateViewports()
		return m, nil

	case StatusMsg:
		m.status = msg.Status
		return m, nil

	case LogMsg:
		m.addLog(msg.Level, fmt.Sprintf("%s: %s", msg.Source, msg.Message))
		m.updateViewports()
		return m, m.listenLogs()

	case ChatEventMsg:
		// 更新当前 agent 名称
		if msg.AgentName != "" {
			m.currentAgentName = msg.AgentName
		}

		switch msg.State {
		case bus.ChatEventStateThinking:
			m.status = fmt.Sprintf("[%s] 思考中...", m.currentAgentName)
			m.currentThinking += msg.Content // 累加思考内容
			// 只在第一次思考时记录日志
			if !m.isThinkingLogged {
				m.addLog("debug", fmt.Sprintf("[%s] 思考中", m.currentAgentName))
				m.isThinkingLogged = true
			}
		case bus.ChatEventStateDelta:
			m.status = fmt.Sprintf("[%s] 生成中...", m.currentAgentName)
			m.currentAIMsg += msg.Content
		case bus.ChatEventStateTool:
			m.status = fmt.Sprintf("[%s] 工具调用...", m.currentAgentName)
			m.currentToolInfo = msg.Content
			m.addLog("info", fmt.Sprintf("[%s] 工具: %s", m.currentAgentName, msg.Content))
		case bus.ChatEventStateFinal:
			m.status = fmt.Sprintf("[%s] 完成", m.currentAgentName)
			m.currentAIMsg = msg.Content
			// 只在第一次完成时记录日志
			if !m.isFinalLogged {
				m.addLog("info", fmt.Sprintf("[%s] 响应完成", m.currentAgentName))
				m.isFinalLogged = true
			}
		case bus.ChatEventStateError:
			m.status = fmt.Sprintf("[%s] 错误", m.currentAgentName)
			m.currentAIMsg = fmt.Sprintf("错误: %s", msg.Content)
			m.addLog("error", fmt.Sprintf("[%s] 错误: %s", m.currentAgentName, msg.Content))
		case bus.ChatEventStateInterrupt:
			m.status = fmt.Sprintf("[%s] 等待确认", m.currentAgentName)
			m.currentToolInfo = msg.Content
			m.addLog("warn", fmt.Sprintf("[%s] 等待确认: %s", m.currentAgentName, msg.Content))
			// 检查中断类型
			if msg.Metadata != nil {
				if interruptType, ok := msg.Metadata["interrupt_type"].(string); ok {
					m.interruptType = interruptType
					m.interruptContent = msg.Content
					// yes_no 类型进入等待确认状态
					if interruptType == bus.InterruptTypeYesNo {
						m.state = StateWaitingApproval
					}
				}
			}
		}
		m.updateViewports()
		return m, m.listenChatEvents()
	}

	// 更新输入框
	var inputCmd tea.Cmd
	m.input, inputCmd = m.input.Update(msg)
	cmds = append(cmds, inputCmd)

	// 更新viewport
	var chatCmd, logCmd tea.Cmd
	m.chatViewport, chatCmd = m.chatViewport.Update(msg)
	m.logViewport, logCmd = m.logViewport.Update(msg)
	cmds = append(cmds, chatCmd, logCmd)

	return m, tea.Batch(cmds...)
}

// View 渲染视图
func (m *Model) View() string {
	if !m.ready {
		return "\n  正在初始化..."
	}

	// 标题栏
	title := m.styles.Title.Render(" KanFlux AI Agent ")
	titleBar := lipgloss.NewStyle().
		Width(m.width).
		Background(lipgloss.Color("62")).
		Render(title)

	// 状态栏
	statusBar := m.styles.Status.Render(fmt.Sprintf(" [%s] ", m.status))
	if m.state == StateProcessing {
		statusBar = m.styles.StatusProcessing.Render(fmt.Sprintf(" [%s] ", m.status))
	} else if m.state == StateWaitingApproval {
		statusBar = m.styles.StatusProcessing.Render(fmt.Sprintf(" [%s] ", m.status))
	}

	// 左侧：对话 | 右侧：系统日志
	chatArea := m.styles.ChatBox.Render(m.chatViewport.View())
	logArea := m.styles.LogBox.Render(m.logViewport.View())

	// 左右并排布局
	contentRow := lipgloss.JoinHorizontal(lipgloss.Top, chatArea, " ", logArea)

	// 输入框/确认框
	var inputBox, help string
	if m.state == StateWaitingApproval && m.interruptType == bus.InterruptTypeYesNo {
		// 显示确认框
		confirmBox := m.styles.ConfirmBox.Render(fmt.Sprintf("\n  %s\n\n  按 Y 确认 / 按 N 拒绝 / ESC 取消\n", m.interruptContent))
		inputBox = confirmBox
		help = m.styles.Help.Render(" Y=确认 | N=拒绝 | ESC=取消")
	} else {
		inputBox = m.styles.Input.Render(" 输入: " + m.input.View())
		help = m.styles.Help.Render(" Ctrl+C退出 | Enter发送 | ↑↓对话 | ←→日志 | /help 查看命令")
	}

	// 组合界面
	return lipgloss.JoinVertical(lipgloss.Left,
		titleBar,
		statusBar,
		"",
		contentRow,
		"",
		inputBox,
		help,
	)
}

// sendMessage 发送消息
func (m *Model) sendMessage() tea.Cmd {
	content := m.input.Value()
	m.input.SetValue("")
	m.state = StateProcessing
	m.status = "处理中..."

	// 设置当前用户消息
	m.currentUserMsg = content
	m.currentAIMsg = ""
	m.currentThinking = ""
	m.currentToolInfo = ""
	m.currentAgentName = ""    // 重置 agent 名称，等待 ChatEvent 更新
	m.isThinkingLogged = false // 重置思考日志标志
	m.isFinalLogged = false    // 重置完成日志标志

	// 添加用户消息
	m.addMessage("user", content, "")
	m.addLog("info", "发送: "+content)
	m.updateViewports()

	// 发送到Agent
	return func() tea.Msg {
		inMsg := bus.InboundMessage{
			ID:        fmt.Sprintf("%d", time.Now().UnixNano()),
			Channel:   bus.ChannelTUI,
			AccountID: bus.ChannelTUI,
			SenderID:  "",
			ChatID:    m.chatID,
			Content:   content,
			Media:     nil,
			Metadata:  nil,
			Timestamp: time.Now(),
		}

		err := m.bus.PublishInbound(m.ctx, &inMsg)
		if err != nil {
			return AgentResponseMsg{Error: err}
		}

		resp, err := m.bus.ConsumeOutboundFiltered(m.ctx, []string{bus.ChannelTUI})
		if err != nil {
			return AgentResponseMsg{Error: err}
		}

		return AgentResponseMsg{Content: resp.Content, ReasoningContent: resp.ReasoningContent, Metadata: resp.Metadata}
	}
}

// sendApproval 发送审批确认（Y/N）
func (m *Model) sendApproval(approved bool) tea.Cmd {
	content := "Y"
	if !approved {
		content = "N"
	}

	// 重置状态
	m.state = StateProcessing
	m.status = "处理中..."
	m.interruptType = ""
	m.interruptContent = ""

	// 添加用户响应日志
	m.addLog("info", fmt.Sprintf("审批响应: %s", content))
	m.updateViewports()

	// 发送到Agent
	return func() tea.Msg {
		inMsg := bus.InboundMessage{
			ID:        fmt.Sprintf("%d", time.Now().UnixNano()),
			Channel:   bus.ChannelTUI,
			AccountID: bus.ChannelTUI,
			SenderID:  "",
			ChatID:    m.chatID,
			Content:   content,
			Media:     nil,
			Metadata:  nil,
			Timestamp: time.Now(),
		}

		err := m.bus.PublishInbound(m.ctx, &inMsg)
		if err != nil {
			return AgentResponseMsg{Error: err}
		}

		resp, err := m.bus.ConsumeOutboundFiltered(m.ctx, []string{bus.ChannelTUI})
		if err != nil {
			return AgentResponseMsg{Error: err}
		}

		return AgentResponseMsg{Content: resp.Content, ReasoningContent: resp.ReasoningContent, Metadata: resp.Metadata}
	}
}

// waitForResponse 等待响应
func (m *Model) waitForResponse() tea.Cmd {
	return func() tea.Msg {
		return nil
	}
}

// addMessage 添加消息
func (m *Model) addMessage(role, content, thinkingContent string) {
	m.messageMu.Lock()
	defer m.messageMu.Unlock()

	m.messages = append(m.messages, Message{
		Role:            role,
		Content:         content,
		ThinkingContent: thinkingContent,
		Timestamp:       time.Now(),
	})
}

// addLog 添加日志
func (m *Model) addLog(level, log string) {
	m.logMu.Lock()
	defer m.logMu.Unlock()

	timestamp := time.Now().Format("15:04:05")
	m.logs = append(m.logs, fmt.Sprintf("[%s] [%s] %s", timestamp, level, log))

	// 限制日志数量
	if len(m.logs) > 100 {
		m.logs = m.logs[len(m.logs)-100:]
	}
}

// updateViewports 更新两个viewport内容
func (m *Model) updateViewports() {
	// 左侧：对话内容
	m.messageMu.Lock()
	var chatContent strings.Builder
	chatContent.WriteString(" [对话]\n\n")

	// 显示最近的消息（最多20条）
	start := 0
	if len(m.messages) > 20 {
		start = len(m.messages) - 20
	}

	for _, msg := range m.messages[start:] {
		var styledContent string
		switch msg.Role {
		case "user":
			styledContent = m.styles.UserMessage.Render(" 你: " + msg.Content)
			chatContent.WriteString(styledContent + "\n\n") // 用户消息后加空行
		case "assistant":
			// 先显示思考内容（如果有），与AI消息紧挨着
			if msg.ThinkingContent != "" {
				chatContent.WriteString(m.styles.ThinkingMessage.Render(" [思考] "+msg.ThinkingContent) + "\n")
			}
			styledContent = m.styles.AssistantMessage.Render(" AI: " + msg.Content)
			chatContent.WriteString(styledContent + "\n\n") // AI消息后加空行，分隔两次对话
		default:
			styledContent = msg.Content
			chatContent.WriteString(styledContent + "\n")
		}
	}

	// 显示当前流式内容（如果有）
	if m.state == StateProcessing {
		// 显示思考内容
		if m.currentThinking != "" {
			chatContent.WriteString(m.styles.ThinkingMessage.Render(" [思考] "+m.currentThinking) + "\n")
		}
		// 显示工具调用信息
		if m.currentToolInfo != "" {
			chatContent.WriteString(m.styles.ToolMessage.Render(" [工具] "+m.currentToolInfo) + "\n")
		}
		// 显示当前生成的回复（增量）
		if m.currentAIMsg != "" {
			chatContent.WriteString(m.styles.AssistantMessage.Render(" AI: "+m.currentAIMsg) + "\n")
		}
	}

	m.messageMu.Unlock()

	m.chatViewport.SetContent(chatContent.String())
	m.chatViewport.GotoBottom()

	// 右侧：系统日志
	m.logMu.Lock()
	var logContent strings.Builder
	logContent.WriteString(" [系统日志]\n\n")
	for _, log := range m.logs {
		// 根据日志级别设置颜色
		if strings.Contains(log, "[error]") {
			logContent.WriteString(m.styles.LogError.Render(log) + "\n")
		} else if strings.Contains(log, "[warn]") {
			logContent.WriteString(m.styles.LogWarn.Render(log) + "\n")
		} else if strings.Contains(log, "[debug]") {
			logContent.WriteString(m.styles.LogDebug.Render(log) + "\n")
		} else {
			logContent.WriteString(m.styles.LogInfo.Render(log) + "\n")
		}
	}
	m.logMu.Unlock()

	m.logViewport.SetContent(logContent.String())
	m.logViewport.GotoBottom()
}
