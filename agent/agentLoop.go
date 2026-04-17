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

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/kinwyb/kanflux/bus"
)

type looper struct {
	cfg         *Config
	agent       adk.Agent
	runner      *adk.Runner
	eventChan   chan *Event             // 内部事件 channel
	callbacks   map[int64]EventCallback // 回调映射
	callbackMu  sync.RWMutex
	callbackID  int64         // 回调 ID 计数器
	done        chan struct{} // 停止信号
	stopped     bool          // 防止重复停止
	stopMu      sync.Mutex    // 停止锁
	cancelFunc  context.CancelFunc
	mu          sync.Mutex
	checkPoints map[string]*checkPoint // 按sessionID存储中断检查点
}

// checkPoint 存储中断时的状态信息
type checkPoint struct {
	InterruptID       string
	InterruptInfo     string
	EndpointParamType reflect.Type  // 端点参数类型（中断时传递的信息类型）
	ResumeParamType   reflect.Type  // 恢复参数类型（恢复时期望接收的参数类型）
	Messages          []adk.Message //中断之前的消息内容
}

// ResumeParamTypeProvider 定义获取恢复参数类型的接口
// 中断信息类型可以实现此接口来声明期望的恢复参数类型
type ResumeParamTypeProvider interface {
	ResumeParamType() reflect.Type
}

// InterruptReasonProvider 中断原因
type InterruptReasonProvider interface {
	InterruptReason() string
}

func newLooper(ctx context.Context, agent adk.Agent, cfg *Config) *looper {
	// 使用内存存储作为CheckPointStore，生产环境建议使用Redis
	checkPointStore := NewInMemoryStore()

	runner := adk.NewRunner(ctx, adk.RunnerConfig{
		Agent:           agent,
		EnableStreaming: cfg.Streaming,
		CheckPointStore: checkPointStore,
	})

	o := &looper{
		agent:       agent,
		cfg:         cfg,
		runner:      runner,
		eventChan:   make(chan *Event, 1000),
		callbacks:   make(map[int64]EventCallback),
		done:        make(chan struct{}),
		cancelFunc:  context.CancelFunc(nil),
		checkPoints: make(map[string]*checkPoint),
	}
	// 启动事件转发 goroutine
	go o.eventLoop()
	return o
}

// eventLoop 事件转发循环，统一处理所有事件
func (o *looper) eventLoop() {
	for {
		select {
		case event := <-o.eventChan:
			if event == nil {
				continue
			}
			if event.Type == EventUnregisterCallback {
				id := event.Metadata["id"].(int64)
				if id > 0 {
					o.UnregisterCallback(id)
				}
				continue
			}
			o.callbackMu.RLock()
			callbacks := make([]EventCallback, 0, len(o.callbacks))
			for _, cb := range o.callbacks {
				callbacks = append(callbacks, cb)
			}
			o.callbackMu.RUnlock()
			// 同步调用所有回调
			for _, cb := range callbacks {
				cb(event)
			}
		case <-o.done:
			return
		}
	}
}

// Stop 停止事件循环
func (o *looper) Stop() {
	o.stopMu.Lock()
	defer o.stopMu.Unlock()
	if o.stopped {
		return
	}
	o.stopped = true
	close(o.done)
}

// Run starts the agent loop with initial prompts
func (o *looper) Run(ctx context.Context, prompts []adk.Message, checkPointID string, userPreferences string) ([]adk.Message, error) {
	slog.Debug("=== Looper Run Start ===")

	ctx, cancel := context.WithCancel(ctx)
	o.cancelFunc = cancel

	// Main loop
	msg, err := o.runLoop(ctx, prompts, checkPointID, userPreferences)

	if err == nil && len(msg) < 1 {
		return nil, fmt.Errorf("agent loop failed: result msg empty")
	}
	if err != nil {
		if _, ok := compose.IsInterruptRerunError(err); ok {
			return nil, err
		}
		if errors.Is(err, adk.ErrExceedMaxIterations) {
			maxPropmt := fmt.Sprintf("\n\n[SYSTEM NOTICE: You have reached the maximum iteration limit (%d). Please provide a final response to the user based on the previous tool results. Do not make any further tool calls.]", o.cfg.MaxIteration)
			msg = append(msg, schema.UserMessage(maxPropmt))
			resultMsg, lerr := o.cfg.LLM.Generate(ctx, msg)
			if lerr != nil {
				return nil, fmt.Errorf("agent loop failed: %w", err)
			}
			if resultMsg != nil && resultMsg.Role == schema.Assistant {
				msg = append(msg, resultMsg)
			}
		} else {
			return nil, fmt.Errorf("agent loop failed: %w", err)
		}
	}

	cancel()

	return msg, nil
}

