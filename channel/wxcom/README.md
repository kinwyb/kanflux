# 企业微信智能机器人 Channel (wxCom)

基于企业微信智能机器人 WebSocket 长连接实现的 Channel，支持消息收发、流式回复、模板卡片等功能。

## 功能特性

- WebSocket 长连接 (`wss://openws.work.weixin.qq.com`)
- 自动认证 (bot_id + secret)
- 心跳保活和断线重连 (指数退避)
- 流式消息回复
- 模板卡片消息
- 主动消息推送
- 事件回调处理
- 文件下载与 AES-256-CBC 解密

## 配置

```go
cfg := &wxcom.WxComConfig{
    Enabled:           true,
    BotID:             "your-bot-id",      // 企业微信后台获取
    Secret:            "your-bot-secret",   // 企业微信后台获取
    WSURL:             "",                  // 可选，自定义 WebSocket 地址
    HeartbeatInterval: 30000,               // 心跳间隔(ms)，默认 30000
    ReconnectInterval: 1000,                // 重连基础延迟(ms)，默认 1000
    MaxReconnect:      10,                  // 最大重连次数，默认 10，-1 表示无限
    RequestTimeout:    10000,               // 请求超时(ms)，默认 10000
}
```

## 快速开始

```go
package main

import (
    "context"
    "log"

    "github.com/kinwyb/kanflux/bus"
    "github.com/kinwyb/kanflux/channel"
    "github.com/kinwyb/kanflux/channel/wxcom"
)

func main() {
    // 创建消息总线
    msgBus := bus.NewMessageBus(100)

    // 创建企业微信 Channel
    cfg := &wxcom.WxComConfig{
        Enabled: true,
        BotID:   "your-bot-id",
        Secret:  "your-bot-secret",
    }

    wxcomCh, err := wxcom.NewWxComChannel(msgBus, cfg)
    if err != nil {
        log.Fatal(err)
    }

    // 创建 Channel Manager 并注册
    manager := channel.NewManager(msgBus)
    manager.Register(wxcomCh)

    // 启动
    ctx := context.Background()
    if err := manager.StartAll(ctx); err != nil {
        log.Fatal(err)
    }

    // 运行...
    select {}
}
```

## 消息类型

### 入站消息类型

| 类型 | 常量 | 说明 |
|------|------|------|
| 文本 | `MsgTypeText` | 普通文本消息 |
| 图片 | `MsgTypeImage` | 图片消息，包含 URL 和 aeskey |
| 图文混排 | `MsgTypeMixed` | 包含文本和图片的混合消息 |
| 语音 | `MsgTypeVoice` | 语音消息，已转文本 |
| 文件 | `MsgTypeFile` | 文件消息，包含 URL 和 aeskey |

### 出站消息类型

| 类型 | 常量 | 说明 |
|------|------|------|
| 流式消息 | `MsgTypeStream` | 支持增量更新的流式回复 |
| Markdown | `MsgTypeMarkdown` | Markdown 格式消息 |
| 模板卡片 | `MsgTypeTemplateCard` | 交互式模板卡片 |

## 发送消息

### 1. 普通回复

通过 Agent 处理入站消息后，Manager 自动分发出站消息到 Channel。

### 2. 流式回复

```go
// HandleChatEvent 处理流式事件
wxcomChannel.HandleChatEvent(ctx, event)

// 或手动发送流式消息
streamID := "stream_xxx"
wxcomChannel.ReplyStream(ctx, reqID, streamID, "正在思考...", false) // 中间内容
wxcomChannel.ReplyStream(ctx, reqID, streamID, "最终答案", true)      // 结束流
```

### 3. Markdown 回复

```go
err := wxcomChannel.ReplyMarkdown(ctx, reqID, `# 标题
这是 **Markdown** 格式的消息`)
```

### 4. 欢迎语回复

需在收到 `enter_chat` 事件 5 秒内调用：

```go
// 从入站消息 metadata 获取 req_id
reqID := inbound.Metadata["req_id"].(string)
wxcomChannel.ReplyWelcome(ctx, reqID, "您好！我是智能助手。")
```

### 5. 主动发送消息

无需依赖入站消息，可主动推送：

```go
// 发送 Markdown 消息
err := wxcomChannel.SendMessage(ctx, chatID, "这是主动推送的消息")

// 发送模板卡片
card := wxcom.NewTextNoticeCard("通知标题", "通知内容")
err := wxcomChannel.SendTemplateCard(ctx, chatID, card)
```

