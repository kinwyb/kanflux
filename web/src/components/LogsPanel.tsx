import { useState, useEffect, useRef } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import { Terminal, Filter, Trash2, Pause, Play, Download, Clock } from 'lucide-react'
import { format } from 'date-fns'
import { useWebSocket } from '../hooks/useWebSocket'
import type { LogEvent } from '../types'

// Log level colors and labels
const logLevelConfig = {
  debug: { color: 'text-foam/50', bg: 'bg-ocean-depth/30', label: 'DEBUG' },
  info: { color: 'text-cyan-electric', bg: 'bg-cyan-electric/10', label: 'INFO' },
  warn: { color: 'text-yellow-400', bg: 'bg-yellow-400/10', label: 'WARN' },
  error: { color: 'text-red-400', bg: 'bg-red-400/10', label: 'ERROR' },
}

export default function LogsPanel() {
  const { logs, clearLogs } = useWebSocket()
  const [displayLogs, setDisplayLogs] = useState<LogEvent[]>([])
  const [isPaused, setIsPaused] = useState(false)
  const [levelFilter, setLevelFilter] = useState<string>('all')
  const [searchFilter, setSearchFilter] = useState('')
  const logsEndRef = useRef<HTMLDivElement>(null)

  // Auto-scroll to bottom
  useEffect(() => {
    if (!isPaused) {
      logsEndRef.current?.scrollIntoView({ behavior: 'smooth' })
    }
  }, [displayLogs, isPaused])

  // Update display logs when not paused
  useEffect(() => {
    if (!isPaused) {
      let filtered = logs

      // Level filter
      if (levelFilter !== 'all') {
        filtered = filtered.filter(log => log.level === levelFilter)
      }

      // Search filter
      if (searchFilter) {
        const query = searchFilter.toLowerCase()
        filtered = filtered.filter(log =>
          log.message.toLowerCase().includes(query) ||
          log.source.toLowerCase().includes(query)
        )
      }

      setDisplayLogs(filtered.slice(-500)) // Keep last 500 logs
    }
  }, [logs, isPaused, levelFilter, searchFilter])

  // Mock logs for demo when no real logs
  useEffect(() => {
    if (logs.length === 0 && displayLogs.length === 0) {
      const mockLogs: LogEvent[] = [
        { id: '1', level: 'info', message: 'WebSocket server started on port 8765', timestamp: new Date(), source: 'gateway' },
        { id: '2', level: 'info', message: 'AgentManager initialized', timestamp: new Date(), source: 'agent' },
        { id: '3', level: 'debug', message: 'Loading configuration from kanflux.json', timestamp: new Date(), source: 'config' },
        { id: '4', level: 'info', message: 'ChannelManager initialized with 2 channels', timestamp: new Date(), source: 'channel' },
        { id: '5', level: 'info', message: 'WxCom channel registered', timestamp: new Date(), source: 'wxcom' },
        { id: '6', level: 'debug', message: 'SessionManager cache size: 5', timestamp: new Date(), source: 'session' },
        { id: '7', level: 'warn', message: 'API rate limit approaching (85% used)', timestamp: new Date(), source: 'provider' },
        { id: '8', level: 'info', message: 'Incoming message from web channel', timestamp: new Date(), source: 'bus' },
        { id: '9', level: 'debug', message: 'Routing message to agent: main', timestamp: new Date(), source: 'router' },
        { id: '10', level: 'info', message: 'Agent processing started', timestamp: new Date(), source: 'agent' },
      ]
      setDisplayLogs(mockLogs)
    }
  }, [])

  const handleDownload = () => {
    const content = displayLogs.map(log =>
      `[${format(log.timestamp, 'HH:mm:ss')}] [${log.level.toUpperCase()}] [${log.source}] ${log.message}`
    ).join('\n')
    const blob = new Blob([content], { type: 'text/plain' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `kanflux-logs-${format(new Date(), 'yyyyMMdd-HHmmss')}.txt`
    a.click()
    URL.revokeObjectURL(url)
  }

  return (
    <div className="glass-card-solid h-full flex flex-col overflow-hidden">
      {/* Header */}
      <div className="flex items-center justify-between px-4 py-3 border-b border-cyan-electric/10">
        <div className="flex items-center gap-3">
          <Terminal size={18} className="text-cyan-electric" />
          <h2 className="font-display font-semibold text-foam">System Logs</h2>
          <span className="text-xs font-mono text-foam/40 px-2 py-0.5 rounded bg-ocean-depth/50">
            {displayLogs.length} entries
          </span>
        </div>

        <div className="flex items-center gap-2">
          {/* Pause/Play */}
          <motion.button
            onClick={() => setIsPaused(!isPaused)}
            className={`btn-glass p-2 rounded-lg ${isPaused ? 'bg-cyan-electric/20' : ''}`}
            whileHover={{ scale: 1.05 }}
            whileTap={{ scale: 0.95 }}
          >
            {isPaused ? (
              <Play size={16} className="text-cyan-electric" />
            ) : (
              <Pause size={16} className="text-cyan-electric" />
            )}
          </motion.button>

          {/* Clear */}
          <motion.button
            onClick={() => {
              clearLogs()
              setDisplayLogs([])
            }}
            className="btn-glass p-2 rounded-lg"
            whileHover={{ scale: 1.05 }}
            whileTap={{ scale: 0.95 }}
          >
            <Trash2 size={16} className="text-cyan-electric" />
          </motion.button>

          {/* Download */}
          <motion.button
            onClick={handleDownload}
            className="btn-glass p-2 rounded-lg"
            whileHover={{ scale: 1.05 }}
            whileTap={{ scale: 0.95 }}
          >
            <Download size={16} className="text-cyan-electric" />
          </motion.button>
        </div>
      </div>

      {/* Filters */}
      <div className="px-4 py-2 border-b border-cyan-electric/10 flex items-center gap-4 flex-wrap">
        {/* Level Filter */}
        <div className="flex items-center gap-2">
          <Filter size={14} className="text-foam/40" />
          <div className="flex gap-1">
            {['all', 'debug', 'info', 'warn', 'error'].map((level) => (
              <motion.button
                key={level}
                onClick={() => setLevelFilter(level)}
                className={`px-2 py-1 rounded text-xs font-mono transition-colors ${
                  levelFilter === level
                    ? 'bg-cyan-electric/20 text-cyan-electric'
                    : 'text-foam/40 hover:text-foam/60 hover:bg-ocean-depth/30'
                }`}
                whileTap={{ scale: 0.95 }}
              >
                {level.toUpperCase()}
              </motion.button>
            ))}
          </div>
        </div>

        {/* Search */}
        <input
          type="text"
          value={searchFilter}
          onChange={(e) => setSearchFilter(e.target.value)}
          placeholder="Filter logs..."
          className="input-glass text-sm py-1.5 flex-1 min-w-[150px]"
        />
      </div>

      {/* Log Terminal */}
      <div className="flex-1 overflow-y-auto font-mono text-sm bg-ocean-deep/50">
        <AnimatePresence initial={false}>
          {displayLogs.map((log) => (
            <motion.div
              key={log.id}
              initial={{ opacity: 0, y: -5 }}
              animate={{ opacity: 1, y: 0 }}
              className="log-entry"
            >
              {/* Time */}
              <span className="log-time">
                <Clock size={12} className="inline mr-1 opacity-50" />
                {format(log.timestamp, 'HH:mm:ss')}
              </span>

              {/* Level */}
              <span className={`log-level log-level-${log.level} px-1.5 py-0.5 rounded ${
                logLevelConfig[log.level].bg
              }`}>
                {logLevelConfig[log.level].label}
              </span>

              {/* Source */}
              <span className="text-cyan-mist/60 min-w-[80px]">
                [{log.source}]
              </span>

              {/* Message */}
              <span className="log-message flex-1">
                {log.message}
              </span>
            </motion.div>
          ))}
        </AnimatePresence>

        {/* Paused Indicator */}
        {isPaused && (
          <div className="sticky bottom-0 left-0 right-0 p-2 bg-cyan-electric/10 border-t border-cyan-electric/20 flex items-center justify-center gap-2">
            <Pause size={14} className="text-cyan-electric" />
            <span className="text-xs font-mono text-cyan-electric">Log stream paused</span>
          </div>
        )}

        <div ref={logsEndRef} />
      </div>
    </div>
  )
}