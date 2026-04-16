// WebSocket Message Types (matching Go backend bus/events.go)

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
  error?: string
  metadata?: Record<string, unknown>
  timestamp: Date
}

export interface ChatEvent {
  id: string
  channel: string
  chat_id: string
  run_id: string
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

// Chat State
export interface ChatMessage {
  id: string
  role: 'user' | 'assistant'
  content: string
  reasoning?: string
  timestamp: Date
  isStreaming?: boolean
  toolCalls?: ToolCall[]
  toolCallDisplays?: ToolCallDisplay[]
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
  | 'outbound'
  | 'chat_event'
  | 'log_event'
  | 'heartbeat_ack'
  | 'control_ack'
  | 'error'
  | 'session_list_ack'
  | 'session_get_ack'

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