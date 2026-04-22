// WebSocket Message Types (matching Go backend bus/events.go)

// Conversation 类型 - 用于多对话管理
export interface Conversation {
  id: string           // chat_id (UUID)
  title: string        // 对话标题
  createdAt: Date
  updatedAt: Date
  messageCount: number
}

export interface InboundMessage {
  id: string
  channel: string
  account_id: string
  sender_id: string
  chat_id: string
  content: string
  streaming_mode: 'delta' | 'accumulate'
  metadata?: Record<string, unknown>
  timestamp: Date
}

export interface OutboundMessage {
  id: string
  channel: string
  chat_id: string
  content: string
  reasoning_content?: string
  is_streaming: boolean
  is_thinking: boolean
  is_final: boolean
  chunk_index: number
  reply_to?: string // 关联的入站消息ID，与 ChatEvent.run_id 一致
  error?: string
  metadata?: Record<string, unknown>
  timestamp: Date
}

export interface ChatEvent {
  id: string
  channel: string
  chat_id: string
  reply_to: string // 关联的入站消息ID，与 OutboundMessage.reply_to 一致
  seq: number
  agent_name: string
  state: 'start' | 'tool' | 'complete' | 'error' | 'interrupt'
  error?: string
  tool_info?: ToolEventInfo
  timestamp: Date
  metadata?: unknown
}

export interface ToolEventInfo {
  name: string
  id: string
  arguments?: string
  result?: string
  is_start: boolean
}

export interface LogEvent {
  id: string
  level: 'debug' | 'info' | 'warn' | 'error'
  message: string
  timestamp: Date
  source: string
}

// Session Types
export interface Session {
  key: string
  instructions?: InstructionEntry[]
  messages: SessionMessage[]
  created_at: Date
  updated_at: Date
  metadata?: Record<string, unknown>
}

export interface InstructionEntry {
  type: 'instruction'
  agent_name: string
  content: string
  timestamp: Date
  content_hash: string
}

export interface SessionMessage {
  role: 'user' | 'assistant' | 'tool' | 'system'
  content: string
  tool_calls?: ToolCall[]
  tool_call_id?: string
  name?: string
  timestamp?: Date
}

export interface ToolCall {
  id: string
  type: 'function'
  function: {
    name: string
    arguments: string
  }
}

// Tool call result for display
export interface ToolCallDisplay {
  id: string
  name: string
  arguments: string
  result?: string
  status: 'running' | 'completed' | 'error'
  startTime: Date
  endTime?: Date
}

// Chat State - 按时间顺序排列的消息块
export type MessageBlockType = 'start' | 'thinking' | 'output' | 'tool_call' | 'tool_result'

export interface MessageBlock {
  id: string
  type: MessageBlockType
  content?: string
  reasoning?: string
  toolInfo?: ToolCallDisplay
  timestamp: Date
}

export interface ChatMessage {
  id: string
  role: 'user' | 'assistant'
  content: string
  timestamp: Date
  isStreaming?: boolean
  messageBlocks?: MessageBlock[] // 按时间顺序的消息块数组
}

// WebSocket Connection State
export type ConnectionState = 'connecting' | 'connected' | 'disconnected' | 'error'

// WebSocket Message Types (matching Go backend gateway/types/types.go)
export interface WSMessage {
  type: MessageType
  id: string
  timestamp: number
  payload: unknown
}

export type MessageType =
  | 'inbound'
  | 'subscribe'
  | 'heartbeat'
  | 'control'
  | 'session_list'
  | 'session_get'
  | 'session_delete'
  | 'task_list'
  | 'task_add'
  | 'task_update'
  | 'task_remove'
  | 'task_trigger'
  | 'task_status'
  | 'config_get'
  | 'config_update'
  | 'outbound'
  | 'chat_event'
  | 'log_event'
  | 'heartbeat_ack'
  | 'control_ack'
  | 'error'
  | 'session_list_ack'
  | 'session_get_ack'
  | 'session_delete_ack'
  | 'task_list_ack'
  | 'task_add_ack'
  | 'task_update_ack'
  | 'task_remove_ack'
  | 'task_trigger_ack'
  | 'task_status_ack'
  | 'config_get_ack'
  | 'config_update_ack'

// Session List Request/Response
export interface SessionListPayload {
  date_start?: string // YYYY-MM-DD
  date_end?: string // YYYY-MM-DD
}

