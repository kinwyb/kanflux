package cmd

import (
	"context"
	"fmt"

	"github.com/kinwyb/kanflux/cli/tui"

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
	)

	cmd := &cobra.Command{
		Use:   "tui",
		Short: "启动终端交互界面",
		Long:  `启动一个交互式的终端用户界面(TUI)来与AI Agent进行对话`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			// 设置默认值
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

			// 创建配置
			cfg := &tui.Config{
				Workspace:    workspace,
				Model:        model,
				APIKey:       apiKey,
				APIBaseURL:   apiBaseURL,
				MaxIteration: maxIteration,
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
	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "工作目录 (默认为当前目录)")
	cmd.Flags().StringVarP(&model, "model", "m", "", "模型名称 (默认: qwen3.5-plus)")
	cmd.Flags().StringVarP(&apiKey, "api-key", "k", "", "API密钥")
	cmd.Flags().StringVarP(&apiBaseURL, "api-url", "u", "", "API基础URL")
	cmd.Flags().IntVarP(&maxIteration, "max-iter", "i", 0, "最大迭代次数 (默认: 10)")

	return cmd
}
