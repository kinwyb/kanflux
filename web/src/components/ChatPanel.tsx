import { useState, useEffect, useRef } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import { Send, Bot, User, Sparkles, Loader2, Wrench, CheckCircle2, XCircle, ChevronDown, ChevronRight, MessageSquare, Plus, AlertCircle } from 'lucide-react'
import { useWebSocketContext } from '../contexts/WebSocketContext'
import { useConversationContext, sessionToChatMessage, WEB_CHANNEL, WEB_ACCOUNT_ID } from '../contexts/ConversationContext'
import type { ChatMessage, InboundMessage, MessageBlock } from '../types'
import { format } from 'date-fns'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'

// Interrupt info type for confirmation dialog
interface InterruptInfo {
  chatId: string
  content: string
  interruptType: string
  replyTo: string
}

export default function ChatPanel() {
  const { connectionState, messages, events, sendMessage } = useWebSocketContext()
  const {
    activeConversationId,
    getActiveSessionKey,
    createConversation,
    loadHistory
  } = useConversationContext()
  const [inputValue, setInputValue] = useState('')
  const [chatMessages, setChatMessages] = useState<ChatMessage[]>([])
  const [isAgentThinking, setIsAgentThinking] = useState(false)
  const [expandedTools, setExpandedTools] = useState<Set<string>>(new Set())
  const [interruptInfo, setInterruptInfo] = useState<InterruptInfo | null>(null)
  const messagesEndRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLInputElement>(null)

  const processedEventIds = useRef<Set<string>>(new Set())
  const lastLoadedSessionKey = useRef<string | null>(null)
  const messageSendCounts = useRef<Map<string, number>>(new Map())

  const scrollToBottom = () => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }

  useEffect(() => {
    scrollToBottom()
  }, [chatMessages])

  useEffect(() => {
    const sessionKey = getActiveSessionKey()

    if (sessionKey && sessionKey !== lastLoadedSessionKey.current && connectionState === 'connected') {
      lastLoadedSessionKey.current = sessionKey

      setChatMessages([])
      setIsAgentThinking(false)
      processedEventIds.current.clear()

      loadHistory(sessionKey).then(session => {
        if (session && session.messages.length > 0) {
          const historyMessages = session.messages
            .filter(msg => msg.role === 'user' || msg.role === 'assistant')
            .map((msg, idx) => sessionToChatMessage(msg, idx))
          setChatMessages(historyMessages)
        }
      })
    }
  }, [connectionState, activeConversationId, getActiveSessionKey, loadHistory])

  const extractOriginalId = (id: string): string => {
    const match = id.match(/^(.+)_(\d+)$/)
    return match ? match[1] : id
  }

  const generateMessageId = (originalId: string): string => {
    // 检查是否已经有序号了
    if (originalId.match(/_\d+$/)) {
      // 已经有序号，说明是中断恢复后再次恢复，需要再+1
      const baseId = extractOriginalId(originalId)
      const count = (messageSendCounts.current.get(baseId) || 0) + 1
      messageSendCounts.current.set(baseId, count)
      return `${baseId}_${count}`
    }
    // 第一次发送（无序号）
    const count = messageSendCounts.current.get(originalId) || 0
    messageSendCounts.current.set(originalId, count + 1)
    // count=0 时是第一次，返回原始ID；count>=1 时返回 _1, _2...
    if (count === 0) {
      return originalId
    }
    return `${originalId}_${count}`
  }

  useEffect(() => {
    const chatId = activeConversationId
    if (!chatId) return

    for (const event of events) {
      if (event.chat_id !== chatId) continue
      if (processedEventIds.current.has(event.id)) continue
      processedEventIds.current.add(event.id)

      // 提取原始ID用于分组，完整的reply_to用于匹配
      const originalReplyTo = extractOriginalId(event.reply_to)
      const replyTo = event.reply_to

      if (event.state === 'start') {
        setIsAgentThinking(true)

        // 用原始ID找到消息组
        const existingMsg = chatMessages.find(m => extractOriginalId(m.id) === originalReplyTo)

        if (existingMsg) {
          // 已有消息组，添加新的消息块
          const newBlock: MessageBlock = {
            id: `block-${Date.now()}-start`,
            type: 'start',
            timestamp: new Date()
          }
          setChatMessages(prev => prev.map(msg => {
            if (extractOriginalId(msg.id) === originalReplyTo) {
              return {
                ...msg,
                id: replyTo, // 更新为带序号的ID
                isStreaming: true,
                messageBlocks: [...(msg.messageBlocks || []), newBlock]
              }
            }
            return msg
          }))
        } else {
          // 没有消息组，创建新消息
          const newMsg: ChatMessage = {
            id: replyTo,
            role: 'assistant',
            content: '',
            timestamp: new Date(),
            isStreaming: true,
            messageBlocks: [
              {
                id: `block-${Date.now()}-start`,
                type: 'start',
                timestamp: new Date()
              }
            ]
          }
          setChatMessages(prev => [...prev, newMsg])
        }

      } else if (event.state === 'tool' && event.tool_info) {
        const toolId = event.tool_info.id
        const toolName = event.tool_info.name
        const replyTo = event.reply_to

        // 用原始ID找到消息组
        const existingMsg = chatMessages.find(m => extractOriginalId(m.id) === originalReplyTo)

        if (!existingMsg) return

        if (event.tool_info.is_start) {
          const newTool: MessageBlock = {
            id: `block-${toolId}`,
            type: 'tool_call',
            toolInfo: {
              id: toolId,
              name: toolName,
              arguments: event.tool_info.arguments || '',
              status: 'running',
              startTime: new Date(),
            },
            timestamp: new Date()
          }

          // 用原始ID找到消息组，添加新块
          setChatMessages(prev => prev.map(msg => {
            if (extractOriginalId(msg.id) === originalReplyTo) {
              return {
                ...msg,
                id: replyTo, // 更新为带序号的ID
                messageBlocks: [...(msg.messageBlocks || []), newTool]
              }
            }
            return msg
          }))

        } else {
          // 用原始ID找到消息组
          setChatMessages(prev => prev.map(msg => {
            if (extractOriginalId(msg.id) === originalReplyTo) {
              return {
                ...msg,
                id: replyTo, // 更新为带序号的ID
                messageBlocks: (msg.messageBlocks || []).map(block => {
                  if (block.type === 'tool_call' && block.toolInfo?.id === toolId) {
                    return {
                      ...block,
                      toolInfo: {
                        ...block.toolInfo,
                        result: event.tool_info?.result || '',
                        status: event.tool_info?.result && !event.tool_info.result.startsWith('Error') ? 'completed' : 'error',
                        endTime: new Date(),
                      }
                    }
                  }
                  return block
                })
              }
            }
            return msg
          }))
        }

      } else if (event.state === 'complete' || event.state === 'error') {
        setIsAgentThinking(false)
        const replyTo = event.reply_to

        // 用原始ID找到消息组
        setChatMessages(prev => prev.map(msg => {
          if (extractOriginalId(msg.id) === originalReplyTo) {
            return {
              ...msg,
              id: replyTo, // 更新为带序号的ID
              isStreaming: false
            }
          }
          return msg
        }))

      } else if (event.state === 'interrupt') {
        setIsAgentThinking(false)
        const metadata = event.metadata as Record<string, unknown> | undefined
        const interruptType = metadata?.interrupt_type as string | undefined
        const interruptContent = metadata?.interrupt_content as string | undefined

        if (interruptType === 'yes_no' && interruptContent) {
          setInterruptInfo({
            chatId: event.chat_id,
            content: interruptContent,
            interruptType: interruptType,
            replyTo: event.reply_to,
          })
        }
      }
    }

    for (const msg of messages) {
      if (msg.chat_id !== chatId) continue
      if (!msg.reply_to) continue

      // 用原始ID找到消息组
      const originalReplyTo = extractOriginalId(msg.reply_to)
      const replyTo = msg.reply_to

      if (msg.is_streaming) {
        const isThinking = msg.is_thinking && msg.reasoning_content
        const hasContent = msg.content

        if (!isThinking && !hasContent) continue

        setChatMessages(prev => {
          // 用原始ID找到消息组
          const targetMsg = prev.find(m => extractOriginalId(m.id) === originalReplyTo)
          if (!targetMsg) return prev

          const blockType = isThinking ? 'thinking' : 'output'
          const content = isThinking ? msg.reasoning_content : msg.content

          // 找到同类型的块，直接覆盖更新（不用累积）
          const existingBlocks = targetMsg.messageBlocks || []
          const existingBlockIdx = existingBlocks.findIndex(b => b.type === blockType)

          let newBlocks: MessageBlock[]
          if (existingBlockIdx >= 0) {
            // 同类型块已存在，直接覆盖更新
            newBlocks = existingBlocks.map((block, idx) => {
              if (idx === existingBlockIdx) {
                return {
                  ...block,
                  content: content,
                  reasoning: isThinking ? content : block.reasoning,
                  timestamp: new Date()
                }
              }
              return block
            })
          } else {
            // 没有同类型块，创建新块
            const newBlock: MessageBlock = {
              id: `block-${Date.now()}-${blockType}`,
              type: blockType,
              content: content,
              reasoning: isThinking ? content : undefined,
              timestamp: new Date()
            }
            newBlocks = [...existingBlocks, newBlock]
          }

          // 更新消息ID为带序号的ID
          return prev.map(m => {
            if (extractOriginalId(m.id) === originalReplyTo) {
              return { ...m, id: replyTo, messageBlocks: newBlocks }
            }
            return m
          })
        })
      }
    }
  }, [messages, events, activeConversationId])

  const handleSendMessage = () => {
    if (!inputValue.trim() || connectionState !== 'connected') return
    if (!activeConversationId) return

    const userMessage: ChatMessage = {
      id: `user-${Date.now()}`,
      role: 'user',
      content: inputValue.trim(),
      timestamp: new Date(),
    }

    setChatMessages(prev => [...prev, userMessage])

    const messageId = `msg-${Date.now()}`
    const inbound: InboundMessage = {
      id: messageId,
      channel: WEB_CHANNEL,
      account_id: WEB_ACCOUNT_ID,
      sender_id: 'web-user',
      chat_id: activeConversationId,
      content: inputValue.trim(),
      streaming_mode: 'accumulate',
      timestamp: new Date(),
    }

    sendMessage(inbound)
    setInputValue('')
    inputRef.current?.focus()
  }

  const handleInterruptConfirm = (approved: boolean) => {
    if (!interruptInfo) return

    const response = approved ? 'y' : 'n'
    const messageId = generateMessageId(interruptInfo.replyTo)
    const inbound: InboundMessage = {
      id: messageId,
      channel: WEB_CHANNEL,
      account_id: WEB_ACCOUNT_ID,
      sender_id: 'web-user',
      chat_id: interruptInfo.chatId,
      content: response,
      streaming_mode: 'accumulate',
      timestamp: new Date(),
    }

    sendMessage(inbound)
    setInterruptInfo(null)
    setIsAgentThinking(true)
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

  const renderToolCall = (toolInfo: MessageBlock['toolInfo'], isRunning: boolean = false) => {
    if (!toolInfo) return null
    const isExpanded = expandedTools.has(toolInfo.id)

    return (
      <motion.div
        key={toolInfo.id}
        initial={{ opacity: 0, x: -10 }}
        animate={{ opacity: 1, x: 0 }}
        className="tool-call-container"
      >
        <div
          className="flex items-center gap-2 px-3 py-2 rounded-lg bg-ocean-depth/10 cursor-pointer hover:bg-ocean-depth/15"
          onClick={() => toggleToolExpand(toolInfo.id)}
        >
          <Wrench size={14} className="text-cyan-electric" />
          {isRunning ? (
            <Loader2 size={12} className="text-cyan-mist animate-spin" />
          ) : toolInfo.status === 'completed' ? (
            <CheckCircle2 size={12} className="text-green-500" />
          ) : (
            <XCircle size={12} className="text-red-500" />
          )}
          <span className="text-xs font-mono text-cyan-electric font-medium">
            {toolInfo.name}
          </span>
          {isExpanded ? (
            <ChevronDown size={14} className="text-ocean-depth/50" />
          ) : (
            <ChevronRight size={14} className="text-ocean-depth/50" />
          )}
          {!isRunning && toolInfo.endTime && (
            <span className="text-xs text-ocean-depth/40 font-body ml-auto">
              {Math.round((toolInfo.endTime.getTime() - toolInfo.startTime.getTime()) / 1000)}s
            </span>
          )}
        </div>

        <AnimatePresence>
          {isExpanded && (
            <motion.div
              initial={{ opacity: 0, height: 0 }}
              animate={{ opacity: 1, height: 'auto' }}
              exit={{ opacity: 0, height: 0 }}
              className="mt-2 ml-4 space-y-2 overflow-hidden"
            >
              <div className="tool-section">
                <div className="flex items-center gap-2 mb-1">
                  <Wrench size={12} className="text-cyan-mist" />
                  <span className="text-xs font-body text-ocean-depth/60 uppercase">工具调用</span>
                </div>
                <pre className="text-xs font-mono text-ocean-depth/70 bg-ocean-depth/5 p-2 rounded overflow-x-auto">
                  {parseJsonArgs(toolInfo.arguments)}
                </pre>
              </div>

              {!isRunning && toolInfo.result && (
                <div className="tool-section">
                  <div className="flex items-center gap-2 mb-1">
                    <Wrench size={12} className={toolInfo.status === 'completed' ? 'text-green-500' : 'text-red-500'} />
                    <span className="text-xs font-body text-ocean-depth/60 uppercase">工具结果</span>
                  </div>
                  <pre className={`text-xs font-mono p-2 rounded overflow-x-auto ${
                    toolInfo.status === 'completed'
                      ? 'text-green-600 bg-green-500/10'
                      : 'text-red-500 bg-red-500/10'
                  }`}>
                    {toolInfo.result}
                  </pre>
                </div>
              )}
            </motion.div>
          )}
        </AnimatePresence>
      </motion.div>
    )
  }

  const renderMessageBlock = (block: MessageBlock) => {
    switch (block.type) {
      case 'start':
        // 只渲染 loading 状态
        return (
          <div key={block.id} className="flex items-center gap-2 py-2">
            <Loader2 size={14} className="text-cyan-electric animate-spin" />
            <span className="text-sm text-ocean-depth/60">正在处理...</span>
          </div>
        )

      case 'thinking':
        // 只渲染有内容的 thinking 块
        if (!block.reasoning) return null
        return (
          <div key={block.id} className="inline-block px-3 py-2 rounded-lg bg-ocean-depth/10 border border-cyan-electric/10 mb-1 max-w-full">
            <p className="text-xs text-ocean-depth/60 font-body italic whitespace-pre-wrap break-words">{block.reasoning}</p>
          </div>
        )

      case 'output':
        // 只渲染有内容的 output 块
        if (!block.content) return null
        return (
          <div key={block.id} className="message-bubble message-agent">
            <div className="markdown-content font-body">
              <ReactMarkdown remarkPlugins={[remarkGfm]}>
                {block.content}
              </ReactMarkdown>
            </div>
          </div>
        )

      case 'tool_call':
        // 只渲染有工具信息的块
        if (!block.toolInfo) return null
        return (
          <div key={block.id} className="flex flex-col gap-2 mb-2">
            {renderToolCall(block.toolInfo, true)}
          </div>
        )

      case 'tool_result':
        // 只渲染有工具信息的块
        if (!block.toolInfo) return null
        return (
          <div key={block.id} className="flex flex-col gap-2 mb-2">
            {renderToolCall(block.toolInfo, false)}
          </div>
        )

      default:
        return null
    }
  }

  return (
    <div className="glass-card-solid h-full flex flex-col overflow-hidden">
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

      {!activeConversationId && (
        <div className="flex-1 flex flex-col items-center justify-center px-4">
          <motion.div
            initial={{ opacity: 0, y: 20 }}
            animate={{ opacity: 1, y: 0 }}
            className="flex flex-col items-center justify-center text-center"
          >
            <div className="w-16 h-16 rounded-2xl bg-gradient-to-br from-cyan-electric/20 to-cyan-glow/20 flex items-center justify-center mb-4 shadow-md">
              <MessageSquare size={32} className="text-cyan-electric" />
            </div>
            <h3 className="font-display font-semibold text-ocean-deep text-lg mb-2">
              No Conversation Selected
            </h3>
            <p className="text-sm text-ocean-depth/50 font-body max-w-xs mb-4">
              Select a conversation from the sidebar or create a new one to start chatting.
            </p>
            <motion.button
              onClick={() => createConversation()}
              className="btn-primary flex items-center gap-2"
              whileHover={{ scale: 1.02 }}
              whileTap={{ scale: 0.98 }}
            >
              <Plus size={16} />
              <span className="font-body">Start New Chat</span>
            </motion.button>
          </motion.div>
        </div>
      )}

      {activeConversationId && (
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

              <div className="flex flex-col gap-1 max-w-[75%]">
                {msg.role === 'user' ? (
                  <>
                    <div className="message-bubble message-user">
                      <div className="markdown-content font-body">
                        <ReactMarkdown remarkPlugins={[remarkGfm]}>
                          {msg.content}
                        </ReactMarkdown>
                      </div>
                    </div>
                    <span className={`text-xs text-ocean-depth/40 font-body ${msg.role === 'user' ? 'text-right' : ''}`}>
                      {format(msg.timestamp, 'HH:mm:ss')}
                    </span>
                  </>
                ) : (
                  <>
                    {msg.messageBlocks?.map(block => renderMessageBlock(block))}
                    {msg.messageBlocks && msg.messageBlocks.length > 0 && (
                      <span className="text-xs text-ocean-depth/40 font-body">
                        {format(msg.messageBlocks[msg.messageBlocks.length - 1].timestamp, 'HH:mm:ss')}
                      </span>
                    )}
                  </>
                )}
              </div>
            </motion.div>
          ))}
        </AnimatePresence>

        <div ref={messagesEndRef} />
      </div>
      )}

      {activeConversationId && (
      <div className="px-4 py-3 border-t border-cyan-electric/15">
        <div className="flex items-center gap-3">
          <input
            ref={inputRef}
            type="text"
            value={inputValue}
            onChange={(e) => setInputValue(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder={connectionState === 'connected' ? 'Type your message...' : 'Waiting for connection...'}
            disabled={connectionState !== 'connected' || !activeConversationId}
            className="input-glass flex-1"
          />
          <motion.button
            onClick={handleSendMessage}
            disabled={!inputValue.trim() || connectionState !== 'connected' || !activeConversationId}
            className="btn-primary px-4 py-2.5 flex items-center gap-2 disabled:opacity-50 disabled:cursor-not-allowed"
            whileHover={{ scale: 1.02 }}
            whileTap={{ scale: 0.98 }}
          >
            <Send size={18} />
            <span className="hidden sm:inline font-body">Send</span>
          </motion.button>
        </div>
      </div>
      )}

      <AnimatePresence>
        {interruptInfo && (
          <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-ocean-depth/20 backdrop-blur-sm"
            onClick={() => setInterruptInfo(null)}
          >
            <motion.div
              initial={{ scale: 0.9, opacity: 0 }}
              animate={{ scale: 1, opacity: 1 }}
              exit={{ scale: 0.9, opacity: 0 }}
              className="glass-card-solid max-w-md w-full p-6 rounded-xl"
              onClick={(e) => e.stopPropagation()}
            >
              <div className="flex items-center gap-3 mb-4">
                <div className="w-10 h-10 rounded-lg bg-cyan-electric/20 flex items-center justify-center">
                  <AlertCircle size={20} className="text-cyan-electric" />
                </div>
                <h3 className="font-display font-semibold text-ocean-deep">确认操作</h3>
              </div>

              <div className="mb-6">
                <p className="text-sm text-ocean-depth/70 font-body whitespace-pre-wrap break-words">
                  {interruptInfo.content}
                </p>
              </div>

              <div className="flex items-center justify-end gap-3">
                <motion.button
                  onClick={() => handleInterruptConfirm(false)}
                  className="btn-glass px-4 py-2 rounded-lg flex items-center gap-2 text-red-500 border-red-500/30 hover:bg-red-500/10"
                  whileHover={{ scale: 1.02 }}
                  whileTap={{ scale: 0.98 }}
                >
                  <XCircle size={16} />
                  <span className="font-body">拒绝</span>
                </motion.button>
                <motion.button
                  onClick={() => handleInterruptConfirm(true)}
                  className="btn-primary px-4 py-2 rounded-lg flex items-center gap-2"
                  whileHover={{ scale: 1.02 }}
                  whileTap={{ scale: 0.98 }}
                >
                  <CheckCircle2 size={16} />
                  <span className="font-body">同意</span>
                </motion.button>
              </div>
            </motion.div>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  )
}
