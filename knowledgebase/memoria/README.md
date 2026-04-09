# Memoria - 后台记忆代理模块

Memoria 是一个独立的后台运行 agent 模块，用于处理聊天记录和文件内容，按 MemPalace 分层设计组织记忆，供其他 agent 作为长期记忆和知识库使用。

## 设计思路

### MemPalace 分层架构

基于 MemPalace 的三层记忆模型（跳过 L0 身份层）：

| 层级 | 名称 | Token 限制 | 特点 | 存储类型 |
|------|------|-----------|------|---------|
| **L1** | 关键事实层 | ~120 tokens | 始终加载，内存缓存 | `hall_facts`, `hall_preferences` |
| **L2** | 房间回忆层 | ~500 tokens | 按需加载，按日期分区 | `hall_events`, `hall_discoveries` |
| **L3** | 深度搜索层 | 无限制 | 全库语义搜索 | 所有类型原始内容 |

### 五个 Hall 类型（记忆大厅）

| Hall 类型 | 说明 | 示例 |
|-----------|------|------|
| `hall_facts` | 决策、锁定选择 | "项目使用 PostgreSQL 作为主数据库" |
| `hall_events` | 会话、里程碑、调试过程 | "2024-01-15 完成用户认证模块开发" |
| `hall_discoveries` | 突破、新洞察 | "发现 index 未命中导致查询慢，添加索引后提升 10x" |
| `hall_preferences` | 习惯、偏好、观点 | "用户偏好使用 dark mode" |
| `hall_advice` | 推荐、解决方案 | "建议使用 connection pool 减少数据库连接开销" |

### 处理流程

```
输入源 (聊天记录/文件) → 解析/分块 → LLM 分类+摘要 → 存储到对应 Layer
                                                    ↓
                                            定时触发更新
                                                    ↓
                                            其他 Agent 查询使用
```

**聊天记录处理流程：**
1. 扫描 session 目录 (`*.jsonl` 文件)
2. 解析 JSONL，提取消息
3. 按 session key 分割用户：`Channel:AccountID:ChatID`
4. 提取 QA pairs (用户问题 + AI 回答)
5. **批量合并** (默认 5 组问答一次 LLM 调用)
6. LLM 分类到 HallType 和 Layer
7. LLM 生成摘要
8. 存储到对应层 MD 文件

**文件处理流程：**
1. 定时扫描监听路径
2. 检测修改文件
3. 读取文件内容并分块
4. LLM 提取有用信息
5. 分类、摘要、存储

### 批量优化

为减少 LLM API 调用次数：
- **聊天处理**：默认每 5 组问答合并一次请求
- **L1 压缩**：当接近 token 限制时，使用 `CompactPrompt` 合并多条记忆
- **关键词预判**：先用 `HallKeywords` / `LayerKeywords` 快速分类，不确定时才调用 LLM

## 文件结构

```
knowledgebase/memoria/
├── types.go              # 类型重导出
├── config.go             # 配置结构
├── memoria.go            # 主服务入口 (Memoria orchestrator)
├── scheduler.go          # 定时触发调度器
├── layers.go             # 三层实现 (L1, L2, L3)
├── memoria_test.go       # 测试文件
├── types/
│   └── types.go          # 核心类型定义
├── llm/
│   ├── summarizer.go     # LLM 摘要器实现
│   └── prompts.go        # 提示模板 (批量、压缩等)
├── processor/
│   ├── processor.go      # 处理器基类
│   ├── chat_processor.go # 聊天历史处理器
│   └── file_processor.go # 文件修改处理器
│   └── watcher.go        # 文件监听器
└── storage/
    └── md_store.go       # MD 文件存储管理
```

## 存储结构

```
<workspace>/memoria/
├── l1/
│   ├── facts_<user>.md      # L1 关键事实（按用户）
│   └── prefs_<user>.md      # L1 偏好
├── l2/
│   ├── 2026-04-09_<user>.md # L2 按日期分文件
│   └── events/
│   └── discoveries/
├── files/                    # 文件来源的记忆（按文件hash存储）
│   ├── facts/
│   ├── preferences/
│   ├── events/
│   └── discoveries/
├── l3_index.db              # SQLite 向量索引
└── metadata/
    ├── users.json           # 用户注册表
    └── file_index.json      # 文件处理状态索引
```

**存储区分：**

| 来源 | 存储位置 | 命名规则 | 更新策略 |
|------|---------|---------|---------|
| 聊天记录 | `l1/`, `l2/` | `<user>.md` / `<date>_<user>.md` | 追加 |
| 文件内容 | `files/` | `<file_hash>.md` | 替换（文件变化时删除旧数据） |

**文件索引** (`metadata/file_index.json`)：
- 记录每个已处理文件的内容 hash
- 文件内容变化时自动重新处理
- 避免重复处理相同内容的文件

## 核心接口

### UserIdentity - 用户身份接口

