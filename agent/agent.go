package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"sync"

	"github.com/cloudwego/eino/adk/middlewares/filesystem"
	"github.com/kinwyb/kanflux/agent/rag"
	"github.com/kinwyb/kanflux/agent/tools"
	"github.com/kinwyb/kanflux/config"
	"github.com/kinwyb/kanflux/session"

	localbk "github.com/cloudwego/eino-ext/adk/backend/local"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/skill"
	"github.com/cloudwego/eino/adk/prebuilt/deep"
	"github.com/cloudwego/eino/adk/prebuilt/planexecute"
	"github.com/cloudwego/eino/adk/prebuilt/supervisor"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

// AgentType alias to config.AgentType
type AgentType = config.AgentType

// Agent type constants
const (
	AgentTypeChatModel   AgentType = config.AgentTypeChatModel
	AgentTypeDeep        AgentType = config.AgentTypeDeep
	AgentTypePlanExecute AgentType = config.AgentTypePlanExecute
	AgentTypeSupervisor  AgentType = config.AgentTypeSupervisor
)

type Agent struct {
	loop   *looper
	cfg    *Config
	mu     sync.Mutex
	cancel context.CancelFunc
}

type Config struct {
	Name          string
	Type          AgentType // Agent 类型
	Description   string    // Agent 描述
	LLM           model.ToolCallingChatModel
	Workspace     string
	MaxIteration  int
	ToolRegister  *tools.Registry
	SkillDirs     []string // 支持多个 skill 目录
	SubAgents     []*Agent // 子 agent 实例
	SubAgentNames []string // 子 agent 名称（用于配置引用）
	Streaming     bool
	Tools         []string // 允许使用的工具列表，空表示所有工具可用
	ToolsApproval []string // 需要审批的工具列表
	// RAG 配置
	RAGManager rag.RAGManagerInterface // RAG 管理器接口
	// Session 配置
	SessionManager *session.Manager // Session 管理器（用于长期记忆）
}

// applyToolConfig 应用工具配置到 Registry
func applyToolConfig(cfg *Config) {
	if cfg.ToolRegister == nil {
		return
	}
	// 设置允许的工具白名单（空表示所有工具可用）
	if len(cfg.Tools) > 0 {
		cfg.ToolRegister.SetAllowedTools(cfg.Tools)
	}
	// 设置需要审批的工具列表
	if len(cfg.ToolsApproval) > 0 {
		cfg.ToolRegister.SetToolsApproval(cfg.ToolsApproval)
	}
}

// NewAgent 创建一个 agent（根据类型自动选择）
func NewAgent(ctx context.Context, cfg *Config) (*Agent, error) {
	// 默认使用 DeepAgent
	if cfg.Type == "" {
		cfg.Type = AgentTypeDeep
	}

	switch cfg.Type {
	case AgentTypeChatModel:
		return NewChatModelAgent(ctx, cfg)
	case AgentTypeDeep:
		return NewDeepAgent(ctx, cfg)
	case AgentTypePlanExecute:
		return NewPlanExecuteAgent(ctx, cfg)
	case AgentTypeSupervisor:
		return NewSupervisorAgent(ctx, cfg)
	default:
		return nil, fmt.Errorf("unknown agent type: %s", cfg.Type)
	}
}

