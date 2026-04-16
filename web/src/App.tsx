import { useState, useEffect } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import { MessageSquare, Clock, Terminal, Settings, Menu, X } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import ChatPanel from './components/ChatPanel'
import SessionsPanel from './components/SessionsPanel'
import LogsPanel from './components/LogsPanel'
import './index.css'

type TabType = 'chat' | 'sessions' | 'logs'

interface Tab {
  id: TabType
  label: string
  icon: LucideIcon
}

function App() {
  const [activeTab, setActiveTab] = useState<TabType>('chat')
  const [isMobileMenuOpen, setIsMobileMenuOpen] = useState(false)
  const [wsConnected, setWsConnected] = useState(false)

  const tabs: Tab[] = [
    { id: 'chat', label: 'Chat', icon: MessageSquare },
    { id: 'sessions', label: 'Sessions', icon: Clock },
    { id: 'logs', label: 'Logs', icon: Terminal },
  ]

  // WebSocket connection check
  useEffect(() => {
    const checkConnection = () => {
      // This will be updated when WebSocket is implemented
      setWsConnected(true)
    }
    checkConnection()
  }, [])

  return (
    <div className="min-h-screen relative">
      {/* Fluid Background */}
      <div className="fluid-background">
        <div className="floating-orb orb-1" />
        <div className="floating-orb orb-2" />
        <div className="floating-orb orb-3" />
      </div>

      {/* Header */}
      <header className="sticky top-0 z-50 px-4 py-3">
        <div className="glass-card flex items-center justify-between px-4 py-3 md:px-6">
          {/* Logo */}
          <div className="flex items-center gap-3">
            <div className="w-10 h-10 rounded-xl bg-gradient-to-br from-cyan-electric to-cyan-glow flex items-center justify-center">
              <span className="font-display font-bold text-ocean-deep text-lg">K</span>
            </div>
            <div className="hidden sm:block">
              <h1 className="font-display font-semibold text-foam text-lg">KanFlux</h1>
              <p className="text-xs text-foam/50 font-body">AI Agent Control Panel</p>
            </div>
          </div>

          {/* Desktop Navigation */}
          <nav className="hidden md:flex items-center gap-1">
            {tabs.map((tab) => (
              <motion.button
                key={tab.id}
                onClick={() => setActiveTab(tab.id)}
                className={`nav-tab flex items-center gap-2 font-body ${activeTab === tab.id ? 'active' : ''}`}
                whileHover={{ scale: 1.02 }}
                whileTap={{ scale: 0.98 }}
              >
                <tab.icon size={16} />
                <span>{tab.label}</span>
              </motion.button>
            ))}
          </nav>

          {/* Status & Mobile Menu */}
          <div className="flex items-center gap-3">
            {/* Connection Status */}
            <div className="flex items-center gap-2 px-3 py-1.5 rounded-lg bg-ocean-depth/50">
              <div className={`status-indicator ${wsConnected ? 'status-connected' : 'status-disconnected'}`} />
              <span className="text-xs font-body text-foam/60">
                {wsConnected ? 'Connected' : 'Offline'}
              </span>
            </div>

            {/* Settings Button */}
            <motion.button
              className="btn-glass p-2 rounded-lg hidden md:flex"
              whileHover={{ scale: 1.05 }}
              whileTap={{ scale: 0.95 }}
            >
              <Settings size={18} className="text-cyan-electric" />
            </motion.button>

            {/* Mobile Menu Toggle */}
            <motion.button
              className="btn-glass p-2 rounded-lg md:hidden"
              onClick={() => setIsMobileMenuOpen(!isMobileMenuOpen)}
              whileHover={{ scale: 1.05 }}
              whileTap={{ scale: 0.95 }}
            >
              {isMobileMenuOpen ? (
                <X size={18} className="text-cyan-electric" />
              ) : (
                <Menu size={18} className="text-cyan-electric" />
              )}
            </motion.button>
          </div>
        </div>

        {/* Mobile Menu */}
        <AnimatePresence>
          {isMobileMenuOpen && (
            <motion.div
              initial={{ opacity: 0, y: -10 }}
              animate={{ opacity: 1, y: 0 }}
              exit={{ opacity: 0, y: -10 }}
              className="glass-card mt-2 p-3 md:hidden"
            >
              <nav className="flex flex-col gap-2">
                {tabs.map((tab) => (
                  <motion.button
                    key={tab.id}
                    onClick={() => {
                      setActiveTab(tab.id)
                      setIsMobileMenuOpen(false)
                    }}
                    className={`nav-tab flex items-center gap-2 font-body py-2 ${activeTab === tab.id ? 'active' : ''}`}
                    whileTap={{ scale: 0.98 }}
                  >
                    <tab.icon size={16} />
                    <span>{tab.label}</span>
                  </motion.button>
                ))}
              </nav>
            </motion.div>
          )}
        </AnimatePresence>
      </header>

      {/* Main Content */}
      <main className="px-4 pb-4 pt-2">
        <AnimatePresence mode="wait">
          {activeTab === 'chat' && (
            <motion.div
              key="chat"
              initial={{ opacity: 0, x: -20 }}
              animate={{ opacity: 1, x: 0 }}
              exit={{ opacity: 0, x: 20 }}
              transition={{ duration: 0.3 }}
              className="h-[calc(100vh-120px)]"
            >
              <ChatPanel />
            </motion.div>
          )}

          {activeTab === 'sessions' && (
            <motion.div
              key="sessions"
              initial={{ opacity: 0, x: -20 }}
              animate={{ opacity: 1, x: 0 }}
              exit={{ opacity: 0, x: 20 }}
              transition={{ duration: 0.3 }}
              className="h-[calc(100vh-120px)]"
            >
              <SessionsPanel />
            </motion.div>
          )}

          {activeTab === 'logs' && (
            <motion.div
              key="logs"
              initial={{ opacity: 0, x: -20 }}
              animate={{ opacity: 1, x: 0 }}
              exit={{ opacity: 0, x: 20 }}
              transition={{ duration: 0.3 }}
              className="h-[calc(100vh-120px)]"
            >
              <LogsPanel />
            </motion.div>
          )}
        </AnimatePresence>
      </main>
    </div>
  )
}

export default App