func (o *looper) runLoop(ctx context.Context, messages []adk.Message, checkPointID string, userPreferences string) ([]adk.Message, error) {
	o.mu.Lock()
	if checkPointID != "" {
		// 清理该session之前的检查点（如果有）
		delete(o.checkPoints, checkPointID)
	}
	o.mu.Unlock()

	var opts []adk.AgentRunOption
	if checkPointID != "" {
		opts = append(opts, adk.WithCheckPointID(checkPointID))
	}

	opts = append(opts, adk.WithSessionValues(map[string]any{
		"user_preferences": userPreferences,
		"session_id":       checkPointID, // session ID for instruction logging
		"agent_name":       o.cfg.Name,   // agent name for instruction logging
	}))

	events := o.runner.Run(ctx, messages, opts...)
	return o.processEvents(ctx, messages, events, checkPointID)
}

// processEvents 公共事件处理逻辑
func (o *looper) processEvents(ctx context.Context, allMsgs []adk.Message, events *adk.AsyncIterator[*adk.AgentEvent], checkPointID string) ([]adk.Message, error) {
	for {
		event, ok := events.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			return allMsgs, event.Err
		}

		// 检测中断事件
		if event.Action != nil && event.Action.Interrupted != nil {
			interruptInfo := o.handleInterrupt(event, allMsgs, checkPointID)
			if interruptInfo == "" {
				interruptInfo = "Interrupted"
			}
			// 中断后返回，等待用户恢复
			return allMsgs, compose.Interrupt(ctx, interruptInfo)
		}

		if event.Output != nil && event.Output.MessageOutput != nil {

			o.emit(NewEvent(EventMessageStart))

			mv := event.Output.MessageOutput

			msg, err := o.messageParse(mv)
			if err != nil {
				return nil, err
			}

			if mv.Role == schema.Tool {
				o.emit(NewEvent(EventToolEnd).WithMessage(msg))
			} else if mv.Role == schema.Assistant && len(msg.ToolCalls) > 0 {
				o.emit(NewEvent(EventToolStart).WithMessage(msg))
			} else {
				o.emit(NewEvent(EventMessageEnd).WithMessage(msg))
			}

			allMsgs = append(allMsgs, msg)
		}
	}
	return allMsgs, nil
}

func (o *looper) messageParse(mv *adk.MessageVariant) (adk.Message, error) {
	if mv.IsStreaming {
		mv.MessageStream.SetAutomaticClose()
		var msgs []adk.Message
		for {
			msg, err := mv.MessageStream.Recv()
			if err != nil {
				if err == io.EOF {
					break
				}
				return nil, err
			}
			o.emit(NewEvent(EventMessageUpdate).WithMessage(msg))
			msgs = append(msgs, msg)
		}
		return schema.ConcatMessages(msgs)
	}
	return mv.Message, nil
}

// emit 发送事件到内部 channel，由统一的 eventLoop 处理
func (o *looper) emit(event *Event) {
	if event == nil {
		return
	}
	if event.Type == EventToolStart {
		for _, v := range event.Message.ToolCalls {
			slog.Debug("Tool call", "toolName", v.Function.Name, "params", v.Function.Arguments)
		}
	} else if event.Type == EventToolEnd {
		slog.Debug("Tool result", "content", event.Message.Content)
	}

	// 非阻塞发送，避免阻塞主流程
	select {
	case o.eventChan <- event:
	default:
		if event.Type == EventUnregisterCallback {
			id := event.Metadata["id"].(int64)
			if id > 0 {
				o.UnregisterCallback(id)
			}
		}
		slog.Debug("Event channel full, dropping event")
	}
}

// RegisterCallback 注册事件回调，返回唯一 ID 用于注销
func (o *looper) RegisterCallback(cb EventCallback) int64 {
	o.callbackMu.Lock()
	o.callbackID++
	id := o.callbackID
	o.callbacks[id] = cb
	o.callbackMu.Unlock()
	return id
}

// UnregisterCallback 注销事件回调
func (o *looper) UnregisterCallback(id int64) {
	o.callbackMu.Lock()
	delete(o.callbacks, id)
	o.callbackMu.Unlock()
}

