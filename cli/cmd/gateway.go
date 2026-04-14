package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/kinwyb/kanflux/agent"
	"github.com/kinwyb/kanflux/bus"
	"github.com/kinwyb/kanflux/channel"
	"github.com/kinwyb/kanflux/config"
	"github.com/kinwyb/kanflux/session"
	"github.com/kinwyb/kanflux/ws"

	// 导入 channel 类型以触发 init() 注册
	_ "github.com/kinwyb/kanflux/channel/wxcom"

	"github.com/spf13/cobra"
)

// multiHandler 同时输出到多个 handler 的 slog Handler
type multiHandler struct {
	handlers []slog.Handler
}

// newMultiHandler 创建一个 multi handler
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

// NewGatewayCmd 创建 gateway 子命令
func NewGatewayCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gateway",
		Short: "Gateway 服务管理命令",
		Long: `Gateway 命令用于管理后台服务：
  start   - 启动 gateway 服务
  stop    - 停止运行中的 gateway 服务
  status  - 检查 gateway 服务状态`,
	}

	// 添加子命令
	cmd.AddCommand(NewGatewayStartCmd())
	cmd.AddCommand(NewGatewayStopCmd())
	cmd.AddCommand(NewGatewayStatusCmd())

	return cmd
}

// NewGatewayStartCmd 创建 gateway start 子命令
func NewGatewayStartCmd() *cobra.Command {
	var (
		configPath string
		workspace  string
		logLevel   string
	)

	cmd := &cobra.Command{
		Use:   "start",
		Short: "启动 Gateway 服务",
		Long: `启动 Gateway 后台服务，负责：
- 加载配置并初始化所有 Agent
- 启动所有 Channel（如企业微信等）
- 通过消息总线处理请求和响应
- 支持 SIGINT/SIGTERM 信号优雅关闭`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// 1. 加载配置
			cfg, err := loadConfig(configPath)
			if err != nil {
				return fmt.Errorf("加载配置失败: %w", err)
			}

			// 验证配置中有 Agent
			if len(cfg.Agents) == 0 {
				return fmt.Errorf("配置文件中没有定义 Agent")
			}

			// 2. 解析日志级别：CLI 参数 > 配置文件 > 默认值
			level := parseLogLevelWithConfig(logLevel, cfg.Log)

			// 3. 处理 workspace 覆盖：如果 CLI 指定了 workspace，覆盖所有 agent 的 workspace
			if workspace != "" {
				for _, agentCfg := range cfg.Agents {
					agentCfg.Workspace = workspace
				}
			} else {
				// 使用默认 Agent 的工作目录
				defaultAgent := cfg.GetDefaultAgentName()
				if resolved, err := cfg.ResolveAgentConfig(defaultAgent); err == nil {
					workspace = resolved.Workspace
				}
			}
			if workspace == "" {
				workspace = "."
			}

			// 4. 检查是否已有 gateway 运行
			wsConfig := resolveWsConfig(cfg)
			detector := ws.NewDetector(wsConfig)
			if detector.IsRunning() {
				return fmt.Errorf("Gateway 已在运行中 (WebSocket: %s)，请先使用 'gateway stop' 停止", wsConfig.URL())
			}

			// 5. 创建带取消的上下文
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// 6. 创建 MessageBus
			msgBus := bus.NewMessageBus(100)

			// 7. 设置日志：同时输出到文件/stdout 和 bus
			logFile, err := setupLogger(ctx, workspace, level, msgBus, cfg.Log)
			if err != nil {
				return fmt.Errorf("设置日志失败: %w", err)
			}
			if logFile != nil {
				defer logFile.Close()
			}

			slog.Info("配置加载成功", "agents", len(cfg.Agents), "channels", cfg.Channels != nil)
			slog.Info("工作目录", "path", workspace)

			// 8. 创建 SessionManager
			sessionMgr, err := session.NewManager(workspace)
			if err != nil {
				return fmt.Errorf("创建 SessionManager 失败: %w", err)
			}
			slog.Info("会话管理器创建完成")

			// 9. 创建 AgentManager 并注册 Agents
			agentMgr := agent.NewManager(msgBus, sessionMgr)
			if err := agentMgr.RegisterAgentsFromConfig(ctx, cfg, nil); err != nil {
				return fmt.Errorf("注册 Agents 失败: %w", err)
			}
			slog.Info("Agents 注册完成", "count", len(agentMgr.ListAgents()))

			// 10. 创建 ChannelManager 并初始化 Channels
			channelMgr := channel.NewManager(msgBus)
			if cfg.Channels != nil {
				if err := channelMgr.InitializeFromConfig(ctx, cfg.Channels); err != nil {
					slog.Warn("初始化部分 Channel 失败", "error", err)
					// 继续运行，即使部分 Channel 失败
				}
			}
			slog.Info("Channels 初始化完成", "count", channelMgr.ChannelCount())

			// 10. 启动 AgentManager
			if err := agentMgr.Start(ctx); err != nil {
				return fmt.Errorf("启动 AgentManager 失败: %w", err)
			}
			slog.Info("AgentManager 启动完成")

			// 11. 启动 ChannelManager
			if err := channelMgr.StartAll(ctx); err != nil {
				cancel()
				agentMgr.Stop()
				return fmt.Errorf("启动 ChannelManager 失败: %w", err)
			}
			slog.Info("ChannelManager 启动完成")

			// 12. 创建 WebSocket 服务器
			wsServer := ws.NewServer(msgBus, wsConfig)
			// 设置 shutdown callback
			wsServer.SetShutdownCallback(func() {
				slog.Info("收到远程关闭请求")
				// 发送 SIGTERM 信号，触发优雅关闭
				p, _ := os.FindProcess(os.Getpid())
				p.Signal(syscall.SIGTERM)
			})
			if err := wsServer.Start(ctx); err != nil {
				slog.Warn("启动 WebSocket 服务器失败", "error", err)
				// WebSocket 启动失败不影响主服务
			} else {
				slog.Info("WebSocket 服务器启动完成", "url", wsConfig.URL())
			}

			// 13. 设置信号处理
			sigChan := make(chan os.Signal, 1)
			signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

			slog.Info("Gateway 服务启动完成，等待退出信号...")
			fmt.Println("Gateway 服务已启动，按 Ctrl+C 退出")
			fmt.Printf("WebSocket: %s\n", wsConfig.URL())

			// 14. 等待退出信号
			select {
			case <-ctx.Done():
				// 上下文被外部取消
			case sig := <-sigChan:
				slog.Info("收到退出信号", "signal", sig)
				fmt.Printf("\n收到信号 %v，正在关闭...\n", sig)
			}

			// 15. 优雅关闭
			return gracefulShutdown(cancel, channelMgr, agentMgr, msgBus, wsServer)
		},
	}

	// 命令行参数
	cmd.Flags().StringVarP(&configPath, "config", "c", "", "配置文件路径")
	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "工作目录（覆盖配置文件）")
	cmd.Flags().StringVarP(&logLevel, "log-level", "l", "info", "日志级别 (debug, info, warn, error)")

	return cmd
}

