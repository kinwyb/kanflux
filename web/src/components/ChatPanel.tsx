import { useState, useEffect, useRef } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import { Send, Bot, User, Sparkles, Loader2, Wrench, CheckCircle2, XCircle, ChevronDown, ChevronRight } from 'lucide-react'
import { useWebSocketContext } from '../contexts/WebSocketContext'
import type { ChatMessage, InboundMessage, ToolCallDisplay } from '../types'
import { format } from 'date-fns'

export default function ChatPanel() {
  const { connectionState, messages, events, sendMessage } = useWebSocketContext()
  const [inputValue, setInputValue] = useState('')
  const [chatMessages, setChatMessages] = useState<ChatMessage[]>([])
  const [isAgentThinking, setIsAgentThinking] = useState(false)
  const [_currentStreamingId, setCurrentStreamingId] = useState<string | null>(null)
  const [runningToolCalls, setRunningToolCalls] = useState<Map<string, ToolCallDisplay>>(new Map())
  const [expandedTools, setExpandedTools] = useState<Set<string>>(new Set())
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
          toolCallDisplays: [],
        }
        setChatMessages(prev => [...prev, newMsg])
        setCurrentStreamingId(event.run_id)
      } else if (event.state === 'tool' && event.tool_info) {
        const toolId = event.tool_info.id
        const toolName = event.tool_info.name

        if (event.tool_info.is_start) {
          // Tool call started
          const newTool: ToolCallDisplay = {
            id: toolId,
            name: toolName,
            arguments: event.tool_info.arguments || '',
            status: 'running',
            startTime: new Date(),
          }
          setRunningToolCalls(prev => new Map(prev).set(toolId, newTool))
        } else {
          // Tool call completed
          const completedTool = runningToolCalls.get(toolId)
          if (completedTool) {
            const updatedTool: ToolCallDisplay = {
              ...completedTool,
              result: event.tool_info.result || '',
              status: event.tool_info.result && !event.tool_info.result.startsWith('Error') ? 'completed' : 'error',
              endTime: new Date(),
            }

            // Remove from running and add to message's tool displays
            setRunningToolCalls(prev => {
              const newMap = new Map(prev)
              newMap.delete(toolId)
              return newMap
            })

            // Add to the current streaming message
            setChatMessages(prev => prev.map(msg => {
              if (msg.isStreaming) {
                const displays = msg.toolCallDisplays || []
                return { ...msg, toolCallDisplays: [...displays, updatedTool] }
              }
              return msg
            }))
          }
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
        // Clear running tool calls
        setRunningToolCalls(new Map())
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

  const toggleToolExpand = (toolId: string) => {
    setExpandedTools(prev => {
      const newSet = new Set(prev)
      if (newSet.has(toolId)) {
        newSet.delete(toolId)
      } else {
        newSet.add(toolId)
      }
      return newSet
    })
  }

  const parseJsonArgs = (args: string): string => {
    try {
      const parsed = JSON.parse(args)
      return JSON.stringify(parsed, null, 2)
    } catch (_e) {
      return args
    }
  }

  const renderToolCall = (tool: ToolCallDisplay, isRunning: boolean = false) => {
    const isExpanded = expandedTools.has(tool.id)

    return (
      <motion.div
        key={tool.id}
        initial={{ opacity: 0, x: -10 }}
        animate={{ opacity: 1, x: 0 }}
        className="tool-call-container"
      >
        {/* Tool Call Section - 参数 */}
        <div
          className="flex items-center gap-2 px-3 py-2 rounded-lg bg-ocean-depth/10 cursor-pointer hover:bg-ocean-depth/15"
          onClick={() => toggleToolExpand(tool.id)}
        >
          {/* Tool Icon */}
          <Wrench size={14} className="text-cyan-mist" />

          {/* Status Icon */}
          {isRunning ? (
            <Loader2 size={12} className="text-cyan-electric animate-spin" />
          ) : tool.status === 'completed' ? (
            <CheckCircle2 size={12} className="text-green-500" />
          ) : (
            <XCircle size={12} className="text-red-500" />
          )}

          {/* Tool Name */}
          <div className="flex items-center gap-1">
            <Wrench size={10} className="text-cyan-electric" />
            <span className="text-xs font-mono text-cyan-electric font-medium">
              {tool.name}
            </span>
          </div>

          {/* Expand Icon */}
          {isExpanded ? (
            <ChevronDown size={14} className="text-ocean-depth/50" />
          ) : (
            <ChevronRight size={14} className="text-ocean-depth/50" />
          )}

          {/* Time */}
          {!isRunning && tool.endTime && (
            <span className="text-xs text-ocean-depth/40 font-body ml-auto">
              {Math.round((tool.endTime.getTime() - tool.startTime.getTime()) / 1000)}s
            </span>
          )}
        </div>

        {/* Expanded Content */}
        <AnimatePresence>
          {isExpanded && (
            <motion.div
              initial={{ opacity: 0, height: 0 }}
              animate={{ opacity: 1, height: 'auto' }}
              exit={{ opacity: 0, height: 0 }}
              className="mt-2 ml-4 space-y-2 overflow-hidden"
            >
              {/* Arguments - 工具调用参数 */}
              <div className="tool-section">
                <div className="flex items-center gap-2 mb-1">
                  <Wrench size={12} className="text-cyan-mist" />
                  <span className="text-xs font-body text-ocean-depth/60 uppercase">工具调用</span>
                </div>
                <pre className="text-xs font-mono text-ocean-depth/70 bg-ocean-depth/5 p-2 rounded overflow-x-auto">
                  {parseJsonArgs(tool.arguments)}
                </pre>
              </div>

              {/* Result - 工具结果 */}
              {!isRunning && tool.result && (
                <div className="tool-section">
                  <div className="flex items-center gap-2 mb-1">
                    <Wrench size={12} className={tool.status === 'completed' ? 'text-green-500' : 'text-red-500'} />
                    <span className="text-xs font-body text-ocean-depth/60 uppercase">
                      工具结果
                    </span>
                  </div>
                  <pre className={`text-xs font-mono p-2 rounded overflow-x-auto ${
                    tool.status === 'completed'
                      ? 'text-green-600 bg-green-500/10'
                      : 'text-red-500 bg-red-500/10'
                  }`}>
                    {tool.result}
                  </pre>
                </div>
              )}
            </motion.div>
          )}
        </AnimatePresence>
      </motion.div>
    )
  }

  return (
    <div className="glass-card-solid h-full flex flex-col overflow-hidden">
      {/* Chat Header */}
      <div className="flex items-center justify-between px-4 py-3 border-b border-cyan-electric/15">
        <div className="flex items-center gap-3">
          <div className="w-8 h-8 rounded-lg bg-gradient-to-br from-cyan-electric to-cyan-glow flex items-center justify-center shadow-md">
            <Bot size={16} className="text-white" />
          </div>
          <div>
            <h2 className="font-display font-semibold text-ocean-deep">AI Agent</h2>
            <p className="text-xs text-ocean-depth/50 font-body">
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
              className="flex items-center gap-2 px-3 py-1.5 rounded-lg bg-cyan-electric/15"
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
            <div className="w-16 h-16 rounded-2xl bg-gradient-to-br from-cyan-electric/20 to-cyan-glow/20 flex items-center justify-center mb-4 shadow-md">
              <Bot size={32} className="text-cyan-electric" />
            </div>
            <h3 className="font-display font-semibold text-ocean-deep text-lg mb-2">
              Start a Conversation
            </h3>
            <p className="text-sm text-ocean-depth/50 font-body max-w-xs">
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
              <div className={`w-8 h-8 rounded-lg flex items-center justify-center shrink-0 shadow-md ${
                msg.role === 'user'
                  ? 'bg-ocean-surface'
                  : 'bg-gradient-to-br from-cyan-electric to-cyan-glow'
              }`}>
                {msg.role === 'user' ? (
                  <User size={16} className="text-white" />
                ) : (
                  <Bot size={16} className="text-white" />
                )}
              </div>

              {/* Message Content */}
              <div className="flex flex-col gap-1 max-w-[75%]">
                {/* Running Tool Calls */}
                {msg.role === 'assistant' && msg.isStreaming && runningToolCalls.size > 0 && (
                  <div className="flex flex-col gap-2 mb-2">
                    {Array.from(runningToolCalls.values()).map((tool) => renderToolCall(tool, true))}
                  </div>
                )}

                {/* Completed Tool Calls */}
                {msg.role === 'assistant' && msg.toolCallDisplays && msg.toolCallDisplays.length > 0 && (
                  <div className="flex flex-col gap-2 mb-2">
                    {msg.toolCallDisplays.map((tool) => renderToolCall(tool, false))}
                  </div>
                )}

                {/* Reasoning */}
                {msg.reasoning && (
                  <div className="px-3 py-2 rounded-lg bg-ocean-depth/10 border border-cyan-electric/10 mb-1">
                    <p className="text-xs text-ocean-depth/60 font-body italic">{msg.reasoning}</p>
                  </div>
                )}

                {/* Message Bubble */}
                {msg.content && (
                  <div className={`message-bubble ${msg.role === 'user' ? 'message-user' : 'message-agent'}`}>
                    <p className="font-body whitespace-pre-wrap">{msg.content}</p>
                  </div>
                )}

                {/* Timestamp */}
                <span className={`text-xs text-ocean-depth/40 font-body ${msg.role === 'user' ? 'text-right' : ''}`}>
                  {format(msg.timestamp, 'HH:mm:ss')}
                </span>
              </div>
            </motion.div>
          ))}
        </AnimatePresence>

        <div ref={messagesEndRef} />
      </div>

      {/* Input Area */}
      <div className="px-4 py-3 border-t border-cyan-electric/15">
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