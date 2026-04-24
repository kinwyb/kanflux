package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kinwyb/kanflux/agent"
	"github.com/kinwyb/kanflux/agent/tools"
	"github.com/kinwyb/kanflux/bus"
	"github.com/kinwyb/kanflux/channel"
	"github.com/kinwyb/kanflux/config"
	"github.com/kinwyb/kanflux/providers"
	"github.com/kinwyb/kanflux/session"

	"github.com/cloudwego/eino/schema"
	"github.com/spf13/cobra"
)

// NewAgentCmd 创建agent子命令
func NewAgentCmd() *cobra.Command {
	var (
		configPath  string
		agentName   string
		message     string
		streaming   bool
		listAgents  bool
		channelName string
		chatID      string
	)

	cmd := &cobra.Command{
		Use:   "agent [message]",
		Short: "直接向指定的Agent发送消息并返回结果",
		Long: `通过命令行直接向配置文件中定义的Agent发送消息，输出Agent的响应结果。

支持两种模式：
1. 直接模式（无 --channel 参数）：直接输出到终端
2. Channel 模式（有 --channel 参数）：通过指定 channel 发送消息，用于验证 channel 功能`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			// 加载配置文件
			cfg, _, err := loadConfig(configPath)
			if err != nil {
				return fmt.Errorf("加载配置失败: %w", err)
			}

			// 如果只是列出agents
			if listAgents {
				return listAvailableAgents(cfg)
			}

			// 消息来源：参数或 --message 标志
			if len(args) > 0 {
				message = args[0]
			}
			if message == "" {
				return fmt.Errorf("请提供要发送的消息（作为参数或使用 --message 标志）")
			}

			// 如果指定了 channel，使用 Channel 模式
			if channelName != "" {
				return runWithChannel(ctx, cfg, agentName, channelName, chatID, message, streaming)
			}

			// 否则使用直接模式
			return runDirect(ctx, cfg, agentName, message, streaming)
		},
	}

	// 命令行参数
	cmd.Flags().StringVarP(&configPath, "config", "c", "", "配置文件路径")
	cmd.Flags().StringVarP(&agentName, "agent", "a", "", "Agent名称 (未指定时使用默认Agent)")
	cmd.Flags().StringVarP(&message, "message", "m", "", "要发送的消息")
	cmd.Flags().BoolVarP(&streaming, "stream", "s", true, "启用流式输出")
	cmd.Flags().BoolVarP(&listAgents, "list", "l", false, "列出配置文件中所有可用的Agent")
	cmd.Flags().StringVarP(&channelName, "channel", "C", "", "指定 channel 名称 (如 wxcom, telegram)")
	cmd.Flags().StringVar(&chatID, "chat-id", "", "指定聊天ID (channel 模式必需)")

	return cmd
}

