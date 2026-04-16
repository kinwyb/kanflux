import { useState, useEffect, useRef, useCallback } from 'react'
import type {
  OutboundMessage,
  ChatEvent,
  LogEvent,
  ConnectionState,
  InboundMessage
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
}

export function useWebSocket(): UseWebSocketReturn {
  const [connectionState, setConnectionState] = useState<ConnectionState>('disconnected')
  const [messages, setMessages] = useState<OutboundMessage[]>([])
  const [events, setEvents] = useState<ChatEvent[]>([])
  const [logs, setLogs] = useState<LogEvent[]>([])
  const wsRef = useRef<WebSocket | null>(null)
  const reconnectTimeoutRef = useRef<number | undefined>(undefined)

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
        const data = JSON.parse(event.data)

        // Handle different message types
        if (data.content !== undefined && data.channel !== undefined) {
          // OutboundMessage
          setMessages(prev => [...prev.slice(-100), data as OutboundMessage])
        } else if (data.state !== undefined) {
          // ChatEvent
          setEvents(prev => [...prev.slice(-50), data as ChatEvent])
        } else if (data.level !== undefined) {
          // LogEvent
          setLogs(prev => [...prev.slice(-200), data as LogEvent])
        }
      } catch (err) {
        console.error('[WebSocket] Parse error:', err)
      }
    }

    wsRef.current = ws
  }, [])

  const sendMessage = useCallback((message: InboundMessage) => {
    if (wsRef.current?.readyState === WebSocket.OPEN) {
      wsRef.current.send(JSON.stringify(message))
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

  useEffect(() => {
    connect()

    return () => {
      if (reconnectTimeoutRef.current) {
        clearTimeout(reconnectTimeoutRef.current)
      }
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
  }
}