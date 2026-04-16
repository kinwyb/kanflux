import { useState, useEffect } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import { Calendar, ChevronRight, ChevronDown, FileText, User, Bot, Search, X, RefreshCw, Wifi, WifiOff, Wrench } from 'lucide-react'
import { format, parseISO, startOfDay, endOfDay, isWithinInterval } from 'date-fns'
import { useWebSocket } from '../hooks/useWebSocket'
import type { Session, SessionMetaPayload, SessionMessage } from '../types'

export default function SessionsPanel() {
  const { connectionState, fetchSessionList, fetchSession } = useWebSocket()
  const [sessionMetas, setSessionMetas] = useState<SessionMetaPayload[]>([])
  const [sessions, setSessions] = useState<Map<string, Session>>(new Map())
  const [selectedSession, setSelectedSession] = useState<Session | null>(null)
  const [expandedSessions, setExpandedSessions] = useState<Set<string>>(new Set())
  const [searchDate, setSearchDate] = useState<string>('')
  const [searchQuery, setSearchQuery] = useState('')
  const [isLoading, setIsLoading] = useState(false)
  const [loadingSessionKeys, setLoadingSessionKeys] = useState<Set<string>>(new Set())

  // Fetch session list on mount and when connection state changes
  useEffect(() => {
    if (connectionState === 'connected') {
      handleRefresh()
    }
  }, [connectionState])

  // Filter sessions by date
  const filteredMetas = sessionMetas.filter(meta => {
    // Date filter
    if (searchDate) {
      const date = parseISO(searchDate)
      const dayStart = startOfDay(date)
      const dayEnd = endOfDay(date)
      const createdAt = new Date(meta.created_at)
      if (!isWithinInterval(createdAt, { start: dayStart, end: dayEnd })) {
        return false
      }
    }

    // Key search
    if (searchQuery) {
      const query = searchQuery.toLowerCase()
      return meta.key.toLowerCase().includes(query)
    }

    return true
  })

  // Group sessions by date
  const sessionsByDate = filteredMetas.reduce((acc, meta) => {
    const dateKey = format(new Date(meta.created_at), 'yyyy-MM-dd')
    if (!acc[dateKey]) {
      acc[dateKey] = []
    }
    acc[dateKey].push(meta)
    return acc
  }, {} as Record<string, SessionMetaPayload[]>)

  const handleRefresh = async () => {
    setIsLoading(true)
    const metas = await fetchSessionList()
    setSessionMetas(metas)
    setIsLoading(false)
  }

  const handleDateFilterChange = async (newDate: string) => {
    setSearchDate(newDate)
    if (newDate) {
      // Fetch sessions for the specific date
      setIsLoading(true)
      const metas = await fetchSessionList(newDate, newDate)
      setSessionMetas(metas)
      setIsLoading(false)
    } else {
      // Fetch all sessions
      handleRefresh()
    }
  }

  const toggleExpand = async (sessionKey: string) => {
    const newExpanded = new Set(expandedSessions)

    if (newExpanded.has(sessionKey)) {
      newExpanded.delete(sessionKey)
      setExpandedSessions(newExpanded)
    } else {
      // Load session data if not already loaded
      if (!sessions.has(sessionKey)) {
        setLoadingSessionKeys(prev => new Set(prev).add(sessionKey))
        const session = await fetchSession(sessionKey)
        if (session) {
          setSessions(prev => new Map(prev).set(sessionKey, session))
        }
        setLoadingSessionKeys(prev => {
          const newSet = new Set(prev)
          newSet.delete(sessionKey)
          return newSet
        })
      }
      newExpanded.add(sessionKey)
      setExpandedSessions(newExpanded)
    }
  }

  const openSessionDetail = async (meta: SessionMetaPayload) => {
    const sessionKey = meta.key
    // Load full session data
    setLoadingSessionKeys(prev => new Set(prev).add(sessionKey))
    const session = await fetchSession(sessionKey)
    setLoadingSessionKeys(prev => {
      const newSet = new Set(prev)
      newSet.delete(sessionKey)
      return newSet
    })

    if (session) {
      setSelectedSession(session)
    }
  }

  const isConnected = connectionState === 'connected'
  const isLoadingSession = (key: string) => loadingSessionKeys.has(key)

  // 渲染消息角色图标
  const renderRoleIcon = (role: string) => {
    switch (role) {
      case 'user':
        return <User size={12} className="text-ocean-surface shrink-0 mt-0.5" />
      case 'assistant':
        return <Bot size={12} className="text-cyan-electric shrink-0 mt-0.5" />
      case 'tool':
        return <Wrench size={12} className="text-cyan-mist shrink-0 mt-0.5" />
      default:
        return <FileText size={12} className="text-ocean-depth/50 shrink-0 mt-0.5" />
    }
  }

  // 渲染消息角色图标（大尺寸，用于详情弹窗）
  const renderRoleIconLarge = (role: string) => {
    switch (role) {
      case 'user':
        return <User size={14} className="text-white shrink-0" />
      case 'assistant':
        return <Bot size={14} className="text-cyan-electric shrink-0" />
      case 'tool':
        return <Wrench size={14} className="text-cyan-mist shrink-0" />
      default:
        return <FileText size={14} className="text-ocean-depth/50 shrink-0" />
    }
  }

  // 渲染消息内容（包括工具调用和工具结果）
  const renderMessageContent = (msg: SessionMessage, idx: number) => {
    // 工具结果消息
    if (msg.role === 'tool') {
      return (
        <motion.div
          key={idx}
          initial={{ opacity: 0, x: -10 }}
          animate={{ opacity: 1, x: 0 }}
          transition={{ delay: idx * 0.03 }}
          className="bg-green-500/15 border border-green-500/30 rounded-lg p-3"
        >
          <div className="flex items-center gap-2 mb-1">
            <Wrench size={14} className="text-green-600" />
            <span className="font-mono text-xs text-green-600">工具结果</span>
            {msg.name && <span className="font-mono text-xs text-ocean-depth/50">({msg.name})</span>}
          </div>
          <pre className="font-mono text-xs text-ocean-depth whitespace-pre-wrap overflow-x-auto">
            {msg.content || '(无结果)'}
          </pre>
        </motion.div>
      )
    }

    // 工具调用消息（assistant 消息包含 tool_calls）
    if (msg.role === 'assistant' && msg.tool_calls && msg.tool_calls.length > 0) {
      return (
        <motion.div
          key={idx}
          initial={{ opacity: 0, x: -10 }}
          animate={{ opacity: 1, x: 0 }}
          transition={{ delay: idx * 0.03 }}
          className="space-y-2"
        >
          {msg.tool_calls.map((tool, toolIdx) => (
            <div key={toolIdx} className="bg-ocean-depth/10 border border-cyan-electric/20 rounded-lg p-3">
              <div className="flex items-center gap-2 mb-2">
                <Wrench size={14} className="text-cyan-electric" />
                <span className="font-mono text-xs text-cyan-electric font-medium">{tool.function.name}</span>
              </div>
              <div>
                <span className="text-xs text-ocean-depth/50 font-body block mb-1">参数:</span>
                <pre className="font-mono text-xs text-ocean-depth/70 whitespace-pre-wrap overflow-x-auto bg-ocean-depth/5 p-2 rounded">
                  {(() => {
                    try {
                      return JSON.stringify(JSON.parse(tool.function.arguments), null, 2)
                    } catch (_e) {
                      return tool.function.arguments || '(无参数)'
                    }
                  })()}
                </pre>
              </div>
            </div>
          ))}
          {/* 如果还有普通内容 */}
          {msg.content && (
            <div className="message-bubble message-agent">
              <div className="flex items-start gap-2">
                <Bot size={14} className="text-cyan-electric shrink-0" />
                <p className="font-body whitespace-pre-wrap">{msg.content}</p>
              </div>
            </div>
          )}
        </motion.div>
      )
    }

    // 普通消息
    return (
      <motion.div
        key={idx}
        initial={{ opacity: 0, x: msg.role === 'user' ? 20 : -20 }}
        animate={{ opacity: 1, x: 0 }}
        transition={{ delay: idx * 0.03 }}
        className={`message-bubble ${msg.role === 'user' ? 'message-user ml-auto' : 'message-agent'}`}
      >
        <div className="flex items-start gap-2">
          {renderRoleIconLarge(msg.role)}
          <p className="font-body whitespace-pre-wrap">{msg.content}</p>
        </div>
      </motion.div>
    )
  }

  return (
    <div className="glass-card-solid h-full flex flex-col overflow-hidden">
      {/* Header */}
      <div className="flex items-center justify-between px-4 py-3 border-b border-cyan-electric/15">
        <div className="flex items-center gap-3">
          <Calendar size={18} className="text-cyan-electric" />
          <h2 className="font-display font-semibold text-ocean-deep">Session History</h2>
          {isConnected ? (
            <Wifi size={14} className="text-green-500" />
          ) : (
            <WifiOff size={14} className="text-red-500" />
          )}
        </div>
        <motion.button
          onClick={handleRefresh}
          className="btn-glass p-2 rounded-lg"
          whileHover={{ scale: 1.05 }}
          whileTap={{ scale: 0.95 }}
          disabled={isLoading || !isConnected}
        >
          <RefreshCw size={16} className={`text-cyan-electric ${isLoading ? 'animate-spin' : ''}`} />
        </motion.button>
      </div>

      {/* Filters */}
      <div className="px-4 py-3 border-b border-cyan-electric/15 space-y-3">
        {/* Date Filter */}
        <div className="flex items-center gap-3">
          <label className="text-xs font-body text-ocean-depth/60 shrink-0">Date:</label>
          <input
            type="date"
            value={searchDate}
            onChange={(e) => handleDateFilterChange(e.target.value)}
            className="input-glass text-sm py-2"
            disabled={!isConnected}
          />
          {searchDate && (
            <motion.button
              onClick={() => handleDateFilterChange('')}
              className="p-1"
              whileTap={{ scale: 0.9 }}
            >
              <X size={14} className="text-ocean-depth/40 hover:text-ocean-depth" />
            </motion.button>
          )}
        </div>

        {/* Search Filter */}
        <div className="flex items-center gap-3">
          <Search size={14} className="text-ocean-depth/40" />
          <input
            type="text"
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            placeholder="Search sessions..."
            className="input-glass text-sm py-2 flex-1"
          />
          {searchQuery && (
            <motion.button
              onClick={() => setSearchQuery('')}
              className="p-1"
              whileTap={{ scale: 0.9 }}
            >
              <X size={14} className="text-ocean-depth/40 hover:text-ocean-depth" />
            </motion.button>
          )}
        </div>
      </div>

      {/* Connection Status Warning */}
      {!isConnected && (
        <div className="px-4 py-2 bg-yellow-500/10 border-b border-yellow-500/20">
          <p className="text-xs text-yellow-600 font-body flex items-center gap-2">
            <WifiOff size={14} />
            Connecting to gateway...
          </p>
        </div>
      )}

      {/* Session List */}
      <div className="flex-1 overflow-y-auto px-4 py-3">
        {Object.keys(sessionsByDate).length === 0 ? (
          <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            className="flex flex-col items-center justify-center h-full text-center"
          >
            <FileText size={32} className="text-ocean-depth/30 mb-3" />
            <p className="text-sm text-ocean-depth/50 font-body">
              {isConnected ? 'No sessions found' : 'Waiting for connection...'}
            </p>
          </motion.div>
        ) : (
          <div className="space-y-4">
            {Object.entries(sessionsByDate)
              .sort(([a], [b]) => b.localeCompare(a))
              .map(([date, dateSessions]) => (
                <motion.div
                  key={date}
                  initial={{ opacity: 0, y: 10 }}
                  animate={{ opacity: 1, y: 0 }}
                >
                  {/* Date Header */}
                  <div className="flex items-center gap-2 mb-2">
                    <div className="w-2 h-2 rounded-full bg-cyan-electric" />
                    <span className="text-xs font-mono text-cyan-electric">
                      {format(parseISO(date), 'EEEE, MMMM d')}
                    </span>
                    <span className="text-xs text-ocean-depth/50 font-body">
                      ({dateSessions.length} sessions)
                    </span>
                  </div>

                  {/* Sessions for this date */}
                  <div className="space-y-2">
                    {dateSessions.map((meta) => {
                      const session = sessions.get(meta.key)
                      const isExpanded = expandedSessions.has(meta.key)
                      const isLoadingThis = isLoadingSession(meta.key)

                      return (
                        <motion.div
                          key={meta.key}
                          className="glass-card p-3 cursor-pointer"
                          onClick={() => openSessionDetail(meta)}
                          whileHover={{ scale: 1.01 }}
                          whileTap={{ scale: 0.99 }}
                        >
                          {/* Session Header */}
                          <div
                            className="flex items-center justify-between"
                            onClick={(e) => {
                              e.stopPropagation()
                              toggleExpand(meta.key)
                            }}
                          >
                            <div className="flex items-center gap-2">
                              {isLoadingThis ? (
                                <RefreshCw size={16} className="text-cyan-electric animate-spin" />
                              ) : isExpanded ? (
                                <ChevronDown size={16} className="text-cyan-electric" />
                              ) : (
                                <ChevronRight size={16} className="text-cyan-electric" />
                              )}
                              <span className="font-mono text-sm text-ocean-deep truncate max-w-[200px]">
                                {meta.key}
                              </span>
                            </div>
                            <div className="flex items-center gap-2">
                              <span className="text-xs text-ocean-depth/50 font-body">
                                {meta.message_count} msgs
                              </span>
                              <span className="text-xs text-ocean-depth/50 font-body">
                                {format(new Date(meta.created_at), 'HH:mm')}
                              </span>
                            </div>
                          </div>

                          {/* Expanded Messages */}
                          <AnimatePresence>
                            {isExpanded && session && (
                              <motion.div
                                initial={{ opacity: 0, height: 0 }}
                                animate={{ opacity: 1, height: 'auto' }}
                                exit={{ opacity: 0, height: 0 }}
                                className="mt-3 pl-6 space-y-2 overflow-hidden"
                              >
                                {session.messages.slice(-4).map((msg, idx) => {
                                  // 工具调用消息
                                  if (msg.role === 'assistant' && msg.tool_calls && msg.tool_calls.length > 0) {
                                    return msg.tool_calls.map((tool, toolIdx) => (
                                      <div key={`${idx}-${toolIdx}`} className="flex items-start gap-2">
                                        <Wrench size={12} className="text-cyan-electric shrink-0 mt-0.5" />
                                        <div className="flex-1 min-w-0">
                                          <span className="text-xs font-mono text-cyan-electric font-medium">{tool.function.name}</span>
                                          <p className="text-xs text-ocean-depth/50 font-mono line-clamp-1">
                                            {tool.function.arguments}
                                          </p>
                                        </div>
                                      </div>
                                    ))
                                  }
                                  // 工具结果消息
                                  if (msg.role === 'tool') {
                                    return (
                                      <div key={idx} className="flex items-start gap-2">
                                        <Wrench size={12} className="text-green-500 shrink-0 mt-0.5" />
                                        <p className="text-xs text-ocean-depth/70 font-body line-clamp-2">
                                          {msg.content || '(无结果)'}
                                        </p>
                                      </div>
                                    )
                                  }
                                  // 普通消息
                                  return (
                                    <div key={idx} className="flex items-start gap-2">
                                      {renderRoleIcon(msg.role)}
                                      <p className="text-xs text-ocean-depth/70 font-body line-clamp-2">
                                        {msg.content}
                                      </p>
                                    </div>
                                  )
                                })}
                                {session.messages.length > 4 && (
                                  <p className="text-xs text-ocean-depth/40 italic">
                                    +{session.messages.length - 4} more messages
                                  </p>
                                )}
                              </motion.div>
                            )}
                          </AnimatePresence>
                        </motion.div>
                      )
                    })}
                  </div>
                </motion.div>
              ))}
          </div>
        )}
      </div>

      {/* Session Detail Modal */}
      <AnimatePresence>
        {selectedSession && (
          <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-ocean-depth/20 backdrop-blur-sm"
            onClick={() => setSelectedSession(null)}
          >
            <motion.div
              initial={{ scale: 0.9, opacity: 0 }}
              animate={{ scale: 1, opacity: 1 }}
              exit={{ scale: 0.9, opacity: 0 }}
              className="glass-card-solid max-w-2xl w-full max-h-[80vh] flex flex-col overflow-hidden"
              onClick={(e) => e.stopPropagation()}
            >
              {/* Modal Header */}
              <div className="flex items-center justify-between px-4 py-3 border-b border-cyan-electric/15">
                <div>
                  <h3 className="font-display font-semibold text-ocean-deep">Session Detail</h3>
                  <p className="text-xs font-mono text-cyan-electric mt-1">{selectedSession.key}</p>
                </div>
                <motion.button
                  onClick={() => setSelectedSession(null)}
                  className="btn-glass p-2 rounded-lg"
                  whileHover={{ scale: 1.05 }}
                  whileTap={{ scale: 0.95 }}
                >
                  <X size={16} className="text-cyan-electric" />
                </motion.button>
              </div>

              {/* Session Meta */}
              <div className="px-4 py-3 border-b border-cyan-electric/15 flex items-center gap-4">
                <div className="flex items-center gap-2">
                  <Calendar size={14} className="text-cyan-electric/70" />
                  <span className="text-xs text-ocean-depth/60 font-body">
                    Created: {format(selectedSession.created_at, 'PPpp')}
                  </span>
                </div>
                <div className="flex items-center gap-2">
                  <FileText size={14} className="text-cyan-electric/70" />
                  <span className="text-xs text-ocean-depth/60 font-body">
                    {selectedSession.messages.length} messages
                  </span>
                </div>
              </div>

              {/* Messages */}
              <div className="flex-1 overflow-y-auto px-4 py-3 space-y-3">
                {selectedSession.messages.map((msg, idx) => renderMessageContent(msg, idx))}
              </div>
            </motion.div>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  )
}