```go
type UserIdentity interface {
    GetUserID() string      // 用户唯一标识
    GetDisplayName() string // 显示名称
    GetChannel() string     // 来源渠道
    GetAccountID() string   // 账户ID
    GetMetadata() map[string]any
}

// 默认实现（从 session key 解析）
type DefaultUserIdentity struct {
    UserID    string
    Channel   string
    AccountID string
    ChatID    string
}
```

### ChatModel - LLM 模型接口

```go
type ChatModel interface {
    Generate(ctx context.Context, prompt string) (string, error)
    GenerateWithSystem(ctx context.Context, system, prompt string) (string, error)
}
```

### Storage - 存储接口

```go
type Storage interface {
    Store(ctx context.Context, item *MemoryItem) error
    Retrieve(ctx context.Context, opts *RetrieveOptions) ([]*MemoryItem, error)
    Delete(ctx context.Context, id string) error
    Close() error
}
```

### Summarizer - 摘要器接口

```go
type Summarizer interface {
    Summarize(ctx context.Context, content string, hallType HallType, layer Layer) (string, error)
    ExtractFacts(ctx context.Context, content string, userCtx UserIdentity) ([]*MemoryItem, error)
    Categorize(ctx context.Context, content string) (HallType, Layer, error)
    ProcessContent(ctx context.Context, content string, userCtx UserIdentity) ([]*MemoryItem, error)
}
```

## 使用方法

### 基本初始化

```go
package main

import (
    "context"
    "github.com/kinwyb/kanflux/knowledgebase/memoria"
    "github.com/kinwyb/kanflux/knowledgebase/memoria/types"
)

func main() {
    // 1. 创建配置
    config := &types.Config{
        Workspace: "/path/to/workspace",
        Schedule: types.ScheduleConfig{
            Enabled:         true,
            ChatInterval:    5 * time.Minute,  // 聊天扫描间隔
            FileInterval:    10 * time.Minute, // 文件扫描间隔
            CleanupInterval: 1 * time.Hour,    // 清理间隔
        },
    }

    // 2. 创建 LLM 模型 (需实现 ChatModel 接口)
    model := &MyChatModel{} // 用户自行实现

    // 3. 创建 Memoria 服务
    service := memoria.NewMemoria(config, model)

    // 4. 初始化
    if err := service.Initialize(context.Background()); err != nil {
        panic(err)
    }

    // 5. 启动后台服务
    if err := service.Start(context.Background()); err != nil {
        panic(err)
    }

    // 6. 运行结束后停止
    service.Stop()
}
```

### 自定义监听路径

```go
watchPaths := []types.WatchPath{
    {
        Path:       "/path/to/sessions/",
        Extensions: []string{".jsonl"},
        Recursive:  false,
    },
    {
        Path:       "/path/to/docs/",
        Extensions: []string{".md", ".txt"},
        Recursive:  true,
        Exclude:    []string{"node_modules", ".git"},
    },
}

config.WatchPaths = watchPaths
```

### 手动处理内容

```go
// 处理单个聊天会话
result, err := service.ProcessChatSession(ctx, sessionContent, userCtx)

// 处理文件内容
result, err := service.ProcessFile(ctx, filePath, content, userCtx)

// 检索记忆
items, err := service.Retrieve(ctx, &types.RetrieveOptions{
    Layers:   []types.Layer{types.LayerL1, types.LayerL2},
    HallType: types.HallFacts,
    UserID:   "user123",
    Query:    "database decision",
    Limit:    20,
})
```

### 批量处理配置

```go
// ProcessorConfig 配置说明
processorConfig := &types.ProcessorConfig{
    MaxBatchSize:    8000, // 文件处理: 分块大小(字符), 默认 8000
                        // 聊天处理: QA pairs 数量, 默认 5
    EnableParallel:  true, // 启用并行处理
    MaxConcurrency:  3,    // 最大并发数
}
```

**MaxBatchSize 含义区分：**

| 处理器 | 含义 | 默认值 | 说明 |
|--------|------|--------|------|
| FileProcessor | 字符数 | 8000 | 文件内容分块大小，超过此值切分 |
| ChatProcessor | QA数量 | 5 | 每次LLM调用处理的问答对数量 |

## MD 文件格式

### L1 文件格式 (`l1/facts_<user>.md`)

```markdown
# L1 Facts - user123

## 2026-04-09 10:30:00
- **ID**: mem_1234567890
- **Hall**: hall_facts
- **Summary**: 项目使用 PostgreSQL 作为主数据库，已锁定选择
- **Source**: chat
- **Keywords**: database, postgres, decision

## 2026-04-09 11:00:00
- **ID**: mem_1234567891
- **Hall**: hall_preferences
- **Summary**: 用户偏好使用 dark mode 界面
- **Source**: chat
- **Keywords**: ui, preference, dark-mode
```

### L2 文件格式 (`l2/2026-04-09_<user>.md`)