## 模板卡片

### 卡片类型

| 类型 | 常量 | 说明 |
|------|------|------|
| 文本通知 | `CardTypeTextNotice` | 简单文本通知 |
| 图文展示 | `CardTypeNewsNotice` | 图文展示卡片 |
| 按钮交互 | `CardTypeButtonInteraction` | 带按钮的交互卡片 |
| 投票选择 | `CardTypeVoteInteraction` | 单选投票 |
| 多项选择 | `CardTypeMultipleInteraction` | 多选项选择 |

### 快捷创建卡片

```go
// 文本通知卡片
card := wxcom.NewTextNoticeCard("标题", "描述内容")

// 按钮交互卡片
buttons := []wxcom.CardButtonOption{
    {ID: "btn1", Text: "同意"},
    {ID: "btn2", Text: "拒绝"},
}
card := wxcom.NewButtonInteractionCard("审批申请", "请选择操作", buttons, "task_123")

// 投票卡片
options := []wxcom.CardSelectOption{
    {ID: "opt1", Text: "选项A"},
    {ID: "opt2", Text: "选项B"},
}
card := wxcom.NewVoteInteractionCard("投票标题", options, "vote_123")

// 多项选择卡片
selectItems := []wxcom.CardSelectItem{
    {
        QuestionKey: "q1",
        Title:       "问题1",
        OptionList: []wxcom.CardSelectOption{
            {ID: "a1", Text: "答案A"},
            {ID: "a2", Text: "答案B"},
        },
    },
    {
        QuestionKey: "q2",
        Title:       "问题2",
        OptionList: []wxcom.CardSelectOption{
            {ID: "a3", Text: "答案C"},
        },
    },
}
card := wxcom.NewMultipleInteractionCard("问卷调查", selectItems, "survey_123")
```

### 自定义卡片

```go
card := &wxcom.TemplateCard{
    CardType: wxcom.CardTypeButtonInteraction,
    Source: &wxcom.CardSource{
        IconURL: "https://example.com/icon.png",
        Desc:    "来源描述",
    },
    MainTitle: &wxcom.CardMainTitle{
        Title: "主标题",
        Desc:  "主标题描述",
    },
    SubTitle: &wxcom.CardSubTitle{
        Title: "副标题",
        Desc:  "副标题描述",
    },
    EmphasisTitle: &wxcom.CardEmphasisTitle{
        Title: "关键信息",
        Desc:  "关键内容描述",
    },
    TaskID: "task_custom",
    ButtonSelection: &wxcom.CardButtonSelection{
        QuestionKey: "approval",
        Title:       "审批决定",
        Disable:     false,
        SelectedID:  "btn1",
        OptionList: []wxcom.CardButtonOption{
            {ID: "btn1", Text: "批准", Disable: false},
            {ID: "btn2", Text: "驳回", Disable: false},
        },
    },
    CardAction: &wxcom.CardAction{
        Type:     1,              // 1: URL跳转, 2: 小程序
        URL:      "https://link", // 跳转链接
    },
}
```

### 发送模板卡片

```go
// 回复模板卡片
feedback := &wxcom.CardFeedback{ButtonDesc: "点击反馈"}
err := wxcomChannel.ReplyTemplateCard(ctx, reqID, card, feedback)

// 流式消息 + 卡片组合回复
err := wxcomChannel.ReplyStreamWithCard(ctx, reqID, streamID, "流式内容", false, card, feedback)

// 主动发送卡片
err := wxcomChannel.SendTemplateCard(ctx, chatID, card)
```

### 更新模板卡片

需在收到 `template_card_event` 事件 5 秒内调用，task_id 必须与回调一致：

```go
// 更新卡片内容
card := wxcom.NewTextNoticeCard("已处理", "您已选择同意")
card.TaskID = eventTaskID  // 使用事件中的 task_id

err := wxcomChannel.UpdateTemplateCard(ctx, reqID, card, nil)
```

## 事件处理

### 事件类型

| 类型 | 常量 | 说明 |
|------|------|------|
| 进入会话 | `EventTypeEnterChat` | 用户首次进入机器人会话 |
| 模板卡片事件 | `EventTypeTemplateCardEvent` | 用户点击卡片按钮 |
| 反馈事件 | `EventTypeFeedbackEvent` | 用户对回复进行反馈 |

