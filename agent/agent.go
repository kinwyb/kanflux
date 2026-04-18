package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"sync"

	"github.com/cloudwego/eino/adk/middlewares/filesystem"
	"github.com/cloudwego/eino/schema"
	jsonschema "github.com/eino-contrib/jsonschema"
	"github.com/kinwyb/kanflux/agent/tools"
	"github.com/kinwyb/kanflux/bus"
	"github.com/kinwyb/kanflux/config"
	"github.com/kinwyb/kanflux/memoria"
	"github.com/kinwyb/kanflux/scheduler"
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
	// MCP 工具配置
	MCPConfigs []tools.MCPConfig // MCP 工具配置列表
	// Browser 工具配置
	BrowserEnabled    bool   // 是否启用 Browser 工具
	BrowserHeadless   bool   // 是否使用 headless 模式
	BrowserTimeout    int    // 操作超时时间（秒）
	BrowserRelayURL   string // Relay 服务 URL
	BrowserRelayMode  string // 连接模式
	// Web 工具配置
	WebEnabled      bool   // 是否启用 Web 工具
	WebSearchAPIKey string // 搜索 API Key
	WebSearchEngine string // 搜索引擎
	WebTimeout      int    // 请求超时时间（秒）
	// Scheduler 工具配置
	SchedulerEnabled bool              // 是否启用 Scheduler 工具
	Scheduler        scheduler.Accessor // Scheduler 访问接口
	// Memoria 统一记忆系统（替代 RAGManager）
	Memoria *memoria.Memoria // 统一记忆系统：L1/L2/L3 三层架构
	// Session 配置
	SessionManager *session.Manager // Session 管理器
	// Bus 配置（用于 SendFileTool 等需要 bus 的工具）
	Bus       interface{} // *bus.MessageBus（避免循环导入）
	ResponseMgr interface{} // *bus.RequestResponseManager
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