// NewChatModelAgent 创建基础的 ChatModelAgent（ReAct 模式）
func NewChatModelAgent(ctx context.Context, cfg *Config) (*Agent, error) {
	if cfg.Name == "" {
		cfg.Name = "main"
	}
	description := cfg.Description
	if description == "" {
		description = fmt.Sprintf("Agent %s for general tasks", cfg.Name)
	}

	prompt, err := NewContextBuilder(cfg.Workspace)
	if err != nil {
		return nil, fmt.Errorf("上下文初始化失败: %w", err)
	}
	if cfg.RAGManager != nil {
		prompt.SetRAGManager(cfg.RAGManager)
	}
	// 设置长期记忆可用标志
	if cfg.SessionManager != nil && cfg.SessionManager.GetHistory() != nil {
		prompt.SetHasHistory(true)
	}
	if cfg.ToolRegister == nil {
		cfg.ToolRegister = tools.NewRegistry()
	}
	cfg.ToolRegister.Register(tools.NewMemoryTool(prompt.memory))

	// 注册历史对话检索工具
	if cfg.SessionManager != nil && cfg.SessionManager.GetHistory() != nil {
		cfg.ToolRegister.Register(session.NewHistorySearchTool(cfg.SessionManager.GetHistory()))
	}

	// 应用工具配置
	applyToolConfig(cfg)

	agentConfig := &adk.ChatModelAgentConfig{
		Name:          cfg.Name,
		Description:   description,
		Instruction:   prompt.BuildSystemPrompt(),
		Model:         cfg.LLM,
		MaxIterations: cfg.MaxIteration,
		Handlers: []adk.ChatModelAgentMiddleware{
			cfg.ToolRegister,
			&safeToolMiddleware{},
		},
	}

	toolAndShellMiddleware, _ := buildBuiltinAgentMiddlewares(ctx)
	if len(toolAndShellMiddleware) > 0 {
		agentConfig.Handlers = append(toolAndShellMiddleware, agentConfig.Handlers...)
	}

	if cfg.ToolRegister.ToolCount() > 0 {
		useTools, err := cfg.ToolRegister.GetTools()
		if err != nil {
			return nil, fmt.Errorf("工具注册失败: %w", err)
		}
		agentConfig.ToolsConfig = adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: useTools,
			},
		}
	}

	ag, err := adk.NewChatModelAgent(ctx, agentConfig)
	if err != nil {
		return nil, err
	}

	loop := newLooper(ctx, ag, cfg)
	ctx, cancel := context.WithCancel(ctx)

	return &Agent{
		loop:   loop,
		cfg:    cfg,
		cancel: cancel,
	}, nil
}

// NewDeepAgent 创建 DeepAgent（规划+文件系统+子agent）
func NewDeepAgent(ctx context.Context, cfg *Config) (*Agent, error) {
	if cfg.Name == "" {
		cfg.Name = "main"
	}
	description := cfg.Description
	if description == "" {
		description = fmt.Sprintf("Agent %s for general tasks", cfg.Name)
	}

	prompt, err := NewContextBuilder(cfg.Workspace)
	if err != nil {
		return nil, fmt.Errorf("上下文初始化失败: %w", err)
	}
	if cfg.RAGManager != nil {
		prompt.SetRAGManager(cfg.RAGManager)
	}
	// 设置长期记忆可用标志
	if cfg.SessionManager != nil && cfg.SessionManager.GetHistory() != nil {
		prompt.SetHasHistory(true)
	}
	if cfg.ToolRegister == nil {
		cfg.ToolRegister = tools.NewRegistry()
	}
	cfg.ToolRegister.Register(tools.NewMemoryTool(prompt.memory))

	// 注册 RAG 知识检索工具
	if cfg.RAGManager != nil {
		if kt := cfg.RAGManager.GetKnowledgeTool(); kt != nil {
			cfg.ToolRegister.Register(kt)
		}
	}

	// 应用工具配置
	applyToolConfig(cfg)

	backend, err := localbk.NewBackend(ctx, &localbk.Config{})
	if err != nil {
		return nil, fmt.Errorf("文件工具创建失败: %w", err)
	}

	// 构建子 agent 列表（adk.Agent 接口）
	subAgents := make([]adk.Agent, 0, len(cfg.SubAgents))
	for _, subAg := range cfg.SubAgents {
		if subAg.loop != nil && subAg.loop.agent != nil {
			subAgents = append(subAgents, subAg.loop.agent)
		}
	}

	agentConfig := &deep.Config{
		Name:                   cfg.Name,
		Description:            description,
		ChatModel:              cfg.LLM,
		Instruction:            prompt.BuildSystemPrompt(),
		SubAgents:              subAgents,
		MaxIteration:           cfg.MaxIteration,
		Backend:                backend,
		Shell:                  backend,
		WithoutWriteTodos:      false,
		WithoutGeneralSubAgent: false,
		Handlers: []adk.ChatModelAgentMiddleware{
			cfg.ToolRegister,
			&safeToolMiddleware{},
		},
	}

	if cfg.ToolRegister.ToolCount() > 0 {
		useTools, err := cfg.ToolRegister.GetTools()
		if err != nil {
			return nil, fmt.Errorf("工具注册失败: %w", err)
		}
		agentConfig.ToolsConfig = adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: useTools,
			},
		}
	}

	if len(cfg.SkillDirs) > 0 {
		skillBackends, err := NewSkillBackends(ctx, backend, cfg.SkillDirs)
		if err != nil {
			return nil, fmt.Errorf("failed to create skill backends: %w", err)
		}
		if len(skillBackends) > 0 {
			multiSkillBackend := NewMultiSkillBackend(skillBackends...)
			skillMiddleware, err := skill.NewMiddleware(ctx, &skill.Config{
				Backend: multiSkillBackend,
			})
			if err != nil {
				return nil, fmt.Errorf("failed to create skill middleware: %w", err)
			}
			agentConfig.Handlers = append(agentConfig.Handlers, skillMiddleware)
		}
	}

	ag, err := deep.New(ctx, agentConfig)
	if err != nil {
		return nil, err
	}

	loop := newLooper(ctx, ag, cfg)
	ctx, cancel := context.WithCancel(ctx)

	return &Agent{
		loop:   loop,
		cfg:    cfg,
		cancel: cancel,
	}, nil
}

