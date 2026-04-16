// Package gateway provides the gateway service for kanflux.
// Gateway service manages agents, channels, and WebSocket connections.
package gateway

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/kinwyb/kanflux/agent"
	"github.com/kinwyb/kanflux/bus"
	"github.com/kinwyb/kanflux/channel"
	"github.com/kinwyb/kanflux/config"
	"github.com/kinwyb/kanflux/gateway/ws"
	"github.com/kinwyb/kanflux/session"
)

// Gateway 后台服务
type Gateway struct {
	cfg          *config.Config
	workspace    string
	msgBus       *bus.MessageBus
	sessionMgr   *session.Manager
	agentMgr     *agent.Manager
	channelMgr   *channel.Manager
	wsServer     *ws.Server
	wsConfig     *ws.ServerConfig

	ctx          context.Context
	cancel       context.CancelFunc

	// 日志文件（需要关闭）
	logFile      *os.File
}

// New 创建 Gateway 实例
func New(cfg *config.Config, workspace string) (*Gateway, error) {
	// 验证配置中有 Agent
	if len(cfg.Agents) == 0 {
		return nil, fmt.Errorf("配置文件中没有定义 Agent")
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Gateway{
		cfg:       cfg,
		workspace: workspace,
		ctx:       ctx,
		cancel:    cancel,
		wsConfig:  resolveWsConfig(cfg),
	}, nil
}

// Start 启动 Gateway 服务
func (g *Gateway) Start(ctx context.Context) error {
	// 1. 检查是否已有 gateway 运行
	detector := ws.NewDetector(g.wsConfig)
	if detector.IsRunning() {
		return fmt.Errorf("Gateway 已在运行中 (WebSocket: %s)", g.wsConfig.URL())
	}

	// 2. 创建 MessageBus
	g.msgBus = bus.NewMessageBus(100)

	// 3. 设置日志
	if err := g.setupLogger(ctx); err != nil {
		return fmt.Errorf("设置日志失败: %w", err)
	}

	slog.Info("配置加载成功", "agents", len(g.cfg.Agents), "channels", g.cfg.Channels != nil)
	slog.Info("工作目录", "path", g.workspace)

	// 4. 创建 SessionManager
	sessionMgr, err := session.NewManager(g.workspace)
	if err != nil {
		return fmt.Errorf("创建 SessionManager 失败: %w", err)
	}
	g.sessionMgr = sessionMgr
	slog.Info("会话管理器创建完成")

	// 5. 创建 AgentManager 并注册 Agents
	g.agentMgr = agent.NewManager(g.msgBus, g.sessionMgr)
	if err := g.agentMgr.RegisterAgentsFromConfig(ctx, g.cfg, nil); err != nil {
		return fmt.Errorf("注册 Agents 失败: %w", err)
	}
	slog.Info("Agents 注册完成", "count", len(g.agentMgr.ListAgents()))

	// 6. 创建 ChannelManager 并初始化 Channels
	g.channelMgr = channel.NewManager(g.msgBus)
	if g.cfg.Channels != nil {
		if err := g.channelMgr.InitializeFromConfig(ctx, g.cfg.Channels); err != nil {
			slog.Warn("初始化部分 Channel 失败", "error", err)
		}
	}
	slog.Info("Channels 初始化完成", "count", g.channelMgr.ChannelCount())

	// 7. 启动 AgentManager
	if err := g.agentMgr.Start(ctx); err != nil {
		return fmt.Errorf("启动 AgentManager 失败: %w", err)
	}
	slog.Info("AgentManager 启动完成")

	// 8. 启动 ChannelManager
	if err := g.channelMgr.StartAll(ctx); err != nil {
		g.cancel()
		g.agentMgr.Stop()
		return fmt.Errorf("启动 ChannelManager 失败: %w", err)
	}
	slog.Info("ChannelManager 启动完成")

	// 9. 创建 WebSocket 服务器
	g.wsServer = ws.NewServer(g.msgBus, g.wsConfig)
	g.wsServer.SetShutdownCallback(func() {
		slog.Info("收到远程关闭请求")
		p, _ := os.FindProcess(os.Getpid())
		p.Signal(syscall.SIGTERM)
	})
	if err := g.wsServer.Start(ctx); err != nil {
		slog.Warn("启动 WebSocket 服务器失败", "error", err)
	} else {
		slog.Info("WebSocket 服务器启动完成", "url", g.wsConfig.URL())
	}

	return nil
}

// Stop 停止 Gateway 服务
func (g *Gateway) Stop() error {
	slog.Info("开始优雅关闭...")

	// 1. 取消上下文
	g.cancel()

	// 2. 停止 WebSocket Server
	if g.wsServer != nil {
		if err := g.wsServer.Stop(); err != nil {
			slog.Warn("停止 WebSocket Server 时出错", "error", err)
		}
		slog.Info("WebSocket Server 已停止")
	}

	// 3. 停止 Channels
	if g.channelMgr != nil {
		if err := g.channelMgr.StopAll(); err != nil {
			slog.Warn("停止 Channels 时出错", "error", err)
		}
		slog.Info("Channels 已停止")
	}

	// 4. 停止 Agents
	if g.agentMgr != nil {
		if err := g.agentMgr.Stop(); err != nil {
			slog.Warn("停止 Agents 时出错", "error", err)
		}
		slog.Info("Agents 已停止")
	}

	// 5. 关闭消息总线
	if g.msgBus != nil {
		if err := g.msgBus.Close(); err != nil {
			slog.Warn("关闭消息总线时出错", "error", err)
		}
		slog.Info("消息总线已关闭")
	}

	// 6. 关闭日志文件
	if g.logFile != nil {
		g.logFile.Close()
	}

	slog.Info("Gateway 关闭完成")
	fmt.Println("Gateway 服务已停止")

	return nil
}

// WaitForSignal 等待退出信号
func (g *Gateway) WaitForSignal() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	slog.Info("Gateway 服务启动完成，等待退出信号...")
	fmt.Println("Gateway 服务已启动，按 Ctrl+C 退出")
	fmt.Printf("WebSocket: %s\n", g.wsConfig.URL())

	select {
	case <-g.ctx.Done():
	case sig := <-sigChan:
		slog.Info("收到退出信号", "signal", sig)
		fmt.Printf("\n收到信号 %v，正在关闭...\n", sig)
	}
}

