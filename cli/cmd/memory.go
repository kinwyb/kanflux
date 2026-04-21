package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/kinwyb/kanflux/config"
	"github.com/kinwyb/kanflux/memoria"
	"github.com/kinwyb/kanflux/memoria/types"
	"github.com/kinwyb/kanflux/providers"

	"github.com/spf13/cobra"
)

// NewMemoryCmd 创建 memory 子命令
func NewMemoryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "memory",
		Short: "Memoria 记忆代理模块",
		Long:  `使用 Memoria 模块处理聊天记录和文件，提取记忆并分类存储。`,
	}

	cmd.AddCommand(NewMemoryProcessCmd())
	cmd.AddCommand(NewMemoryShowCmd())
	cmd.AddCommand(NewMemoryServeCmd())
	cmd.AddCommand(NewMemorySearchCmd())
	cmd.AddCommand(NewMemoryInitCmd())

	return cmd
}

// NewMemoryProcessCmd 创建处理命令
func NewMemoryProcessCmd() *cobra.Command {
	var (
		configPath string
		workspace  string
		userID     string
		source     string
	)

	cmd := &cobra.Command{
		Use:   "process <file>",
		Short: "处理文件内容提取记忆",
		Long:  `处理指定的聊天记录文件(.jsonl)或文本文件，提取记忆并存储。`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			filePath := args[0]

			// 加载配置
			cfg, _, err := loadConfig(configPath)
			if err != nil {
				return fmt.Errorf("加载配置失败: %w", err)
			}

			// 获取 agent 配置
			agentName := cfg.GetDefaultAgentName()
			if agentName == "" {
				agentName = "main"
			}

			resolved, err := cfg.ResolveAgentConfig(agentName)
			if err != nil {
				return fmt.Errorf("解析 Agent 配置失败: %w", err)
			}

			// 使用配置中的 workspace 或命令行参数
			ws := resolved.Workspace
			if workspace != "" {
				ws = workspace
			}

			// 读取文件内容
			content, err := os.ReadFile(filePath)
			if err != nil {
				return fmt.Errorf("读取文件失败: %w", err)
			}

			// 确定用户 ID
			if userID == "" {
				userID = "default"
			}

			// 创建 ChatModel
			chatModel, err := createMemoriaChatModel(ctx, resolved)
			if err != nil {
				return fmt.Errorf("创建 ChatModel 失败: %w", err)
			}

			// 创建 Memoria 服务
			memoriaConfig := &memoria.Config{
				Workspace: ws,
			}

			mem, err := memoria.New(memoriaConfig)
			if err != nil {
				return fmt.Errorf("创建 Memoria 服务失败: %w", err)
			}
			defer mem.Close()

			// 设置 ChatModel
			mem.SetChatModel(chatModel)

			// 创建用户上下文
			userCtx := &types.DefaultUserIdentity{
				UserID: userID,
			}

			// 确定来源类型
			if source == "" {
				ext := strings.ToLower(filepath.Ext(filePath))
				if ext == ".jsonl" {
					source = "chat"
				} else {
					source = "file"
				}
			}

			// 处理文件
			fmt.Printf("处理文件: %s\n", filePath)
			fmt.Printf("来源类型: %s\n", source)
			fmt.Printf("用户: %s\n\n", userID)

			var result *types.ProcessingResult
			if source == "chat" {
				result, err = mem.ProcessChat(ctx, filePath, string(content), userCtx)
			} else {
				result, err = mem.ProcessFile(ctx, filePath, string(content), userCtx)
			}

			if err != nil {
				return fmt.Errorf("处理失败: %w", err)
			}

			// 输出结果
			fmt.Printf("提取记忆: %d 条\n", len(result.Items))
			for layer, count := range result.LayerCounts {
				fmt.Printf("  Layer %d: %d 条\n", layer, count)
			}
			for hall, count := range result.HallCounts {
				fmt.Printf("  %s: %d 条\n", hall, count)
			}

			if len(result.Errors) > 0 {
				fmt.Printf("\n错误: %d 个\n", len(result.Errors))
				for _, e := range result.Errors {
					fmt.Printf("  - %v\n", e)
				}
			}

			// 显示提取的记忆
			if len(result.Items) > 0 {
				fmt.Println("\n提取的记忆:")
				for i, item := range result.Items {
					fmt.Printf("--- [%d] ---\n", i+1)
					fmt.Printf("Hall: %s\n", item.HallType)
					fmt.Printf("Layer: L%d\n", item.Layer)
					fmt.Printf("Summary: %s\n", truncateStr(item.Summary, 200))
					fmt.Printf("Tokens: %d\n", item.Tokens)
				}
			}

			fmt.Printf("\n存储目录: %s\n", mem.GetMemoriaDir())

			return nil
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "配置文件路径")
	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "工作目录")
	cmd.Flags().StringVarP(&userID, "user", "u", "", "用户 ID (默认: default)")
	cmd.Flags().StringVarP(&source, "source", "s", "", "来源类型 (chat/file)")

	return cmd
}

