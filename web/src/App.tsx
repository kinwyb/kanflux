import { useState, useEffect } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import { MessageSquare, Clock, Terminal, Settings, ChevronLeft, ChevronRight, Calendar } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import ChatPanel from './components/ChatPanel'
import SessionsPanel from './components/SessionsPanel'
import LogsPanel from './components/LogsPanel'
import TasksPanel from './components/TasksPanel'
import './index.css'

type TabType = 'chat' | 'sessions' | 'tasks' | 'logs'

interface Tab {
  id: TabType
  label: string
  icon: LucideIcon
}

function App() {
  const [activeTab, setActiveTab] = useState<TabType>('chat')
  const [sidebarCollapsed, setSidebarCollapsed] = useState(false)
  const [wsConnected, setWsConnected] = useState(false)

  const tabs: Tab[] = [
    { id: 'chat', label: 'Chat', icon: MessageSquare },
    { id: 'sessions', label: 'Sessions', icon: Clock },
    { id: 'tasks', label: 'Tasks', icon: Calendar },
    { id: 'logs', label: 'Logs', icon: Terminal },
  ]

  // WebSocket connection check
  useEffect(() => {
    const checkConnection = () => {
      setWsConnected(true)
    }
    checkConnection()
  }, [])

  const sidebarWidth = sidebarCollapsed ? 'w-16' : 'w-64'

  return (
    <div className="min-h-screen relative flex">
      {/* Fluid Background */}
      <div className="fluid-background">
        <div className="floating-orb orb-1" />
        <div className="floating-orb orb-2" />
        <div className="floating-orb orb-3" />
      </div>

      {/* Sidebar */}
      <motion.aside
        initial={false}
        animate={{ width: sidebarCollapsed ? 64 : 256 }}
        transition={{ duration: 0.3, ease: 'easeInOut' }}
        className={`fixed left-0 top-0 bottom-0 z-40 ${sidebarWidth}`}
      >
        <div className="sidebar-card h-full flex flex-col m-3 overflow-hidden">
          {/* Logo Section */}
          <div className="flex items-center justify-between px-4 py-4 border-b border-cyan-electric/10">
            <AnimatePresence mode="wait">
              {!sidebarCollapsed && (
                <motion.div
                  key="logo-full"
                  initial={{ opacity: 0 }}
                  animate={{ opacity: 1 }}
                  exit={{ opacity: 0 }}
                  className="flex items-center gap-3"
                >
                  <div className="w-10 h-10 rounded-xl bg-gradient-to-br from-cyan-electric to-cyan-glow flex items-center justify-center shadow-lg">
                    <span className="font-display font-bold text-white text-lg">K</span>
                  </div>
                  <div>
                    <h1 className="font-display font-semibold text-ocean-deep text-lg">KanFlux</h1>
                    <p className="text-xs text-ocean-depth/60 font-body">AI Agent Panel</p>
                  </div>
                </motion.div>
              )}
            </AnimatePresence>

            {sidebarCollapsed && (
              <motion.div
                key="logo-mini"
                initial={{ opacity: 0 }}
                animate={{ opacity: 1 }}
                className="w-10 h-10 rounded-xl bg-gradient-to-br from-cyan-electric to-cyan-glow flex items-center justify-center shadow-lg mx-auto"
              >
                <span className="font-display font-bold text-white text-lg">K</span>
              </motion.div>
            )}

            {/* Collapse Toggle Button */}
            <motion.button
              onClick={() => setSidebarCollapsed(!sidebarCollapsed)}
              className="btn-glass p-2 rounded-lg mt-2 hidden md:flex"
              whileHover={{ scale: 1.05 }}
              whileTap={{ scale: 0.95 }}
            >
              {sidebarCollapsed ? (
                <ChevronRight size={16} className="text-cyan-electric" />
              ) : (
                <ChevronLeft size={16} className="text-cyan-electric" />
              )}
            </motion.button>
          </div>

          {/* Navigation */}
          <nav className="flex-1 px-3 py-4 space-y-2">
            {tabs.map((tab) => (
              <motion.button
                key={tab.id}
                onClick={() => setActiveTab(tab.id)}
                className={`sidebar-nav-tab w-full ${activeTab === tab.id ? 'active' : ''}`}
                whileHover={{ scale: sidebarCollapsed ? 1.05 : 1.02 }}
                whileTap={{ scale: 0.98 }}
              >
                <tab.icon size={20} />
                <AnimatePresence mode="wait">
                  {!sidebarCollapsed && (
                    <motion.span
                      key="label"
                      initial={{ opacity: 0, x: -10 }}
                      animate={{ opacity: 1, x: 0 }}
                      exit={{ opacity: 0, x: -10 }}
                      transition={{ duration: 0.2 }}
                      className="font-body"
                    >
                      {tab.label}
                    </motion.span>
                  )}
                </AnimatePresence>
              </motion.button>
            ))}
          </nav>

          {/* Bottom Section */}
          <div className="px-3 py-4 border-t border-cyan-electric/10 space-y-3">
            {/* Connection Status */}
            <div className="flex items-center justify-center gap-2 px-3 py-2 rounded-lg bg-ocean-depth/10">
              <div className={`status-indicator ${wsConnected ? 'status-connected' : 'status-disconnected'}`} />
              <AnimatePresence mode="wait">
                {!sidebarCollapsed && (
                  <motion.span
                    key="status-text"
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    className="text-xs font-body text-ocean-depth/70"
                  >
                    {wsConnected ? 'Connected' : 'Offline'}
                  </motion.span>
                )}
              </AnimatePresence>
            </div>

            {/* Settings Button */}
            <motion.button
              className="sidebar-nav-tab w-full justify-center"
              whileHover={{ scale: sidebarCollapsed ? 1.05 : 1.02 }}
              whileTap={{ scale: 0.98 }}
            >
              <Settings size={20} className="text-ocean-depth/60" />
              <AnimatePresence mode="wait">
                {!sidebarCollapsed && (
                  <motion.span
                    key="settings-label"
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    className="font-body text-ocean-depth/60"
                  >
                    Settings
                  </motion.span>
                )}
              </AnimatePresence>
            </motion.button>
          </div>
        </div>
      </motion.aside>

      {/* Main Content */}
      <motion.main
        initial={false}
        animate={{ marginLeft: sidebarCollapsed ? 64 : 256 }}
        transition={{ duration: 0.3, ease: 'easeInOut' }}
        className="flex-1 px-4 py-4"
      >
        <AnimatePresence mode="wait">
          {activeTab === 'chat' && (
            <motion.div
              key="chat"
              initial={{ opacity: 0, y: 10 }}
              animate={{ opacity: 1, y: 0 }}
              exit={{ opacity: 0, y: -10 }}
              transition={{ duration: 0.25 }}
              className="h-[calc(100vh-32px)]"
            >
              <ChatPanel />
            </motion.div>
          )}

          {activeTab === 'sessions' && (
            <motion.div
              key="sessions"
              initial={{ opacity: 0, y: 10 }}
              animate={{ opacity: 1, y: 0 }}
              exit={{ opacity: 0, y: -10 }}
              transition={{ duration: 0.25 }}
              className="h-[calc(100vh-32px)]"
            >
              <SessionsPanel />
            </motion.div>
          )}

          {activeTab === 'tasks' && (
            <motion.div
              key="tasks"
              initial={{ opacity: 0, y: 10 }}
              animate={{ opacity: 1, y: 0 }}
              exit={{ opacity: 0, y: -10 }}
              transition={{ duration: 0.25 }}
              className="h-[calc(100vh-32px)]"
            >
              <TasksPanel />
            </motion.div>
          )}

          {activeTab === 'logs' && (
            <motion.div
              key="logs"
              initial={{ opacity: 0, y: 10 }}
              animate={{ opacity: 1, y: 0 }}
              exit={{ opacity: 0, y: -10 }}
              transition={{ duration: 0.25 }}
              className="h-[calc(100vh-32px)]"
            >
              <LogsPanel />
            </motion.div>
          )}
        </AnimatePresence>
      </motion.main>
    </div>
  )
}

export default App