// GetWsConfig 获取 WebSocket 配置
func (g *Gateway) GetWsConfig() *ws.ServerConfig {
	return g.wsConfig
}

// GetWsServer 获取 WebSocket 服务器
func (g *Gateway) GetWsServer() *ws.Server {
	return g.wsServer
}

// setupLogger 设置日志
func (g *Gateway) setupLogger(ctx context.Context) error {
	var outputHandler slog.Handler

	// 确定日志输出位置
	var logPath string

	if g.cfg.Log != nil && g.cfg.Log.File != "" {
		logPath = g.cfg.Log.File
		if !filepath.IsAbs(logPath) {
			logPath = filepath.Join(g.workspace, logPath)
		}

		// 确保目录存在
		logDir := filepath.Dir(logPath)
		if err := os.MkdirAll(logDir, 0755); err != nil {
			return fmt.Errorf("创建日志目录失败: %w", err)
		}

		// 打开日志文件
		logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return fmt.Errorf("打开日志文件失败: %w", err)
		}
		g.logFile = logFile

		outputHandler = slog.NewTextHandler(g.logFile, &slog.HandlerOptions{
			Level: parseLogLevelWithConfig(g.cfg.Log),
		})
	} else {
		outputHandler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		})
	}

	// 创建 bus handler
	busHandler := newBusLogHandler(g.msgBus, slog.LevelInfo, "gateway")

	// 创建 multi handler
	multiHandler := newMultiHandler(outputHandler, busHandler)

	slog.SetDefault(slog.New(multiHandler))

	if logPath != "" {
		slog.Info("日志系统启动", "file", logPath)
	} else {
		slog.Info("日志系统启动", "output", "stdout")
	}

	return nil
}

