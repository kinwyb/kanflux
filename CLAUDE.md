# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Kanflux (CLI name: `kanflux`) is an AI Agent CLI tool built on the **Eino framework** (cloudwego/eino). It provides a Terminal User Interface (TUI) for interacting with AI agents that can execute tools, manage memory, and handle multi-turn conversations with streaming support.

## Build and Test Commands

```bash
# Build all packages
go build ./...

# Run all tests
go test ./...

# Run tests for a specific package with verbose output
go test -v ./agent/...

# Run a single test file
go test -v ./agent/agent_test.go

# Build the CLI binary
go build -o kanflux .

# Run the TUI application
./kanflux tui --workspace <path> --model <model-name> --api-key <key>
```

## Architecture

### Core Packages

- **`agent/`**: Core agent implementation using Eino ADK's `deep` prebuilt agent
  - `agent.go`: Agent struct wrapping the deep agent with config, callbacks, and lifecycle
  - `agentLoop.go`: `looper` handles event processing, interrupts/resume, checkpoint management
  - `context.go`: `ContextBuilder` builds system prompts from memory and workspace context
  - `memory.go`: `MemoryStore` manages long-term memory (MEMORY.md) and daily notes
  - `skill.go`: Multi-skill backend for loading skills from multiple directories

- **`agent/tools/`**: Tool system
  - `register.go`: `Registry` manages tool registration and implements `ChatModelAgentMiddleware` for tool approval flow
  - `tool.go`: `Tool` interface definition
  - `memory_tool.go`: Memory management tool implementation

- **`bus/`**: Event bus for message routing
  - `queue.go`: `MessageBus` with pub/sub for inbound/outbound messages, chat events, log events
  - `streaming.go`: `StreamMessage` for streaming message chunks (used by Channel.SendStream)
  - `events.go`: Event types and structures for agent events, including StreamingMode configuration

- **`session/`**: Session management
  - `session.go`: `Session` struct with message history, safe truncation preserving tool call sequences
  - `manager.go`: Session creation and lookup by channel/chat ID

- **`cli/tui/`**: Terminal UI using BubbleTea
  - `app.go`: TUI application entry point
  - `model.go`: BubbleTea model with agent integration
  - `styles.go`: Lipgloss styling

- **`providers/`**: LLM providers
  - `openai.go`: OpenAI-compatible model initialization

### Key Design Patterns

1. **Deep Agent Pattern**: Uses Eino ADK's `deep.New()` prebuilt agent with:
   - Tool calling via `ToolsConfig`
   - Skill middleware for filesystem-based skills
   - Interrupt/resume mechanism for tool approval

2. **Interrupt/Resume Flow**: Tools requiring approval use `tool.StatefulInterrupt()` to pause execution, storing state in checkpoints. Users approve/disapprove via `ApprovalResult`, then `runner.ResumeWithParams()` continues.

3. **Message Bus Pattern**: Pub/sub architecture with channel filtering:
   - `SubscribeOutboundFiltered(channels)` for channel-specific subscriptions
   - Fanout goroutines distribute messages to subscribers

4. **Tool Registry Middleware**: Implements `WrapInvokableToolCall` and `WrapStreamableToolCall` to intercept tool calls needing approval.

5. **Session History Safety**: `GetHistorySafe()` ensures tool call sequences (assistant+tool messages) aren't truncated mid-sequence.

## Message Sending Flow

消息和事件架构完整隔离，所有内容通过 OutboundMessage 发送，ChatEvent 只做状态通知。

### StreamingMode 配置

Channel 发布 InboundMessage 时设置 `StreamingMode` 字段：

| StreamingMode | Content 含义 | 适用 Channel |
|---------------|--------------|--------------|
| `"delta"` | 增量内容（当前 chunk） | TUI（实时显示） |
| `"accumulate"` | 累积后的完整内容 | WxCom（需要完整内容） |

### Streaming Mode (Streaming=true, StreamingMode 由 Channel 配置)

```
Agent Event → handleAgentEvent → Manager 累积 → OutboundMessage → Channel Manager 分发
                                              ↓
                              Channel 根据 IsStreaming 选择 SendStream 或 Send

EventMessageUpdate: OutboundMessage{IsStreaming=true, IsFinal=false}
EventMessageEnd: OutboundMessage{IsStreaming=true, IsFinal=true}

同时发送 ChatEvent 状态通知：
  - ChatEventStateStart: 开始处理
  - ChatEventStateTool: 工具调用通知
  - ChatEventStateComplete: 处理完成
```

### Non-Streaming Mode (Streaming=false)

```
Agent 完成 → handleInboundMessage → OutboundMessage → Channel Manager → Channel.Send

OutboundMessage{IsStreaming=false, IsFinal=true, Content=完整内容}
```

### OutboundMessage 流式状态字段

| 字段 | 流式模式 | 非流式模式 |
|------|----------|-----------|
| IsStreaming | true | false |
| IsThinking | 当前 chunk 是否思考内容 | 不使用 |
| IsFinal | 流结束标记 | true |
| ChunkIndex | chunk 序号 | 0 |

### ChatEvent States (状态通知，不携带内容)

| State | 常量 | 说明 |
|-------|------|------|
| start | `ChatEventStateStart` | 开始处理（UI 显示 loading） |
| tool | `ChatEventStateTool` | 工具调用通知 |
| complete | `ChatEventStateComplete` | 处理完成 |
| error | `ChatEventStateError` | 错误通知 |
| interrupt | `ChatEventStateInterrupt` | 中断（等待用户确认） |

### Channel 分发逻辑

Channel Manager 根据 `IsStreaming` 和 `IsFinal` 选择发送方式：

```go
if msg.IsStreaming && !msg.IsFinal {
    // 流式增量：转换为 StreamMessage 使用 SendStream
    ch.SendStream(ctx, chatID, stream)
} else {
    // 完整消息：使用 Send
    ch.Send(ctx, msg)
}
```

## Eino Framework Integration

This project uses the Eino framework extensively. Key imports:

- `github.com/cloudwego/eino/adk`: Agent Development Kit
- `github.com/cloudwego/eino/adk/prebuilt/deep`: Deep agent prebuilt
- `github.com/cloudwego/eino/adk/middlewares/skill`: Skill middleware
- `github.com/cloudwego/eino/components/model`: Chat model interface
- `github.com/cloudwego/eino/components/tool`: Tool interfaces
- `github.com/cloudwego/eino/compose`: Composition utilities (CheckPointStore, Interrupt)
- `github.com/cloudwego/eino/schema`: Message schemas

## Configuration

Agent configuration (`agent.Config`):
- `Name`: Agent identifier
- `LLM`: `model.ToolCallingChatModel` instance
- `Workspace`: Working directory path
- `MaxIteration`: Maximum agent loop iterations
- `ToolRegister`: Tool registry instance
- `SkillDirs`: Paths to skill directories
- `Streaming`: Enable streaming responses

## Memory System

Files stored in `<workspace>/memory/`:
- `MEMORY.md`: Long-term memory
- `days/<date>.md`: Daily notes (e.g., `2024-01-15.md`)
- `IDENTITY.md`, `AGENTS.md`, `SOUL.md`, `USER.md`: Bootstrap files

## Environment Variables

- `OPENAI_API_KEY`: Default API key if not provided via CLI flag