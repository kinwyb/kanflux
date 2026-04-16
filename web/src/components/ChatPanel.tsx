import { useState, useEffect, useRef } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import { Send, Bot, User, Sparkles, Loader2, Wrench } from 'lucide-react'
import { useWebSocket } from '../hooks/useWebSocket'
import type { ChatMessage, InboundMessage, ToolCall } from '../types'
import { format } from 'date-fns'

export default function ChatPanel() {
  const { connectionState, messages, events, sendMessage } = useWebSocket()
  const [inputValue, setInputValue] = useState('')
  const [chatMessages, setChatMessages] = useState<ChatMessage[]>([])
  const [isAgentThinking, setIsAgentThinking] = useState(false)
  const [_currentStreamingId, setCurrentStreamingId] = useState<string | null>(null)
  const [currentToolCalls, setCurrentToolCalls] = useState<ToolCall[]>([])
  const messagesEndRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLInputElement>(null)

  const scrollToBottom = () => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }

  useEffect(() => {
    scrollToBottom()
  }, [chatMessages, messages])

  // Process WebSocket messages into chat messages
  useEffect(() => {
    // Group messages by chat_id and process streaming content
    const chatId = 'web-chat' // Our chat ID

    const relevantMessages = messages.filter(m => m.chat_id === chatId)
    const relevantEvents = events.filter(e => e.chat_id === chatId)

    // Handle events
    for (const event of relevantEvents) {
      if (event.state === 'start') {
        setIsAgentThinking(true)
        const newMsg: ChatMessage = {
          id: event.run_id,
          role: 'assistant',
          content: '',
          timestamp: new Date(),
          isStreaming: true,
        }
        setChatMessages(prev => [...prev, newMsg])
        setCurrentStreamingId(event.run_id)
      } else if (event.state === 'tool' && event.tool_info) {
        if (event.tool_info.is_start) {
          setCurrentToolCalls(prev => [...prev, {
            id: event.tool_info!.id,
            type: 'function',
            function: {
              name: event.tool_info!.name,
              arguments: event.tool_info!.arguments || '',
            }
          }])
        } else {
          setCurrentToolCalls(prev => prev.filter(t => t.id !== event.tool_info!.id))
        }
      } else if (event.state === 'complete' || event.state === 'error') {
        setIsAgentThinking(false)
        setCurrentStreamingId(null)
        // Mark message as complete
        setChatMessages(prev => prev.map(msg =>
          msg.id === event.run_id
            ? { ...msg, isStreaming: false }
            : msg
        ))
      }
    }

    // Handle content messages
    for (const msg of relevantMessages) {
      if (msg.is_streaming && !msg.is_final) {
        // Update streaming content
        setChatMessages(prev => prev.map(chatMsg => {
          if (chatMsg.isStreaming) {
            return {
              ...chatMsg,
              content: msg.content, // For accumulate mode, this is the full content
              reasoning: msg.reasoning_content || chatMsg.reasoning,
            }
          }
          return chatMsg
        }))
      } else if (msg.is_final) {
        // Final message
        setChatMessages(prev => prev.map(chatMsg =>
          chatMsg.isStreaming
            ? { ...chatMsg, content: msg.content, isStreaming: false }
            : chatMsg
        ))
        setIsAgentThinking(false)
        setCurrentStreamingId(null)
      }
    }
  }, [messages, events])

  const handleSendMessage = () => {
    if (!inputValue.trim() || connectionState !== 'connected') return

    const userMessage: ChatMessage = {
      id: `user-${Date.now()}`,
      role: 'user',
      content: inputValue.trim(),
      timestamp: new Date(),
    }

    setChatMessages(prev => [...prev, userMessage])

    // Send to WebSocket
    const inbound: InboundMessage = {
      id: `msg-${Date.now()}`,
      channel: 'web',
      account_id: '',
      sender_id: 'web-user',
      chat_id: 'web-chat',
      content: inputValue.trim(),
      streaming_mode: 'accumulate',
      timestamp: new Date(),
    }

    sendMessage(inbound)
    setInputValue('')
    inputRef.current?.focus()
  }

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      handleSendMessage()
    }
  }

  return (
    <div className="glass-card-solid h-full flex flex-col overflow-hidden">
      {/* Chat Header */}
      <div className="flex items-center justify-between px-4 py-3 border-b border-cyan-electric/10">
        <div className="flex items-center gap-3">
          <div className="w-8 h-8 rounded-lg bg-gradient-to-br from-cyan-electric to-cyan-glow flex items-center justify-center">
            <Bot size={16} className="text-ocean-deep" />
          </div>
          <div>
            <h2 className="font-display font-semibold text-foam">AI Agent</h2>
            <p className="text-xs text-foam/50 font-body">
              {connectionState === 'connected' ? 'Ready to assist' : 'Connecting...'}
            </p>
          </div>
        </div>

        {/* Thinking Indicator */}
        <AnimatePresence>
          {isAgentThinking && (
            <motion.div
              initial={{ opacity: 0, scale: 0.9 }}
              animate={{ opacity: 1, scale: 1 }}
              exit={{ opacity: 0, scale: 0.9 }}
              className="flex items-center gap-2 px-3 py-1.5 rounded-lg bg-cyan-electric/10"
            >
              <Sparkles size={14} className="text-cyan-electric animate-pulse" />
              <span className="text-xs font-body text-cyan-electric">Thinking</span>
            </motion.div>
          )}
        </AnimatePresence>
      </div>

      {/* Messages Area */}
      <div className="flex-1 overflow-y-auto px-4 py-3 space-y-4">
        {chatMessages.length === 0 && (
          <motion.div
            initial={{ opacity: 0, y: 20 }}
            animate={{ opacity: 1, y: 0 }}
            className="flex flex-col items-center justify-center h-full text-center"
          >
            <div className="w-16 h-16 rounded-2xl bg-gradient-to-br from-cyan-electric/20 to-cyan-glow/20 flex items-center justify-center mb-4">
              <Bot size={32} className="text-cyan-electric" />
            </div>
            <h3 className="font-display font-semibold text-foam text-lg mb-2">
              Start a Conversation
            </h3>
            <p className="text-sm text-foam/50 font-body max-w-xs">
              Send a message to begin interacting with your AI agent.
            </p>
          </motion.div>
        )}

        <AnimatePresence>
          {chatMessages.map((msg) => (
            <motion.div
              key={msg.id}
              initial={{ opacity: 0, y: 10 }}
              animate={{ opacity: 1, y: 0 }}
              className={`flex items-start gap-3 ${msg.role === 'user' ? 'flex-row-reverse' : ''}`}
            >
              {/* Avatar */}
              <div className={`w-8 h-8 rounded-lg flex items-center justify-center shrink-0 ${
                msg.role === 'user'
                  ? 'bg-ocean-surface'
                  : 'bg-gradient-to-br from-cyan-electric to-cyan-glow'
              }`}>
                {msg.role === 'user' ? (
                  <User size={16} className="text-foam" />
                ) : (
                  <Bot size={16} className="text-ocean-deep" />
                )}
              </div>

              {/* Message Content */}
              <div className="flex flex-col gap-1 max-w-[75%]">
                {/* Tool Calls */}
                {msg.role === 'assistant' && msg.isStreaming && currentToolCalls.length > 0 && (
                  <div className="flex flex-col gap-2 mb-2">
                    {currentToolCalls.map((tool) => (
                      <motion.div
                        key={tool.id}
                        initial={{ opacity: 0, x: -10 }}
                        animate={{ opacity: 1, x: 0 }}
                        className="flex items-center gap-2 px-3 py-2 rounded-lg bg-ocean-depth/50"
                      >
                        <Loader2 size={14} className="text-cyan-electric animate-spin" />
                        <Wrench size={14} className="text-cyan-mist" />
                        <span className="text-xs font-mono text-cyan-electric">{tool.function.name}</span>
                      </motion.div>
                    ))}
                  </div>
                )}

                {/* Reasoning */}
                {msg.reasoning && (
                  <div className="px-3 py-2 rounded-lg bg-ocean-depth/30 border border-cyan-electric/5 mb-1">
                    <p className="text-xs text-foam/60 font-body italic">{msg.reasoning}</p>
                  </div>
                )}

                {/* Message Bubble */}
                <div className={`message-bubble ${msg.role === 'user' ? 'message-user' : 'message-agent'}`}>
                  <p className="font-body whitespace-pre-wrap">{msg.content}</p>
                </div>

                {/* Timestamp */}
                <span className={`text-xs text-foam/40 font-body ${msg.role === 'user' ? 'text-right' : ''}`}>
                  {format(msg.timestamp, 'HH:mm:ss')}
                </span>
              </div>
            </motion.div>
          ))}
        </AnimatePresence>

        <div ref={messagesEndRef} />
      </div>

      {/* Input Area */}
      <div className="px-4 py-3 border-t border-cyan-electric/10">
        <div className="flex items-center gap-3">
          <input
            ref={inputRef}
            type="text"
            value={inputValue}
            onChange={(e) => setInputValue(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder={connectionState === 'connected' ? 'Type your message...' : 'Waiting for connection...'}
            disabled={connectionState !== 'connected'}
            className="input-glass flex-1"
          />
          <motion.button
            onClick={handleSendMessage}
            disabled={!inputValue.trim() || connectionState !== 'connected'}
            className="btn-primary px-4 py-2.5 flex items-center gap-2 disabled:opacity-50 disabled:cursor-not-allowed"
            whileHover={{ scale: 1.02 }}
            whileTap={{ scale: 0.98 }}
          >
            <Send size={18} />
            <span className="hidden sm:inline font-body">Send</span>
          </motion.button>
        </div>
      </div>
    </div>
  )
}