import { useState } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import { Calendar, ChevronRight, ChevronDown, FileText, User, Bot, Search, X, RefreshCw } from 'lucide-react'
import { format, parseISO, startOfDay, endOfDay, isWithinInterval } from 'date-fns'
import type { Session } from '../types'

// Mock session data - in production this would fetch from backend
const mockSessions: Session[] = [
  {
    key: 'web-chat',
    messages: [
      { role: 'user', content: 'Hello, how are you?', timestamp: new Date('2026-04-16T09:00:00') },
      { role: 'assistant', content: 'I am doing well, thank you for asking! How can I help you today?', timestamp: new Date('2026-04-16T09:00:05') },
    ],
    created_at: new Date('2026-04-16T09:00:00'),
    updated_at: new Date('2026-04-16T09:00:05'),
  },
  {
    key: 'cli:main:1713820800',
    messages: [
      { role: 'user', content: 'What files are in the workspace?', timestamp: new Date('2026-04-16T08:30:00') },
      { role: 'assistant', content: 'I found the following files:\n- README.md\n- main.go\n- config.yaml', timestamp: new Date('2026-04-16T08:30:10') },
    ],
    created_at: new Date('2026-04-16T08:30:00'),
    updated_at: new Date('2026-04-16T08:30:10'),
  },
  {
    key: 'wxcom:work:chat001',
    messages: [
      { role: 'user', content: '帮我分析一下这个项目的结构', timestamp: new Date('2026-04-15T14:00:00') },
      { role: 'assistant', content: '这个项目采用了模块化的架构设计...', timestamp: new Date('2026-04-15T14:00:20') },
    ],
    created_at: new Date('2026-04-15T14:00:00'),
    updated_at: new Date('2026-04-15T14:00:20'),
  },
]