// NewMemoryShowCmd 创建显示命令
func NewMemoryShowCmd() *cobra.Command {
	var (
		configPath string
		workspace  string
		userID     string
		layer      int
		days       int
	)

	cmd := &cobra.Command{
		Use:   "show",
		Short: "显示存储的记忆",
		Long:  `显示已存储的记忆内容。`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// 加载配置
			cfg, _, err := loadConfig(configPath)
			if err != nil {
				return fmt.Errorf("加载配置失败: %w", err)
			}

			agentName := cfg.GetDefaultAgentName()
			if agentName == "" {
				agentName = "main"
			}

			resolved, err := cfg.ResolveAgentConfig(agentName)
			if err != nil {
				return fmt.Errorf("解析 Agent 配置失败: %w", err)
			}

			ws := resolved.Workspace
			if workspace != "" {
				ws = workspace
			}

			// 创建 Memoria 服务（不需要 ChatModel 来显示）
			memoriaConfig := &memoria.Config{
				Workspace: ws,
			}

			mem, err := memoria.New(memoriaConfig)
			if err != nil {
				return fmt.Errorf("创建 Memoria 服务失败: %w", err)
			}
			defer mem.Close()

			if userID == "" {
				userID = "default"
			}

			fmt.Printf("存储目录: %s\n", mem.GetMemoriaDir())
			fmt.Printf("用户: %s\n\n", userID)

			// 显示 L1 记忆
			if layer == 0 || layer == 1 {
				l1Items := mem.GetL1Facts(userID)
				fmt.Printf("=== L1 关键事实 (%d 条) ===\n", len(l1Items))
				for i, item := range l1Items {
					fmt.Printf("[%d] %s (%s)\n", i+1, truncateStr(item.Summary, 100), item.HallType)
				}
				fmt.Println()
			}

			// 显示 L2 记忆
			if layer == 0 || layer == 2 {
				if days == 0 {
					days = 7
				}
				l2Items, err := mem.GetL2Recent(context.Background(), userID, days)
				if err != nil {
					fmt.Printf("获取 L2 记忆失败: %v\n", err)
				} else {
					fmt.Printf("=== L2 最近 %d 天回忆 (%d 条) ===\n", days, len(l2Items))
					for i, item := range l2Items {
						fmt.Printf("[%d] %s (%s, %s)\n", i+1,
							truncateStr(item.Summary, 100),
							item.HallType,
							item.Timestamp.Format("2006-01-02"))
					}
					fmt.Println()
				}
			}

			// 显示统计
			stats := mem.GetStats()
			fmt.Printf("=== 统计 ===\n")
			fmt.Printf("Workspace: %s\n", stats["workspace"])
			fmt.Printf("L1 记忆总数: %d\n", stats["l1_items"])

			return nil
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "配置文件路径")
	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "工作目录")
	cmd.Flags().StringVarP(&userID, "user", "u", "", "用户 ID")
	cmd.Flags().IntVarP(&layer, "layer", "l", 0, "显示层级 (0=全部, 1=L1, 2=L2)")
	cmd.Flags().IntVarP(&days, "days", "d", 7, "L2 显示最近天数")

	return cmd
}