```markdown
# L2 Events - 2026-04-09 - user123

## Session Summary
- **ID**: mem_1234567892
- **Hall**: hall_events
- **Time**: 2026-04-09 09:00-11:30
- **Summary**: 完成用户认证模块开发，包含登录、注册、权限验证
- **Keywords**: auth, development, milestone

## Discovery
- **ID**: mem_1234567893
- **Hall**: hall_discoveries
- **Summary**: 发现 index 未命中导致查询慢，添加索引后性能提升 10 倍
- **Keywords**: performance, index, optimization
```

## 提示模板说明

| 模板 | 用途 | 调用位置 |
|------|------|---------|
| `SystemPrompt` | 基础系统提示 | 所有 LLM 调用 |
| `ChatSummarizePrompt` | 单组问答摘要 | 单条处理 |
| `ChatBatchSummarizePrompt` | 批量问答摘要 | 批量处理 |
| `FileSummarizePrompt` | 文件内容摘要 | 文件处理器 |
| `CompactPrompt` | L1 压缩合并 | L1 层压缩 |
| `GetHallPrompt()` | Hall 类型指导 | 摘要生成 |

## 关键词分类

### HallKeywords - 自动分类关键词

```go
hall_facts:    decided, decision, chose, locked, confirmed, approved...
hall_events:   session, meeting, milestone, completed, finished, debugged...
hall_discoveries: discovered, found, learned, realized, insight, breakthrough...
hall_preferences: prefer, always use, never, like, dislike, habit...
hall_advice:   recommend, suggest, advice, tip, should, best practice...
```

### LayerKeywords - 层级判断关键词

```go
L1: critical, important, essential, key, core, must, always...
L3: detailed, full, complete, raw, verbatim, entire...
```

## 集成指南

### 1. 实现 ChatModel 接口

使用 Eino 的 ToolCallingChatModel：

```go
import "github.com/cloudwego/eino/components/model"

type EinoChatModel struct {
    model model.ChatModel
}

func (m *EinoChatModel) Generate(ctx context.Context, prompt string) (string, error) {
    resp, err := m.model.Generate(ctx, []*schema.Message{
        {Role: schema.User, Content: prompt},
    })
    if err != nil {
        return "", err
    }
    return resp.Content, nil
}

func (m *EinoChatModel) GenerateWithSystem(ctx context.Context, system, prompt string) (string, error) {
    resp, err := m.model.Generate(ctx, []*schema.Message{
        {Role: schema.System, Content: system},
        {Role: schema.User, Content: prompt},
    })
    if err != nil {
        return "", err
    }
    return resp.Content, nil
}
```

### 2. 继承 UserIdentity

集成主系统用户信息：

```go
type SystemUser struct {
    types.DefaultUserIdentity
    Email       string
    Department  string
}

func (u *SystemUser) GetMetadata() map[string]any {
    return map[string]any{
        "email":      u.Email,
        "department": u.Department,
    }
}
```

### 3. 作为知识库使用

```go
// 在其他 agent 中检索记忆
memoriaService := memoria.NewMemoria(config, model)

// 获取 L1 关键事实（始终加载）
l1Items := memoriaService.GetL1Facts("user123")

// 按需检索 L2/L3
items, _ := memoriaService.Retrieve(ctx, &types.RetrieveOptions{
    UserID: "user123",
    Query:  "authentication implementation",
    Layers: []types.Layer{types.LayerL2, types.LayerL3},
    Limit:  10,
})

// 将记忆注入 agent 的上下文
contextBuilder.AddMemories(items)
```

## 定时调度

默认调度配置：

| 任务 | 默认间隔 | 说明 |
|------|---------|------|
| ChatScan | 5 分钟 | 扫描新/修改的 session 文件 |
| FileScan | 10 分钟 | 扫描监听路径的文件修改 |
| Cleanup | 1 小时 | 清理过期 L2 记忆（超过 30 天） |

可通过 `ScheduleConfig` 自定义：

```go
config.Schedule = types.ScheduleConfig{
    Enabled:         true,
    ChatInterval:    2 * time.Minute,  // 更频繁
    FileInterval:    5 * time.Minute,
    CleanupInterval: 30 * time.Minute,
    CleanupDays:     7, // 只保留 7 天内的 L2 记忆
}
```

## 测试

```bash
# 运行测试
go test ./knowledgebase/memoria/... -v

# 运行单个测试
go test -v -run TestMemoriaService ./knowledgebase/memoria/
```

## 注意事项

1. **LLM 成本**：批量处理默认 5 组问答一次请求，可根据成本/效率需求调整 `MaxBatchSize`
2. **Token 限制**：L1 层限制约 120 tokens，超出时自动压缩合并
3. **用户分区**：所有记忆按 UserID 分区存储，不同用户数据隔离
4. **文件监听**：使用定时扫描而非 fsnotify，减少系统资源占用
5. **Session 格式**：JSONL 格式，每行一条消息，包含 `role` 和 `content` 字段