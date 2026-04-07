package cmd

import (
	"context"
	"fmt"

	"github.com/kinwyb/kanflux/agent/rag"

	"github.com/spf13/cobra"
)

// NewRAGCmd 创建RAG检索命令
func NewRAGCmd() *cobra.Command {
	var (
		configPath string
		workspace  string
		topK       int
	)

	cmd := &cobra.Command{
		Use:   "rag <query>",
		Short: "检索知识库内容",
		Long:  `使用向量检索在配置的知识库中搜索相关内容，用于验证RAG功能。`,
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

			// 转换知识库路径配置
			var knowledgePaths []rag.KnowledgePath
			for _, kp := range resolved.KnowledgePaths {
				knowledgePaths = append(knowledgePaths, rag.KnowledgePath{
					Path:       kp.Path,
					Extensions: kp.Extensions,
					Recursive:  kp.Recursive,
					Exclude:    kp.Exclude,
				})
			}

			// 创建 RAG 配置
			ragConfig := rag.DefaultConfig()
			ragConfig.Workspace = ws
			ragConfig.KnowledgePaths = knowledgePaths
			ragConfig.Embedder = embedder

			if resolved.RAGConfig != nil {
				ragConfig.ChunkSize = resolved.RAGConfig.ChunkSize
				ragConfig.ChunkOverlap = resolved.RAGConfig.ChunkOverlap
				ragConfig.TopK = topK
				ragConfig.ScoreThreshold = resolved.RAGConfig.ScoreThreshold
			} else {
				ragConfig.TopK = topK
			}

			// 创建 RAG Manager
			ragMgr, err := rag.NewManager(ctx, ragConfig)
			if err != nil {
				return fmt.Errorf("创建RAG Manager失败: %w", err)
			}

			// 初始化（同步，以便立即使用）
			fmt.Println("正在初始化知识库索引...")
			if err := ragMgr.Initialize(ctx); err != nil {
				return fmt.Errorf("初始化RAG失败: %w", err)
			}

			// 显示统计信息
			stats := ragMgr.GetStats()
			fmt.Printf("知识库: %d 文档, %d 文本块\n\n", stats.TotalDocuments, stats.TotalChunks)

			// 执行检索
			fmt.Printf("查询: %s\n\n", query)
			results, err := ragMgr.Retrieve(ctx, query)
			if err != nil {
				return fmt.Errorf("检索失败: %w", err)
			}

			if len(results) == 0 {
				fmt.Println("未找到相关内容")
				return nil
			}

			// 输出结果
			fmt.Printf("找到 %d 条相关记录:\n\n", len(results))
			for i, r := range results {
				fmt.Printf("--- [%d] Score: %.4f ---\n", i+1, r.Score)
				fmt.Printf("来源: %s\n", r.SourcePath)
				fmt.Printf("内容:\n%s\n\n", r.Content)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "配置文件路径")
	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "工作目录")
	cmd.Flags().IntVarP(&topK, "top-k", "k", 5, "返回结果数量")

	return cmd
}