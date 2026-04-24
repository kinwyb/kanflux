import { createContext, useContext } from 'react'
import type {
  OutboundMessage,
  ChatEvent,
  LogEvent,
  ConnectionState,
  InboundMessage,
  SessionMetaPayload,
  Session,
  TaskDetail,
  TaskConfig,
  TaskStatusAckPayload,
  ConfigGetAckPayload,
  ConfigUpdateAckPayload,
} from '../types'

interface WebSocketContextValue {
  connectionState: ConnectionState
  messages: OutboundMessage[]
  events: ChatEvent[]
  logs: LogEvent[]
  sendMessage: (message: InboundMessage) => void
  clearMessages: () => void
  clearLogs: () => void
  clearEvents: () => void
  // Session methods
  fetchSessionList: (dateStart?: string, dateEnd?: string) => Promise<SessionMetaPayload[]>
  fetchSession: (key: string) => Promise<Session | null>
  deleteSession: (key: string) => Promise<{ success: boolean; error?: string }>
  // Task methods
  fetchTaskList: () => Promise<TaskDetail[]>
  addTask: (config: TaskConfig) => Promise<{ success: boolean; id?: string; error?: string }>
  updateTask: (id: string, config: Partial<TaskConfig>) => Promise<{ success: boolean; id?: string; error?: string }>
  removeTask: (id: string) => Promise<{ success: boolean; id?: string; error?: string }>
  triggerTask: (id: string) => Promise<{ success: boolean; id?: string; error?: string }>
  fetchTaskStatus: (id: string) => Promise<TaskStatusAckPayload>
  // Config methods
  fetchConfig: () => Promise<ConfigGetAckPayload>
  updateConfig: (config: Record<string, unknown>) => Promise<ConfigUpdateAckPayload>
}

export const WebSocketContext = createContext<WebSocketContextValue | null>(null)

export function useWebSocketContext(): WebSocketContextValue {
  const context = useContext(WebSocketContext)
  if (!context) {
    throw new Error('useWebSocketContext must be used within WebSocketProvider')
  }
  return context
}