// NewMemoryServeCmd 创建服务命令
func NewMemoryServeCmd() *cobra.Command {
	var (
		configPath   string
		workspace    string
		chatInterval int
		fileInterval int
		skipScan     bool
		skipL3       bool
	)

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "启动后台记忆服务",
		Long:  `启动 Memoria 后台服务，定时处理聊天记录和文件修改。初始化时会扫描知识库文件和聊天记录。`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// 加载配置
			cfg, _, err := loadConfig(configPath)
			if err != nil {
				return fmt.Errorf("加载配置失败: %w", err)
			}

			agentName := cfg.GetDefaultAgentName()
			if agentName == "" {
				agentName = "main"
			}

			resolved, err := cfg.ResolveAgentConfig(agentName)
			if err != nil {
				return fmt.Errorf("解析 Agent 配置失败: %w", err)
			}

			ws := resolved.Workspace
			if workspace != "" {
				ws = workspace
			}

			// 创建 ChatModel（用于记忆提取）
			chatModel, err := createMemoriaChatModel(ctx, resolved)
			if err != nil {
				return fmt.Errorf("创建 ChatModel 失败: %w", err)
			}

			// 构建 Memoria 配置选项
			opts := []memoria.ConfigOption{
				memoria.WithWorkspace(ws),
				memoria.WithKnowledgePaths(resolved.KnowledgePaths),
				memoria.WithInitialScan(!skipScan),
			}

			// 添加 Embedding 配置（L3 层）
			if !skipL3 {
				if embCfg := getEmbeddingConfig(resolved); embCfg != nil {
					opts = append(opts, memoria.WithEmbedding(embCfg))
				}
			}

			// 设置定时调度
			if chatInterval > 0 || fileInterval > 0 {
				scheduleCfg := &memoria.ScheduleConfig{
					Enabled:         true,
					ChatInterval:    time.Duration(chatInterval) * time.Minute,
					FileInterval:    time.Duration(fileInterval) * time.Minute,
					CleanupInterval: time.Hour,
				}
				opts = append(opts, memoria.WithScheduleConfig(scheduleCfg))
			}

			// 创建 Memoria 服务
			memoriaConfig := memoria.NewConfig(opts...)
			mem, err := memoria.New(memoriaConfig)
			if err != nil {
				return fmt.Errorf("创建 Memoria 服务失败: %w", err)
			}

			// 设置 ChatModel（用于记忆提取）
			mem.SetChatModel(chatModel)

			// 启动服务
			fmt.Printf("启动 Memoria 服务...\n")
			fmt.Printf("Workspace: %s\n", ws)
			fmt.Printf("存储目录: %s\n", mem.GetMemoriaDir())
			fmt.Printf("摘要模型: %s (Provider: %s)\n", resolved.SummarizeModel, resolved.SummarizeProvider)
			if resolved.EmbeddingProvider != "" && resolved.EmbeddingModel != "" {
				fmt.Printf("Embedding: %s (Provider: %s) [L3 已启用]\n", resolved.EmbeddingModel, resolved.EmbeddingProvider)
			} else {
				fmt.Printf("Embedding: 未配置 [L3 未启用]\n")
			}
			fmt.Printf("知识库路径: %d 个\n", len(resolved.KnowledgePaths))
			for i, kp := range resolved.KnowledgePaths {
				fmt.Printf("  [%d] %s (ext: %v, recursive: %v)\n", i+1, kp.Path, kp.Extensions, kp.Recursive)
			}
			fmt.Printf("Session 目录: %s\n", memoriaConfig.GetSessionDir())
			if !skipScan {
				fmt.Printf("初始扫描: 启用\n")
			} else {
				fmt.Printf("初始扫描: 跳过\n")
			}
			fmt.Println()

			if err := mem.Start(ctx); err != nil {
				mem.Close()
				return fmt.Errorf("启动服务失败: %w", err)
			}

			fmt.Println("服务已启动，按 Ctrl+C 停止")

			// 等待中断信号
			<-ctx.Done()

			fmt.Println("\n停止服务...")
			return mem.Close()
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "配置文件路径")
	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "工作目录")
	cmd.Flags().IntVarP(&chatInterval, "chat-interval", "", 5, "聊天处理间隔(分钟)")
	cmd.Flags().IntVarP(&fileInterval, "file-interval", "", 10, "文件处理间隔(分钟)")
	cmd.Flags().BoolVar(&skipScan, "skip-scan", false, "跳过初始扫描")
	cmd.Flags().BoolVar(&skipL3, "skip-l3", false, "跳过 L3 层初始化")

	return cmd
}

