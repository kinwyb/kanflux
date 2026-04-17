# Kanflux Web Control Panel

Kanflux Web 是 Kanflux AI Agent 平台的前端控制面板，通过 WebSocket 与 Gateway 服务实时通信。

## 功能模块

### 📱 Chat 面板
实时对话界面，支持：
- 发送消息到 Agent
- 流式响应显示
- 工具调用展示（参数、结果）
- 思考内容（Reasoning）显示

### 📋 Sessions 面板
会话历史管理，支持：
- 按日期筛选会话
- 搜索会话 Key
- 查看会话详情（消息历史、工具调用）
- 展开/折叠会话预览

### ⏰ Tasks 面板（定时任务）
定时任务管理，支持：
- 任务列表展示（ID、名称、启用状态、Cron、Channel、执行状态）
- 添加新任务（ID、名称、Cron 表达式、目标 Channel/ChatID、Prompt）
- 编辑任务配置
- 启用/禁用任务
- 手动触发任务
- 删除任务
- Cron 表达式快捷选择（每天9点、每小时、每30分钟、工作日9点等）

### 📊 Logs 面板
实时日志监控，支持：
- 日志级别过滤（debug、info、warn、error）
- 日志来源显示
- 自动滚动到最新日志

## WebSocket 通信

Web 通过 WebSocket 连接 Gateway 服务，实现：

| 消息类型 | 方向 | 说明 |
|---------|------|------|
| `inbound` | 客户端 → 服务端 | 发送消息到 Agent |
| `outbound` | 服务端 → 客户端 | Agent 响应（流式/完整） |
| `chat_event` | 服务端 → 客户端 | 聊天事件（start、tool、complete、error、interrupt） |
| `log_event` | 服务端 → 客户端 | 日志事件 |
| `heartbeat` | 客户端 → 服务端 | 心跳检测 |
| `session_list` | 客户端 → 服务端 | 获取会话列表 |
| `session_get` | 客户端 → 服务端 | 获取会话详情 |
| `task_list` | 客户端 → 服务端 | 获取任务列表 |
| `task_add` | 客户端 → 服务端 | 添加任务 |
| `task_update` | 客户端 → 服务端 | 更新任务 |
| `task_remove` | 客户端 → 服务端 | 删除任务 |
| `task_trigger` | 客户端 → 服务端 | 触发任务 |

## 技术栈

- **React 19** - 前端框架
- **TypeScript** - 类型安全
- **Vite** - 构建工具
- **Framer Motion** - 动画库
- **Tailwind CSS** - 样式框架
- **Lucide React** - 图标库
- **date-fns** - 日期处理

## 目录结构

```
web/
├── src/
│   ├── components/       # 组件
│   │   ├── ChatPanel.tsx    # 对话面板
│   │   ├── SessionsPanel.tsx # 会话面板
│   │   ├── TasksPanel.tsx   # 任务面板
│   │   └── LogsPanel.tsx    # 日志面板
│   ├── hooks/           # Hooks
│   │   ├── useWebSocket.ts  # WebSocket 连接管理
│   │   └── index.ts         # Hook 导出
│   ├── types/           # 类型定义
│   │   └── index.ts         # 所有类型定义
│   ├── App.tsx          # 主应用
│   ├── main.tsx         # 入口
│   └── index.css        # 样式（含 Tailwind）
├── public/              # 静态资源
├── package.json         # 依赖配置
├── vite.config.ts       # Vite 配置
├── tsconfig.json        # TypeScript 配置
└── eslint.config.js     # ESLint 配置
```

## 开发

```bash
# 安装依赖
npm install

# 启动开发服务器
npm run dev

# 构建生产版本
npm run build

# 预览生产版本
npm run preview
```

## 默认 WebSocket 地址

开发环境下默认连接 `ws://localhost:8765/ws`。

可通过修改 `src/hooks/useWebSocket.ts` 中的 `WS_URL` 常量来更改地址。

## UI 设计

采用玻璃态（Glassmorphism）设计风格：

- **Fluid Background** - 流动的背景球体动画
- **Glass Cards** - 半透明玻璃卡片效果
- **Sidebar Navigation** - 可折叠侧边栏
- **Connection Indicator** - WebSocket 连接状态指示

颜色主题：
- `cyan-electric` - 主强调色
- `cyan-glow` - 辅助强调色
- `ocean-deep` - 深色文字
- `ocean-depth` - 中性文字
- `ocean-surface` - 浅色背景

## 定时任务 Cron 表达式示例

| 表达式 | 说明 |
|--------|------|
| `0 9 * * *` | 每天 9:00 |
| `0 * * * *` | 每小时整点 |
| `*/30 * * * *` | 每 30 分钟 |
| `0 9 * * 1-5` | 工作日 9:00 |
| `0 18 * * *` | 每天 18:00 |

## 相关文档

- [Kanflux 主项目 README](../README.md)
- [Kanflux CLAUDE.md](../CLAUDE.md)
- [Gateway WebSocket API](../gateway/README.md)