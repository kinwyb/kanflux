import { useState } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import { MessageSquare, Clock, Terminal, Settings, Calendar } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import ChatPanel from './components/ChatPanel'
import SessionsPanel from './components/SessionsPanel'
import LogsPanel from './components/LogsPanel'
import TasksPanel from './components/TasksPanel'
import SettingsPanel from './components/SettingsPanel'
import { WebSocketContext } from './contexts/WebSocketContext'
import { useWebSocket } from './hooks/useWebSocket'
import './index.css'

type TabType = 'chat' | 'sessions' | 'tasks' | 'logs'

interface Tab {
  id: TabType
  label: string
  icon: LucideIcon
}

function App() {
  const [activeTab, setActiveTab] = useState<TabType>('chat')
  const [showSettings, setShowSettings] = useState(false)

  // 单一 WebSocket 连接在 App 层级管理
  const ws = useWebSocket()
  const wsConnected = ws.connectionState === 'connected'

  const tabs: Tab[] = [
    { id: 'chat', label: 'Chat', icon: MessageSquare },
    { id: 'sessions', label: 'Sessions', icon: Clock },
    { id: 'tasks', label: 'Tasks', icon: Calendar },
    { id: 'logs', label: 'Logs', icon: Terminal },
  ]

  return (
    <WebSocketContext.Provider value={ws}>
      <div className="min-h-screen relative flex">
        {/* Fluid Background */}
        <div className="fluid-background">
          <div className="floating-orb orb-1" />
          <div className="floating-orb orb-2" />
          <div className="floating-orb orb-3" />
        </div>

        {/* Sidebar - Fixed width */}
        <aside className="fixed left-0 top-0 bottom-0 z-40 w-64">
          <div className="sidebar-card h-full flex flex-col m-3 overflow-hidden">
            {/* Logo Section */}
            <div className="flex items-center gap-3 px-4 py-4 border-b border-cyan-electric/10">
              <div className="w-10 h-10 rounded-xl bg-gradient-to-br from-cyan-electric to-cyan-glow flex items-center justify-center shadow-lg">
                <span className="font-display font-bold text-white text-lg">K</span>
              </div>
              <div>
                <h1 className="font-display font-semibold text-ocean-deep text-lg">KanFlux</h1>
                <p className="text-xs text-ocean-depth/60 font-body">AI Agent Panel</p>
              </div>
            </div>

            {/* Navigation */}
            <nav className="flex-1 px-3 py-4 space-y-2">
              {tabs.map((tab) => (
                <motion.button
                  key={tab.id}
                  onClick={() => setActiveTab(tab.id)}
                  className={`sidebar-nav-tab w-full ${activeTab === tab.id ? 'active' : ''}`}
                  whileHover={{ scale: 1.02 }}
                  whileTap={{ scale: 0.98 }}
                >
                  <tab.icon size={20} />
                  <span className="font-body">{tab.label}</span>
                </motion.button>
              ))}
            </nav>

            {/* Bottom Section */}
            <div className="px-3 py-4 border-t border-cyan-electric/10 space-y-3">
              {/* Connection Status */}
              <div className="flex items-center justify-center gap-2 px-3 py-2 rounded-lg bg-ocean-depth/10">
                <div className={`status-indicator ${wsConnected ? 'status-connected' : 'status-disconnected'}`} />
                <span className="text-xs font-body text-ocean-depth/70">
                  {wsConnected ? 'Connected' : 'Offline'}
                </span>
              </div>

              {/* Settings Button */}
              <motion.button
                className="sidebar-nav-tab w-full"
                onClick={() => setShowSettings(true)}
                whileHover={{ scale: 1.02 }}
                whileTap={{ scale: 0.98 }}
              >
                <Settings size={20} className="text-ocean-depth/60" />
                <span className="font-body text-ocean-depth/60">Settings</span>
              </motion.button>
            </div>
          </div>
        </aside>

        {/* Main Content - Fixed margin */}
        <main className="flex-1 ml-64 px-4 py-4">
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
        </main>

        {/* Settings Modal */}
        <SettingsPanel isOpen={showSettings} onClose={() => setShowSettings(false)} />
      </div>
    </WebSocketContext.Provider>
  )
}

export default App