// EinoChatModelAdapter 将 eino ToolCallingChatModel 适配为 types.ChatModel
type EinoChatModelAdapter struct {
	Model model.ToolCallingChatModel
}

// Generate 实现 types.ChatModel 接口
func (a *EinoChatModelAdapter) Generate(ctx context.Context, prompt string) (string, error) {
	msgs := []*schema.Message{
		schema.UserMessage(prompt),
	}

	resp, err := a.Model.Generate(ctx, msgs)
	if err != nil {
		return "", err
	}

	return resp.Content, nil
}

// GenerateWithSystem 实现 types.ChatModel 接口
func (a *EinoChatModelAdapter) GenerateWithSystem(ctx context.Context, system, prompt string) (string, error) {
	msgs := []*schema.Message{
		schema.SystemMessage(system),
		schema.UserMessage(prompt),
	}

	resp, err := a.Model.Generate(ctx, msgs)
	if err != nil {
		return "", err
	}

	return resp.Content, nil
}

// createMemoriaChatModel 创建 Memoria 使用的 ChatModel
func createMemoriaChatModel(ctx context.Context, resolved *config.ResolvedAgentConfig) (types.ChatModel, error) {
	// 使用 SummarizeModel 配置，如果没有则使用主模型
	provider := resolved.SummarizeProvider
	modelName := resolved.SummarizeModel
	apiKey := resolved.SummarizeAPIKey
	apiBaseURL := resolved.SummarizeAPIBaseURL

	if provider == "" || modelName == "" || apiKey == "" {
		return nil, fmt.Errorf("缺少必要的模型配置 (provider=%s, model=%s)", provider, modelName)
	}

	// 使用 providers 包创建模型
	einoModel, err := providers.NewOpenAI(ctx, apiBaseURL, modelName, apiKey)
	if err != nil {
		return nil, err
	}

	// 包装为 types.ChatModel 接口
	return &EinoChatModelAdapter{Model: einoModel}, nil
}

// getEmbeddingConfig 从 resolved 配置获取 Embedding 配置
func getEmbeddingConfig(resolved *config.ResolvedAgentConfig) *memoria.EmbeddingConfig {
	if resolved.EmbeddingProvider == "" || resolved.EmbeddingModel == "" || resolved.EmbeddingAPIKey == "" {
		return nil
	}
	return &memoria.EmbeddingConfig{
		Provider:   resolved.EmbeddingProvider,
		Model:      resolved.EmbeddingModel,
		APIKey:     resolved.EmbeddingAPIKey,
		APIBaseURL: resolved.EmbeddingAPIBaseURL,
	}
}

