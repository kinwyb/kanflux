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
			cfg, err := loadConfig(configPath)
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
			cfg, err := loadConfig(configPath)
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
	)

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "启动后台记忆服务",
		Long:  `启动 Memoria 后台服务，定时处理聊天记录和文件修改。`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// 加载配置
			cfg, err := loadConfig(configPath)
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

			// 创建 ChatModel
			chatModel, err := createMemoriaChatModel(ctx, resolved)
			if err != nil {
				return fmt.Errorf("创建 ChatModel 失败: %w", err)
			}

			// 创建 Memoria 配置
			memoriaConfig := &memoria.Config{
				Workspace: ws,
			}

			// 设置定时调度
			if chatInterval > 0 || fileInterval > 0 {
				memoriaConfig.ScheduleConfig = &memoria.ScheduleConfig{
					Enabled:         true,
					ChatInterval:    time.Duration(chatInterval) * time.Minute,
					FileInterval:    time.Duration(fileInterval) * time.Minute,
					CleanupInterval: time.Hour,
				}
			}

			// 创建 Memoria 服务
			mem, err := memoria.New(memoriaConfig)
			if err != nil {
				return fmt.Errorf("创建 Memoria 服务失败: %w", err)
			}

			// 设置 ChatModel
			mem.SetChatModel(chatModel)

			// 启动服务
			fmt.Printf("启动 Memoria 服务...\n")
			fmt.Printf("Workspace: %s\n", ws)
			fmt.Printf("存储目录: %s\n", mem.GetMemoriaDir())
			fmt.Printf("模型: %s (Provider: %s)\n\n", resolved.SummarizeModel, resolved.SummarizeProvider)

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

// truncateStr 截断字符串
func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// NewMemorySearchCmd 创建搜索命令
func NewMemorySearchCmd() *cobra.Command {
	var (
		configPath string
		workspace  string
		mode       string
		limit      int
		daysBack   int
		minScore   float64
		sourceType string
		hallTypes  string
	)

	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "搜索记忆",
		Long: `搜索已存储的记忆内容。

支持两种搜索模式:
- keyword: 关键词搜索，快速匹配，仅搜索 chat 内容 (L1+L2+L3)
- semantic: 语义搜索，深度理解，搜索 chat+file 内容 (L2+L3)

示例:
  kanflux memory search "database choice"                    # 关键词搜索
  kanflux memory search "performance" -m semantic            # 语义搜索
  kanflux memory search "auth" --days 7 --hall facts         # 最近7天的决策
  kanflux memory search "cache" -m semantic --min-score 0.7  # 高相关性`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			query := args[0]

			// 加载配置
			cfg, err := loadConfig(configPath)
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

			// 创建 Memoria 服务
			memoriaConfig := memoria.NewConfig(
				memoria.WithWorkspace(ws),
				memoria.WithInitialScan(false), // 搜索不需要初始化扫描
			)

			mem, err := memoria.New(memoriaConfig)
			if err != nil {
				return fmt.Errorf("创建 Memoria 服务失败: %w", err)
			}
			defer mem.Close()

			// 设置默认值
			if mode == "" {
				mode = "keyword"
			}
			if limit <= 0 {
				limit = 10
			}
			if daysBack <= 0 {
				daysBack = 30
			}
			if minScore <= 0 {
				minScore = 0.5
			}

			fmt.Printf("搜索模式: %s\n", mode)
			fmt.Printf("查询: %s\n", query)
			fmt.Printf("限制: %d 条结果\n\n", limit)

			// 构建搜索选项
			opts := &types.RetrieveOptions{
				Query: query,
				Limit: limit,
			}

			if mode == "keyword" {
				opts.SearchMode = types.SearchModeKeyword
				opts.SourceType = types.SourceTypeChat
				opts.Layers = []types.Layer{types.LayerL1, types.LayerL2, types.LayerL3}
				opts.TimeRange = &types.TimeRange{
					Start: time.Now().AddDate(0, 0, -daysBack),
					End:   time.Now(),
				}

				// 解析 hallTypes
				if hallTypes != "" {
					typesList := strings.Split(hallTypes, ",")
					opts.HallTypes = make([]types.HallType, 0, len(typesList))
					for _, t := range typesList {
						t = strings.TrimSpace(t)
						if t != "" {
							// 支持简写: facts, events, discoveries, preferences, advice
							fullName := "hall_" + t
							opts.HallTypes = append(opts.HallTypes, types.HallType(fullName))
						}
					}
				}

				fmt.Printf("时间范围: 最近 %d 天\n", daysBack)
				if len(opts.HallTypes) > 0 {
					fmt.Printf("Hall 类型: %v\n", opts.HallTypes)
				}
			} else {
				opts.SearchMode = types.SearchModeSemantic
				opts.Layers = []types.Layer{types.LayerL2, types.LayerL3}

				if sourceType != "" {
					opts.SourceType = types.SourceType(sourceType)
					fmt.Printf("来源类型: %s\n", sourceType)
				}
				fmt.Printf("最小相关性: %.2f\n", minScore)
			}

			// 执行搜索
			results, err := mem.Search(ctx, query, opts)
			if err != nil {
				return fmt.Errorf("搜索失败: %w", err)
			}

			// 过滤语义搜索结果
			if mode == "semantic" && minScore > 0 {
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
				if mode == "keyword" {
					fmt.Println("建议: 尝试不同的关键词，增加 days 范围，或使用 semantic 模式")
				} else {
					fmt.Println("建议: 尝试自然语言描述，降低 min-score，或使用 keyword 模式")
				}
				return nil
			}

			fmt.Printf("找到 %d 条记忆:\n\n", len(results))

			for i, r := range results {
				layerName := "L1"
				if r.Layer == types.LayerL2 {
					layerName = "L2"
				} else if r.Layer == types.LayerL3 {
					layerName = "L3"
				}

				// 显示结果
				fmt.Printf("--- [%d] %s ---\n", i+1, layerName)
				fmt.Printf("匹配类型: %s (分数: %.2f)\n", r.MatchType, r.Score)

				if r.Item.HallType != "" {
					hallName := strings.Replace(string(r.Item.HallType), "hall_", "", 1)
					fmt.Printf("Hall: %s\n", hallName)
				}

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
	cmd.Flags().StringVarP(&mode, "mode", "m", "keyword", "搜索模式 (keyword/semantic)")
	cmd.Flags().IntVarP(&limit, "limit", "l", 10, "结果数量限制")
	cmd.Flags().IntVarP(&daysBack, "days", "d", 30, "keyword模式: 搜索最近天数")
	cmd.Flags().Float64Var(&minScore, "min-score", 0.5, "semantic模式: 最小相关性分数 (0-1)")
	cmd.Flags().StringVar(&sourceType, "source", "", "semantic模式: 来源类型过滤 (chat/file)")
	cmd.Flags().StringVar(&hallTypes, "hall", "", "keyword模式: Hall类型过滤 (facts,events,discoveries,preferences,advice)")

	return cmd
}