// registerBuiltinTools 注册内置工具（Browser、Web、Scheduler）
func registerBuiltinTools(cfg *Config) {
	if cfg.ToolRegister == nil {
		return
	}

	// 注册 Browser 工具
	if cfg.BrowserEnabled {
		browserTool := tools.NewBrowserCDPToolWithRelay(
			cfg.BrowserHeadless,
			cfg.BrowserTimeout,
			cfg.BrowserRelayURL,
			cfg.BrowserRelayMode,
		)
		for _, tool := range browserTool.GetCDPTools() {
			if err := cfg.ToolRegister.Register(tool); err != nil {
				slog.Warn("Failed to register browser tool", "error", err)
			}
		}
		slog.Debug("Browser tools registered", "headless", cfg.BrowserHeadless, "timeout", cfg.BrowserTimeout)
	}

	// 注册 Web 工具
	if cfg.WebEnabled {
		webTool := tools.NewWebTool(cfg.WebSearchAPIKey, cfg.WebSearchEngine, cfg.WebTimeout)
		for _, tool := range webTool.GetTools() {
			if err := cfg.ToolRegister.Register(tool); err != nil {
				slog.Warn("Failed to register web tool", "error", err)
			}
		}
		slog.Debug("Web tools registered", "engine", cfg.WebSearchEngine, "timeout", cfg.WebTimeout)
	}

	// 注册 Scheduler 工具
	if cfg.SchedulerEnabled && cfg.Scheduler != nil {
		schedulerTool := tools.NewSchedulerTool(cfg.Scheduler)
		if err := cfg.ToolRegister.Register(schedulerTool); err != nil {
			slog.Warn("Failed to register scheduler tool", "error", err)
		} else {
			slog.Debug("Scheduler tool registered", "tasks", cfg.Scheduler.GetTaskCount())
		}
	}

	// 注册 SendFile 工具
	if cfg.Bus != nil && cfg.ResponseMgr != nil {
		msgBus, ok := cfg.Bus.(*bus.MessageBus)
		if !ok {
			slog.Warn("Bus is not a MessageBus, skipping SendFileTool registration")
			return
		}
		responseMgr, ok := cfg.ResponseMgr.(*bus.RequestResponseManager)
		if !ok {
			slog.Warn("ResponseMgr is not a RequestResponseManager, skipping SendFileTool registration")
			return
		}
		sendFileTool := tools.NewSendFileTool(msgBus, responseMgr)
		if err := cfg.ToolRegister.Register(sendFileTool, true); err != nil { // 默认需要审批
			slog.Warn("Failed to register send_file tool", "error", err)
		} else {
			slog.Debug("SendFile tool registered")
		}
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

	if cfg.ToolRegister == nil {
		cfg.ToolRegister = tools.NewRegistry()
	}
	cfg.ToolRegister.Register(tools.NewMemoryTool(prompt.memory))

	// 注册 Memoria 工具（替代 RAG 和 History 工具）
	buildMemoriaTool(cfg.ToolRegister, cfg.Memoria)

	// 加载 MCP 工具
	if err := loadMCPTools(ctx, cfg.ToolRegister, cfg.MCPConfigs); err != nil {
		slog.Warn("Failed to load MCP tools", "error", err)
	}

	// 注册内置工具（Browser、Web）
	registerBuiltinTools(cfg)

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
			NewInstructionLoggerMiddleware(cfg.Workspace, cfg.SessionManager),
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

	if cfg.ToolRegister == nil {
		cfg.ToolRegister = tools.NewRegistry()
	}
	cfg.ToolRegister.Register(tools.NewMemoryTool(prompt.memory))

	// 注册 Memoria 工具（替代 RAG 和 History 工具）
	buildMemoriaTool(cfg.ToolRegister, cfg.Memoria)

	// 加载 MCP 工具
	if err := loadMCPTools(ctx, cfg.ToolRegister, cfg.MCPConfigs); err != nil {
		slog.Warn("Failed to load MCP tools", "error", err)
	}

	// 注册内置工具（Browser、Web）
	registerBuiltinTools(cfg)

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
			NewInstructionLoggerMiddleware(cfg.Workspace, cfg.SessionManager),
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

	if cfg.ToolRegister == nil {
		cfg.ToolRegister = tools.NewRegistry()
	}
	cfg.ToolRegister.Register(tools.NewMemoryTool(prompt.memory))

	// 注册 Memoria 工具（替代 RAG 和 History 工具）
	buildMemoriaTool(cfg.ToolRegister, cfg.Memoria)

	// 加载 MCP 工具
	if err := loadMCPTools(ctx, cfg.ToolRegister, cfg.MCPConfigs); err != nil {
		slog.Warn("Failed to load MCP tools", "error", err)
	}

	// 注册内置工具（Browser、Web）
	registerBuiltinTools(cfg)

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

	if cfg.ToolRegister == nil {
		cfg.ToolRegister = tools.NewRegistry()
	}
	cfg.ToolRegister.Register(tools.NewMemoryTool(prompt.memory))

	// 注册 Memoria 工具（替代 RAG 和 History 工具）
	buildMemoriaTool(cfg.ToolRegister, cfg.Memoria)

	// 加载 MCP 工具
	if err := loadMCPTools(ctx, cfg.ToolRegister, cfg.MCPConfigs); err != nil {
		slog.Warn("Failed to load MCP tools", "error", err)
	}

	// 注册内置工具（Browser、Web）
	registerBuiltinTools(cfg)

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
			NewInstructionLoggerMiddleware(cfg.Workspace, cfg.SessionManager),
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

// buildMemoriaTool 构建memoria工具
func buildMemoriaTool(register *tools.Registry, memoriaObj *memoria.Memoria) {
	if memoriaObj == nil || register == nil {
		return
	}
	register.Register(memoria.NewHistoryTool(memoriaObj))
	register.Register(memoria.NewRAGTool(memoriaObj))
}

// loadMCPTools 加载 MCP 工具到注册表
func loadMCPTools(ctx context.Context, register *tools.Registry, mcpConfigs []tools.MCPConfig) error {
	if len(mcpConfigs) == 0 || register == nil {
		return nil
	}

	loader := tools.NewMCPLoader()
	mcpTools, err := loader.LoadTools(ctx, mcpConfigs)
	if err != nil {
		return fmt.Errorf("failed to load MCP tools: %w", err)
	}

	for _, t := range mcpTools {
		info, err := t.Info(ctx)
		if err != nil {
			slog.Warn("Failed to get MCP tool info", "error", err)
			continue
		}
		// 将 MCP 工具包装为 invokeableTool
		// 从 ParamsOneOf 获取参数 schema
		params := make(map[string]interface{})
		if info.ParamsOneOf != nil {
			js, err := info.ParamsOneOf.ToJSONSchema()
			if err == nil && js != nil {
				// 将 jsonschema.Schema 转换为 map
				params = schemaToMap(js)
			}
		}
		wrappedTool := &mcpToolWrapper{baseTool: t, name: info.Name, desc: info.Desc, params: params}
		if err := register.Register(wrappedTool); err != nil {
			slog.Warn("Failed to register MCP tool", "name", info.Name, "error", err)
		}
	}

	return nil
}

// schemaToMap 将 jsonschema.Schema 转换为 map[string]interface{}
func schemaToMap(js *jsonschema.Schema) map[string]interface{} {
	if js == nil {
		return nil
	}
	// 使用 json 序列化再反序列化来转换
	data, err := json.Marshal(js)
	if err != nil {
		return nil
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil
	}
	return result
}

// mcpToolWrapper 包装 MCP 工具以实现 Tool 接口
type mcpToolWrapper struct {
	baseTool tool.BaseTool
	name     string
	desc     string
	params   map[string]interface{}
}

func (w *mcpToolWrapper) Name() string {
	return w.name
}

func (w *mcpToolWrapper) Description() string {
	return w.desc
}

func (w *mcpToolWrapper) Parameters() map[string]interface{} {
	return w.params
}

func (w *mcpToolWrapper) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	// MCP 工具通过 InvokableRun 执行
	invokable, ok := w.baseTool.(tool.InvokableTool)
	if !ok {
		return "", fmt.Errorf("MCP tool %s is not invokable", w.name)
	}

	argsJSON, err := jsonArgs(args)
	if err != nil {
		return "", err
	}

	return invokable.InvokableRun(ctx, argsJSON)
}

func jsonArgs(args map[string]interface{}) (string, error) {
	if len(args) == 0 {
		return "{}", nil
	}
	data, err := json.Marshal(args)
	if err != nil {
		return "", fmt.Errorf("failed to marshal args: %w", err)
	}
	return string(data), nil
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
func (a *Agent) Prompt(ctx context.Context, messages []adk.Message, checkPointID string, userPreferences string) ([]adk.Message, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Run orchestrator
	msg, err := a.loop.Run(ctx, messages, checkPointID, userPreferences)
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
	// 停止 looper 的 eventLoop goroutine
	if a.loop != nil {
		a.loop.Stop()
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
