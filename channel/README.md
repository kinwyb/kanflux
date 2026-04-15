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
Channel Manager (注册、分发、自动初始化)
         ↓                           ↑
Agent Manager (处理 inbound，发布 outbound)
```

## 核心接口

### Channel

```go
type Channel interface {
    Name() string                              // 通道名称标识
    AccountID() string                         // 号ID（多账号）
    Start(ctx context.Context) error           // 启动通道
    Stop() error                               // 停止通道
    IsRunning() bool                           // 是否运行中
    Send(ctx context.Context, msg *bus.OutboundMessage) error  // 发送完整消息
    SendStream(ctx context.Context, msg *bus.OutboundMessage) error // 发送流式增量消息
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

## 工厂模式与自动初始化

Channel 使用工厂模式实现自动初始化，支持从配置文件自动创建所有 Channel 实例。

### ChannelFactory 接口

```go
// ChannelFactory Channel 工厂接口
// 每种 Channel 类型实现此接口，在 init() 中注册到全局注册表
type ChannelFactory interface {
    // Type 返回 Channel 类型标识 (如 "wxcom", "telegram")
    Type() string

    // CreateFromConfig 从 ChannelsConfig 创建 Channel 实例
    // 工厂从 cfg 中提取自己需要的字段进行解析
    // common: 公共参数（MessageBus 等）
    // 返回: Channel 实例列表（支持多账号），未启用时返回 nil
    CreateFromConfig(ctx context.Context, cfg *config.ChannelsConfig, common *ChannelCommonParams) ([]Channel, error)
}
```

### 注册工厂

在 Channel 包的 `register.go` 中，通过 `init()` 自动注册：

```go
// wxcom/register.go
type WxComFactory struct{}

func init() {
    // 在 init 时自动注册到全局注册表
    channel.RegisterFactory(&WxComFactory{})
}

func (f *WxComFactory) Type() string {
    return "wxcom"
}

func (f *WxComFactory) CreateFromConfig(ctx context.Context, cfg *config.ChannelsConfig, common *channel.ChannelCommonParams) ([]channel.Channel, error) {
    // 提取 wxcom 配置
    if cfg.WxCom == nil || !cfg.WxCom.Enabled {
        return nil, nil  // 未启用，返回 nil
    }

    var channels []channel.Channel

    // 从 accounts 创建多账号实例
    for accountID, accountCfg := range cfg.WxCom.Accounts {
        if !accountCfg.Enabled {
            continue
        }

        ch, err := NewWxComChannelWithAccount(common.Bus, wxConfig, accountID)
        if err != nil {
            return nil, err
        }
        channels = append(channels, ch)
    }

    return channels, nil
}
```

### 工厂注册表 API

```go
// 注册工厂（通常在 init() 中调用）
channel.RegisterFactory(factory)

// 获取指定类型的工厂
factory, ok := channel.GetFactory("wxcom")

// 列出所有已注册的工厂类型
types := channel.ListFactories()  // ["wxcom", "telegram", ...]
```

## Channel Manager

管理所有注册的通道，分发出站消息和聊天事件：

### 创建与自动初始化

```go
// 创建 manager
manager := channel.NewManager(bus)

// 从配置自动初始化所有 Channel（推荐方式）
manager.InitializeFromConfig(ctx, &config.ChannelsConfig{
    WxCom: &config.WxComChannelConfig{
        Enabled: true,
        Accounts: map[string]config.WxComAccountConfig{
            "work": {Enabled: true, BotID: "xxx", Secret: "yyy"},
            "personal": {Enabled: true, BotID: "aaa", Secret: "bbb"},
        },
    },
})

// 启动所有通道
manager.StartAll(ctx)

// 停止所有通道
manager.StopAll()
```

### 手动注册（可选）

```go
// 手动创建并注册单个通道
wxcomChannel, err := wxcom.NewWxComChannel(bus, cfg)
manager.Register(wxcomChannel)

// 使用指定名称注册（多账号同类型）
manager.RegisterWithName(ch, "wxcom:work")
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

### 1. 创建结构体，嵌入 ChannelBase

```go
// mychannel/mychannel.go
type MyChannel struct {
    *ChannelBase
    client *MyClient
}
```

### 2. 实现构造函数（支持多账号）

```go
// NewMyChannel 创建单个实例
func NewMyChannel(msgBus *bus.MessageBus, cfg *MyConfig) (*MyChannel, error) {
    return NewMyChannelWithAccount(msgBus, cfg, "")
}

// NewMyChannelWithAccount 创建带账号标识的实例（多账号场景）
// accountID 用于生成唯一 channel 名称：mychannel:accountID
func NewMyChannelWithAccount(msgBus *bus.MessageBus, cfg *MyConfig, accountID string) (*MyChannel, error) {
    // 生成唯一的 channel 名称
    channelName := "mychannel"
    if accountID != "" {
        channelName = "mychannel:" + accountID
    }

    base := NewChannelBase(channelName, cfg.AccountID, BaseChannelConfig{
        Enabled:    cfg.Enabled,
        AllowedIDs: cfg.AllowedIDs,
    }, msgBus)

    return &MyChannel{
        ChannelBase: base,
        client:      client,
    }, nil
}
```

### 3. 实现工厂并自动注册

```go
// mychannel/register.go
type MyFactory struct{}

func init() {
    // 在 init 时自动注册到全局注册表
    channel.RegisterFactory(&MyFactory{})
}

func (f *MyFactory) Type() string {
    return "mychannel"
}

func (f *MyFactory) CreateFromConfig(ctx context.Context, cfg *config.ChannelsConfig, common *channel.ChannelCommonParams) ([]channel.Channel, error) {
    // 提取 mychannel 配置
    if cfg.MyChannel == nil || !cfg.MyChannel.Enabled {
        return nil, nil  // 未启用时返回 nil
    }

    var channels []channel.Channel

    // 支持多账号配置
    for accountID, accountCfg := range cfg.MyChannel.Accounts {
        if !accountCfg.Enabled {
            continue
        }

        myConfig := f.toMyConfig(&accountCfg)
        ch, err := NewMyChannelWithAccount(common.Bus, myConfig, accountID)
        if err != nil {
            return nil, fmt.Errorf("account '%s': %w", accountID, err)
        }
        channels = append(channels, ch)
    }

    return channels, nil
}
```

### 4. 实现 Start 方法（接收外部消息）

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

### 5. 实现 Send 方法（发送响应）

```go
func (c *MyChannel) Send(ctx context.Context, msg *bus.OutboundMessage) error {
    return c.client.SendMessage(msg.ChatID, msg.Content)
}
```

### 6. 实现 HandleChatEvent（处理流式事件）

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

### 7. 添加配置结构

```go
// config/config.go
type MyChannelConfig struct {
    Enabled  bool                     `json:"enabled"`
    Accounts map[string]MyAccountConfig `json:"accounts"`
}

type MyAccountConfig struct {
    Enabled    bool     `json:"enabled"`
    Token      string   `json:"token"`
    AllowedIDs []string `json:"allowed_ids,omitempty"`
}
```

## 配置

在 `kanflux.json` 中配置通道：

```json
{
  "channels": {
    "wxcom": {
      "enabled": true,
      "accounts": {
        "work": {
          "enabled": true,
          "bot_id": "work-bot-id",
          "secret": "work-bot-secret"
        },
        "personal": {
          "enabled": true,
          "bot_id": "personal-bot-id",
          "secret": "personal-bot-secret"
        }
      }
    },
    "telegram": {
      "enabled": true,
      "accounts": {
        "main": {
          "enabled": true,
          "token": "your-bot-token",
          "allowed_ids": ["user1", "user2"]
        }
      }
    },
    "thread_bindings": [
      {
        "session_key": "tui:chat123",
        "target_channel": "wxcom:work",
        "target_agent": "work-agent"
      }
    ]
  }
}
```

配置说明：
- 每种 Channel 类型使用 `accounts` 字段配置多账号
- 每个账号自动生成唯一名称：`<type>:<accountID>`（如 `wxcom:work`）
- `thread_bindings` 中 `target_channel` 需使用完整名称

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
    ChannelWxCom    = "wxCom"
)
```

## 文件结构

```
channel/
  base.go           # Channel 接口和 ChannelBase 实现
  manager.go        # Channel Manager（注册、分发、自动初始化）
  factory.go        # ChannelFactory 工厂接口定义
  registry.go       # 全局工厂注册表
  thread_binding.go # ThreadBindingService 会话路由
  channel_test.go   # 单元测试
  README.md         # 本文档
  wxcom/            # 企业微信智能机器人 Channel
    wxcom.go        # WxComChannel 主实现
    register.go     # 工厂注册（init 自动注册）
    ws.go           # WebSocket 管理器
    types.go        # 类型定义
    handler.go      # 消息处理器
    wxcom_test.go   # 测试文件
```

## 企业微信 Channel (wxCom)

企业微信智能机器人 Channel 基于 WebSocket 长连接实现，支持：

- WebSocket 长连接通信 (`wss://openws.work.weixin.qq.com`)
- 消息类型: text, image, mixed, voice, file
- 流式回复 (stream)
- 模板卡片 (template_card)
- 事件回调: enter_chat, template_card_event, feedback_event
- 多账号支持：每个账号生成唯一 channel 名称

### 配置

```json
{
  "channels": {
    "wxcom": {
      "enabled": true,
      "accounts": {
        "work": {
          "enabled": true,
          "bot_id": "your-bot-id",
          "secret": "your-bot-secret",
          "heartbeat_interval": 30000,
          "reconnect_interval": 1000,
          "max_reconnect": 10,
          "request_timeout": 10000
        },
        "personal": {
          "enabled": true,
          "bot_id": "another-bot-id",
          "secret": "another-secret"
        }
      }
    }
  }
}
```

配置字段：
- `bot_id`: 企业微信后台获取的机器人 ID（必填）
- `secret`: 企业微信后台获取的密钥（必填）
- `ws_url`: 自定义 WebSocket 地址（可选）
- `heartbeat_interval`: 心跳间隔 ms（可选，默认 30000）
- `reconnect_interval`: 重连基础延迟 ms（可选，默认 1000）
- `max_reconnect`: 最大重连次数（可选，默认 10）
- `request_timeout`: 请求超时 ms（可选，默认 10000）

### 使用示例

```go
// 创建 manager
manager := channel.NewManager(bus)

// 从配置自动初始化（推荐）
manager.InitializeFromConfig(ctx, channelsConfig)

// 启动所有通道
manager.StartAll(ctx)

// 获取特定账号的 channel
workChannel, ok := manager.Get("wxcom:work")

// 发送欢迎语 (需在 enter_chat 事件 5 秒内调用)
workChannel.ReplyWelcome(ctx, reqID, "您好！我是智能助手。")

// 主动发送消息
workChannel.SendMessage(ctx, chatID, "这是一条主动推送的消息")
```

## 测试

```bash
go test ./channel/... -v
```

## 参考

- goclaw 项目：`/Users/wangyingbin/Developer/go/src/github.com/kinwyb/goclaw/channels/`
- MessageBus：`../bus/queue.go`
- 事件类型：`../bus/events.go`