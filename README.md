# Kanflux

Kanflux 是一个基于 [Eino](https://github.com/cloudwego/eino) 框架构建的多用户 AI Agent 平台，通过智能记忆系统理解每一位用户，为每个人提供个性化的对话体验。

## 核心特性

### 🧠 用户画像记忆系统

Kanflux 不是传统的个人助手，而是面向所有用户的智能平台。它通过 **Memoria** 记忆系统实现：

- **自动提取用户偏好**：从对话历史中识别并记录每个人的习惯、偏好、决策风格
- **分层记忆架构**：基于 MemPalace 设计的三层记忆模型，高效存储与检索
- **个性化上下文注入**：每次对话自动注入用户的 L1 偏好总结，让 Agent 懂你

```
用户 A 对话 → Agent 知道 A 喜欢简洁回复、偏好 Go 语言
用户 B 对话 → Agent 知道 B 喜欢详细解释、偏好 Python
用户 C 对话 → Agent 知道 C 是新手，需要更多引导
```

### 🔄 多渠道接入

支持多种对话渠道，统一管理：

- **Terminal TUI**（本地交互，通过 WebSocket 连接）
- **企业微信智能机器人**（wxCom Channel）
- **Web 界面**（通过 WebSocket）
- 自定义渠道扩展

每个渠道的用户独立识别，偏好互不干扰。通过 ThreadBinding 可将不同渠道的消息路由到指定 Agent。

### 🔌 WebSocket 服务

Kanflux 使用 WebSocket 作为消息中心枢纽，所有客户端通过 WebSocket 连接：

- **Gateway 命令**：启动后台服务，管理 Agent 和 Channel
- **TUI 命令**：自动检测/启动 WebSocket，连接同一服务
- **统一架构**：所有 Channel 运行在同一实例，ThreadBinding 配置生效

### 📦 三层记忆模型

基于 [MemPalace](https://github.com/MemPalace/mempalace) 的分层记忆设计：

| 层级 | Token 限制 | 存储内容 | 访问方式 |
|------|-----------|---------|---------|
| **L1 偏好层** | ~120 | 用户习惯、偏好、决策风格 | 始终加载，直接注入 prompt |
| **L2 事件层** | ~500 | 关键事件、决策、发现 | 按需检索，语义搜索 |
| **L3 原始层** | 无限制 | 完整对话记录、文档内容 | 深度语义搜索 |

### 🛠 工具与技能系统

- **内置工具**：文件操作、代码搜索、Shell 执行
- **技能扩展**：基于文件系统的技能加载，渐进式披露
- **工具审批**：敏感操作需用户确认，安全可控

### ⏰ 定时任务系统

Kanflux 支持定时任务，可让 Agent 在指定时间主动发送消息：

- **Cron 表达式**：支持标准5字段格式（分 时 日 月 周）
- **目标灵活**：指定 Channel、Chat、Account，可选指定 Agent
- **动态管理**：通过 WebSocket API 添加/删除/修改/触发任务
- **状态持久化**：任务执行状态自动保存，重启后恢复

```
每天9点提醒 → "早上好！请检查待办事项"
每小时报告 → "系统运行正常，CPU 使用率 30%"
工作日提醒 → "下班前别忘了提交日报"
```

## 快速开始

### 安装

```bash
git clone https://github.com/kinwyb/kanflux.git
cd kanflux
go build -o kanflux .
```

### 运行

```bash
# Gateway 模式（后台服务）
./kanflux gateway --config kanflux.json
# WebSocket 服务自动启动，监听 ws://localhost:8765/ws

# TUI 模式（自动检测/启动 WebSocket）
./kanflux tui --config kanflux.json
# 自动检测 WebSocket：已运行则连接，未运行则启动完整服务后连接

# TUI 模式（连接远程 Gateway）
./kanflux tui --gateway ws://remote-server:8765/ws

# 单 Agent 模式（无配置文件）
./kanflux tui --workspace ~/my-workspace --model gpt-4o --api-key your-key

# 或使用环境变量
export OPENAI_API_KEY=your-key
./kanflux tui --workspace ~/my-workspace
```

### 目录结构

```
kanflux/
├── agent/           # Agent 核心实现
│   ├── agent.go     # Agent 结构和生命周期
│   ├── agentLoop.go # Agent 事件循环处理
│   ├── context.go   # ContextBuilder 系统提示构建
│   ├── memory.go    # MemoryStore 记忆管理
│   ├── skill.go     # 多目录技能加载
│   ├── manager.go   # AgentManager 多 Agent 管理
│   └── tools/       # 工具系统
│       ├── register.go  # ToolRegistry 工具注册
│       ├── tool.go      # Tool 接口定义
│       └── memory_tool.go # 记忆管理工具
├── bus/             # 消息总线
│   ├── queue.go     # MessageBus 入站/出站消息
│   └ events.go     # 事件类型定义
├── channel/         # Channel 接口和实现
│   ├── manager.go   # ChannelManager 分发和 ThreadBinding
│   ├── wxcom/       # 企业微信智能机器人 Channel
│   └ interface.go  # Channel 接口定义
├── gateway/         # Gateway 服务包
│   ├── gateway.go   # Gateway 主服务（启动/停止）
│   ├── types/       # WebSocket 类型定义
│   │   └── types.go # 消息类型、Payload、转换函数
│   ├── handler/     # WebSocket 消息处理器
│   │   ├── handler.go    # Handler 接口、Registry
│   │   ├── inbound.go    # 入站消息处理
│   │   ├── subscribe.go  # 订阅请求处理
│   │   ├── heartbeat.go  # 心跳处理
│   │   ├── control.go    # 控制消息处理
│   │   ├── session.go    # Session 管理处理
│   │   └── task.go       # 定时任务管理处理
│   └── ws/          # WebSocket 核心
│       ├── server.go    # WebSocket 服务器
│       ├── client.go    # WebSocket 客户端
│       ├── connection.go # WebSocket 连接
│       ├── detector.go  # 状态检测器
│       └ protocol.go   # 类型别名
├── web/             # Web 控制面板前端
├── memoria/         # 记忆系统
├── scheduler/       # 定时任务调度器
│   ├── scheduler.go # Scheduler 核心调度器
│   ├── cron.go      # Cron 表达式解析
│   ├── task.go      # Task/TaskState 结构
│   ├── store.go     # TaskStore 接口
│   └── store_json.go # JSON 文件存储实现
├── session/         # 会话管理
│   ├── session.go   # Session 消息历史
│   └ manager.go    # SessionManager 会话管理
├── config/          # 配置解析
├── cli/             # CLI 命令
│   ├── cli.go       # CLI 入口
│   ├── cmd/         # 子命令（tui, gateway, agent, config）
│   └ tui/           # TUI 应用（BubbleTea）
└── providers/       # LLM 供应商适配
    └ openai.go      # OpenAI 兼容适配
```

## 记忆系统工作原理

Memoria 记忆系统参考 [MemPalace](https://github.com/MemPalace/mempalace) 的分层记忆架构设计。

### 用户偏好自动提取

当用户与 Agent 对话时，Memoria 后台服务自动：

1. **扫描对话历史**：定时或实时处理 session 文件
2. **LLM 分类提取**：识别对话中的偏好、决策、事件
3. **分层存储**：按重要性存入 L1/L2/L3
4. **智能压缩**：当 L1 接近限制时，自动合并成一条总结

### 对话时偏好注入

每次对话开始，Agent 通过 Eino SessionValues 机制：

```go
// 解析 session key: "wechat:user123:chat456"
sessionValues := map[string]any{
    "channel":    "wechat",
    "user_id":    "user123",
    "user_preferences": memoria.GetL1Summary("user123"),
}

// Instruction 占位符被替换
Instruction: "... {user_preferences} ..."
```

L1 偏好总结直接注入到 Agent 的系统提示中，实现个性化响应。

## 配置示例

```json
{
  "providers": {
    "openai": {
      "api_key": "your-api-key",
      "default_model": "gpt-4o"
    }
  },
  "default_provider": "openai",
  "agents": [
    {
      "name": "assistant",
      "workspace": "~/workspace",
      "model": "gpt-4o",
      "max_iteration": 20,
      "streaming": true
    }
  ],
  "channels": {
    "wxcom": {
      "enabled": true,
      "accounts": {
        "work": {
          "enabled": true,
          "bot_id": "your-bot-id",
          "secret": "your-bot-secret"
        }
      }
    },
    "thread_bindings": [
      {
        "session_key": "tui:chat123",
        "target_channel": "wxcom:work"
      }
    ]
  },
  "websocket": {
    "enabled": true,
    "port": 8765,
    "host": "localhost",
    "path": "/ws"
  },
  "scheduler": {
    "enabled": true,
    "check_interval": "1m",
    "persist_state": true,
    "tasks": [
      {
        "id": "morning_reminder",
        "name": "早间提醒",
        "enabled": true,
        "schedule": {"cron": "0 9 * * *"},
        "target": {
          "channel": "tui",
          "account_id": "default",
          "chat_id": "main",
          "agent_name": "assistant"
        },
        "content": {"prompt": "早上好！请提醒用户检查今天的待办事项。"}
      },
      {
        "id": "hourly_report",
        "name": "每小时状态报告",
        "enabled": true,
        "schedule": {"cron": "0 * * * *"},
        "target": {
          "channel": "wxcom",
          "account_id": "work",
          "chat_id": "user_123"
        },
        "content": {"prompt": "请检查当前系统状态并报告。"}
      }
    ]
  }
}
```

## 技术架构

基于 [Eino](https://github.com/cloudwego/eino) ADK 构建：

### Gateway 架构

Gateway 包采用模块化设计，分为三层：

```
gateway/
├── gateway.go           # 主服务入口（集成 TaskScheduler）
├── types/               # 类型定义层（无依赖）
│   └── types.go         # MessageType, Payload, 转换函数（含任务管理消息类型）
├── handler/             # 处理器层（依赖 types）
│   ├── handler.go       # Handler 接口、Registry
│   ├── inbound.go       # 入站消息处理
│   ├── subscribe.go     # 订阅处理
│   ├── heartbeat.go     # 心跳处理
│   ├── control.go       # 控制消息处理
│   ├── session.go       # Session 管理处理
│   └── task.go          # 定时任务管理处理（WebSocket API）
└── ws/                  # WebSocket 核心层
    ├── server.go        # 服务器（实现 handler.Server，含 TaskScheduler 接口）
    ├── client.go        # 客户端
    ├── connection.go    # 连接（实现 handler.Conn）
    ├── detector.go      # 状态检测
    └── protocol.go      # 类型别名
```

**设计特点：**

- **types 包**：独立的类型定义，避免循环导入
- **handler 包**：通过接口解耦，支持自定义处理器注册
- **ws 包**：通过 `init()` 自动注册 handlers 到 Registry
- **TaskScheduler**：通过 WebSocket API 支持动态任务管理

### WebSocket 架构

```
┌──────────────────────────────────────────────────────────────────────┐
│                          Gateway Process                               │
│  ┌──────────────┐  ┌────────────────┐  ┌──────────────────┐           │
│  │ MessageBus   │◄─│ AgentManager   │◄─│ WebSocket Server │           │
│  │              │  │                │  │ (gateway/ws)     │           │
│  └──────┬───────┘  └────────────────┘  └──────┬───────────┘           │
│         │                                      │                       │
│         │            ┌──────────────┐          │ WebSocket             │
│         ├───────────►│TaskScheduler │──────────┤                       │
│         │            │ (scheduler)  │          │                       │
│         │            └──────────────┘          │                       │
│         ▼                                      │                       │
│  ┌──────────────┐                              │                       │
│  │ChannelManager│◄─────────────────────────────┤                       │
│  │  (wxcom,     │                              │                       │
│  │   web)       │                              │                       │
│  └──────────────┘                              │                       │
│         ThreadBinding                          │                       │
│         (tui:chat123 -> wxcom:work)            │                       │
└────────────────────────────────────────────────┼──────────────────────┘
                                                 │
                    ┌────────────────────────────┼───────────────────┐
                    │                            │                   │
                    ▼                            ▼                   ▼
              ┌──────────┐              ┌──────────┐          ┌──────────┐
              │ TUI      │              │ Web UI   │          │ Mobile   │
              │ Client   │              │ Client   │          │ Client   │
              └──────────┘              └──────────┘          └──────────┘
```

**消息流程说明：**

- 用户消息 → WebSocket → MessageBus.Inbound → AgentManager → Agent.Prompt → MessageBus.Outbound → ChannelManager → Channel
- 定时任务 → TaskScheduler → MessageBus.Inbound → AgentManager → Agent.Prompt → MessageBus.Outbound → ChannelManager → Channel

定时任务触发时发布 `InboundMessage`，走统一的 Agent 处理流程，整个架构保持一致。

### 核心组件

- **MessageBus**：消息总线，入站/出站消息、聊天事件、日志事件
- **AgentManager**：多 Agent 管理，消息路由，会话处理
- **ChannelManager**：Channel 注册和消息分发，ThreadBinding 路由
- **WebSocket Server**：消息中心枢纽，广播消息到所有客户端
- **WebSocket Client**：客户端实现，发送/接收消息，订阅过滤
- **TaskScheduler**：定时任务调度器，触发时发布 InboundMessage，走统一的 Agent 处理流程

### Eino ADK 集成

- **ChatModelAgent**：ReAct 模式的智能循环
- **Runner**：事件流管理、中断/恢复
- **Middleware**：技能加载、工具搜索、历史压缩
- **SessionValues**：运行时上下文注入

详见 [CLAUDE.md](./CLAUDE.md) 和 [channel/README.md](./channel/README.md)。

## 开发

```bash
# 构建
go build ./...

# 测试
go test ./...

# 特定包测试
go test -v ./memoria/...
```

## 许可证

MIT License