// truncateStr 截断字符串
func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// NewMemoryInitCmd 创建初始化命令
func NewMemoryInitCmd() *cobra.Command {
	var (
		configPath string
		workspace  string
		skipL3     bool
	)

	cmd := &cobra.Command{
		Use:   "init",
		Short: "初始化记忆库",
		Long: `扫描知识库文件和聊天记录，提取记忆并存储。

此命令会:
1. 扫描配置的知识库路径 (knowledge_paths)
2. 扫描 session 目录下的聊天记录
3. 使用 LLM 提取记忆并分类存储到 L1/L2/L3 层
4. 初始化 L3 语义搜索层（需要 Embedding 配置）

需要配置:
- SummarizeModel: 用于记忆提取
- Embedding: 用于 L3 语义搜索`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			// 加载配置
			cfg, _, err := loadConfig(configPath)
			if err != nil {
				return fmt.Errorf("加载配置失败: %w", err)
			}

			agentName := cfg.GetDefaultAgentName()
			if agentName == "" {
				agentName = "main"
			}

			resolved, err := cfg.ResolveAgentConfig(agentName)
			if err != nil {
				return fmt.Errorf("解析 Agent 配置失败: %w", err)
			}

			ws := resolved.Workspace
			if workspace != "" {
				ws = workspace
			}

			// 创建 ChatModel（用于记忆提取）
			chatModel, err := createMemoriaChatModel(ctx, resolved)
			if err != nil {
				return fmt.Errorf("创建 ChatModel 失败: %w", err)
			}

			// 构建 Memoria 配置选项
			opts := []memoria.ConfigOption{
				memoria.WithWorkspace(ws),
				memoria.WithKnowledgePaths(resolved.KnowledgePaths),
				memoria.WithInitialScan(false), // 手动控制扫描
			}

			// 添加 Embedding 配置（L3 层）
			if !skipL3 {
				if embCfg := getEmbeddingConfig(resolved); embCfg != nil {
					opts = append(opts, memoria.WithEmbedding(embCfg))
				}
			}

			// 创建 Memoria 服务
			memoriaConfig := memoria.NewConfig(opts...)
			mem, err := memoria.New(memoriaConfig)
			if err != nil {
				return fmt.Errorf("创建 Memoria 服务失败: %w", err)
			}
			defer mem.Close()

			// 设置 ChatModel（用于记忆提取）
			mem.SetChatModel(chatModel)

			// 显示配置信息
			fmt.Printf("\n初始化记忆库...\n")
			fmt.Printf("Workspace: %s\n", ws)
			fmt.Printf("存储目录: %s\n", mem.GetMemoriaDir())
			fmt.Printf("摘要模型: %s (Provider: %s)\n", resolved.SummarizeModel, resolved.SummarizeProvider)
			fmt.Printf("知识库路径: %d 个\n", len(resolved.KnowledgePaths))
			for i, kp := range resolved.KnowledgePaths {
				fmt.Printf("  [%d] %s (ext: %v, recursive: %v)\n", i+1, kp.Path, kp.Extensions, kp.Recursive)
			}
			fmt.Printf("Session 目录: %s\n\n", memoriaConfig.GetSessionDir())

			// 显示扫描前的统计
			stats := mem.GetStats()
			fmt.Printf("扫描前 L1 记忆数: %d\n\n", stats["l1_items"])

			// 执行同步扫描
			fmt.Println("正在扫描知识库文件和聊天记录...")
			knowledgeItems, chatItems, err := mem.ScanAndProcess(ctx)
			if err != nil {
				return fmt.Errorf("扫描失败: %w", err)
			}

			// 显示扫描后的统计
			stats = mem.GetStats()
			fmt.Printf("\n扫描结果:\n")
			fmt.Printf("  知识库条目: %d\n", knowledgeItems)
			fmt.Printf("  聊天条目: %d\n", chatItems)
			fmt.Printf("  L1 记忆总数: %d\n", stats["l1_items"])
			if resolved.EmbeddingProvider != "" && resolved.EmbeddingModel != "" {
				fmt.Printf("  L3 层: 已启用语义搜索\n")
			}

			fmt.Println("\n初始化完成!")
			return nil
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "配置文件路径")
	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "工作目录")
	cmd.Flags().BoolVar(&skipL3, "skip-l3", false, "跳过 L3 层初始化")

	return cmd
}

