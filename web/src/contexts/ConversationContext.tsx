import { createContext, useContext, useState, useEffect, useCallback, useRef } from 'react'
import { useWebSocketContext } from './WebSocketContext'
import type { Conversation, Session, SessionMessage, ToolCall } from '../types'

// Web channel constants
export const WEB_CHANNEL = 'web'
export const WEB_ACCOUNT_ID = 'default'
export const WEB_SESSION_PREFIX = `${WEB_CHANNEL}_${WEB_ACCOUNT_ID}_`
export const WEB_SESSION_PREFIX_LENGTH = WEB_SESSION_PREFIX.length // 12

const STORAGE_KEY = 'kanflux_web_conversations'

interface StoredConversations {
  version: number
  conversations: Conversation[]
  lastActiveId: string | null
}

interface ConversationContextValue {
  conversations: Conversation[]
  activeConversationId: string | null
  isLoading: boolean
  createConversation: () => Promise<string | null>
  switchConversation: (id: string) => void
  deleteConversation: (id: string) => Promise<boolean>
  updateConversationTitle: (id: string, title: string) => void
  refreshConversations: () => Promise<void>
  getActiveConversation: () => Conversation | null
  getActiveSessionKey: () => string
  // History loading
  loadHistory: (sessionKey: string) => Promise<Session | null>
  historyLoadedKeys: Set<string>
  markHistoryLoaded: (key: string) => void
}

export const ConversationContext = createContext<ConversationContextValue | null>(null)

export function useConversationContext(): ConversationContextValue {
  const context = useContext(ConversationContext)
  if (!context) {
    throw new Error('useConversationContext must be used within ConversationProvider')
  }
  return context
}

// Convert SessionMessage to display message format
function sessionToChatMessage(msg: SessionMessage, idx: number): { id: string; role: 'user' | 'assistant'; content: string; timestamp: Date; toolCalls?: ToolCall[] } {
  return {
    id: `history-${idx}`,
    role: msg.role === 'tool' || msg.role === 'system' ? 'assistant' : msg.role,
    content: msg.content,
    timestamp: msg.timestamp || new Date(),
    toolCalls: msg.tool_calls,
  }
}

// Extract title from first user message
function extractTitle(content: string): string {
  // Take first line, limit to 50 chars
  const firstLine = content.split('\n')[0]
  if (firstLine.length > 50) {
    return firstLine.substring(0, 50) + '...'
  }
  return firstLine
}

