package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/kinwyb/kanflux/config"
	"github.com/kinwyb/kanflux/gateway"
	"github.com/kinwyb/kanflux/gateway/ws"

	// 导入 channel 类型以触发 init() 注册
	_ "github.com/kinwyb/kanflux/channel/wxcom"

	"github.com/spf13/cobra"
)

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

			// 2. 解析日志级别
			level := parseLogLevelWithConfig(logLevel, cfg.Log)

			// 3. 处理 workspace
			if workspace != "" {
				for _, agentCfg := range cfg.Agents {
					agentCfg.Workspace = workspace
				}
			} else {
				defaultAgent := cfg.GetDefaultAgentName()
				if resolved, err := cfg.ResolveAgentConfig(defaultAgent); err == nil {
					workspace = resolved.Workspace
				}
			}
			if workspace == "" {
				workspace = "."
			}

			// 4. 创建 Gateway 实例
			gw, err := gateway.New(cfg, configPath, workspace)
			if err != nil {
				return err
			}

			// 5. 设置日志级别（临时设置，Gateway.Start 会重新设置）
			slog.SetDefault(slog.New(slog.NewTextHandler(nil, &slog.HandlerOptions{Level: level})))

			// 6. 创建上下文
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// 7. 启动 Gateway
			if err := gw.Start(ctx); err != nil {
				return err
			}

			// 8. 等待退出信号
			gw.WaitForSignal()

			// 9. 优雅关闭
			return gw.Stop()
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