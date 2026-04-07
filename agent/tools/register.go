package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"slices"
	"sync"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/eino-contrib/jsonschema"
	"github.com/kinwyb/kanflux/bus"
)

// Registry 工具注册表
type Registry struct {
	*adk.BaseChatModelAgentMiddleware
	tools           map[string]Tool
	mu              sync.RWMutex
	needApproveTool []string
	prompters       map[string]ApprovalPrompter // 按工具名称注册的审批提示器
}

func init() {
	schema.Register[*ApprovalInfo]()
}

// NewRegistry 创建工具注册表
func NewRegistry() *Registry {
	return &Registry{
		tools:     make(map[string]Tool),
		prompters: make(map[string]ApprovalPrompter),
	}
}

// Register 注册工具
func (r *Registry) Register(tool Tool, approve ...bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	name := tool.Name()
	if _, ok := r.tools[name]; ok {
		return fmt.Errorf("tool %s already registered", name)
	}

	r.tools[name] = tool
	if len(approve) > 0 && approve[0] {
		r.needApproveTool = append(r.needApproveTool, name)
	}
	slog.Info("Tool registered", "tool", name)
	return nil
}

func (r *Registry) NeedApprove(toolName string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if slices.Contains(r.needApproveTool, toolName) {
		return
	}
	r.needApproveTool = append(r.needApproveTool, toolName)
}

// GetTools 获取所有执行工具
func (r *Registry) GetTools() ([]tool.BaseTool, error) {
	var tools []tool.BaseTool
	for _, tool := range r.tools {
		nt, err := NewInvokeableTool(tool)
		if err != nil {
			return nil, err
		}
		tools = append(tools, nt)
	}
	return tools, nil
}

// ToolCount 工具数量统计
func (r *Registry) ToolCount() int {
	return len(r.tools)
}

// SetAllowedTools 设置允许的工具白名单
// 如果 whitelist 为空，表示所有工具都可用
func (r *Registry) SetAllowedTools(whitelist []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(whitelist) == 0 {
		return // 空白名单表示所有工具可用
	}
	// 删除不在白名单中的工具
	for name := range r.tools {
		if !slices.Contains(whitelist, name) {
			delete(r.tools, name)
			slog.Debug("Tool removed (not in whitelist)", "tool", name)
		}
	}
}

// SetToolsApproval 设置需要审批的工具列表
func (r *Registry) SetToolsApproval(approvalList []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.needApproveTool = approvalList
	slog.Debug("Tools approval list set", "tools", approvalList)
}

// GetToolNames 获取所有已注册的工具名称
func (r *Registry) GetToolNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	return names
}

// RegisterApprovalPrompter 注册工具的审批提示器
// 允许为工具单独设置审批提示，即使工具本身未实现 ApprovalPrompter 接口
func (r *Registry) RegisterApprovalPrompter(toolName string, prompter ApprovalPrompter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.prompters[toolName] = prompter
}

// getApprovalPrompt 获取工具的审批提示
// 优先使用注册的 ApprovalPrompter，如果没有则检查工具是否实现了 ApprovalPrompter 接口
func (r *Registry) getApprovalPrompt(toolName, argsJSON string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// 优先检查注册的 ApprovalPrompter
	if prompter, ok := r.prompters[toolName]; ok {
		return prompter.ApprovalPrompt(argsJSON)
	}

	// 然后检查工具本身是否实现了 ApprovalPrompter
	tool, ok := r.tools[toolName]
	if !ok {
		return ""
	}

	if prompter, ok := tool.(ApprovalPrompter); ok {
		return prompter.ApprovalPrompt(argsJSON)
	}
	return ""
}

func (r *Registry) WrapInvokableToolCall(
	_ context.Context,
	endpoint adk.InvokableToolCallEndpoint,
	tCtx *adk.ToolContext,
) (adk.InvokableToolCallEndpoint, error) {
	// 只拦截需要审批的 Tool
	if !slices.Contains(r.needApproveTool, tCtx.Name) {
		return endpoint, nil
	}

	return func(ctx context.Context, args string, opts ...tool.Option) (string, error) {
		wasInterrupted, _, storedArgs := tool.GetInterruptState[string](ctx)

		if !wasInterrupted {
			return "", tool.StatefulInterrupt(ctx, &ApprovalInfo{
				ToolName:        tCtx.Name,
				ArgumentsInJSON: args,
				customPrompt:    r.getApprovalPrompt(tCtx.Name, args),
			}, args)
		}

		isTarget, hasData, data := tool.GetResumeContext[*ApprovalResult](ctx)
		if isTarget && hasData {
			if data.Approved {
				return endpoint(ctx, storedArgs, opts...)
			}
			if data.DisapproveReason != nil {
				return fmt.Sprintf("tool '%s' disapproved: %s", tCtx.Name, *data.DisapproveReason), nil
			}
			return fmt.Sprintf("tool '%s' disapproved", tCtx.Name), nil
		}

		// 重新中断
		return "", tool.StatefulInterrupt(ctx, &ApprovalInfo{
			ToolName:        tCtx.Name,
			ArgumentsInJSON: storedArgs,
			customPrompt:    r.getApprovalPrompt(tCtx.Name, storedArgs),
		}, storedArgs)
	}, nil
}

