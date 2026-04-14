package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

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

// NewGatewayCmd 创建 gateway 子命令
func NewGatewayCmd() *cobra.Command {
	var (
		configPath string
		workspace  string
		logLevel   string
	)

	cmd := &cobra.Command{
		Use:   "gateway",
		Short: "运行后台服务，管理 Agent 和 Channel",
		Long: `Gateway 命令运行一个后台服务，负责：
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

			// 2. 设置日志级别
			level := parseLogLevel(logLevel)
			slog.SetLogLoggerLevel(level)
			slog.Info("配置加载成功", "agents", len(cfg.Agents), "channels", cfg.Channels != nil)

			// 3. 处理 workspace 覆盖：如果 CLI 指定了 workspace，覆盖所有 agent 的 workspace
			if workspace != "" {
				for _, agentCfg := range cfg.Agents {
					agentCfg.Workspace = workspace
				}
				slog.Info("CLI workspace 覆盖所有 Agent 配置", "path", workspace)
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
			slog.Info("工作目录", "path", workspace)

			// 4. 创建带取消的上下文
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// 5. 创建 MessageBus
			msgBus := bus.NewMessageBus(100)
			slog.Info("消息总线创建完成")

			// 6. 创建 SessionManager
			sessionMgr, err := session.NewManager(workspace)
			if err != nil {
				return fmt.Errorf("创建 SessionManager 失败: %w", err)
			}
			slog.Info("会话管理器创建完成")

			// 7. 创建 AgentManager 并注册 Agents
			agentMgr := agent.NewManager(msgBus, sessionMgr)
			if err := agentMgr.RegisterAgentsFromConfig(ctx, cfg, nil); err != nil {
				return fmt.Errorf("注册 Agents 失败: %w", err)
			}
			slog.Info("Agents 注册完成", "count", len(agentMgr.ListAgents()))

			// 8. 创建 ChannelManager 并初始化 Channels
			channelMgr := channel.NewManager(msgBus)
			if cfg.Channels != nil {
				if err := channelMgr.InitializeFromConfig(ctx, cfg.Channels); err != nil {
					slog.Warn("初始化部分 Channel 失败", "error", err)
					// 继续运行，即使部分 Channel 失败
				}
			}
			slog.Info("Channels 初始化完成", "count", channelMgr.ChannelCount())

			// 9. 启动 AgentManager
			if err := agentMgr.Start(ctx); err != nil {
				return fmt.Errorf("启动 AgentManager 失败: %w", err)
			}
			slog.Info("AgentManager 启动完成")

			// 10. 启动 ChannelManager
			if err := channelMgr.StartAll(ctx); err != nil {
				cancel()
				agentMgr.Stop()
				return fmt.Errorf("启动 ChannelManager 失败: %w", err)
			}
			slog.Info("ChannelManager 启动完成")

			// 11. 创建 WebSocket 服务器
			cfgWsConfig := cfg.WebSocket
			if cfgWsConfig == nil || !cfgWsConfig.Enabled {
				// 默认启用 WebSocket
				cfgWsConfig = &config.WebSocketConfig{Enabled: true}
			}
			// 转换为 ws.ServerConfig
			wsConfig := &ws.ServerConfig{
				Enabled:      cfgWsConfig.Enabled,
				Port:         cfgWsConfig.Port,
				Host:         cfgWsConfig.Host,
				Path:         cfgWsConfig.Path,
				AuthToken:    cfgWsConfig.AuthToken,
				ReadTimeout:  cfgWsConfig.ReadTimeout,
				WriteTimeout: cfgWsConfig.WriteTimeout,
			}
			wsServer := ws.NewServer(msgBus, wsConfig)
			if err := wsServer.Start(ctx); err != nil {
				slog.Warn("启动 WebSocket 服务器失败", "error", err)
				// WebSocket 启动失败不影响主服务
			} else {
				slog.Info("WebSocket 服务器启动完成", "url", wsConfig.URL())
			}

			// 12. 设置信号处理
			sigChan := make(chan os.Signal, 1)
			signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

			slog.Info("Gateway 服务启动完成，等待退出信号...")
			fmt.Println("Gateway 服务已启动，按 Ctrl+C 退出")

			// 13. 等待退出信号
			select {
			case <-ctx.Done():
				// 上下文被外部取消
			case sig := <-sigChan:
				slog.Info("收到退出信号", "signal", sig)
				fmt.Printf("\n收到信号 %v，正在关闭...\n", sig)
			}

			// 14. 优雅关闭
			return gracefulShutdown(cancel, channelMgr, agentMgr, msgBus, wsServer)
		},
	}

	// 命令行参数
	cmd.Flags().StringVarP(&configPath, "config", "c", "", "配置文件路径")
	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "工作目录（覆盖配置文件）")
	cmd.Flags().StringVarP(&logLevel, "log-level", "l", "info", "日志级别 (debug, info, warn, error)")

	return cmd
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