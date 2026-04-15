package tui

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/kinwyb/kanflux/agent"
	"github.com/kinwyb/kanflux/bus"
	"github.com/kinwyb/kanflux/channel"
	"github.com/kinwyb/kanflux/config"
	"github.com/kinwyb/kanflux/session"
	"github.com/kinwyb/kanflux/ws"

	"github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
)

// Config TUI配置
type Config struct {
	// 单 agent 模式（无配置文件时使用）
	Workspace    string
	Model        string
	APIKey       string
	APIBaseURL   string
	MaxIteration int

	// 多 agent 模式（有配置文件时使用）
	AppConfig    *config.Config // 完整配置
	DefaultAgent string         // 默认 agent 名称

	// WebSocket 配置
	WSConfig   *ws.ServerConfig // WebSocket 服务配置（用于启动本地服务）
	GatewayURL string           // 外部 Gateway URL（可选，用于连接远程 Gateway）
}

// App TUI应用
type App struct {
	program  *tea.Program
	model    *Model

	// WebSocket 客户端
	wsClient *ws.Client

	// 服务实例（当 TUI 自己启动时持有）
	service  *Service
}

// Service 封装完整服务
type Service struct {
	Bus         *bus.MessageBus
	AgentMgr    *agent.Manager
	ChannelMgr  *channel.Manager
	WSServer    *ws.Server
	SessionMgr  *session.Manager
}

// NewApp 创建TUI应用
func NewApp(ctx context.Context, cfg *Config) (*App, error) {
	// 如果API密钥未提供，尝试从环境变量获取
	if cfg.APIKey == "" {
		cfg.APIKey = os.Getenv("OPENAI_API_KEY")
	}

	// 设置默认 WebSocket 配置
	if cfg.WSConfig == nil {
		cfg.WSConfig = &ws.ServerConfig{Enabled: true}
	}
	cfg.WSConfig.SetDefaults()

	// 1. 检测 WebSocket 是否已运行
	detector := ws.NewDetector(cfg.WSConfig)

	var wsClient *ws.Client
	var service *Service
	var err error

	if cfg.GatewayURL != "" {
		// 指定了外部 Gateway，直接连接
		slog.Info("连接外部 Gateway", "url", cfg.GatewayURL)
		clientCfg := &ws.ClientConfig{URL: cfg.GatewayURL}
		wsClient = ws.NewClient(clientCfg)
		err = wsClient.Connect(ctx)
	} else if detector.IsRunning() {
		// WebSocket 已运行，直接连接
		slog.Info("检测到已运行的 WebSocket 服务，直接连接")
		wsClient, err = detector.TryConnect(ctx)
	} else {
		// WebSocket 未运行，启动完整服务
		slog.Info("WebSocket 服务未运行，启动本地服务")
		service, err = StartService(ctx, cfg)
		if err != nil {
			return nil, fmt.Errorf("启动本地服务失败: %w", err)
		}

		// 等待服务启动
		if !detector.WaitForRunning(ctx, 2*time.Second) {
			return nil, fmt.Errorf("WebSocket 服务启动超时")
		}

		// 连接本地 WebSocket
		wsClient, err = detector.TryConnect(ctx)
	}

	if err != nil {
		return nil, fmt.Errorf("连接 WebSocket 失败: %w", err)
	}

	slog.Info("WebSocket 连接成功")

	// 2. 创建 Model（使用 WebSocket 客户端）
	model, err := NewModel(ctx, cfg, wsClient)
	if err != nil {
		wsClient.Close()
		if service != nil {
			StopService(service)
		}
		return nil, err
	}

	// 3. 设置 WebSocket 回调
	var mu sync.Mutex
	var pendingOutbound []*bus.OutboundMessage
	var pendingEvents []*bus.ChatEvent

	wsClient.SetOnOutbound(func(payload *ws.OutboundPayload) {
		msg := convertPayloadToOutbound(payload)
		mu.Lock()
		if model.IsReady() {
			model.ReceiveOutbound(msg)
		} else {
			pendingOutbound = append(pendingOutbound, msg)
		}
		mu.Unlock()
	})

	wsClient.SetOnChatEvent(func(payload *ws.ChatEventPayload) {
		event := convertPayloadToChatEvent(payload)
		mu.Lock()
		if model.IsReady() {
			model.ReceiveChatEvent(event)
		} else {
			pendingEvents = append(pendingEvents, event)
		}
		mu.Unlock()
	})

	// 设置日志事件回调
	wsClient.SetOnLogEvent(func(payload *ws.LogEventPayload) {
		event := convertPayloadToLogEvent(payload)
		mu.Lock()
		if model.IsReady() {
			model.ReceiveLogEvent(event)
		}
		mu.Unlock()
	})

	// 4. 订阅当前 channel/chatID
	wsClient.Subscribe([]string{bus.ChannelTUI}, []string{model.GetChatID()})

	// 5. 创建 BubbleTea Program
	p := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	// 等待 model ready 后处理 pending 消息
	go func() {
		time.Sleep(100 * time.Millisecond)
		mu.Lock()
		for _, msg := range pendingOutbound {
			model.ReceiveOutbound(msg)
		}
		for _, event := range pendingEvents {
			model.ReceiveChatEvent(event)
		}
		pendingOutbound = nil
		pendingEvents = nil
		mu.Unlock()
	}()

	return &App{
		program:  p,
		model:    model,
		wsClient: wsClient,
		service:  service,
	}, nil
}

