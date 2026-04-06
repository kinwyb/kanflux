package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/kinwyb/kanflux/cli/tui"
	"github.com/kinwyb/kanflux/config"

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
	)

	cmd := &cobra.Command{
		Use:   "tui",
		Short: "启动终端交互界面",
		Long:  `启动一个交互式的终端用户界面(TUI)来与AI Agent进行对话`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			var cfg *tui.Config

			// 尝试从配置文件加载
			if configPath != "" || agentName != "" {
				// 指定了配置文件或 agent 名称，必须从配置文件加载
				var loadedConfig *config.Config
				var err error

				if configPath != "" {
					loadedConfig, err = config.Load(configPath)
				} else {
					loadedConfig, err = config.LoadDefault()
				}

				if err != nil {
					return fmt.Errorf("failed to load config: %w", err)
				}

				if agentName == "" {
					// 如果有配置文件但没有指定 agent，使用第一个 agent
					if len(loadedConfig.Agents) == 0 {
						return fmt.Errorf("no agents defined in config file")
					}
					agentName = loadedConfig.Agents[0].Name
				}

				// 解析 agent 配置
				resolved, err := loadedConfig.ResolveAgentConfig(agentName)
				if err != nil {
					return fmt.Errorf("failed to resolve agent config: %w", err)
				}

				// 从解析后的配置创建 tui.Config
				cfg = &tui.Config{
					Workspace:    resolved.Workspace,
					Model:        resolved.Model,
					APIKey:       resolved.APIKey,
					APIBaseURL:   resolved.APIBaseURL,
					MaxIteration: resolved.MaxIteration,
					SkillDirs:    resolved.SkillDirs,
				}

				// CLI 参数覆盖配置文件
				if workspace != "" {
					cfg.Workspace = workspace
				}
				if model != "" {
					cfg.Model = model
				}
				if apiKey != "" {
					cfg.APIKey = apiKey
				}
				if apiBaseURL != "" {
					cfg.APIBaseURL = apiBaseURL
				}
				if maxIteration > 0 {
					cfg.MaxIteration = maxIteration
				}

			} else {
				// 没有配置文件，使用纯 CLI 参数
				// 尝试查找默认配置文件
				loadedConfig, err := config.LoadDefault()
				if err == nil && len(loadedConfig.Agents) > 0 {
					// 找到配置文件，使用第一个 agent
					agentName = loadedConfig.Agents[0].Name
					resolved, err := loadedConfig.ResolveAgentConfig(agentName)
					if err != nil {
						return fmt.Errorf("failed to resolve agent config: %w", err)
					}

					cfg = &tui.Config{
						Workspace:    resolved.Workspace,
						Model:        resolved.Model,
						APIKey:       resolved.APIKey,
						APIBaseURL:   resolved.APIBaseURL,
						MaxIteration: resolved.MaxIteration,
						SkillDirs:    resolved.SkillDirs,
					}

					// CLI 参数覆盖
					if workspace != "" {
						cfg.Workspace = workspace
					}
					if model != "" {
						cfg.Model = model
					}
					if apiKey != "" {
						cfg.APIKey = apiKey
					}
					if apiBaseURL != "" {
						cfg.APIBaseURL = apiBaseURL
					}
					if maxIteration > 0 {
						cfg.MaxIteration = maxIteration
					}
				} else {
					// 完全没有配置文件，使用 CLI 默认值
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
			}

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

	return cmd
}