# Channel 包

Channel 包提供统一的通道接口系统，用于支持多平台消息收发。

## 架构概览

```
External Platforms (TUI, Telegram, WhatsApp, etc.)
         ↓ PublishInbound()          ↑ Send() / HandleChatEvent()
Channel Implementations (embed ChannelBase)
         ↓                           ↑
MessageBus (已有，支持 channel filtering)
         ↓                           ↑
Channel Manager (注册、分发)
         ↓                           ↑
Agent Manager (处理 inbound，发布 outbound)
```

## 核心接口

### Channel

```go
type Channel interface {
    Name() string                              // 通道名称标识
    AccountID() string                         // 账号ID（多账号）
    Start(ctx context.Context) error           // 启动通道
    Stop() error                               // 停止通道
    IsRunning() bool                           // 是否运行中
    Send(ctx context.Context, msg *bus.OutboundMessage) error  // 发送消息
    SendStream(ctx context.Context, chatID string, stream <-chan *bus.StreamMessage) error // 流式发送
    HandleChatEvent(ctx context.Context, event *bus.ChatEvent) error // 处理事件
    IsAllowed(senderID string) bool            // 权限检查
}
```

### ChannelBase

提供基础实现，具体 channel 可嵌入继承：

```go
type TelegramChannel struct {
    *ChannelBase  // 嵌入基础实现
    bot *telegram.BotAPI
    // ... 其他字段
}

// 使用 ChannelBase.PublishInbound 发布入站消息
func (t *TelegramChannel) handleUpdate(ctx context.Context, update *Update) error {
    msg := &bus.InboundMessage{
        SenderID: senderID,
        ChatID:   chatID,
        Content:  content,
    }
    return t.PublishInbound(ctx, msg)  // 自动设置 Channel 字段
}
```

## Channel Manager

管理所有注册的通道，分发出站消息和聊天事件：

```go
// 创建 manager
manager := channel.NewManager(bus)

// 注册通道
manager.Register(tuiChannel)
manager.Register(telegramChannel)

// 启动所有通道
manager.StartAll(ctx)

// 停止所有通道
manager.StopAll()
```

分发机制：
- 订阅 bus 的 outbound 和 chatEvents
- 根据 `msg.Channel` 字段路由到对应 channel
- 使用 ThreadBinding 可改变路由目标

## ThreadBinding

将特定会话绑定到指定通道，实现跨平台交互：

```go
// 绑定会话到目标通道
manager.BindSession("tui:chat123", "telegram")

// 绑定并指定 agent
manager.BindSession("tui:chat456", "slack", 
    channel.WithAgent("work-agent"),
    channel.WithPriority(10),
)

// 获取绑定服务直接操作
bindingService := manager.GetBindingService()
bindingService.Bind("session:key", "target_channel")
bindingService.GetBinding("session:key")
bindingService.Unbind("session:key")
```

使用场景：
- TUI 本地监控 Telegram 群组消息
- 不同 ChatID 路由到不同 Agent
- 跨平台会话同步

## 实现新 Channel

1. 创建结构体，嵌入 ChannelBase：

```go
type MyChannel struct {
    *ChannelBase
    client *MyClient
}
```

2. 实现构造函数：

```go
func NewMyChannel(accountID string, cfg *MyConfig, bus *bus.MessageBus) (*MyChannel, error) {
    base := NewChannelBase("mychannel", accountID, BaseChannelConfig{
        Enabled:    cfg.Enabled,
        AllowedIDs: cfg.AllowedIDs,
    }, bus)
    
    return &MyChannel{
        ChannelBase: base,
        client:      client,
    }, nil
}
```

3. 实现 Start 方法（接收外部消息）：

```go
func (c *MyChannel) Start(ctx context.Context) error {
    c.ChannelBase.Start(ctx)
    go c.receiveMessages(ctx)  // 启动消息接收循环
    return nil
}

func (c *MyChannel) receiveMessages(ctx context.Context) {
    for msg := range c.client.Messages() {
        inbound := &bus.InboundMessage{
            SenderID: msg.SenderID,
            ChatID:   msg.ChatID,
            Content:  msg.Content,
        }
        c.PublishInbound(ctx, inbound)
    }
}
```

4. 实现 Send 方法（发送响应）：

```go
func (c *MyChannel) Send(ctx context.Context, msg *bus.OutboundMessage) error {
    return c.client.SendMessage(msg.ChatID, msg.Content)
}
```

5. 实现 HandleChatEvent（处理流式事件）：

```go
func (c *MyChannel) HandleChatEvent(ctx context.Context, event *bus.ChatEvent) error {
    switch event.State {
    case bus.ChatEventStateDelta:
        // 增量更新
    case bus.ChatEventStateFinal:
        // 最终消息
    }
    return nil
}
```

## 配置

在 `kanflux.json` 中配置通道：

```json
{
  "channels": {
    "telegram": {
      "enabled": true,
      "token": "your-bot-token",
      "allowed_ids": ["user1", "user2"],
      "accounts": {
        "work": {
          "enabled": true,
          "token": "work-bot-token"
        }
      }
    },
    "whatsapp": {
      "enabled": true,
      "bridge_url": "ws://localhost:8080"
    },
    "thread_bindings": [
      {
        "session_key": "tui:chat123",
        "target_channel": "telegram",
        "target_agent": "work-agent"
      }
    ]
  }
}
```

## Channel 常量

定义在 `bus/events.go`：

```go
const (
    ChannelTUI      = "tui"
    ChannelSystem   = "system"
    ChannelTelegram = "telegram"
    ChannelWhatsApp = "whatsapp"
    ChannelFeishu   = "feishu"
    ChannelCLI      = "cli"
    ChannelDiscord  = "discord"
    ChannelSlack    = "slack"
)
```

## 文件结构

```
channel/
  base.go           # Channel 接口和 ChannelBase 实现
  manager.go        # Channel Manager（注册、分发）
  thread_binding.go # ThreadBindingService 会话路由
  tui.go            # TUIChannel 实现
  channel_test.go   # 单元测试
  README.md         # 本文档
```

## 测试

```bash
go test ./channel/... -v
```

## 参考

- goclaw 项目：`/Users/wangyingbin/Developer/go/src/github.com/kinwyb/goclaw/channels/`
- MessageBus：`../bus/queue.go`
- 事件类型：`../bus/events.go`