// NewGatewayStopCmd 创建 gateway stop 子命令
func NewGatewayStopCmd() *cobra.Command {
	var (
		configPath string
		timeout    int
	)

	cmd := &cobra.Command{
		Use:   "stop",
		Short: "停止 Gateway 服务",
		Long:  `通过 WebSocket 发送关闭命令，停止运行中的 Gateway 服务`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// 加载配置获取 WebSocket 地址
			cfg, err := loadConfig(configPath)
			if err != nil {
				// 配置加载失败，使用默认配置尝试连接
				slog.Warn("加载配置失败，使用默认 WebSocket 地址", "error", err)
			}

			wsConfig := resolveWsConfig(cfg)
			detector := ws.NewDetector(wsConfig)

			// 检查是否运行
			if !detector.IsRunning() {
				fmt.Println("Gateway 未运行")
				return nil
			}

			fmt.Printf("正在停止 Gateway (%s)...\n", wsConfig.URL())

			// 连接并发送关闭命令
			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
			defer cancel()

			client, err := detector.TryConnect(ctx)
			if err != nil {
				return fmt.Errorf("连接 Gateway 失败: %w", err)
			}
			defer client.Close()

			// 发送 shutdown 控制消息
			controlPayload := ws.ControlPayload{
				Action: ws.ControlActionShutdown,
			}

			wsMsg, err := ws.NewWSMessage(ws.MsgTypeControl, "shutdown", controlPayload)
			if err != nil {
				return fmt.Errorf("创建关闭消息失败: %w", err)
			}

			if err := client.Send(ctx, wsMsg); err != nil {
				return fmt.Errorf("发送关闭命令失败: %w", err)
			}

			// 等待响应
			resp, err := client.WaitForMessage(ctx, ws.MsgTypeControlAck)
			if err != nil {
				fmt.Println("已发送关闭命令（未收到响应，可能已关闭）")
				return nil
			}

			var ackPayload ws.ControlAckPayload
			if err := resp.ParsePayload(&ackPayload); err == nil && ackPayload.Success {
				fmt.Println("Gateway 已开始关闭")
			} else {
				fmt.Printf("关闭响应: %s\n", ackPayload.Message)
			}

			// 等待服务完全停止
			fmt.Println("等待 Gateway 完全停止...")
			if detector.WaitForStopped(ctx, time.Duration(timeout)*time.Second) {
				fmt.Println("Gateway 已停止")
			} else {
				fmt.Println("Gateway 可能仍在运行，请检查")
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "配置文件路径")
	cmd.Flags().IntVarP(&timeout, "timeout", "t", 10, "等待超时时间（秒）")

	return cmd
}

// NewGatewayStatusCmd 创建 gateway status 子命令
func NewGatewayStatusCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "检查 Gateway 服务状态",
		Long:  `检查当前系统是否有 Gateway 服务在运行`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// 加载配置获取 WebSocket 地址
			cfg, err := loadConfig(configPath)
			if err != nil {
				// 配置加载失败，使用默认配置尝试连接
				slog.Warn("加载配置失败，使用默认 WebSocket 地址", "error", err)
			}

			wsConfig := resolveWsConfig(cfg)
			detector := ws.NewDetector(wsConfig)

			if !detector.IsRunning() {
				fmt.Println("Gateway 未运行")
				fmt.Printf("WebSocket: %s\n", wsConfig.URL())
				return nil
			}

			fmt.Println("Gateway 正在运行")
			fmt.Printf("WebSocket: %s\n", wsConfig.URL())

			// 尝试获取详细状态
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()

			client, err := detector.TryConnect(ctx)
			if err != nil {
				fmt.Println("(无法获取详细状态)")
				return nil
			}
			defer client.Close()

			// 发送 status 控制消息
			controlPayload := ws.ControlPayload{
				Action: ws.ControlActionStatus,
			}

			wsMsg, err := ws.NewWSMessage(ws.MsgTypeControl, "status", controlPayload)
			if err != nil {
				return nil
			}

			if err := client.Send(ctx, wsMsg); err != nil {
				fmt.Println("(无法获取详细状态)")
				return nil
			}

			// 等待响应
			resp, err := client.WaitForMessage(ctx, ws.MsgTypeControlAck)
			if err != nil {
				fmt.Println("(无法获取详细状态)")
				return nil
			}

			var ackPayload ws.ControlAckPayload
			if err := resp.ParsePayload(&ackPayload); err == nil && ackPayload.Success {
				fmt.Printf("状态: %s\n", ackPayload.Message)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "配置文件路径")

	return cmd
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

// setupLogger 设置日志：同时输出到文件/stdout 和 bus
// 返回日志文件（如果配置了文件），需要调用者关闭
func setupLogger(ctx context.Context, workspace string, level slog.Level, msgBus *bus.MessageBus, logCfg *config.LogConfig) (*os.File, error) {
	var outputHandler slog.Handler

	// 确定日志输出位置
	var logFile *os.File
	var logPath string

	if logCfg != nil && logCfg.File != "" {
		// 配置了日志文件路径
		logPath = logCfg.File
		// 如果是相对路径，转换为绝对路径（基于 workspace）
		if !filepath.IsAbs(logPath) {
			logPath = filepath.Join(workspace, logPath)
		}

		// 确保目录存在
		logDir := filepath.Dir(logPath)
		if err := os.MkdirAll(logDir, 0755); err != nil {
			return nil, fmt.Errorf("创建日志目录失败: %w", err)
		}

		// 打开日志文件
		logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return nil, fmt.Errorf("打开日志文件失败: %w", err)
		}

		outputHandler = slog.NewTextHandler(logFile, &slog.HandlerOptions{
			Level: level,
		})
	} else {
		// 未配置日志文件，输出到 stdout
		outputHandler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level: level,
		})
	}

	// 创建 bus handler（始终启用）
	busHandler := newBusLogHandler(msgBus, level, "gateway")

	// 创建 multi handler
	multiHandler := newMultiHandler(outputHandler, busHandler)

	// 设置默认 logger
	slog.SetDefault(slog.New(multiHandler))

	if logPath != "" {
		slog.Info("日志系统启动", "file", logPath)
	} else {
		slog.Info("日志系统启动", "output", "stdout")
	}

	return logFile, nil
}

