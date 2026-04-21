import { useState } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import { Plus, MessageSquare, Trash2, Loader2 } from 'lucide-react'
import { useConversationContext } from '../contexts/ConversationContext'
import { format } from 'date-fns'

export default function ConversationList() {
  const {
    conversations,
    activeConversationId,
    isLoading,
    createConversation,
    switchConversation,
    deleteConversation
  } = useConversationContext()

  const [deletingId, setDeletingId] = useState<string | null>(null)
  const [hoveredId, setHoveredId] = useState<string | null>(null)

  const handleDelete = async (id: string) => {
    setDeletingId(id)
    try {
      await deleteConversation(id)
    } finally {
      setDeletingId(null)
    }
  }

  return (
    <div className="flex flex-col mt-4 border-t border-cyan-electric/10 pt-3">
      {/* Section Header */}
      <div className="px-3 mb-2">
        <span className="text-xs font-body text-ocean-depth/50 uppercase tracking-wider">Conversations</span>
      </div>

      {/* New Chat Button */}
      <motion.button
        onClick={() => createConversation()}
        className="sidebar-nav-tab mx-1 mb-2 flex items-center gap-2 justify-center"
        whileHover={{ scale: 1.02 }}
        whileTap={{ scale: 0.98 }}
        disabled={isLoading}
      >
        <Plus size={16} className="text-cyan-electric" />
        <span className="font-body text-sm">New Chat</span>
      </motion.button>

      {/* Conversation List */}
      <div className="flex-1 overflow-y-auto space-y-1 px-1">
        <AnimatePresence>
          {conversations.map((conv) => (
            <motion.div
              key={conv.id}
              className={`sidebar-nav-tab cursor-pointer flex items-center gap-2 ${
                conv.id === activeConversationId ? 'active' : ''
              }`}
              onClick={() => switchConversation(conv.id)}
              onMouseEnter={() => setHoveredId(conv.id)}
              onMouseLeave={() => setHoveredId(null)}
              initial={{ opacity: 0, x: -10 }}
              animate={{ opacity: 1, x: 0 }}
              exit={{ opacity: 0, x: -10 }}
              whileHover={{ scale: 1.01 }}
              whileTap={{ scale: 0.99 }}
            >
              {/* Icon */}
              <MessageSquare size={16} className={conv.id === activeConversationId ? 'text-cyan-electric' : 'text-ocean-depth/60'} />

              {/* Title and Time */}
              <div className="flex-1 min-w-0">
                <p className="text-sm truncate">{conv.title}</p>
                <p className="text-xs text-ocean-depth/50">{format(conv.updatedAt, 'MM/dd HH:mm')}</p>
              </div>

              {/* Delete Button */}
              <AnimatePresence>
                {(hoveredId === conv.id || deletingId === conv.id) && (
                  <motion.button
                    initial={{ opacity: 0, scale: 0.8 }}
                    animate={{ opacity: 1, scale: 1 }}
                    exit={{ opacity: 0, scale: 0.8 }}
                    onClick={(e) => {
                      e.stopPropagation()
                      handleDelete(conv.id)
                    }}
                    className="p-1 rounded hover:bg-red-500/20"
                    disabled={deletingId === conv.id}
                  >
                    {deletingId === conv.id ? (
                      <Loader2 size={14} className="text-red-500 animate-spin" />
                    ) : (
                      <Trash2 size={14} className="text-ocean-depth/40 hover:text-red-500" />
                    )}
                  </motion.button>
                )}
              </AnimatePresence>
            </motion.div>
          ))}
        </AnimatePresence>
      </div>

      {/* Empty State */}
      {conversations.length === 0 && !isLoading && (
        <div className="flex flex-col items-center justify-center py-4 text-center px-3">
          <MessageSquare size={24} className="text-ocean-depth/30 mb-2" />
          <p className="text-sm text-ocean-depth/50">No conversations</p>
          <p className="text-xs text-ocean-depth/40">Click "New Chat" to start</p>
        </div>
      )}
    </div>
  )
}