### 事件信息获取

```go
// 从入站消息 metadata 获取事件信息
eventType := inbound.Metadata["event_type"]
eventKey := inbound.Metadata["event_key"]    // 卡片按钮的 key
taskID := inbound.Metadata["task_id"]        // 卡片的 task_id
reqID := inbound.Metadata["req_id"]          // 用于回复的 req_id
```

## 文件下载

图片和文件消息包含加密的文件，需要下载并解密：

```go
// 从入站消息获取媒体信息
media := inbound.Media[0]
aesKey := media.Metadata["aeskey"].(string)

// 下载并解密
data, filename, err := wxcomChannel.DownloadFile(ctx, media.URL, aesKey)

// 或使用便捷方法
data, filename, err := wxcomChannel.DownloadImage(ctx, &media)

// 检测 MIME 类型
mimeType := wxcom.DetectMimeType(data)
```

## 错误处理

```go
// 定义的错误
var (
    ErrBotIDRequired    // bot_id 未配置
    ErrSecretRequired   // secret 未配置
    ErrNotConnected     // WebSocket 未连接
    ErrAuthFailed       // 认证失败
    ErrReplyTimeout     // 回复回执超时
    ErrQueueFull        // 回复队列已满
    ErrMaxReconnect     // 达到最大重连次数
)

// 错误检查
if err == wxcom.ErrNotConnected {
    // 处理未连接情况
}
```

## 常量参考

### WebSocket 命令

```go
WsCmdSubscribe       = "aibot_subscribe"            // 认证订阅
WsCmdHeartbeat       = "ping"                       // 心跳
WsCmdResponse        = "aibot_respond_msg"          // 回复消息
WsCmdResponseWelcome = "aibot_respond_welcome_msg"  // 回复欢迎语
WsCmdResponseUpdate  = "aibot_respond_update_msg"   // 更新模板卡片
WsCmdSendMsg         = "aibot_send_msg"             // 主动发送消息
WsCmdCallback        = "aibot_msg_callback"         // 消息推送回调
WsCmdEventCallback   = "aibot_event_callback"       // 事件推送回调
```

### 默认值

```go
DefaultWSURL            = "wss://openws.work.weixin.qq.com"
DefaultHeartbeatInterval = 30000   // 心跳间隔(ms)
DefaultReconnectInterval = 1000    // 重连基础延迟(ms)
DefaultMaxReconnect      = 10      // 最大重连次数
DefaultRequestTimeout    = 10000   // 请求超时(ms)
DefaultMaxMissedPong     = 2       // 最大未收到 pong 次数
DefaultReconnectMaxDelay = 30000   // 重连最大延迟(ms)
DefaultReplyAckTimeout   = 5       // 回复回执超时(s)
```

## API 参考

### Channel 方法

| 方法 | 说明 |
|------|------|
| `Start(ctx)` | 启动 Channel |
| `Stop()` | 停止 Channel |
| `IsRunning()` | 检查运行状态 |
| `Send(ctx, msg)` | 发送 OutboundMessage |
| `SendStream(ctx, chatID, stream)` | 发送流式消息 |
| `HandleChatEvent(ctx, event)` | 处理聊天事件 |
| `ReplyWelcome(ctx, reqID, content)` | 回复欢迎语 |
| `ReplyMarkdown(ctx, reqID, content)` | 回复 Markdown |
| `ReplyTemplateCard(ctx, reqID, card, feedback)` | 回复模板卡片 |
| `ReplyStreamWithCard(ctx, reqID, streamID, content, finish, card, feedback)` | 流式+卡片组合 |
| `UpdateTemplateCard(ctx, reqID, card, userIDs)` | 更新模板卡片 |
| `SendMessage(ctx, chatID, content)` | 主动发送消息 |
| `SendTemplateCard(ctx, chatID, card)` | 主动发送卡片 |
| `DownloadFile(ctx, url, aesKey)` | 下载并解密文件 |
| `DownloadImage(ctx, media)` | 下载并解密图片 |

## 参考资源

- [企业微信智能机器人官方文档](https://developer.work.weixin.qq.com/document/path/101463)
- [Python SDK 参考](https://github.com/WechatWork/wecom-aibot-python-sdk)