// newBusLogHandler 创建 bus log handler（内部使用）
func newBusLogHandler(bus *bus.MessageBus, level slog.Level, source string) slog.Handler {
	// 使用 bus.SetupDefaultLogger 的内部实现
	return &busLogHandlerAdapter{bus: bus, level: level, source: source}
}

// busLogHandlerAdapter 适配 bus 的日志 handler
type busLogHandlerAdapter struct {
	bus    *bus.MessageBus
	level  slog.Level
	source string
}

func (h *busLogHandlerAdapter) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *busLogHandlerAdapter) Handle(_ context.Context, r slog.Record) error {
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

	// 添加属性
	if r.NumAttrs() > 0 {
		r.Attrs(func(attr slog.Attr) bool {
			event.Message += " " + attr.Key + "=" + attr.Value.String()
			return true
		})
	}

	// 发布到 bus（非阻塞）
	ctx := context.Background()
	_ = h.bus.PublishLogEvent(ctx, event)

	return nil
}

func (h *busLogHandlerAdapter) WithAttrs(attrs []slog.Attr) slog.Handler {
	return h
}

func (h *busLogHandlerAdapter) WithGroup(name string) slog.Handler {
	return h
}

// parseLogLevelWithConfig 解析日志级别：CLI 参数 > 配置文件 > 默认值
func parseLogLevelWithConfig(cliLevel string, logCfg *config.LogConfig) slog.Level {
	// CLI 参数优先
	if cliLevel != "" && cliLevel != "info" {
		return parseLogLevel(cliLevel)
	}

	// 配置文件
	if logCfg != nil && logCfg.Level != "" {
		return parseLogLevel(logCfg.Level)
	}

	// 默认值
	return slog.LevelInfo
}