// StartService 启动完整服务
func StartService(ctx context.Context, cfg *Config) (*Service, error) {
	// 确定工作目录
	workspace := cfg.Workspace
	if workspace == "" && cfg.AppConfig != nil {
		defaultAgent := cfg.AppConfig.GetDefaultAgentName()
		if resolved, err := cfg.AppConfig.ResolveAgentConfig(defaultAgent); err == nil {
			workspace = resolved.Workspace
		}
	}
	if workspace == "" {
		workspace = "."
	}

	// 创建 MessageBus
	msgBus := bus.NewMessageBus(100)
	slog.Debug("MessageBus 创建完成")

	// 设置 slog 输出到 bus，以便日志能通过 WebSocket 广播
	bus.SetupDefaultLogger(msgBus, slog.LevelDebug, "tui-service")
	slog.Debug("slog 输出到 bus 设置完成")

	// 创建 SessionManager
	sessionMgr, err := session.NewManager(workspace)
	if err != nil {
		return nil, fmt.Errorf("创建 SessionManager 失败: %w", err)
	}
	slog.Debug("SessionManager 创建完成")

	// 创建 AgentManager
	agentMgr := agent.NewManager(msgBus, sessionMgr)

	// 注册 Agents
	if cfg.AppConfig != nil && len(cfg.AppConfig.Agents) > 0 {
		if err := agentMgr.RegisterAgentsFromConfig(ctx, cfg.AppConfig, nil); err != nil {
			return nil, fmt.Errorf("注册 Agents 失败: %w", err)
		}
		slog.Debug("Agents 注册完成", "count", len(agentMgr.ListAgents()))
	} else {
		// 单 agent 模式
		agentCfg := &config.AgentConfig{
			Name:         "default",
			Workspace:    workspace,
			Model:        cfg.Model,
			MaxIteration: cfg.MaxIteration,
			Streaming:    true,
		}
		if cfg.APIKey != "" {
			agentCfg.Provider = "openai"
		}

		// 创建默认 provider（如果需要）
		if cfg.AppConfig == nil {
			cfg.AppConfig = &config.Config{
				Providers: map[string]*config.ProviderConfig{
					"openai": {
						APIKey:       cfg.APIKey,
						APIBaseURL:   cfg.APIBaseURL,
						DefaultModel: cfg.Model,
					},
				},
				DefaultProvider: "openai",
				Agents:          []*config.AgentConfig{agentCfg},
			}
		}

		if err := agentMgr.RegisterAgentsFromConfig(ctx, cfg.AppConfig, nil); err != nil {
			return nil, fmt.Errorf("注册默认 Agent 失败: %w", err)
		}
		slog.Debug("默认 Agent 注册完成")
	}

	// 启动 AgentManager
	if err := agentMgr.Start(ctx); err != nil {
		return nil, fmt.Errorf("启动 AgentManager 失败: %w", err)
	}
	slog.Debug("AgentManager 启动完成")

	// 创建 ChannelManager
	channelMgr := channel.NewManager(msgBus)

	// 初始化 Channels
	if cfg.AppConfig != nil && cfg.AppConfig.Channels != nil {
		if err := channelMgr.InitializeFromConfig(ctx, cfg.AppConfig.Channels); err != nil {
			slog.Warn("初始化部分 Channel 失败", "error", err)
		}
		slog.Debug("Channels 初始化完成", "count", channelMgr.ChannelCount())
	}

	// 启动 ChannelManager
	if err := channelMgr.StartAll(ctx); err != nil {
		agentMgr.Stop()
		return nil, fmt.Errorf("启动 ChannelManager 失败: %w", err)
	}
	slog.Debug("ChannelManager 启动完成")

	// 创建 WebSocket Server
	wsServer := ws.NewServer(msgBus, cfg.WSConfig)
	if err := wsServer.Start(ctx); err != nil {
		channelMgr.StopAll()
		agentMgr.Stop()
		return nil, fmt.Errorf("启动 WebSocket Server 失败: %w", err)
	}
	slog.Debug("WebSocket Server 启动完成", "url", cfg.WSConfig.URL())

	return &Service{
		Bus:         msgBus,
		AgentMgr:    agentMgr,
		ChannelMgr:  channelMgr,
		WSServer:    wsServer,
		SessionMgr:  sessionMgr,
	}, nil
}

