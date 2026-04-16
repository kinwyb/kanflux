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

// Chat State
export interface ChatMessage {
  id: string
  role: 'user' | 'assistant'
  content: string
  reasoning?: string
  timestamp: Date
  isStreaming?: boolean
  toolCalls?: ToolCall[]
}

// WebSocket Connection State
export type ConnectionState = 'connecting' | 'connected' | 'disconnected' | 'error'