export default function SessionsPanel() {
  const [sessions, setSessions] = useState<Session[]>(mockSessions)
  const [selectedSession, setSelectedSession] = useState<Session | null>(null)
  const [expandedSessions, setExpandedSessions] = useState<Set<string>>(new Set())
  const [searchDate, setSearchDate] = useState<string>('')
  const [searchQuery, setSearchQuery] = useState('')
  const [isLoading, setIsLoading] = useState(false)

  // Filter sessions by date
  const filteredSessions = sessions.filter(session => {
    // Date filter
    if (searchDate) {
      const date = parseISO(searchDate)
      const dayStart = startOfDay(date)
      const dayEnd = endOfDay(date)
      if (!isWithinInterval(session.created_at, { start: dayStart, end: dayEnd })) {
        return false
      }
    }

    // Content search
    if (searchQuery) {
      const query = searchQuery.toLowerCase()
      return session.messages.some(msg =>
        msg.content.toLowerCase().includes(query)
      ) || session.key.toLowerCase().includes(query)
    }

    return true
  })

  // Group sessions by date
  const sessionsByDate = filteredSessions.reduce((acc, session) => {
    const dateKey = format(session.created_at, 'yyyy-MM-dd')
    if (!acc[dateKey]) {
      acc[dateKey] = []
    }
    acc[dateKey].push(session)
    return acc
  }, {} as Record<string, Session[]>)

  const handleRefresh = async () => {
    setIsLoading(true)
    // In production, this would fetch from backend
    // For now, we'll just simulate a refresh
    await new Promise(resolve => setTimeout(resolve, 500))
    setSessions([...sessions]) // Trigger re-render
    setIsLoading(false)
  }

  const toggleExpand = (sessionKey: string) => {
    setExpandedSessions(prev => {
      const newSet = new Set(prev)
      if (newSet.has(sessionKey)) {
        newSet.delete(sessionKey)
      } else {
        newSet.add(sessionKey)
      }
      return newSet
    })
  }

  const openSessionDetail = (session: Session) => {
    setSelectedSession(session)
  }

  return (
    <div className="glass-card-solid h-full flex flex-col overflow-hidden">
      {/* Header */}
      <div className="flex items-center justify-between px-4 py-3 border-b border-cyan-electric/10">
        <div className="flex items-center gap-3">
          <Calendar size={18} className="text-cyan-electric" />
          <h2 className="font-display font-semibold text-foam">Session History</h2>
        </div>
        <motion.button
          onClick={handleRefresh}
          className="btn-glass p-2 rounded-lg"
          whileHover={{ scale: 1.05 }}
          whileTap={{ scale: 0.95 }}
          disabled={isLoading}
        >
          <RefreshCw size={16} className={`text-cyan-electric ${isLoading ? 'animate-spin' : ''}`} />
        </motion.button>
      </div>

      {/* Filters */}
      <div className="px-4 py-3 border-b border-cyan-electric/10 space-y-3">
        {/* Date Filter */}
        <div className="flex items-center gap-3">
          <label className="text-xs font-body text-foam/60 shrink-0">Date:</label>
          <input
            type="date"
            value={searchDate}
            onChange={(e) => setSearchDate(e.target.value)}
            className="input-glass text-sm py-2"
          />
          {searchDate && (
            <motion.button
              onClick={() => setSearchDate('')}
              className="p-1"
              whileTap={{ scale: 0.9 }}
            >
              <X size={14} className="text-foam/40 hover:text-foam" />
            </motion.button>
          )}
        </div>

        {/* Search Filter */}
        <div className="flex items-center gap-3">
          <Search size={14} className="text-foam/40" />
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
              <X size={14} className="text-foam/40 hover:text-foam" />
            </motion.button>
          )}
        </div>
      </div>

      {/* Session List */}
      <div className="flex-1 overflow-y-auto px-4 py-3">
        {Object.keys(sessionsByDate).length === 0 ? (
          <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            className="flex flex-col items-center justify-center h-full text-center"
          >
            <FileText size={32} className="text-foam/30 mb-3" />
            <p className="text-sm text-foam/50 font-body">No sessions found</p>
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
                    <div className="w-2 h-2 rounded-full bg-cyan-electric/50" />
                    <span className="text-xs font-mono text-cyan-electric/60">
                      {format(parseISO(date), 'EEEE, MMMM d')}
                    </span>
                    <span className="text-xs text-foam/40 font-body">
                      ({dateSessions.length} sessions)
                    </span>
                  </div>

                  {/* Sessions for this date */}
                  <div className="space-y-2">
                    {dateSessions.map((session) => (
                      <motion.div
                        key={session.key}
                        className="glass-card p-3 cursor-pointer"
                        onClick={() => openSessionDetail(session)}
                        whileHover={{ scale: 1.01 }}
                        whileTap={{ scale: 0.99 }}
                      >
                        {/* Session Header */}
                        <div
                          className="flex items-center justify-between"
                          onClick={(e) => {
                            e.stopPropagation()
                            toggleExpand(session.key)
                          }}
                        >
                          <div className="flex items-center gap-2">
                            {expandedSessions.has(session.key) ? (
                              <ChevronDown size={16} className="text-cyan-electric/60" />
                            ) : (
                              <ChevronRight size={16} className="text-cyan-electric/60" />
                            )}
                            <span className="font-mono text-sm text-foam truncate max-w-[200px]">
                              {session.key}
                            </span>
                          </div>
                          <span className="text-xs text-foam/40 font-body">
                            {format(session.created_at, 'HH:mm')}
                          </span>
                        </div>

                        {/* Preview */}
                        {!expandedSessions.has(session.key) && session.messages.length > 0 && (
                          <p className="text-xs text-foam/50 font-body mt-2 truncate pl-6">
                            {session.messages[session.messages.length - 1]?.content.slice(0, 60)}...
                          </p>
                        )}

                        {/* Expanded Messages */}
                        <AnimatePresence>
                          {expandedSessions.has(session.key) && (
                            <motion.div
                              initial={{ opacity: 0, height: 0 }}
                              animate={{ opacity: 1, height: 'auto' }}
                              exit={{ opacity: 0, height: 0 }}
                              className="mt-3 pl-6 space-y-2 overflow-hidden"
                            >
                              {session.messages.slice(-4).map((msg, idx) => (
                                <div key={idx} className="flex items-start gap-2">
                                  {msg.role === 'user' ? (
                                    <User size={12} className="text-ocean-light shrink-0 mt-0.5" />
                                  ) : (
                                    <Bot size={12} className="text-cyan-electric shrink-0 mt-0.5" />
                                  )}
                                  <p className="text-xs text-foam/70 font-body line-clamp-2">
                                    {msg.content}
                                  </p>
                                </div>
                              ))}
                              {session.messages.length > 4 && (
                                <p className="text-xs text-foam/40 italic">
                                  +{session.messages.length - 4} more messages
                                </p>
                              )}
                            </motion.div>
                          )}
                        </AnimatePresence>
                      </motion.div>
                    ))}
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
            className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-ocean-deep/80 backdrop-blur-sm"
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
              <div className="flex items-center justify-between px-4 py-3 border-b border-cyan-electric/10">
                <div>
                  <h3 className="font-display font-semibold text-foam">Session Detail</h3>
                  <p className="text-xs font-mono text-cyan-electric/60 mt-1">{selectedSession.key}</p>
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
              <div className="px-4 py-3 border-b border-cyan-electric/10 flex items-center gap-4">
                <div className="flex items-center gap-2">
                  <Calendar size={14} className="text-cyan-electric/60" />
                  <span className="text-xs text-foam/60 font-body">
                    Created: {format(selectedSession.created_at, 'PPpp')}
                  </span>
                </div>
                <div className="flex items-center gap-2">
                  <FileText size={14} className="text-cyan-electric/60" />
                  <span className="text-xs text-foam/60 font-body">
                    {selectedSession.messages.length} messages
                  </span>
                </div>
              </div>

              {/* Messages */}
              <div className="flex-1 overflow-y-auto px-4 py-3 space-y-3">
                {selectedSession.messages.map((msg, idx) => (
                  <motion.div
                    key={idx}
                    initial={{ opacity: 0, x: msg.role === 'user' ? 20 : -20 }}
                    animate={{ opacity: 1, x: 0 }}
                    transition={{ delay: idx * 0.05 }}
                    className={`message-bubble ${msg.role === 'user' ? 'message-user ml-auto' : 'message-agent'}`}
                  >
                    <div className="flex items-start gap-2">
                      {msg.role === 'user' ? (
                        <User size={14} className="text-ocean-deep shrink-0" />
                      ) : (
                        <Bot size={14} className="text-cyan-electric shrink-0" />
                      )}
                      <p className="font-body whitespace-pre-wrap">{msg.content}</p>
                    </div>
                  </motion.div>
                ))}
              </div>
            </motion.div>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  )
}