// runDirect 直接模式：创建 Agent 直接发送消息
func runDirect(ctx context.Context, cfg *config.Config, agentName, message string, streaming bool) error {
	// 如果未指定agent名称，使用默认agent
	if agentName == "" {
		agentName = cfg.GetDefaultAgentName()
	}
	if agentName == "" {
		return fmt.Errorf("未指定Agent名称且配置文件中没有默认Agent")
	}

	// 解析agent配置
	resolved, err := cfg.ResolveAgentConfig(agentName)
	if err != nil {
		return fmt.Errorf("解析Agent配置失败: %w", err)
	}

	// 创建LLM
	llm, err := providers.NewOpenAI(ctx, resolved.APIBaseURL, resolved.Model, resolved.APIKey)
	if err != nil {
		return fmt.Errorf("创建LLM失败: %w", err)
	}

	// 创建工具注册器
	toolRegistry := tools.NewRegistry()

	// 获取默认skills目录
	skillDirs := config.GetDefaultSkillDirs(resolved.Workspace)

	// 处理子agent（如果配置中有）
	var subAgents []*agent.Agent
	if len(resolved.SubAgents) > 0 {
		subAgents, err = createSubAgents(ctx, cfg, resolved.SubAgents)
		if err != nil {
			return fmt.Errorf("创建子Agent失败: %w", err)
		}
	}

	// 创建Agent配置
	agentCfg := &agent.Config{
		Name:          resolved.Name,
		Type:          resolved.Type,
		Description:   resolved.Description,
		LLM:           llm,
		Workspace:     resolved.Workspace,
		MaxIteration:  resolved.MaxIteration,
		ToolRegister:  toolRegistry,
		SkillDirs:     skillDirs,
		SubAgents:     subAgents,
		SubAgentNames: resolved.SubAgents,
		Streaming:     streaming,
	}

	// 创建Agent
	ag, err := agent.NewAgent(ctx, agentCfg)
	if err != nil {
		return fmt.Errorf("创建Agent失败: %w", err)
	}

	// 创建会话管理器
	sessionMgr, err := session.NewManager(resolved.Workspace)
	if err != nil {
		return fmt.Errorf("创建会话管理器失败: %w", err)
	}

	// 创建会话
	sessionKey := fmt.Sprintf("cli:%s:%d", agentName, time.Now().Unix())
	sess, err := sessionMgr.GetOrCreate(sessionKey)
	if err != nil {
		return fmt.Errorf("创建会话失败: %w", err)
	}

	// 加载历史消息
	maxHistory := 50
	history := sess.GetHistorySafe(maxHistory)

	// 构建消息
	userMsg := schema.UserMessage(message)
	allMessages := append(history, userMsg)

	// 注册回调处理流式输出
	if streaming {
		cbID := ag.RegisterCallback(func(event *agent.Event) {
			switch event.Type {
			case agent.EventMessageUpdate:
				if event.Message != nil {
					if event.Message.ReasoningContent != "" {
						fmt.Fprint(os.Stderr, "\r[思考] "+event.Message.ReasoningContent)
					} else if event.Message.Content != "" {
						fmt.Fprint(os.Stdout, event.Message.Content)
					}
				}
			case agent.EventToolStart:
				if event.Message != nil && len(event.Message.ToolCalls) > 0 {
					toolName := event.Message.ToolCalls[0].Function.Name
					fmt.Fprintln(os.Stderr, "\n[工具调用] "+toolName)
				}
			case agent.EventToolEnd:
				if event.Message != nil {
					result := truncate(event.Message.Content, 100)
					fmt.Fprintln(os.Stderr, "[工具结果] "+result)
				}
			}
		})
		defer ag.UnregisterCallback(cbID)
	}

	// 发送消息给Agent
	resp, err := ag.Prompt(ctx, allMessages, sessionKey, "")
	if err != nil {
		return fmt.Errorf("Agent处理失败: %w", err)
	}

	// 保存会话
	if len(resp) > len(history) {
		newResp := resp[len(history):]
		for _, m := range newResp {
			if m.Extra == nil {
				m.Extra = make(map[string]interface{})
			}
			if m.Extra["timestamp"] == nil {
				m.Extra["timestamp"] = time.Now().Format(time.RFC3339)
			}
			sess.AddMessage(m)
		}
	}
	sessionMgr.Save(sess)

	// 输出结果
	if len(resp) > 0 {
		lastMsg := resp[len(resp)-1]
		if !streaming {
			// 非流式模式，直接输出完整响应
			fmt.Fprintln(os.Stdout, lastMsg.Content)
		} else {
			// 流式模式，确保最后有换行
			fmt.Fprintln(os.Stdout)
		}
	}

	return nil
}

