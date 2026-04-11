package cmd

import (
	"context"
	"fmt"

	"github.com/kinwyb/kanflux/memoria"
	"github.com/kinwyb/kanflux/memoria/types"
	"github.com/spf13/cobra"
)

// NewRAGCmd 创建知识库检索命令
func NewRAGCmd() *cobra.Command {
	var (
		configPath string
		workspace  string
		topK       int
	)

	cmd := &cobra.Command{
		Use:   "rag <query>",
		Short: "检索知识库内容",
		Long:  `使用向量检索在配置的知识库中搜索相关内容，用于验证知识库功能。`,
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

			// 检查是否配置了知识库路径
			if len(resolved.KnowledgePaths) == 0 {
				return fmt.Errorf("Agent '%s' 未配置知识库路径 (knowledge_paths)", agentName)
			}

			// 创建 Memoria 配置
			memConfig := memoria.DefaultConfig()
			memConfig.Workspace = ws
			memConfig.KnowledgePaths = resolved.KnowledgePaths
			memConfig.InitialScan = true

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

			// 启动并扫描知识库
			if err := mem.Start(ctx); err != nil {
				return fmt.Errorf("启动 Memoria 失败: %w", err)
			}

			// 显示统计信息
			stats := mem.GetStats()
			fmt.Printf("知识库: L2=%v 条, L3=%v 条\n\n", stats["l2_items"], stats["l3_items"])

			// 执行检索（只搜索文件来源）
			fmt.Printf("查询: %s\n\n", query)
			opts := &types.RetrieveOptions{
				SourceType: types.SourceTypeFile,
				Limit:      topK,
			}
			results, err := mem.Search(ctx, query, opts)
			if err != nil {
				return fmt.Errorf("检索失败: %w", err)
			}

			if len(results) == 0 {
				fmt.Println("未找到相关内容")
			} else {
				// 输出结果
				fmt.Printf("找到 %d 条相关记录:\n\n", len(results))
				for i, r := range results {
					fmt.Printf("--- [%d] Score: %.2f ---\n", i+1, r.Score)
					fmt.Printf("来源: %s\n", r.Item.Source)
					if r.Item.Summary != "" {
						fmt.Printf("摘要: %s\n", r.Item.Summary)
					}
					fmt.Println()
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