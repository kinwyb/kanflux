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

- Terminal TUI（本地交互）
- WeChat / Slack / Discord 等（通过适配器）
- 自定义渠道扩展

每个渠道的用户独立识别，偏好互不干扰。

### 📦 三层记忆模型

| 层级 | Token 限制 | 存储内容 | 访问方式 |
|------|-----------|---------|---------|
| **L1 偏好层** | ~120 | 用户习惯、偏好、决策风格 | 始终加载，直接注入 prompt |
| **L2 事件层** | ~500 | 关键事件、决策、发现 | 按需检索，语义搜索 |
| **L3 原始层** | 无限制 | 完整对话记录、文档内容 | 深度语义搜索 |

### 🛠 工具与技能系统

- **内置工具**：文件操作、代码搜索、Shell 执行
- **技能扩展**：基于文件系统的技能加载，渐进式披露
- **工具审批**：敏感操作需用户确认，安全可控

## 快速开始

### 安装

```bash
git clone https://github.com/kinwyb/kanflux.git
cd kanflux
go build -o kanflux .
```

### 运行

```bash
# TUI 模式
./kanflux tui --workspace ~/my-workspace --model gpt-4o --api-key your-key

# 或使用环境变量
export OPENAI_API_KEY=your-key
./kanflux tui --workspace ~/my-workspace
```

### 目录结构

```
workspace/
├── .kanflux/
│   ├── memoria/          # 记忆系统数据
│   │   ├── l1/           # 用户偏好（按 accountID）
│   │   ├── l2/           # 事件与发现
│   │   ├── l3/           # 原始内容
│   │   └── metadata/     # 处理索引
│   ├── sessions/         # 对话历史
│   ├── skills/           # 技能定义
│   └── config.yaml       # 配置文件
└── your-project-files/   # 工作目录
```

## 记忆系统工作原理

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

```yaml
# config.yaml
agent:
  name: "Kanflux Agent"
  model: "gpt-4o"
  max_iterations: 20

memoria:
  enabled: true
  initial_scan: true
  schedule:
    enabled: true
    chat_interval: "5m"
    file_interval: "10m"

embedding:
  provider: "openai"
  model: "text-embedding-3-small"

watch_paths:
  - path: "./docs"
    extensions: ["md", "txt"]
    recursive: true
```

## 技术架构

基于 [Eino](https://github.com/cloudwego/eino) ADK 构建：

- **ChatModelAgent**：ReAct 模式的智能循环
- **Runner**：事件流管理、中断/恢复
- **Middleware**：技能加载、工具搜索、历史压缩
- **SessionValues**：运行时上下文注入

详见 [CLAUDE.md](./CLAUDE.md)。

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