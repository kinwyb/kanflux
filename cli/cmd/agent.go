package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/kinwyb/kanflux/agent"
	"github.com/kinwyb/kanflux/agent/tools"
	"github.com/kinwyb/kanflux/config"
	"github.com/kinwyb/kanflux/providers"
	"github.com/kinwyb/kanflux/session"

	"github.com/cloudwego/eino/schema"
	"github.com/spf13/cobra"
)

// NewAgentCmd 创建agent子命令
func NewAgentCmd() *cobra.Command {
	var (
		configPath string
		agentName  string
		message    string
		streaming  bool
		listAgents bool
	)

	cmd := &cobra.Command{
		Use:   "agent [message]",
		Short: "直接向指定的Agent发送消息并返回结果",
		Long:  `通过命令行直接向配置文件中定义的Agent发送消息，输出Agent的响应结果。`,
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			// 加载配置文件
			cfg, err := loadConfig(configPath)
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
			resp, err := ag.Prompt(ctx, allMessages, sessionKey)
			if err != nil {
				return fmt.Errorf("Agent处理失败: %w", err)
			}

			// 保存会话
			if len(resp) > len(history) {
				newResp := resp[len(history):]
				for _, m := range newResp {
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
		},
	}

	// 命令行参数
	cmd.Flags().StringVarP(&configPath, "config", "c", "", "配置文件路径")
	cmd.Flags().StringVarP(&agentName, "agent", "a", "", "Agent名称 (未指定时使用默认Agent)")
	cmd.Flags().StringVarP(&message, "message", "m", "", "要发送的消息")
	cmd.Flags().BoolVarP(&streaming, "stream", "s", true, "启用流式输出")
	cmd.Flags().BoolVarP(&listAgents, "list", "l", false, "列出配置文件中所有可用的Agent")

	return cmd
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