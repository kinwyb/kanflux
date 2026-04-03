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
	StateWaitingApproval
)

// 消息类型
type (
	// AgentResponseMsg Agent响应消息
	AgentResponseMsg struct {
		Content string
		Error   error
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
		State   string // delta, thinking, tool, final, error, interrupt
		Content string
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
	currentUserMsg  string
	currentAIMsg    string
	currentThinking string // 当前思考内容（流式）
	currentToolInfo string // 当前工具调用信息

	// 样式
	styles Styles
}

// Message 聊天消息
type Message struct {
	Role       string // "user" 或 "assistant"
	Content    string
	Timestamp  time.Time
	IsThinking bool
}

// NewModel 创建新模型
func NewModel(ctx context.Context, cfg *Config) (*Model, error) {
	// 创建LLM
	llm, err := providers.NewOpenAI(ctx, cfg.APIBaseURL, cfg.Model, cfg.APIKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create LLM: %w", err)
	}

	// 创建工具注册器
	toolRegistry := tools.NewRegistry()
	toolRegistry.NeedApprove("read_file")
	toolRegistry.NeedApprove("write_file")
	toolRegistry.NeedApprove("ls")

	// 创建Agent配置
	agentCfg := &agent.Config{
		LLM:          llm,
		Workspace:    cfg.Workspace,
		MaxIteration: cfg.MaxIteration,
		ToolRegister: toolRegistry,
		Streaming:    true,
	}

	// 创建Agent
	ag, err := agent.NewAgent(ctx, agentCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create agent: %w", err)
	}

	// 创建会话管理器
	sessionMgr, err := session.NewManager(cfg.Workspace)
	if err != nil {
		return nil, fmt.Errorf("failed to create session manager: %w", err)
	}

	// 创建消息总线
	msgBus := bus.NewMessageBus(10)

	// 设置 slog 默认 logger 使用 bus handler
	bus.SetupDefaultLogger(msgBus, slog.LevelDebug, bus.ChannelTUI)

	// 创建Agent管理器
	manager := agent.NewManager(msgBus, sessionMgr)
	manager.RegisterAgent("default", ag, true)
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
		sessionMgr:      sessionMgr,
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
				return ChatEventMsg{
					State:   event.State,
					Content: event.Content,
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
			return m, tea.Quit
		case tea.KeyEnter:
			if m.state == StateIdle && m.input.Value() != "" {
				return m, m.sendMessage()
			}
		case tea.KeyUp:
			m.chatViewport.ScrollUp(1)
		case tea.KeyDown:
			m.chatViewport.ScrollDown(1)
		case tea.KeyLeft:
			m.logViewport.ScrollLeft(1)
		case tea.KeyRight:
			m.logViewport.ScrollRight(1)
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
		m.state = StateIdle
		m.status = "就绪"
		m.currentThinking = ""
		m.currentToolInfo = ""
		m.addLog("info", "响应完成")
		if msg.Error != nil {
			m.currentAIMsg = fmt.Sprintf("错误: %v", msg.Error)
			m.addMessage("assistant", m.currentAIMsg, false)
		} else {
			m.currentAIMsg = msg.Content
			m.addMessage("assistant", msg.Content, false)
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
		switch msg.State {
		case bus.ChatEventStateThinking:
			m.status = "思考中..."
			m.currentThinking += msg.Content // 累加思考内容
		case bus.ChatEventStateDelta:
			m.status = "生成中..."
			m.currentAIMsg += msg.Content
		case bus.ChatEventStateTool:
			m.status = "工具调用..."
			m.currentToolInfo = msg.Content
		case bus.ChatEventStateFinal:
			m.status = "完成"
			m.currentAIMsg = msg.Content
		case bus.ChatEventStateError:
			m.status = "错误"
			m.currentAIMsg = fmt.Sprintf("错误: %s", msg.Content)
		case bus.ChatEventStateInterrupt:
			m.status = "等待确认"
			m.currentToolInfo = msg.Content
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
	}

	// 左侧：对话 | 右侧：系统日志
	chatArea := m.styles.ChatBox.Render(m.chatViewport.View())
	logArea := m.styles.LogBox.Render(m.logViewport.View())

	// 左右并排布局
	contentRow := lipgloss.JoinHorizontal(lipgloss.Top, chatArea, " ", logArea)

	// 输入框
	inputBox := m.styles.Input.Render(" 输入: " + m.input.View())

	// 帮助提示
	help := m.styles.Help.Render(" Ctrl+C退出 | Enter发送 | ↑↓对话 | ←→日志")

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

	// 添加用户消息
	m.addMessage("user", content, false)
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

		return AgentResponseMsg{Content: resp.Content}
	}
}

// waitForResponse 等待响应
func (m *Model) waitForResponse() tea.Cmd {
	return func() tea.Msg {
		return nil
	}
}

// addMessage 添加消息
func (m *Model) addMessage(role, content string, isThinking bool) {
	m.messageMu.Lock()
	defer m.messageMu.Unlock()

	m.messages = append(m.messages, Message{
		Role:       role,
		Content:    content,
		Timestamp:  time.Now(),
		IsThinking: isThinking,
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
		case "assistant":
			if msg.IsThinking {
				styledContent = m.styles.ThinkingMessage.Render(" [思考] " + msg.Content)
			} else {
				styledContent = m.styles.AssistantMessage.Render(" AI: " + msg.Content)
			}
		default:
			styledContent = msg.Content
		}
		chatContent.WriteString(styledContent + "\n")
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
