package cmd

import (
	"context"
	"fmt"

	"github.com/kinwyb/kanflux/agent/rag"
	"github.com/kinwyb/kanflux/session"

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

			// 检查是否配置了 embedding
			if resolved.EmbeddingModel == "" {
				return fmt.Errorf("Agent '%s' 未配置 embedding_model", agentName)
			}

			// 创建 embedder
			embedder, err := rag.CreateEmbedder(ctx, &rag.EmbedderConfig{
				Provider:   resolved.EmbeddingProvider,
				Model:      resolved.EmbeddingModel,
				APIKey:     resolved.EmbeddingAPIKey,
				APIBaseURL: resolved.EmbeddingAPIBaseURL,
			})
			if err != nil {
				return fmt.Errorf("创建Embedder失败: %w", err)
			}

			// 创建 session manager
			sessionMgr, err := session.NewManager(ws)
			if err != nil {
				return fmt.Errorf("创建Session Manager失败: %w", err)
			}

			// 设置 embedder 并同步初始化历史记录
			fmt.Println("正在初始化历史记录索引...")
			if err := sessionMgr.SetEmbedderSync(ctx, embedder); err != nil {
				return fmt.Errorf("初始化历史记录失败: %w", err)
			}

			// 获取历史记录管理器
			history := sessionMgr.GetHistory()
			if history == nil {
				return fmt.Errorf("历史记录管理器未初始化")
			}

			// 显示统计信息
			stats := history.GetStats()
			fmt.Printf("历史记录: %d 索引块, %d 天数据\n\n", stats["indexed_chunks"], stats["days_with_data"])

			// 执行检索
			fmt.Printf("查询: %s\n\n", query)
			result, err := history.Search(ctx, query, topK)
			if err != nil {
				return fmt.Errorf("检索失败: %w", err)
			}

			// 输出结果
			fmt.Println(result)

			return nil
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "配置文件路径")
	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "工作目录")
	cmd.Flags().IntVarP(&topK, "top-k", "k", 5, "返回结果数量")

	return cmd
}