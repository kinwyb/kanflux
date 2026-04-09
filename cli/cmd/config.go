package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kinwyb/kanflux/config"

	"github.com/spf13/cobra"
)

// NewConfigCmd 创建配置命令
func NewConfigCmd() *cobra.Command {
	var workspace string
	var force bool

	cmd := &cobra.Command{
		Use:   "config",
		Short: "创建配置文件",
		Long:  `在工作区中创建 kanflux.json 配置文件`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if workspace == "" {
				workspace = "."
			}

			// 获取绝对路径
			absWorkspace, err := filepath.Abs(workspace)
			if err != nil {
				return fmt.Errorf("failed to get absolute path: %w", err)
			}

			configPath := filepath.Join(absWorkspace, "kanflux.json")

			// 检查文件是否存在
			if _, err := os.Stat(configPath); err == nil && !force {
				return fmt.Errorf("config file already exists at %s, use --force to overwrite", configPath)
			}

			// 创建默认配置
			defaultConfig := createDefaultConfig(absWorkspace)

			// 写入文件
			content, err := json.MarshalIndent(defaultConfig, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal config: %w", err)
			}

			if err := os.WriteFile(configPath, content, 0644); err != nil {
				return fmt.Errorf("failed to write config file: %w", err)
			}

			fmt.Printf("Config file created at: %s\n", configPath)
			fmt.Println("\nPlease edit the config file to add your API key and customize settings.")

			return nil
		},
	}

	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "工作目录 (默认为当前目录)")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "强制覆盖已存在的配置文件")

	return cmd
}

// createDefaultConfig 创建默认配置
func createDefaultConfig(workspace string) *config.Config {
	return &config.Config{
		Providers: map[string]*config.ProviderConfig{
			"openai": {
				APIKey:       "your-api-key-here",
				APIBaseURL:   "https://api.openai.com/v1",
				DefaultModel: "gpt-4o",
			},
			"qwen": {
				APIKey:       "your-api-key-here",
				APIBaseURL:   "https://dashscope.aliyuncs.com/compatible-mode/v1",
				DefaultModel: "qwen3.5-plus",
			},
		},
		DefaultProvider: "qwen",
		Agents: []*config.AgentConfig{
			{
				Name:        "main",
				Type:        config.AgentTypeDeep, // 默认使用 deep agent
				Description: "Main agent for general tasks",
				Workspace:   workspace,
				// 可选：配置专门的记忆摘要模型（用于 memory 命令）
				// SummarizeModel: &config.EmbeddingConfig{
				// 	Provider: "qwen",
				// 	Model:    "qwen3.5-plus",
				// },
			},
		},
	}
}