package agent

import (
	"context"
	"errors"
	"fmt"
	"github.com/kinwyb/kanflux/agent/tools"
	"io"
	"log/slog"
	"reflect"
	"sync"

	localbk "github.com/cloudwego/eino-ext/adk/backend/local"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/skill"
	"github.com/cloudwego/eino/adk/prebuilt/deep"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

type Agent struct {
	loop   *looper
	cfg    *Config
	mu     sync.Mutex
	cancel context.CancelFunc
}

type Config struct {
	Name         string
	LLM          model.ToolCallingChatModel
	Workspace    string
	MaxIteration int
	ToolRegister *tools.Registry
	SkillDir     string
	Streaming    bool
}

// NewAgent 创建一个agent
func NewAgent(ctx context.Context, cfg *Config) (*Agent, error) {
	if cfg.Name == "" {
		cfg.Name = "main"
	}
	prompt, err := NewContextBuilder(cfg.Workspace)
	if err != nil {
		return nil, fmt.Errorf("上下文初始化失败: %w", err)
	}
	if cfg.ToolRegister == nil {
		cfg.ToolRegister = tools.NewRegistry()
	}
	cfg.ToolRegister.Register(tools.NewMemoryTool(prompt.memory))
	backend, err := localbk.NewBackend(ctx, &localbk.Config{})
	if err != nil {
		return nil, fmt.Errorf("文件工具创建失败: %w", err)
	}
	agentConfig := &deep.Config{
		Name:                         cfg.Name,
		Description:                  "You are a friendly and empathetic partner who transcends simple conversation; you are a Deep Agent who anticipates needs, strategically deconstructs complex goals, and takes autonomous action to turn visions into reality.",
		ChatModel:                    cfg.LLM,
		Instruction:                  prompt.BuildSystemPrompt(),
		SubAgents:                    nil,
		MaxIteration:                 cfg.MaxIteration,
		Backend:                      backend,
		Shell:                        backend,
		WithoutWriteTodos:            false,
		WithoutGeneralSubAgent:       false,
		TaskToolDescriptionGenerator: nil,
		Middlewares:                  nil,
		Handlers: []adk.ChatModelAgentMiddleware{
			cfg.ToolRegister,
			&safeToolMiddleware{},
		},
		ModelRetryConfig: nil,
		OutputKey:        "",
	}
	if cfg.ToolRegister != nil && cfg.ToolRegister.ToolCount() > 0 {
		useTools, err := cfg.ToolRegister.GetTools()
		if err != nil {
			return nil, fmt.Errorf("工具注册失败: %w", err)
		}
		agentConfig.ToolsConfig = adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools:                useTools,
				UnknownToolsHandler:  nil,
				ExecuteSequentially:  false,
				ToolArgumentsHandler: nil,
				ToolCallMiddlewares:  nil,
			},
		}
	}
	if cfg.SkillDir != "" {
		skillBackend, _ := skill.NewBackendFromFilesystem(ctx, &skill.BackendFromFilesystemConfig{
			Backend: backend,
			BaseDir: cfg.SkillDir,
		})
		skillMiddleware, _ := skill.NewMiddleware(ctx, &skill.Config{
			Backend: skillBackend,
		})
		agentConfig.Handlers = append(agentConfig.Handlers, skillMiddleware)
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
		a.loop.UnregisterCallback(id)
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
