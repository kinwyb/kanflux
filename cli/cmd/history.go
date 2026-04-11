package cmd

import (
	"context"
	"fmt"

	"github.com/kinwyb/kanflux/memoria"
	"github.com/kinwyb/kanflux/memoria/types"
	"github.com/spf13/cobra"
)

// NewHistoryCmd 创建历史对话检索命令
func NewHistoryCmd() *cobra.Command {
	var (
		configPath string
		workspace  string
		topK       int
	)

	cmd := &cobra.Command{
		Use:   "history <query>",
		Short: "检索历史对话内容",
		Long:  `使用向量检索在历史对话中搜索相关内容，用于验证历史记录功能。`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			query := args[0]

			// 加载配置文件
			cfg, err := loadConfig(configPath)
			if err != nil {
				return fmt.Errorf("加载配置失败: %w", err)
			}

			// 获取默认 agent 配置
			agentName := cfg.GetDefaultAgentName()
			if agentName == "" {
				agentName = "main"
			}

			resolved, err := cfg.ResolveAgentConfig(agentName)
			if err != nil {
				return fmt.Errorf("解析Agent配置失败: %w", err)
			}

			// 使用配置中的 workspace 或命令行参数
			ws := resolved.Workspace
			if workspace != "" {
				ws = workspace
			}

			// 创建 Memoria 配置
			memConfig := memoria.DefaultConfig()
			memConfig.Workspace = ws
			memConfig.KnowledgePaths = resolved.KnowledgePaths

			// 设置 Embedding 配置
			if resolved.EmbeddingModel != "" {
				memConfig.Embedding = &memoria.EmbeddingConfig{
					Provider:   resolved.EmbeddingProvider,
					Model:      resolved.EmbeddingModel,
					APIKey:     resolved.EmbeddingAPIKey,
					APIBaseURL: resolved.EmbeddingAPIBaseURL,
				}
			}

			// 创建 Memoria 实例
			fmt.Println("正在初始化 Memoria...")
			mem, err := memoria.New(memConfig)
			if err != nil {
				return fmt.Errorf("创建 Memoria 失败: %w", err)
			}

			// 初始化并扫描
			if err := mem.Start(ctx); err != nil {
				return fmt.Errorf("启动 Memoria 失败: %w", err)
			}

			// 显示统计信息
			stats := mem.GetStats()
			fmt.Printf("记忆系统: L1=%v, L2=%v, L3=%v\n\n", stats["l1_items"], stats["l2_items"], stats["l3_items"])

			// 执行检索（只搜索聊天历史）
			fmt.Printf("查询: %s\n\n", query)
			opts := &types.RetrieveOptions{
				SourceType: types.SourceTypeChat,
				Limit:      topK,
			}
			results, err := mem.Search(ctx, query, opts)
			if err != nil {
				return fmt.Errorf("检索失败: %w", err)
			}

			// 输出结果
			if len(results) == 0 {
				fmt.Println("未找到相关历史记录")
			} else {
				fmt.Printf("找到 %d 条相关记录:\n\n", len(results))
				for i, r := range results {
					fmt.Printf("**[%d]** (Score: %.2f, Layer: L%d)\n", i+1, r.Score, r.Layer)
					fmt.Printf("%s\n\n", r.Item.Summary)
				}
			}

			mem.Close()
			return nil
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "配置文件路径")
	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "工作目录")
	cmd.Flags().IntVarP(&topK, "top-k", "k", 5, "返回结果数量")

	return cmd
}