export function ConversationProvider({ children }: { children: React.ReactNode }) {
  const { connectionState, fetchSessionList, fetchSession, deleteSession } = useWebSocketContext()
  const [conversations, setConversations] = useState<Conversation[]>([])
  const [activeConversationId, setActiveConversationId] = useState<string | null>(null)
  const [isLoading, setIsLoading] = useState(false)
  const historyLoadedKeysRef = useRef<Set<string>>(new Set())

  // Load from localStorage on mount
  useEffect(() => {
    const stored = localStorage.getItem(STORAGE_KEY)
    if (stored) {
      try {
        const data: StoredConversations = JSON.parse(stored)
        setConversations(data.conversations.map(c => ({
          ...c,
          createdAt: new Date(c.createdAt),
          updatedAt: new Date(c.updatedAt)
        })))
        setActiveConversationId(data.lastActiveId)
      } catch (e) {
        console.error('Failed to parse stored conversations', e)
      }
    }
  }, [])

  // Persist to localStorage on changes
  useEffect(() => {
    const data: StoredConversations = {
      version: 1,
      conversations,
      lastActiveId: activeConversationId
    }
    localStorage.setItem(STORAGE_KEY, JSON.stringify(data))
  }, [conversations, activeConversationId])

  // Sync with backend when connected
  useEffect(() => {
    if (connectionState === 'connected') {
      refreshConversations()
    }
  }, [connectionState])

  const refreshConversations = useCallback(async () => {
    setIsLoading(true)
    try {
      const metas = await fetchSessionList()
      // Filter to web channel sessions
      const webSessions = metas.filter(m => m.key.startsWith(WEB_SESSION_PREFIX))

      // Create conversations from backend sessions
      const backendConvs: Conversation[] = webSessions.map(meta => {
        const chatId = meta.key.substring(WEB_SESSION_PREFIX_LENGTH)
        return {
          id: chatId,
          title: `Chat ${chatId.substring(0, 8)}`, // Default title
          createdAt: new Date(meta.created_at),
          updatedAt: new Date(meta.updated_at),
          messageCount: meta.message_count
        }
      })

      // Merge with local conversations
      const mergedMap = new Map<string, Conversation>()

      // Add local conversations first
      for (const conv of conversations) {
        mergedMap.set(conv.id, conv)
      }

      // Update with backend data
      for (const conv of backendConvs) {
        const existing = mergedMap.get(conv.id)
        if (existing) {
          // Update counts and timestamps from backend
          mergedMap.set(conv.id, {
            ...existing,
            updatedAt: conv.updatedAt,
            messageCount: conv.messageCount
          })
        } else {
          // Add new conversation from backend
          mergedMap.set(conv.id, conv)
        }
      }

      setConversations(Array.from(mergedMap.values()))
    } catch (e) {
      console.error('Failed to refresh conversations', e)
    }
    setIsLoading(false)
  }, [fetchSessionList, conversations])

  const createConversation = useCallback(async (): Promise<string | null> => {
    const chatId = crypto.randomUUID()
    const newConv: Conversation = {
      id: chatId,
      title: `New Chat`,
      createdAt: new Date(),
      updatedAt: new Date(),
      messageCount: 0
    }
    setConversations(prev => [...prev, newConv])
    setActiveConversationId(chatId)
    // Reset history loaded state for new conversation
    historyLoadedKeysRef.current.delete(`${WEB_SESSION_PREFIX}${chatId}`)
    return chatId
  }, [])

  const switchConversation = useCallback((id: string) => {
    setActiveConversationId(id)
  }, [])

  const deleteConversation = useCallback(async (id: string): Promise<boolean> => {
    const conv = conversations.find(c => c.id === id)
    if (!conv) return false

    const sessionKey = `${WEB_SESSION_PREFIX}${id}`
    const result = await deleteSession(sessionKey)

    if (result.success) {
      setConversations(prev => prev.filter(c => c.id !== id))
      // Clear history loaded state
      historyLoadedKeysRef.current.delete(sessionKey)

      if (activeConversationId === id) {
        // Switch to another conversation or null
        const remaining = conversations.filter(c => c.id !== id)
        setActiveConversationId(remaining.length > 0 ? remaining[0].id : null)
      }
      return true
    }
    return false
  }, [conversations, activeConversationId, deleteSession])

  const updateConversationTitle = useCallback((id: string, title: string) => {
    setConversations(prev => prev.map(c =>
      c.id === id ? { ...c, title, updatedAt: new Date() } : c
    ))
  }, [])

  const loadHistory = useCallback(async (sessionKey: string): Promise<Session | null> => {
    const session = await fetchSession(sessionKey)
    if (session && session.messages.length > 0) {
      // Update title from first user message if available
      const firstUserMsg = session.messages.find(m => m.role === 'user')
      if (firstUserMsg) {
        const chatId = sessionKey.substring(WEB_SESSION_PREFIX_LENGTH)
        const title = extractTitle(firstUserMsg.content)
        updateConversationTitle(chatId, title)
      }
    }
    return session
  }, [fetchSession, updateConversationTitle])

  const markHistoryLoaded = useCallback((key: string) => {
    historyLoadedKeysRef.current.add(key)
  }, [])

  return (
    <ConversationContext.Provider value={{
      conversations,
      activeConversationId,
      isLoading,
      createConversation,
      switchConversation,
      deleteConversation,
      updateConversationTitle,
      refreshConversations,
      getActiveConversation: () => conversations.find(c => c.id === activeConversationId) || null,
      getActiveSessionKey: () => activeConversationId ? `${WEB_SESSION_PREFIX}${activeConversationId}` : '',
      loadHistory,
      historyLoadedKeys: historyLoadedKeysRef.current,
      markHistoryLoaded,
    }}>
      {children}
    </ConversationContext.Provider>
  )
}

export { sessionToChatMessage }