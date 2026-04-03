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
)

// Registry 工具注册表
type Registry struct {
	*adk.BaseChatModelAgentMiddleware
	tools           map[string]Tool
	mu              sync.RWMutex
	needApproveTool []string
}

func init() {
	schema.Register[*ApprovalInfo]()
}

// NewRegistry 创建工具注册表
func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]Tool),
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
}

type ApprovalResult struct {
	Approved         bool
	DisapproveReason *string
}

func (ai *ApprovalInfo) InterruptReason() string {
	return ai.String()
}

func (ai *ApprovalInfo) String() string {
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
