package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/kinwyb/kanflux/cli/tui"
	"github.com/kinwyb/kanflux/config"
	"github.com/kinwyb/kanflux/ws"

	"github.com/spf13/cobra"
)

// NewTUICmd 创建TUI子命令
func NewTUICmd() *cobra.Command {
	var (
		workspace    string
		model        string
		apiKey       string
		apiBaseURL   string
		maxIteration int
		configPath   string
		agentName    string
		gatewayURL   string // WebSocket Gateway URL
	)

	cmd := &cobra.Command{
		Use:   "tui",
		Short: "启动终端交互界面",
		Long:  `启动一个交互式的终端用户界面(TUI)来与AI Agent进行对话`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			var cfg *tui.Config

			// 尝试从配置文件加载
			loadedConfig, err := loadConfig(configPath)
			if err == nil && loadedConfig != nil && len(loadedConfig.Agents) > 0 {
				// 有配置文件，使用多 agent 模式
				cfg = &tui.Config{
					AppConfig:    loadedConfig,
					DefaultAgent: agentName,
				}

				// 如果指定了特定 agent，CLI 参数覆盖该 agent 的配置
				if agentName == "" {
					agentName = loadedConfig.GetDefaultAgentName()
				}

				// CLI 参数覆盖
				if workspace != "" {
					cfg.Workspace = workspace
				}
				// 其他 CLI 参数在单 agent 模式下使用

			} else {
				// 无配置文件，使用单 agent 模式
				if workspace == "" {
					workspace = "."
				}
				if model == "" {
					model = "qwen3.5-122b-a10b"
				}
				if apiBaseURL == "" {
					apiBaseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
				}
				if maxIteration == 0 {
					maxIteration = 10
				}
				// API Key 从环境变量获取
				if apiKey == "" {
					apiKey = os.Getenv("OPENAI_API_KEY")
				}

				cfg = &tui.Config{
					Workspace:    workspace,
					Model:        model,
					APIKey:       apiKey,
					APIBaseURL:   apiBaseURL,
					MaxIteration: maxIteration,
				}
			}

			// 设置 WebSocket 配置
			if gatewayURL != "" {
				cfg.GatewayURL = gatewayURL
			}
			cfg.WSConfig = &ws.ServerConfig{Enabled: true}

			// 启动TUI
			app, err := tui.NewApp(ctx, cfg)
			if err != nil {
				return fmt.Errorf("failed to create TUI app: %w", err)
			}

			return app.Run()
		},
	}

	// 命令行参数
	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "工作目录")
	cmd.Flags().StringVarP(&model, "model", "m", "", "模型名称")
	cmd.Flags().StringVarP(&apiKey, "api-key", "k", "", "API密钥")
	cmd.Flags().StringVarP(&apiBaseURL, "api-url", "u", "", "API基础URL")
	cmd.Flags().IntVarP(&maxIteration, "max-iter", "i", 0, "最大迭代次数")
	cmd.Flags().StringVarP(&configPath, "config", "c", "", "配置文件路径")
	cmd.Flags().StringVarP(&agentName, "agent", "a", "", "Agent名称 (从配置文件中选择)")
	cmd.Flags().StringVarP(&gatewayURL, "gateway", "g", "", "外部 Gateway WebSocket URL (不指定则自动检测/启动本地服务)")

	return cmd
}

// loadConfig 加载配置文件
func loadConfig(configPath string) (*config.Config, error) {
	if configPath != "" {
		return config.Load(configPath)
	}
	return config.LoadDefault()
}