export interface SessionMetaPayload {
  key: string
  created_at: string
  updated_at: string
  message_count: number
  instruction_count: number
  metadata?: Record<string, unknown>
}

export interface SessionListAckPayload {
  success: boolean
  error?: string
  sessions?: SessionMetaPayload[]
}

// Session Get Request/Response
export interface SessionGetPayload {
  key: string
}

export interface SessionGetAckPayload {
  success: boolean
  error?: string
  key?: string
  created_at?: string
  updated_at?: string
  metadata?: Record<string, unknown>
  messages?: SessionMessagePayload[]
  instructions?: InstructionPayload[]
}

export interface SessionMessagePayload {
  role: string
  content: string
  tool_call_id?: string
  name?: string
  tool_calls?: ToolCallPayload[]
}

export interface ToolCallPayload {
  id: string
  type: string
  function: {
    name: string
    arguments: string
  }
}

export interface InstructionPayload {
  agent_name: string
  content: string
  timestamp: string
}

// ========== Task Scheduler Types ==========

export type TaskMessageType =
  | 'task_list'
  | 'task_add'
  | 'task_update'
  | 'task_remove'
  | 'task_trigger'
  | 'task_status'
  | 'task_list_ack'
  | 'task_add_ack'
  | 'task_update_ack'
  | 'task_remove_ack'
  | 'task_trigger_ack'
  | 'task_status_ack'

// Task Schedule Config
export interface ScheduleConfig {
  cron: string
}

// Task Target Config
export interface TargetConfig {
  channel: string
  account_id?: string
  chat_id: string
  agent_name?: string
}

// Task Content Config
export interface ContentConfig {
  prompt: string
}

// Task Config (for add/update)
export interface TaskConfig {
  id: string
  name: string
  description?: string
  enabled: boolean
  schedule: ScheduleConfig
  target: TargetConfig
  content: ContentConfig
}

// Task State
export interface TaskState {
  last_run?: number // Unix timestamp (ms)
  last_result?: string
  last_error?: string
  success_count: number
  fail_count: number
  next_run?: number // Unix timestamp (ms)
}

// Task Detail (full task info with state)
export interface TaskDetail {
  id: string
  name: string
  description?: string
  enabled: boolean
  schedule: ScheduleConfig
  target: TargetConfig
  content: ContentConfig
  next_run?: number // Unix timestamp (ms)
  last_run?: number // Unix timestamp (ms)
  is_running: boolean
  state?: TaskState
}

// Task List Response
export interface TaskListAckPayload {
  success: boolean
  error?: string
  tasks?: TaskDetail[]
}

// Task Add Request
export interface TaskAddPayload {
  id: string
  name: string
  description?: string
  enabled: boolean
  schedule: ScheduleConfig
  target: TargetConfig
  content: ContentConfig
}

// Task Add Response
export interface TaskAddAckPayload {
  success: boolean
  error?: string
  id?: string
}

// Task Update Request
export interface TaskUpdatePayload {
  id: string
  name?: string
  description?: string
  enabled?: boolean
  schedule?: ScheduleConfig
  target?: TargetConfig
  content?: ContentConfig
}

// Task Update Response
export interface TaskUpdateAckPayload {
  success: boolean
  error?: string
  id?: string
}

// Task Remove Request
export interface TaskRemovePayload {
  id: string
}

// Task Remove Response
export interface TaskRemoveAckPayload {
  success: boolean
  error?: string
  id?: string
}

// Task Trigger Request
export interface TaskTriggerPayload {
  id: string
}

// Task Trigger Response
export interface TaskTriggerAckPayload {
  success: boolean
  error?: string
  id?: string
}

// Task Status Request
export interface TaskStatusPayload {
  id: string
}

// Task Status Response
export interface TaskStatusAckPayload {
  success: boolean
  error?: string
  id?: string
  state?: TaskState
}

// ========== Config Management Types ==========

// Config Get Request (empty payload)
export interface ConfigGetPayload {}

// Config Get Response
export interface ConfigGetAckPayload {
  success: boolean
  error?: string
  config?: Record<string, unknown>
}

// Config Update Request
export interface ConfigUpdatePayload {
  config: Record<string, unknown>
}

// Config Update Response
export interface ConfigUpdateAckPayload {
  success: boolean
  error?: string
  message?: string
}

// ========== Session Delete Types ==========

// Session Delete Request
export interface SessionDeletePayload {
  key: string
}

// Session Delete Response
export interface SessionDeleteAckPayload {
  success: boolean
  error?: string
  key?: string
}