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
	"github.com/kinwyb/kanflux/knowledgebase/memoria"
	"github.com/kinwyb/kanflux/knowledgebase/memoria/types"
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