// parseLogLevel 解析日志级别
func parseLogLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// gracefulShutdown 优雅关闭
func gracefulShutdown(
	cancel context.CancelFunc,
	channelMgr *channel.Manager,
	agentMgr *agent.Manager,
	msgBus *bus.MessageBus,
	wsServer *ws.Server,
) error {
	slog.Info("开始优雅关闭...")

	// 1. 取消上下文，停止处理循环
	cancel()

	// 2. 先停止 WebSocket Server（停止接收新连接）
	if wsServer != nil {
		if err := wsServer.Stop(); err != nil {
			slog.Warn("停止 WebSocket Server 时出错", "error", err)
		}
		slog.Info("WebSocket Server 已停止")
	}

	// 3. 先停止 Channels（它们接收 outbound 消息）
	if err := channelMgr.StopAll(); err != nil {
		slog.Warn("停止 Channels 时出错", "error", err)
	}
	slog.Info("Channels 已停止")

	// 4. 停止 Agents
	if err := agentMgr.Stop(); err != nil {
		slog.Warn("停止 Agents 时出错", "error", err)
	}
	slog.Info("Agents 已停止")

	// 5. 关闭消息总线
	if err := msgBus.Close(); err != nil {
		slog.Warn("关闭消息总线时出错", "error", err)
	}
	slog.Info("消息总线已关闭")

	slog.Info("Gateway 关闭完成")
	fmt.Println("Gateway 服务已停止")

	return nil
}