// NewMemorySearchCmd 创建搜索命令
func NewMemorySearchCmd() *cobra.Command {
	var (
		configPath string
		workspace  string
		limit      int
		daysBack   int
		minScore   float64
		sourceType string
	)

	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "搜索记忆",
		Long: `搜索已存储的记忆内容 (L2 + L3)。

搜索策略:
1. 语义搜索优先 (向量相似度，理解含义)
2. 关键词搜索补充 (FTS5，精确匹配)
3. 层级顺序: L2 (摘要) → L3 (原始内容)
4. 不区分 Hall 类型 (搜索所有类型)

示例:
  kanflux memory search "database choice"
  kanflux memory search "performance optimization" --days 7
  kanflux memory search "auth" --min-score 0.7
  kanflux memory search "cache" --source chat`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			query := args[0]

			// 加载配置
			cfg, _, err := loadConfig(configPath)
			if err != nil {
				return fmt.Errorf("加载配置失败: %w", err)
			}

			agentName := cfg.GetDefaultAgentName()
			if agentName == "" {
				agentName = "main"
			}

			resolved, err := cfg.ResolveAgentConfig(agentName)
			if err != nil {
				return fmt.Errorf("解析 Agent 配置失败: %w", err)
			}

			ws := resolved.Workspace
			if workspace != "" {
				ws = workspace
			}

			// 构建 Memoria 配置选项
			opts := []memoria.ConfigOption{
				memoria.WithWorkspace(ws),
				memoria.WithInitialScan(false), // 搜索不需要初始化扫描
			}

			// 添加 Embedding 配置（语义搜索需要）
			if embCfg := getEmbeddingConfig(resolved); embCfg != nil {
				opts = append(opts, memoria.WithEmbedding(embCfg))
			} else {
				fmt.Println("警告: 无 Embedding 配置，语义搜索将不可用")
			}

			// 创建 Memoria 服务
			memoriaConfig := memoria.NewConfig(opts...)
			mem, err := memoria.New(memoriaConfig)
			if err != nil {
				return fmt.Errorf("创建 Memoria 服务失败: %w", err)
			}
			defer mem.Close()

			// 设置默认值
			if limit <= 0 {
				limit = 10
			}
			if daysBack <= 0 {
				daysBack = 30
			}
			if minScore <= 0 {
				minScore = 0.5
			}

			fmt.Printf("查询: %s\n", query)
			fmt.Printf("限制: %d 条结果\n", limit)
			fmt.Printf("时间范围: 最近 %d 天\n", daysBack)
			if sourceType != "" {
				fmt.Printf("来源类型: %s\n", sourceType)
			}
			fmt.Printf("最小相关性: %.2f\n\n", minScore)

			// 构建搜索选项 (L2 + L3 only, no HallType filter)
			searchOpts := &types.RetrieveOptions{
				Query: query,
				Limit: limit,
				TimeRange: &types.TimeRange{
					Start: time.Now().AddDate(0, 0, -daysBack),
					End:   time.Now(),
				},
			}

			if sourceType != "" {
				searchOpts.SourceType = types.SourceType(sourceType)
			}

			// 执行搜索
			results, err := mem.Search(ctx, query, searchOpts)
			if err != nil {
				return fmt.Errorf("搜索失败: %w", err)
			}

			// 过滤低分结果
			if minScore > 0 {
				filtered := make([]*types.SearchResult, 0)
				for _, r := range results {
					if r.Score >= minScore {
						filtered = append(filtered, r)
					}
				}
				results = filtered
			}

			fmt.Printf("存储目录: %s\n", mem.GetMemoriaDir())
			fmt.Println()

			if len(results) == 0 {
				fmt.Println("未找到匹配的记忆")
				fmt.Println("建议: 尝试自然语言描述，降低 min-score，或增加 days 范围")
				return nil
			}

			fmt.Printf("找到 %d 条记忆:\n\n", len(results))

			for i, r := range results {
				layerName := "L2"
				if r.Layer == types.LayerL3 {
					layerName = "L3"
				}

				// 显示结果
				fmt.Printf("--- [%d] %s ---\n", i+1, layerName)
				fmt.Printf("匹配类型: %s (分数: %.2f)\n", r.MatchType, r.Score)

				if r.Item.SourceType != "" {
					fmt.Printf("来源类型: %s\n", r.Item.SourceType)
				}

				// 显示内容
				if r.Item.Summary != "" {
					fmt.Printf("摘要: %s\n", truncateStr(r.Item.Summary, 150))
				} else {
					fmt.Printf("内容: %s\n", truncateStr(r.Item.Content, 150))
				}

				fmt.Printf("时间: %s\n", r.Item.Timestamp.Format("2006-01-02 15:04"))
				fmt.Printf("来源: %s\n", r.Item.Source)
				fmt.Println()
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "配置文件路径")
	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "工作目录")
	cmd.Flags().IntVarP(&limit, "limit", "l", 10, "结果数量限制")
	cmd.Flags().IntVarP(&daysBack, "days", "d", 30, "搜索最近天数")
	cmd.Flags().Float64Var(&minScore, "min-score", 0.5, "最小相关性分数 (0-1)")
	cmd.Flags().StringVar(&sourceType, "source", "", "来源类型过滤 (chat/file)")

	return cmd
}