// runWithChannel Channel 模式：通过 channel 发送消息
func runWithChannel(ctx context.Context, cfg *config.Config, agentName, channelName, chatID, message string, streaming bool) error {
	// 验证参数
	if chatID == "" {
		return fmt.Errorf("channel 模式需要指定 --chat-id 参数")
	}

	// 如果未指定agent名称，使用默认agent
	if agentName == "" {
		agentName = cfg.GetDefaultAgentName()
	}
	if agentName == "" {
		return fmt.Errorf("未指定Agent名称且配置文件中没有默认Agent")
	}

	// 解析 agent 配置获取 workspace
	resolved, err := cfg.ResolveAgentConfig(agentName)
	if err != nil {
		return fmt.Errorf("解析Agent配置失败: %w", err)
	}

	// 创建 MessageBus
	msgBus := bus.NewMessageBus(100)

	// 创建 SessionManager
	sessionMgr, err := session.NewManager(resolved.Workspace)
	if err != nil {
		return fmt.Errorf("创建会话管理器失败: %w", err)
	}

	// 创建 AgentManager
	agentMgr := agent.NewManager(msgBus, sessionMgr)
	if err := agentMgr.RegisterAgentsFromConfig(ctx, cfg, nil); err != nil {
		return fmt.Errorf("注册Agent失败: %w", err)
	}

	// 创建 ChannelManager
	channelMgr := channel.NewManager(msgBus)
	if err := channelMgr.InitializeFromConfig(ctx, cfg.Channels); err != nil {
		return fmt.Errorf("初始化Channel失败: %w", err)
	}

	// 验证指定的 channel 是否存在
	channels := channelMgr.List()
	channelFound := false
	for _, ch := range channels {
		if ch == channelName {
			channelFound = true
			break
		}
	}
	if !channelFound {
		return fmt.Errorf("指定的 channel '%s' 不存在，可用 channels: %v", channelName, channels)
	}

	// 订阅 OutboundMessage
	streamingMode := bus.StreamingModeDelta
	if !streaming {
		streamingMode = bus.StreamingModeAccumulate
	}

	outSub := msgBus.SubscribeOutboundFiltered([]string{channelName})
	chatSub := msgBus.SubscribeChatEventFiltered([]string{channelName})

	// 处理完成信号
	done := make(chan struct{})
	var lastContent string

	// 处理出站消息
	go func() {
		for {
			select {
			case msg, ok := <-outSub.Channel:
				if !ok {
					return
				}
				if msg.ChatID == chatID {
					if msg.IsStreaming && !msg.IsFinal {
						// 流式增量输出
						if msg.IsThinking {
							fmt.Fprint(os.Stderr, "\r[思考] "+msg.Content)
						} else {
							fmt.Fprint(os.Stdout, msg.Content)
						}
					} else if msg.IsFinal {
						// 最终消息
						lastContent = msg.Content
					}
					if msg.Error != "" {
						fmt.Fprintln(os.Stderr, "[错误] "+msg.Error)
					}
				}
			case event, ok := <-chatSub.Channel:
				if !ok {
					return
				}
				if event.ChatID == chatID {
					switch event.State {
					case bus.ChatEventStateStart:
						fmt.Fprintln(os.Stderr, "[开始处理]")
					case bus.ChatEventStateTool:
						if event.ToolInfo != nil {
							if event.ToolInfo.IsStart {
								fmt.Fprintln(os.Stderr, "[工具调用] "+event.ToolInfo.Name)
							} else {
								result := truncate(event.ToolInfo.Result, 100)
								fmt.Fprintln(os.Stderr, "[工具结果] "+result)
							}
						}
					case bus.ChatEventStateComplete:
						fmt.Fprintln(os.Stderr, "[处理完成]")
						close(done)
						return
					case bus.ChatEventStateError:
						fmt.Fprintln(os.Stderr, "[错误] "+event.Error)
						close(done)
						return
					case bus.ChatEventStateInterrupt:
						fmt.Fprintln(os.Stderr, "[中断] 等待用户确认")
					}
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// 启动 AgentManager
	agentMgr.Start(ctx)

	// 启动 ChannelManager
	if err := channelMgr.StartAll(ctx); err != nil {
		return fmt.Errorf("启动Channel失败: %w", err)
	}

	// 构建并发布 InboundMessage
	inbound := &bus.InboundMessage{
		ID:            fmt.Sprintf("cli-%d", time.Now().UnixNano()),
		Channel:       channelName,
		AccountID:     "",
		SenderID:      "cli_user",
		ChatID:        chatID,
		Content:       message,
		StreamingMode: streamingMode,
		Timestamp:     time.Now(),
	}

	// 设置目标 agent
	if agentName != "" {
		inbound.Metadata = map[string]interface{}{
			"target_agent": agentName,
		}
	}

	msgBus.PublishInbound(ctx, inbound)

	// 等待处理完成或中断信号
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-done:
		// 处理完成
		if !streaming && lastContent != "" {
			fmt.Fprintln(os.Stdout, lastContent)
		} else if streaming {
			fmt.Fprintln(os.Stdout) // 确保最后有换行
		}
	case sig := <-sigChan:
		fmt.Fprintf(os.Stderr, "\n收到信号 %v，正在停止...\n", sig)
	case <-ctx.Done():
		fmt.Fprintln(os.Stderr, "\n上下文已取消")
	}

	// 清理
	outSub.Unsubscribe()
	chatSub.Unsubscribe()
	channelMgr.StopAll()
	agentMgr.Stop()

	return nil
}

// listAvailableAgents 列出所有可用的agents
func listAvailableAgents(cfg *config.Config) error {
	agents := cfg.GetAllAgentNames()
	if len(agents) == 0 {
		fmt.Fprintln(os.Stdout, "配置文件中没有定义任何Agent")
		return nil
	}

	defaultAgent := cfg.GetDefaultAgentName()
	fmt.Fprintln(os.Stdout, "可用的Agent列表:")
	for _, name := range agents {
		resolved, err := cfg.ResolveAgentConfig(name)
		if err != nil {
			fmt.Fprintf(os.Stdout, "  - %s (配置解析失败: %v)\n", name, err)
			continue
		}
		marker := ""
		if name == defaultAgent {
			marker = " [默认]"
		}
		fmt.Fprintf(os.Stdout, "  - %s%s\n", name, marker)
		fmt.Fprintf(os.Stdout, "      类型: %s\n", resolved.Type)
		fmt.Fprintf(os.Stdout, "      描述: %s\n", resolved.Description)
		fmt.Fprintf(os.Stdout, "      模型: %s\n", resolved.Model)
		fmt.Fprintf(os.Stdout, "      工作区: %s\n", resolved.Workspace)
		if len(resolved.SubAgents) > 0 {
			fmt.Fprintf(os.Stdout, "      子Agent: %v\n", resolved.SubAgents)
		}
	}
	return nil
}

// createSubAgents 创建子agent实例
func createSubAgents(ctx context.Context, cfg *config.Config, subAgentNames []string) ([]*agent.Agent, error) {
	var subAgents []*agent.Agent

	// 按依赖顺序创建（先创建叶子节点）
	created := make(map[string]*agent.Agent)

	// 简单递归创建
	for _, name := range subAgentNames {
		ag, err := createAgentWithSubs(ctx, cfg, name, created)
		if err != nil {
			return nil, err
		}
		created[name] = ag
		subAgents = append(subAgents, ag)
	}

	return subAgents, nil
}

// createAgentWithSubs 递归创建agent及其子agent
func createAgentWithSubs(ctx context.Context, cfg *config.Config, name string, created map[string]*agent.Agent) (*agent.Agent, error) {
	// 如果已创建，直接返回
	if ag, ok := created[name]; ok {
		return ag, nil
	}

	// 解析配置
	resolved, err := cfg.ResolveAgentConfig(name)
	if err != nil {
		return nil, err
	}

	// 先创建子agent
	var subAgents []*agent.Agent
	for _, subName := range resolved.SubAgents {
		subAg, err := createAgentWithSubs(ctx, cfg, subName, created)
		if err != nil {
			return nil, err
		}
		created[subName] = subAg
		subAgents = append(subAgents, subAg)
	}

	// 创建LLM
	llm, err := providers.NewOpenAI(ctx, resolved.APIBaseURL, resolved.Model, resolved.APIKey)
	if err != nil {
		return nil, err
	}

	// 创建工具注册器
	toolRegistry := tools.NewRegistry()

	// 获取skills目录
	skillDirs := config.GetDefaultSkillDirs(resolved.Workspace)

	// 创建Agent配置
	agentCfg := &agent.Config{
		Name:          resolved.Name,
		Type:          resolved.Type,
		Description:   resolved.Description,
		LLM:           llm,
		Workspace:     resolved.Workspace,
		MaxIteration:  resolved.MaxIteration,
		ToolRegister:  toolRegistry,
		SkillDirs:     skillDirs,
		SubAgents:     subAgents,
		SubAgentNames: resolved.SubAgents,
		Streaming:     false, // 子agent不需要流式
	}

	// 创建Agent
	ag, err := agent.NewAgent(ctx, agentCfg)
	if err != nil {
		return nil, err
	}

	return ag, nil
}

// truncate 截断字符串
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