// NewPlanExecuteAgent 创建 PlanExecute Agent（Plan-Execute-Replan 模式）
func NewPlanExecuteAgent(ctx context.Context, cfg *Config) (*Agent, error) {
	if cfg.Name == "" {
		cfg.Name = "main"
	}
	description := cfg.Description
	if description == "" {
		description = fmt.Sprintf("Agent %s for planning and execution", cfg.Name)
	}

	prompt, err := NewContextBuilder(cfg.Workspace)
	if err != nil {
		return nil, fmt.Errorf("上下文初始化失败: %w", err)
	}
	if cfg.RAGManager != nil {
		prompt.SetRAGManager(cfg.RAGManager)
	}
	// 设置长期记忆可用标志
	if cfg.SessionManager != nil && cfg.SessionManager.GetHistory() != nil {
		prompt.SetHasHistory(true)
	}
	if cfg.ToolRegister == nil {
		cfg.ToolRegister = tools.NewRegistry()
	}
	cfg.ToolRegister.Register(tools.NewMemoryTool(prompt.memory))

	// 注册历史对话检索工具
	if cfg.SessionManager != nil && cfg.SessionManager.GetHistory() != nil {
		cfg.ToolRegister.Register(session.NewHistorySearchTool(cfg.SessionManager.GetHistory()))
	}

	// 应用工具配置
	applyToolConfig(cfg)

	// 创建 Planner
	planner, err := planexecute.NewPlanner(ctx, &planexecute.PlannerConfig{
		ToolCallingChatModel: cfg.LLM,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create planner: %w", err)
	}

	// 准备 Executor 的工具配置
	toolsConfig := adk.ToolsConfig{}
	if cfg.ToolRegister.ToolCount() > 0 {
		useTools, err := cfg.ToolRegister.GetTools()
		if err != nil {
			return nil, fmt.Errorf("工具注册失败: %w", err)
		}
		toolsConfig = adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: useTools,
			},
		}
	}

	// 创建 Executor
	executor, err := planexecute.NewExecutor(ctx, &planexecute.ExecutorConfig{
		Model:         cfg.LLM,
		ToolsConfig:   toolsConfig,
		MaxIterations: cfg.MaxIteration,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create executor: %w", err)
	}

	// 创建 Replanner
	replanner, err := planexecute.NewReplanner(ctx, &planexecute.ReplannerConfig{
		ChatModel: cfg.LLM,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create replanner: %w", err)
	}

	// 创建 PlanExecute agent
	ag, err := planexecute.New(ctx, &planexecute.Config{
		Planner:       planner,
		Executor:      executor,
		Replanner:     replanner,
		MaxIterations: cfg.MaxIteration,
	})
	if err != nil {
		return nil, err
	}

	loop := newLooper(ctx, ag, cfg)
	ctx, cancel := context.WithCancel(ctx)

	return &Agent{
		loop:   loop,
		cfg:    cfg,
		cancel: cancel,
	}, nil
}

// NewSupervisorAgent 创建 Supervisor Agent（监督者模式）
func NewSupervisorAgent(ctx context.Context, cfg *Config) (*Agent, error) {
	if cfg.Name == "" {
		cfg.Name = "supervisor"
	}
	description := cfg.Description
	if description == "" {
		description = fmt.Sprintf("Supervisor agent coordinating %d sub-agents", len(cfg.SubAgents))
	}

	prompt, err := NewContextBuilder(cfg.Workspace)
	if err != nil {
		return nil, fmt.Errorf("上下文初始化失败: %w", err)
	}
	if cfg.RAGManager != nil {
		prompt.SetRAGManager(cfg.RAGManager)
	}
	// 设置长期记忆可用标志
	if cfg.SessionManager != nil && cfg.SessionManager.GetHistory() != nil {
		prompt.SetHasHistory(true)
	}
	if cfg.ToolRegister == nil {
		cfg.ToolRegister = tools.NewRegistry()
	}
	cfg.ToolRegister.Register(tools.NewMemoryTool(prompt.memory))

	// 注册历史对话检索工具
	if cfg.SessionManager != nil && cfg.SessionManager.GetHistory() != nil {
		cfg.ToolRegister.Register(session.NewHistorySearchTool(cfg.SessionManager.GetHistory()))
	}

	// 应用工具配置
	applyToolConfig(cfg)

	// 构建子 agent 列表（adk.Agent 接口）
	subAgents := make([]adk.Agent, 0, len(cfg.SubAgents))
	for _, subAg := range cfg.SubAgents {
		if subAg.loop != nil && subAg.loop.agent != nil {
			subAgents = append(subAgents, subAg.loop.agent)
		}
	}

	// 构建包含子 agent 信息的系统提示词
	systemPrompt := prompt.BuildSystemPrompt()
	if len(subAgents) > 0 {
		systemPrompt += buildSubAgentPrompt(ctx, subAgents)
	}

	// 创建 supervisor agent（使用 ChatModelAgent 作为 supervisor）
	supervisorConfig := &adk.ChatModelAgentConfig{
		Name:          cfg.Name,
		Description:   description,
		Instruction:   systemPrompt,
		Model:         cfg.LLM,
		MaxIterations: cfg.MaxIteration,
		Handlers: []adk.ChatModelAgentMiddleware{
			cfg.ToolRegister,
			&safeToolMiddleware{},
		},
	}

	toolAndShellMiddleware, _ := buildBuiltinAgentMiddlewares(ctx)
	if len(toolAndShellMiddleware) > 0 {
		supervisorConfig.Handlers = append(toolAndShellMiddleware, supervisorConfig.Handlers...)
	}

	supervisorAgent, err := adk.NewChatModelAgent(ctx, supervisorConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create supervisor agent: %w", err)
	}

	// 创建 Supervisor 结构
	supervisorConfig2 := &supervisor.Config{
		Supervisor: supervisorAgent,
		SubAgents:  subAgents,
	}

	ag, err := supervisor.New(ctx, supervisorConfig2)
	if err != nil {
		return nil, err
	}

	loop := newLooper(ctx, ag, cfg)
	ctx, cancel := context.WithCancel(ctx)

	return &Agent{
		loop:   loop,
		cfg:    cfg,
		cancel: cancel,
	}, nil
}

// buildSubAgentPrompt 构建子 agent 描述提示词
func buildSubAgentPrompt(ctx context.Context, subAgents []adk.Agent) string {
	if len(subAgents) == 0 {
		return ""
	}

	var prompt strings.Builder
	prompt.WriteString("\n\n---\n\n## Sub-Agent Task Assignment\n\n")
	prompt.WriteString("You are a Supervisor Agent responsible for coordinating multiple sub-agents to complete tasks.")
	prompt.WriteString("You can delegate tasks to the following sub-agents:\n\n")

	for _, agent := range subAgents {
		name := agent.Name(ctx)
		desc := agent.Description(ctx)
		prompt.WriteString(fmt.Sprintf("- **%s**: %s\n", name, desc))
	}

	prompt.WriteString("\n### Task Assignment Guidelines\n\n")
	prompt.WriteString("Assign tasks to the appropriate sub-agent based on task type and the sub-agent's capability description.\n")
	prompt.WriteString("When delegating a task, provide a clear task description and expected output format.\n")
	prompt.WriteString("After a sub-agent completes a task, it will return the result. You need to integrate the results and decide the next action.\n")

	return prompt.String()
}

// buildBuiltinAgentMiddlewares 生成文件操作及shell执行中间件
func buildBuiltinAgentMiddlewares(ctx context.Context) ([]adk.ChatModelAgentMiddleware, error) {
	var ms []adk.ChatModelAgentMiddleware
	backend, err := localbk.NewBackend(ctx, &localbk.Config{})
	if err != nil {
		return nil, fmt.Errorf("文件工具创建失败: %w", err)
	}

	if backend != nil {
		fm, err := filesystem.New(ctx, &filesystem.MiddlewareConfig{
			Backend:        backend,
			Shell:          backend,
			StreamingShell: nil,
		})
		if err != nil {
			return nil, err
		}
		ms = append(ms, fm)
	}
	return ms, nil
}

// Prompt sends a user message to the agent
func (a *Agent) Prompt(ctx context.Context, messages []adk.Message, checkPointID string) ([]adk.Message, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Run orchestrator
	msg, err := a.loop.Run(ctx, messages, checkPointID)
	if err != nil {
		slog.Error("Agent execution failed", "error", err)
		return nil, err
	}
	return msg, nil
}

// Stop 停止 agent
func (a *Agent) Stop() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.cancel != nil {
		a.cancel()
	}
	return nil
}

