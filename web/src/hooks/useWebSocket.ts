import { useState, useEffect, useRef, useCallback } from 'react'
import type {
  OutboundMessage,
  ChatEvent,
  LogEvent,
  ConnectionState,
  InboundMessage,
  WSMessage,
  SessionListPayload,
  SessionListAckPayload,
  SessionGetPayload,
  SessionGetAckPayload,
  SessionMetaPayload,
  Session,
  SessionMessage,
  MessageType,
  ToolCall,
} from '../types'

const WS_URL = 'ws://localhost:8765/ws'

interface UseWebSocketReturn {
  connectionState: ConnectionState
  messages: OutboundMessage[]
  events: ChatEvent[]
  logs: LogEvent[]
  sendMessage: (message: InboundMessage) => void
  clearMessages: () => void
  clearLogs: () => void
  // Session methods
  fetchSessionList: (dateStart?: string, dateEnd?: string) => Promise<SessionMetaPayload[]>
  fetchSession: (key: string) => Promise<Session | null>
}

// Generate unique message ID
function generateId(): string {
  return `${Date.now()}-${Math.random().toString(36).substr(2, 9)}`
}

export function useWebSocket(): UseWebSocketReturn {
  const [connectionState, setConnectionState] = useState<ConnectionState>('disconnected')
  const [messages, setMessages] = useState<OutboundMessage[]>([])
  const [events, setEvents] = useState<ChatEvent[]>([])
  const [logs, setLogs] = useState<LogEvent[]>([])
  const wsRef = useRef<WebSocket | null>(null)
  const reconnectTimeoutRef = useRef<number | undefined>(undefined)

  // Pending requests map for request/response pattern
  const pendingRequestsRef = useRef<Map<string, {
    resolve: (value: unknown) => void
    reject: (error: Error) => void
    timeout: number
  }>>(new Map())

  const connect = useCallback(() => {
    if (wsRef.current?.readyState === WebSocket.OPEN) return

    setConnectionState('connecting')
    const ws = new WebSocket(WS_URL)

    ws.onopen = () => {
      setConnectionState('connected')
      console.log('[WebSocket] Connected to gateway')
    }

    ws.onclose = () => {
      setConnectionState('disconnected')
      console.log('[WebSocket] Disconnected from gateway')
      // Reject all pending requests
      pendingRequestsRef.current.forEach(({ reject, timeout }) => {
        clearTimeout(timeout)
        reject(new Error('Connection closed'))
      })
      pendingRequestsRef.current.clear()
      // Attempt reconnect after 3 seconds
      reconnectTimeoutRef.current = window.setTimeout(() => {
        connect()
      }, 3000)
    }

    ws.onerror = (error) => {
      setConnectionState('error')
      console.error('[WebSocket] Error:', error)
    }

    ws.onmessage = (event) => {
      try {
        const data: WSMessage = JSON.parse(event.data)

        // Check if this is a response to a pending request
        const pending = pendingRequestsRef.current.get(data.id)
        if (pending) {
          clearTimeout(pending.timeout)
          pendingRequestsRef.current.delete(data.id)
          pending.resolve(data.payload)
          return
        }

        // Handle different message types
        switch (data.type) {
          case 'outbound':
            setMessages(prev => [...prev.slice(-100), data.payload as OutboundMessage])
            break
          case 'chat_event':
            setEvents(prev => [...prev.slice(-50), data.payload as ChatEvent])
            break
          case 'log_event':
            setLogs(prev => [...prev.slice(-200), data.payload as LogEvent])
            break
          case 'error':
            console.error('[WebSocket] Server error:', data.payload)
            break
        }
      } catch (err) {
        console.error('[WebSocket] Parse error:', err)
      }
    }

    wsRef.current = ws
  }, [])

  // Send a request and wait for response
  const sendRequest = useCallback(<T>(type: MessageType, payload: unknown, timeoutMs = 5000): Promise<T> => {
    return new Promise((resolve, reject) => {
      if (wsRef.current?.readyState !== WebSocket.OPEN) {
        reject(new Error('WebSocket not connected'))
        return
      }

      const id = generateId()
      const message: WSMessage = {
        type,
        id,
        timestamp: Date.now(),
        payload,
      }

      const timeout = window.setTimeout(() => {
        pendingRequestsRef.current.delete(id)
        reject(new Error('Request timeout'))
      }, timeoutMs)

      pendingRequestsRef.current.set(id, {
        resolve: (value) => resolve(value as T),
        reject,
        timeout,
      })

      wsRef.current.send(JSON.stringify(message))
    })
  }, [])

  const sendMessage = useCallback((message: InboundMessage) => {
    if (wsRef.current?.readyState === WebSocket.OPEN) {
      const wsMessage: WSMessage = {
        type: 'inbound',
        id: generateId(),
        timestamp: Date.now(),
        payload: message,
      }
      wsRef.current.send(JSON.stringify(wsMessage))
    } else {
      console.warn('[WebSocket] Cannot send message - not connected')
    }
  }, [])

  const clearMessages = useCallback(() => {
    setMessages([])
  }, [])

  const clearLogs = useCallback(() => {
    setLogs([])
  }, [])

  // Fetch session list
  const fetchSessionList = useCallback(async (dateStart?: string, dateEnd?: string): Promise<SessionMetaPayload[]> => {
    try {
      const payload: SessionListPayload = {}
      if (dateStart) payload.date_start = dateStart
      if (dateEnd) payload.date_end = dateEnd

      const response = await sendRequest<SessionListAckPayload>('session_list', payload)
      if (!response.success) {
        throw new Error(response.error || 'Failed to fetch session list')
      }
      return response.sessions || []
    } catch (error) {
      console.error('[WebSocket] Fetch session list error:', error)
      return []
    }
  }, [sendRequest])

  // Fetch single session
  const fetchSession = useCallback(async (key: string): Promise<Session | null> => {
    try {
      const payload: SessionGetPayload = { key }
      const response = await sendRequest<SessionGetAckPayload>('session_get', payload)
      if (!response.success) {
        throw new Error(response.error || 'Failed to fetch session')
      }

      // Convert to Session type
      const session: Session = {
        key: response.key || key,
        created_at: new Date(response.created_at || ''),
        updated_at: new Date(response.updated_at || ''),
        metadata: response.metadata,
        messages: (response.messages || []).map((m): SessionMessage => ({
          role: m.role as 'user' | 'assistant' | 'tool' | 'system',
          content: m.content,
          tool_call_id: m.tool_call_id,
          name: m.name,
          tool_calls: m.tool_calls?.map((tc): ToolCall => ({
            id: tc.id,
            type: tc.type as 'function',
            function: {
              name: tc.function.name,
              arguments: tc.function.arguments,
            },
          })),
        })),
        instructions: (response.instructions || []).map((i) => ({
          type: 'instruction',
          agent_name: i.agent_name,
          content: i.content,
          timestamp: new Date(i.timestamp),
          content_hash: '',
        })),
      }
      return session
    } catch (error) {
      console.error('[WebSocket] Fetch session error:', error)
      return null
    }
  }, [sendRequest])

  useEffect(() => {
    connect()

    return () => {
      if (reconnectTimeoutRef.current) {
        clearTimeout(reconnectTimeoutRef.current)
      }
      pendingRequestsRef.current.forEach(({ timeout }) => {
        clearTimeout(timeout)
      })
      if (wsRef.current) {
        wsRef.current.close()
      }
    }
  }, [connect])

  return {
    connectionState,
    messages,
    events,
    logs,
    sendMessage,
    clearMessages,
    clearLogs,
    fetchSessionList,
    fetchSession,
  }
}