// parseLogLevelWithConfig 解析日志级别
func parseLogLevelWithConfig(logCfg *config.LogConfig) slog.Level {
	if logCfg != nil && logCfg.Level != "" {
		switch logCfg.Level {
		case "debug":
			return slog.LevelDebug
		case "info":
			return slog.LevelInfo
		case "warn":
			return slog.LevelWarn
		case "error":
			return slog.LevelError
		}
	}
	return slog.LevelInfo
}

// resolveWsConfig 解析 WebSocket 配置
func resolveWsConfig(cfg *config.Config) *ws.ServerConfig {
	if cfg == nil || cfg.WebSocket == nil {
		return &ws.ServerConfig{Enabled: true}
	}
	return &ws.ServerConfig{
		Enabled:      cfg.WebSocket.Enabled,
		Port:         cfg.WebSocket.Port,
		Host:         cfg.WebSocket.Host,
		Path:         cfg.WebSocket.Path,
		AuthToken:    cfg.WebSocket.AuthToken,
		ReadTimeout:  cfg.WebSocket.ReadTimeout,
		WriteTimeout: cfg.WebSocket.WriteTimeout,
	}
}

// multiHandler 同时输出到多个 handler 的 slog Handler
type multiHandler struct {
	handlers []slog.Handler
}

func newMultiHandler(handlers ...slog.Handler) *multiHandler {
	return &multiHandler{handlers: handlers}
}

func (h *multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (h *multiHandler) Handle(ctx context.Context, r slog.Record) error {
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, r.Level) {
			if err := handler.Handle(ctx, r); err != nil {
				return err
			}
		}
	}
	return nil
}

func (h *multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newHandlers := make([]slog.Handler, len(h.handlers))
	for i, handler := range h.handlers {
		newHandlers[i] = handler.WithAttrs(attrs)
	}
	return newMultiHandler(newHandlers...)
}

func (h *multiHandler) WithGroup(name string) slog.Handler {
	newHandlers := make([]slog.Handler, len(h.handlers))
	for i, handler := range h.handlers {
		newHandlers[i] = handler.WithGroup(name)
	}
	return newMultiHandler(newHandlers...)
}

// busLogHandler 适配 bus 的日志 handler
type busLogHandler struct {
	bus    *bus.MessageBus
	level  slog.Level
	source string
}

func newBusLogHandler(bus *bus.MessageBus, level slog.Level, source string) slog.Handler {
	return &busLogHandler{bus: bus, level: level, source: source}
}

func (h *busLogHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *busLogHandler) Handle(_ context.Context, r slog.Record) error {
	if h.bus == nil || h.bus.IsClosed() {
		return nil
	}

	var levelStr string
	switch r.Level {
	case slog.LevelDebug:
		levelStr = bus.LogLevelDebug
	case slog.LevelInfo:
		levelStr = bus.LogLevelInfo
	case slog.LevelWarn:
		levelStr = bus.LogLevelWarn
	case slog.LevelError:
		levelStr = bus.LogLevelError
	default:
		levelStr = bus.LogLevelInfo
	}

	event := &bus.LogEvent{
		Level:     levelStr,
		Message:   r.Message,
		Source:    h.source,
		Timestamp: r.Time,
	}

	if r.NumAttrs() > 0 {
		r.Attrs(func(attr slog.Attr) bool {
			event.Message += " " + attr.Key + "=" + attr.Value.String()
			return true
		})
	}

	_ = h.bus.PublishLogEvent(context.Background(), event)
	return nil
}

func (h *busLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return h
}

func (h *busLogHandler) WithGroup(name string) slog.Handler {
	return h
}