// RegisterCallback 注册事件回调，返回唯一 ID 用于注销
func (a *Agent) RegisterCallback(cb EventCallback) int64 {
	if a.loop != nil {
		return a.loop.RegisterCallback(cb)
	}
	return 0
}

// UnregisterCallback 注销事件回调
func (a *Agent) UnregisterCallback(id int64) {
	if a.loop != nil {
		a.loop.emit(NewEvent(EventUnregisterCallback).WithMetadata(map[string]interface{}{"id": id}))
	}
}

// Resume 恢复被中断的agent执行
func (a *Agent) Resume(ctx context.Context, checkPointID string, params any) ([]adk.Message, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.loop == nil {
		return nil, fmt.Errorf("looper not initialized")
	}

	msg, err := a.loop.Resume(ctx, checkPointID, params)
	if err != nil {
		slog.Error("Agent resume failed", "error", err)
		return nil, err
	}
	return msg, nil
}

// GetInterruptInfo 获取指定session的中断信息
func (a *Agent) GetInterruptInfo(checkPointID string) *InterruptInfo {
	cp := a.loop.GetInterruptInfo(checkPointID)
	if cp == nil {
		return nil
	}
	return &InterruptInfo{
		InterruptID:       cp.InterruptID,
		InterruptInfo:     cp.InterruptInfo,
		EndpointParamType: cp.EndpointParamType,
		ResumeParamType:   cp.ResumeParamType,
	}
}