// handleInterrupt 处理中断事件
func (o *looper) handleInterrupt(event *adk.AgentEvent, messages []adk.Message, checkPointID string) string {
	if event.Action == nil || event.Action.Interrupted == nil {
		return ""
	}

	if checkPointID == "" {
		slog.Warn("Interrupt occurred but no checkPointID provided")
		return ""
	}

	o.mu.Lock()
	defer o.mu.Unlock()
	sb := strings.Builder{}
	// 获取中断上下文
	if len(event.Action.Interrupted.InterruptContexts) > 0 {
		for _, point := range event.Action.Interrupted.InterruptContexts {
			// 获取端点参数类型
			var endpointParamType, resumeParamType reflect.Type
			var interruptReason, interruptType string
			if point.Info != nil {
				endpointParamType = reflect.TypeOf(point.Info)
				// 如果中断信息实现了 ResumeParamTypeProvider 接口，获取恢复参数类型
				if provider, ok := point.Info.(ResumeParamTypeProvider); ok {
					resumeParamType = provider.ResumeParamType()
				}
				if provider, ok := point.Info.(InterruptReasonProvider); ok {
					interruptReason = provider.InterruptReason()
				}
				if provider, ok := point.Info.(bus.InterruptTypeProvider); ok {
					interruptType = provider.InterruptType()
				}
			}
			if interruptReason == "" {
				interruptReason = fmt.Sprintf("[INTERRUPT] %v", point.Info)
			}

			o.checkPoints[checkPointID] = &checkPoint{
				InterruptID:       point.ID,
				InterruptInfo:     interruptReason,
				EndpointParamType: endpointParamType,
				ResumeParamType:   resumeParamType,
				Messages:          messages,
			}

			// 构建 metadata
			metadata := map[string]interface{}{
				"interrupt_type": interruptType,
			}

			// 发送中断事件
			o.emit(NewEvent(EventInterrupt).WithMessage(&schema.Message{
				Role:    schema.Assistant,
				Content: interruptReason,
			}).WithMetadata(metadata))
			sb.WriteString(interruptReason + "\n")
		}
	}
	return sb.String()
}

// Resume 恢复被中断的执行
func (o *looper) Resume(ctx context.Context, checkPointID string, params any) ([]adk.Message, error) {
	o.mu.Lock()
	cp, exists := o.checkPoints[checkPointID]
	if !exists {
		o.mu.Unlock()
		return nil, fmt.Errorf("no pending interrupt for session %s", checkPointID)
	}
	// 恢复后删除该断点
	delete(o.checkPoints, checkPointID)
	o.mu.Unlock()

	// 校验参数类型
	if cp.ResumeParamType != nil && params != nil {
		paramsType := reflect.TypeOf(params)
		// 检查参数类型是否匹配（允许指针类型匹配）
		if paramsType != cp.ResumeParamType {
			// 如果期望的是指针类型，检查传入的是否是对应的指针或值类型
			if cp.ResumeParamType.Kind() == reflect.Ptr {
				if paramsType != cp.ResumeParamType && paramsType.Kind() != reflect.Struct {
					return nil, fmt.Errorf("resume parameter type mismatch: expected %v, got %v", cp.ResumeParamType, paramsType)
				}
				// 如果传入的是结构体值，检查是否与期望的指针指向的结构体类型一致
				if paramsType.Kind() == reflect.Struct && paramsType != cp.ResumeParamType.Elem() {
					return nil, fmt.Errorf("resume parameter type mismatch: expected %v, got %v", cp.ResumeParamType, paramsType)
				}
			} else {
				// 期望的不是指针类型，直接比较
				return nil, fmt.Errorf("resume parameter type mismatch: expected %v, got %v", cp.ResumeParamType, paramsType)
			}
		}
	}

	slog.Debug("Resuming from interrupt",
		"sessionID", checkPointID,
		"interruptID", cp.InterruptID,
		"endpointParamType", cp.EndpointParamType,
		"resumeParamType", cp.ResumeParamType)

	// 使用ResumeWithParams恢复执行
	events, err := o.runner.ResumeWithParams(ctx, checkPointID, &adk.ResumeParams{
		Targets: map[string]any{
			cp.InterruptID: params,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("resume failed: %w", err)
	}

	return o.processEvents(ctx, cp.Messages, events, checkPointID)
}

// GetInterruptInfo 获取指定session的中断信息
func (o *looper) GetInterruptInfo(checkPointID string) *checkPoint {
	o.mu.Lock()
	defer o.mu.Unlock()
	if cp, exists := o.checkPoints[checkPointID]; exists {
		// 返回副本避免外部修改
		return &checkPoint{
			InterruptID:       cp.InterruptID,
			InterruptInfo:     cp.InterruptInfo,
			EndpointParamType: cp.EndpointParamType,
			ResumeParamType:   cp.ResumeParamType,
		}
	}
	return nil
}

func NewInMemoryStore() compose.CheckPointStore {
	return &inMemoryStore{
		mem: map[string][]byte{},
	}
}

type inMemoryStore struct {
	mem map[string][]byte
}

func (i *inMemoryStore) Set(ctx context.Context, key string, value []byte) error {
	i.mem[key] = value
	return nil
}

func (i *inMemoryStore) Get(ctx context.Context, key string) ([]byte, bool, error) {
	v, ok := i.mem[key]
	return v, ok, nil
}