// StopService 停止服务
func StopService(s *Service) {
	if s == nil {
		return
	}

	slog.Debug("开始停止服务")

	// 停止 WebSocket Server
	if s.WSServer != nil {
		s.WSServer.Stop()
		slog.Debug("WebSocket Server 已停止")
	}

	// 停止 ChannelManager
	if s.ChannelMgr != nil {
		s.ChannelMgr.StopAll()
		slog.Debug("ChannelManager 已停止")
	}

	// 停止 AgentManager
	if s.AgentMgr != nil {
		s.AgentMgr.Stop()
		slog.Debug("AgentManager 已停止")
	}

	// 关闭 MessageBus
	if s.Bus != nil {
		s.Bus.Close()
		slog.Debug("MessageBus 已关闭")
	}

	slog.Debug("服务停止完成")
}

// Run 运行TUI应用
func (a *App) Run() error {
	_, err := a.program.Run()
	return err
}

// Stop 停止应用
func (a *App) Stop() error {
	// 关闭 WebSocket 客户端
	if a.wsClient != nil {
		a.wsClient.Close()
		slog.Debug("WebSocket 客户端已关闭")
	}

	// 如果是自己启动的服务，需要停止
	if a.service != nil {
		StopService(a.service)
		slog.Debug("本地服务已停止")
	}

	return nil
}

// GetChatID 获取当前 chatID
func (a *App) GetChatID() string {
	return a.model.GetChatID()
}

// 内部转换函数

func convertPayloadToOutbound(p *ws.OutboundPayload) *bus.OutboundMessage {
	return &bus.OutboundMessage{
		ID:               p.ID,
		Channel:          p.Channel,
		ChatID:           p.ChatID,
		Content:          p.Content,
		ReasoningContent: p.ReasoningContent,
		Media:            convertPayloadMediaToBus(p.Media),
		ReplyTo:          p.ReplyTo,
		IsStreaming:      p.IsStreaming,
		IsThinking:       p.IsThinking,
		IsFinal:          p.IsFinal,
		ChunkIndex:       p.ChunkIndex,
		Error:            p.Error,
		Metadata:         p.Metadata,
	}
}

func convertPayloadToChatEvent(p *ws.ChatEventPayload) *bus.ChatEvent {
	return &bus.ChatEvent{
		ID:        p.ID,
		Channel:   p.Channel,
		ChatID:    p.ChatID,
		RunID:     p.RunID,
		Seq:       p.Seq,
		AgentName: p.AgentName,
		State:     p.State,
		Error:     p.Error,
		ToolInfo:  convertPayloadToToolInfo(p.ToolInfo),
		Metadata:  p.Metadata,
	}
}

func convertPayloadToToolInfo(p *ws.ToolEventInfoPayload) *bus.ToolEventInfo {
	if p == nil {
		return nil
	}
	return &bus.ToolEventInfo{
		Name:      p.Name,
		ID:        p.ID,
		Arguments: p.Arguments,
		Result:    p.Result,
		IsStart:   p.IsStart,
	}
}

func convertPayloadToLogEvent(p *ws.LogEventPayload) *bus.LogEvent {
	return &bus.LogEvent{
		ID:      p.ID,
		Level:   p.Level,
		Message: p.Message,
		Source:  p.Source,
	}
}

func convertPayloadMediaToBus(media []ws.MediaPayload) []bus.Media {
	if media == nil {
		return nil
	}
	result := make([]bus.Media, len(media))
	for i, m := range media {
		result[i] = bus.Media{
			Type:     m.Type,
			URL:      m.URL,
			Base64:   m.Base64,
			MimeType: m.MimeType,
			Metadata: m.Metadata,
		}
	}
	return result
}

// generateChatID 生成唯一的 chatID
func generateChatID() string {
	return uuid.New().String()[:8]
}

// getWorkspaceFromConfig 从配置获取工作目录
func getWorkspaceFromConfig(cfg *Config) string {
	if cfg.Workspace != "" {
		return cfg.Workspace
	}
	if cfg.AppConfig != nil {
		defaultAgent := cfg.AppConfig.GetDefaultAgentName()
		if resolved, err := cfg.AppConfig.ResolveAgentConfig(defaultAgent); err == nil {
			return resolved.Workspace
		}
	}
	return "."
}