// InterruptInfo 中断信息结构
type InterruptInfo struct {
	InterruptID       string
	InterruptInfo     string
	EndpointParamType reflect.Type // 端点参数类型（中断时传递的信息类型）
	ResumeParamType   reflect.Type // 恢复参数类型（恢复时期望接收的参数类型）
}

type safeToolMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
}

func (m *safeToolMiddleware) WrapInvokableToolCall(
	_ context.Context,
	endpoint adk.InvokableToolCallEndpoint,
	_ *adk.ToolContext,
) (adk.InvokableToolCallEndpoint, error) {
	return func(ctx context.Context, args string, opts ...tool.Option) (string, error) {
		result, err := endpoint(ctx, args, opts...)
		if err != nil {
			if _, ok := compose.IsInterruptRerunError(err); ok {
				return "", err
			}
			return fmt.Sprintf("[tool error] %v", err), nil
		}
		return result, nil
	}, nil
}

func (m *safeToolMiddleware) WrapStreamableToolCall(
	_ context.Context,
	endpoint adk.StreamableToolCallEndpoint,
	_ *adk.ToolContext,
) (adk.StreamableToolCallEndpoint, error) {
	return func(ctx context.Context, args string, opts ...tool.Option) (*schema.StreamReader[string], error) {
		sr, err := endpoint(ctx, args, opts...)
		if err != nil {
			if _, ok := compose.IsInterruptRerunError(err); ok {
				return nil, err
			}
			return m.singleChunkReader(fmt.Sprintf("[tool error] %v", err)), nil
		}
		return m.safeWrapReader(sr), nil
	}, nil
}

func (m *safeToolMiddleware) singleChunkReader(msg string) *schema.StreamReader[string] {
	r, w := schema.Pipe[string](1)
	_ = w.Send(msg, nil)
	w.Close()
	return r
}

func (m *safeToolMiddleware) safeWrapReader(sr *schema.StreamReader[string]) *schema.StreamReader[string] {
	r, w := schema.Pipe[string](64)
	go func() {
		defer w.Close()
		for {
			chunk, err := sr.Recv()
			if errors.Is(err, io.EOF) {
				return
			}
			if err != nil {
				_ = w.Send(fmt.Sprintf("\n[tool error] %v", err), nil)
				return
			}
			_ = w.Send(chunk, nil)
		}
	}()
	return r
}