func (r *Registry) WrapStreamableToolCall(
	_ context.Context,
	endpoint adk.StreamableToolCallEndpoint,
	tCtx *adk.ToolContext,
) (adk.StreamableToolCallEndpoint, error) {
	// 如果 agent 配置了 StreamingShell，则 execute 会走流式调用，需要实现该方法才能拦截到
	if tCtx.Name != "execute" {
		return endpoint, nil
	}
	return func(ctx context.Context, args string, opts ...tool.Option) (*schema.StreamReader[string], error) {
		wasInterrupted, _, storedArgs := tool.GetInterruptState[string](ctx)
		if !wasInterrupted {
			return nil, tool.StatefulInterrupt(ctx, &ApprovalInfo{
				ToolName:        tCtx.Name,
				ArgumentsInJSON: args,
				customPrompt:    r.getApprovalPrompt(tCtx.Name, args),
			}, args)
		}

		isTarget, hasData, data := tool.GetResumeContext[*ApprovalResult](ctx)
		if isTarget && hasData {
			if data.Approved {
				return endpoint(ctx, storedArgs, opts...)
			}
			if data.DisapproveReason != nil {
				return singleChunkReader(fmt.Sprintf("tool '%s' disapproved: %s", tCtx.Name, *data.DisapproveReason)), nil
			}
			return singleChunkReader(fmt.Sprintf("tool '%s' disapproved", tCtx.Name)), nil
		}

		isTarget, _, _ = tool.GetResumeContext[any](ctx)
		if !isTarget {
			return nil, tool.StatefulInterrupt(ctx, &ApprovalInfo{
				ToolName:        tCtx.Name,
				ArgumentsInJSON: storedArgs,
				customPrompt:    r.getApprovalPrompt(tCtx.Name, storedArgs),
			}, storedArgs)
		}

		return endpoint(ctx, storedArgs, opts...)
	}, nil
}

func singleChunkReader(msg string) *schema.StreamReader[string] {
	r, w := schema.Pipe[string](1)
	_ = w.Send(msg, nil)
	w.Close()
	return r
}

type ApprovalInfo struct {
	ToolName        string
	ArgumentsInJSON string
	customPrompt    string // 自定义提示内容，由工具实现 ApprovalPrompter 接口时设置
}

type ApprovalResult struct {
	Approved         bool
	DisapproveReason *string
}

// InterruptType 返回中断类型
func (ai *ApprovalInfo) InterruptType() string {
	return bus.InterruptTypeYesNo
}

func (ai *ApprovalInfo) InterruptReason() string {
	return ai.String()
}

func (ai *ApprovalInfo) String() string {
	if ai.customPrompt != "" {
		return ai.customPrompt
	}
	return fmt.Sprintf("tool '%s' interrupted with arguments '%s', waiting for your approval, "+
		"please answer with Y/N",
		ai.ToolName, ai.ArgumentsInJSON)
}

// ResumeParamType 返回期望的恢复参数类型
func (ai *ApprovalInfo) ResumeParamType() reflect.Type {
	return reflect.TypeOf(&ApprovalResult{})
}

type invokeableTool struct {
	tool Tool
}

// NewInvokeableTool 创建一个执行工具
func NewInvokeableTool(tool Tool) (tool.InvokableTool, error) {
	if tool == nil {
		return nil, errors.New("tool is nil")
	}
	return &invokeableTool{tool: tool}, nil
}

func (i invokeableTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	param := jsonschema.Reflect(i.tool.Parameters())
	toolInfo := &schema.ToolInfo{
		Name:        i.tool.Name(),
		Desc:        i.tool.Description(),
		ParamsOneOf: schema.NewParamsOneOfByJSONSchema(param),
	}
	return toolInfo, nil
}

func (i invokeableTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	param := make(map[string]interface{})
	err := json.Unmarshal([]byte(argumentsInJSON), &param)
	if err != nil {
		return "", err
	}
	return i.tool.Execute(ctx, param)
}
