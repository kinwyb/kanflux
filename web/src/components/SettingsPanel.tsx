import { useState, useEffect } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import {
  Settings,
  X,
  Save,
  RefreshCw,
  AlertCircle,
  CheckCircle,
  ChevronDown,
  ChevronRight,
  Server,
  Bot,
  Globe,
  FileText,
  Clock,
  Wrench,
  Plus,
  Trash2,
} from 'lucide-react'
import { useWebSocketContext } from '../contexts/WebSocketContext'

interface SettingsPanelProps {
  isOpen: boolean
  onClose: () => void
}

// 配置结构类型（完整）
interface AppConfig {
  providers: Record<string, ProviderConfig>
  default_provider: string
  agents: AgentConfig[]
  tools?: ToolsConfig
  knowledge_bases?: Record<string, KnowledgeBaseConfig>
  channels?: ChannelsConfig
  websocket?: WebSocketConfig
  log?: LogConfig
  scheduler?: SchedulerConfig
}

interface ProviderConfig {
  api_key: string
  api_base_url: string
  default_model: string
}

interface AgentConfig {
  name: string
  type: string
  description?: string
  workspace: string
  provider?: string
  model?: string
  max_iteration?: number
  streaming?: boolean
  tools?: string[]
  tools_approval?: string[]
  sub_agents?: string[]
  knowledge_paths?: KnowledgePathConfig[]
  knowledge_base_refs?: string[]
  memoria_enabled?: boolean
}

interface KnowledgePathConfig {
  path: string
  extensions?: string[]
  recursive?: boolean
  exclude?: string[]
}

interface KnowledgeBaseConfig {
  paths: KnowledgePathConfig[]
  embedding?: EmbeddingConfig
  rag_config?: RAGConfigOptions
}

interface EmbeddingConfig {
  provider?: string
  model?: string
  api_key?: string
  api_base_url?: string
}

interface RAGConfigOptions {
  chunk_size?: number
  chunk_overlap?: number
  top_k?: number
  score_threshold?: number
  enable_watcher?: boolean
  store_type?: string
}

interface ToolsConfig {
  approval?: string[]
  mcp?: MCPConfig[]
  browser?: BrowserConfig
  web?: WebConfig
}

interface MCPConfig {
  name: string
  type: string
  url?: string
  command?: string
  args?: string[]
  env?: Record<string, string>
  tools?: string[]
  enabled: boolean
  init_timeout?: number
}

interface BrowserConfig {
  enabled?: boolean
  headless?: boolean
  timeout?: number
  relay_url?: string
  relay_mode?: string
}

interface WebConfig {
  enabled?: boolean
  search_api_key?: string
  search_engine?: string
  timeout?: number
}

interface ChannelsConfig {
  telegram?: { enabled: boolean; token?: string; allowed_ids?: string[] }
  wxcom?: { enabled: boolean; accounts?: Record<string, unknown> }
  feishu?: { enabled: boolean }
  cli?: { enabled: boolean }
}

interface WebSocketConfig {
  enabled?: boolean
  port?: number
  host?: string
  path?: string
  auth_token?: string
  read_timeout?: number
  write_timeout?: number
}

interface LogConfig {
  level?: string
  file?: string
  max_size?: number
  max_backups?: number
  max_age?: number
  compress?: boolean
}

interface SchedulerConfig {
  enabled?: boolean
  check_interval?: string
  persist_state?: boolean
}

// 配置分组
const configSections = [
  { key: 'providers', label: 'Providers', icon: Server, desc: 'LLM 提供商配置' },
  { key: 'agents', label: 'Agents', icon: Bot, desc: 'Agent 实例配置' },
  { key: 'tools', label: 'Tools', icon: Wrench, desc: '工具配置（审批、MCP、Browser、Web）' },
  { key: 'websocket', label: 'WebSocket', icon: Globe, desc: 'WebSocket 服务配置' },
  { key: 'log', label: 'Logging', icon: FileText, desc: '日志配置' },
  { key: 'scheduler', label: 'Scheduler', icon: Clock, desc: '定时任务调度器配置' },
]

export default function SettingsPanel({ isOpen, onClose }: SettingsPanelProps) {
  const { connectionState, fetchConfig, updateConfig } = useWebSocketContext()
  const [config, setConfig] = useState<AppConfig | null>(null)
  const [isLoading, setIsLoading] = useState(false)
  const [isSaving, setIsSaving] = useState(false)
  const [expandedSections, setExpandedSections] = useState<Set<string>>(new Set(['providers', 'agents']))
  const [saveResult, setSaveResult] = useState<{ success: boolean; message: string } | null>(null)
  const [activeProvider, setActiveProvider] = useState<string>('')
  const [activeAgent, setActiveAgent] = useState<number>(0)
  const [activeMcp, setActiveMcp] = useState<number>(0)

  // Load config when panel opens
  useEffect(() => {
    if (isOpen && connectionState === 'connected') {
      loadConfig()
    }
  }, [isOpen, connectionState])

  const loadConfig = async () => {
    setIsLoading(true)
    setSaveResult(null)
    try {
      const response = await fetchConfig()
      if (response.success && response.config) {
        const cfg = response.config as unknown as AppConfig
        setConfig(cfg)
        if (cfg.default_provider && cfg.providers?.[cfg.default_provider]) {
          setActiveProvider(cfg.default_provider)
        } else if (Object.keys(cfg.providers || {}).length > 0) {
          setActiveProvider(Object.keys(cfg.providers)[0])
        }
      } else {
        setSaveResult({
          success: false,
          message: response.error || 'Failed to load configuration',
        })
      }
    } catch (error) {
      setSaveResult({
        success: false,
        message: error instanceof Error ? error.message : 'Unknown error',
      })
    }
    setIsLoading(false)
  }

  const handleSave = async () => {
    if (!config) return

    setIsSaving(true)
    setSaveResult(null)
    try {
      const response = await updateConfig(config as unknown as Record<string, unknown>)
      setSaveResult({
        success: response.success,
        message: response.success ? response.message || 'Saved successfully' : response.error || 'Failed to save',
      })
    } catch (error) {
      setSaveResult({
        success: false,
        message: error instanceof Error ? error.message : 'Unknown error',
      })
    }
    setIsSaving(false)
  }

  const toggleSection = (key: string) => {
    setExpandedSections(prev => {
      const next = new Set(prev)
      if (next.has(key)) {
        next.delete(key)
      } else {
        next.add(key)
      }
      return next
    })
  }

  // 更新顶层字段
  const updateTopField = (field: keyof AppConfig, value: string) => {
    if (!config) return
    setConfig(prev => prev ? { ...prev, [field]: value } : prev)
  }

  // 更新嵌套配置值
  const updateValue = (section: string, field: string, value: string | number | boolean) => {
    if (!config) return
    setConfig(prev => {
      if (!prev) return prev
      const sectionData = prev[section as keyof AppConfig]
      return {
        ...prev,
        [section]: typeof sectionData === 'object' && sectionData !== null
          ? { ...sectionData as object, [field]: value }
          : { [field]: value }
      } as AppConfig
    })
  }

  // 更新 provider 配置
  const updateProvider = (providerName: string, field: string, value: string) => {
    if (!config) return
    setConfig(prev => {
      if (!prev) return prev
      const providers = { ...prev.providers }
      if (!providers[providerName]) {
        providers[providerName] = { api_key: '', api_base_url: '', default_model: '' }
      }
      providers[providerName] = { ...providers[providerName], [field]: value }
      return { ...prev, providers }
    })
  }

  // 新增 Provider
  const addProvider = () => {
    const name = prompt('Enter new provider name:')
    if (!name || !config) return
    if (config.providers?.[name]) {
      alert('Provider already exists')
      return
    }
    setConfig(prev => prev ? {
      ...prev,
      providers: { ...prev.providers, [name]: { api_key: '', api_base_url: '', default_model: '' } }
    } : prev)
    setActiveProvider(name)
  }

  // 删除 Provider
  const removeProvider = (name: string) => {
    if (!confirm(`Delete provider "${name}"?`)) return
    if (!config) return
    // 计算删除后的剩余 providers
    const newProviders = { ...config.providers }
    delete newProviders[name]
    const remaining = Object.keys(newProviders)

    setConfig(prev => prev ? { ...prev, providers: newProviders } : prev)

    // 如果删除的是当前选中的，切换到其他
    if (activeProvider === name && remaining.length > 0) {
      setActiveProvider(remaining[0])
    }
    // 更新 default_provider
    if (config.default_provider === name && remaining.length > 0) {
      setConfig(prev => prev ? { ...prev, default_provider: remaining[0] } : prev)
    }
  }

  // 更新 agent 配置
  const updateAgent = (index: number, field: string, value: string | number | boolean | string[]) => {
    if (!config) return
    setConfig(prev => {
      if (!prev) return prev
      const agents = [...prev.agents]
      if (agents[index]) {
        agents[index] = { ...agents[index], [field]: value }
      }
      return { ...prev, agents }
    })
  }

  // 新增 Agent
  const addAgent = () => {
    const name = prompt('Enter new agent name:')
    if (!name || !config) return
    if (config.agents?.some(a => a.name === name)) {
      alert('Agent already exists')
      return
    }
    const newAgent: AgentConfig = {
      name,
      type: 'deep',
      workspace: '',
      streaming: true,
      memoria_enabled: true,
      max_iteration: 10,
    }
    setConfig(prev => prev ? { ...prev, agents: [...prev.agents, newAgent] } : prev)
    setActiveAgent(config.agents.length)
  }

  // 删除 Agent
  const removeAgent = (index: number) => {
    const agent = config?.agents?.[index]
    if (!agent || !confirm(`Delete agent "${agent.name}"?`)) return
    if (!config) return
    setConfig(prev => {
      if (!prev) return prev
      const agents = prev.agents.filter((_, i) => i !== index)
      return { ...prev, agents }
    })
    if (activeAgent >= config.agents.length - 1) {
      setActiveAgent(Math.max(0, config.agents.length - 2))
    }
  }

  // 新增 MCP
  const addMcp = () => {
    const name = prompt('Enter new MCP name:')
    if (!name || !config) return
    const tools = config.tools || { approval: [], mcp: [], browser: { enabled: false }, web: { enabled: false } }
    if (tools.mcp?.some(m => m.name === name)) {
      alert('MCP already exists')
      return
    }
    const newMcp: MCPConfig = {
      name,
      type: 'stdio',
      enabled: true,
      init_timeout: 30,
    }
    setConfig(prev => prev ? {
      ...prev,
      tools: { ...prev.tools || {}, mcp: [...(prev.tools?.mcp || []), newMcp] }
    } : prev)
    setActiveMcp((config.tools?.mcp?.length || 0))
  }

  // 删除 MCP
  const removeMcp = (index: number) => {
    const mcp = config?.tools?.mcp?.[index]
    if (!mcp || !confirm(`Delete MCP "${mcp.name}"?`)) return
    if (!config) return
    const mcpCount = config.tools?.mcp?.length || 1
    setConfig(prev => {
      if (!prev) return prev
      const mcps = (prev.tools?.mcp || []).filter((_, i) => i !== index)
      return { ...prev, tools: { ...prev.tools || {}, mcp: mcps } }
    })
    if (activeMcp >= mcpCount - 1) {
      setActiveMcp(Math.max(0, mcpCount - 2))
    }
  }

  // 更新 MCP 配置
  const updateMcp = (index: number, field: string, value: string | number | boolean | string[]) => {
    if (!config) return
    setConfig(prev => {
      if (!prev) return prev
      const mcps = [...(prev.tools?.mcp || [])]
      if (mcps[index]) {
        mcps[index] = { ...mcps[index], [field]: value }
      }
      return { ...prev, tools: { ...prev.tools || {}, mcp: mcps } }
    })
  }

  // 渲染输入框
  const renderInput = (
    label: string,
    value: string | number | boolean | undefined,
    onChange: (value: string | number | boolean) => void,
    type: 'text' | 'number' | 'password' | 'checkbox' | 'select' = 'text',
    placeholder?: string,
    desc?: string,
    options?: string[]
  ) => (
    <div className="mb-3">
      <label className="block text-sm font-body text-ocean-depth/70 mb-1">{label}</label>
      {desc && <p className="text-xs text-ocean-depth/50 mb-1">{desc}</p>}
      {type === 'checkbox' ? (
        <input
          type="checkbox"
          checked={value as boolean}
          onChange={e => onChange(e.target.checked)}
          className="w-4 h-4 rounded border-cyan-electric/30 text-cyan-electric focus:ring-cyan-electric"
        />
      ) : type === 'select' ? (
        <select
          value={value as string}
          onChange={e => onChange(e.target.value)}
          className="w-full px-3 py-2 rounded-lg bg-ocean-depth/5 border border-cyan-electric/20 text-ocean-deep font-body text-sm focus:outline-none focus:border-cyan-electric/40"
        >
          {options?.map(opt => (
            <option key={opt} value={opt}>{opt}</option>
          ))}
        </select>
      ) : (
        <input
          type={type}
          value={value as string | number}
          onChange={e => onChange(type === 'number' ? parseInt(e.target.value) || 0 : e.target.value)}
          placeholder={placeholder}
          className="w-full px-3 py-2 rounded-lg bg-ocean-depth/5 border border-cyan-electric/20 text-ocean-deep font-body text-sm focus:outline-none focus:border-cyan-electric/40"
        />
      )}
    </div>
  )

  // 渲染文本数组输入（逗号分隔）
  const renderArrayInput = (
    label: string,
    value: string[] | undefined,
    onChange: (value: string[]) => void,
    placeholder?: string,
    desc?: string
  ) => (
    <div className="mb-3">
      <label className="block text-sm font-body text-ocean-depth/70 mb-1">{label}</label>
      {desc && <p className="text-xs text-ocean-depth/50 mb-1">{desc}</p>}
      <input
        type="text"
        value={(value || []).join(', ')}
        onChange={e => onChange(e.target.value.split(',').map(s => s.trim()).filter(Boolean))}
        placeholder={placeholder}
        className="w-full px-3 py-2 rounded-lg bg-ocean-depth/5 border border-cyan-electric/20 text-ocean-deep font-body text-sm focus:outline-none focus:border-cyan-electric/40"
      />
    </div>
  )

  // 渲染 Providers 部分
  const renderProviders = () => {
    if (!config?.providers) return null
    const providerNames = Object.keys(config.providers)

    return (
      <div className="space-y-4">
        {/* Default Provider */}
        {renderInput(
          'Default Provider',
          config.default_provider,
          v => updateTopField('default_provider', v as string),
          'select',
          '',
          '默认使用的 LLM 提供商',
          ['', ...providerNames]
        )}

        {/* Provider Tabs */}
        <div className="flex gap-2 mb-4 flex-wrap items-center">
          {providerNames.map(name => (
            <div key={name} className="flex items-center">
              <button
                onClick={() => setActiveProvider(name)}
                className={`px-3 py-1 rounded-l-lg text-sm font-body transition-colors ${
                  activeProvider === name
                    ? 'bg-cyan-electric/20 text-cyan-electric border border-cyan-electric/40'
                    : 'bg-ocean-depth/5 text-ocean-depth/70 hover:bg-ocean-depth/10 border border-transparent'
                }`}
              >
                {name}
              </button>
              <button
                onClick={() => removeProvider(name)}
                className="px-2 py-1 rounded-r-lg text-ocean-depth/40 hover:text-red-500 hover:bg-red-500/10 transition-colors"
                title="Delete"
              >
                <Trash2 size={14} />
              </button>
            </div>
          ))}
          <button
            onClick={addProvider}
            className="px-3 py-1 rounded-lg text-sm font-body bg-cyan-electric/10 text-cyan-electric hover:bg-cyan-electric/20 transition-colors flex items-center gap-1"
          >
            <Plus size={14} /> Add
          </button>
        </div>

        {/* Active Provider Config */}
        {activeProvider && config.providers[activeProvider] && (
          <div className="p-4 rounded-lg bg-ocean-depth/5 border border-cyan-electric/10">
            {renderInput(
              'API Key',
              config.providers[activeProvider].api_key,
              v => updateProvider(activeProvider, 'api_key', v as string),
              'password',
              'sk-...',
              'LLM API 密钥'
            )}
            {renderInput(
              'API Base URL',
              config.providers[activeProvider].api_base_url,
              v => updateProvider(activeProvider, 'api_base_url', v as string),
              'text',
              'https://api.openai.com/v1',
              'API 请求地址'
            )}
            {renderInput(
              'Default Model',
              config.providers[activeProvider].default_model,
              v => updateProvider(activeProvider, 'default_model', v as string),
              'text',
              'gpt-4',
              '该提供商默认使用的模型'
            )}
          </div>
        )}
      </div>
    )
  }

  // 渲染 Agents 部分
  const renderAgents = () => {
    if (!config?.agents) return null

    return (
      <div className="space-y-4">
        {/* Agent Tabs */}
        <div className="flex gap-2 mb-4 flex-wrap items-center">
          {config.agents.map((agent, index) => (
            <div key={agent.name} className="flex items-center">
              <button
                onClick={() => setActiveAgent(index)}
                className={`px-3 py-1 rounded-l-lg text-sm font-body transition-colors ${
                  activeAgent === index
                    ? 'bg-cyan-electric/20 text-cyan-electric border border-cyan-electric/40'
                    : 'bg-ocean-depth/5 text-ocean-depth/70 hover:bg-ocean-depth/10 border border-transparent'
                }`}
              >
                {agent.name}
              </button>
              <button
                onClick={() => removeAgent(index)}
                className="px-2 py-1 rounded-r-lg text-ocean-depth/40 hover:text-red-500 hover:bg-red-500/10 transition-colors"
                title="Delete"
              >
                <Trash2 size={14} />
              </button>
            </div>
          ))}
          <button
            onClick={addAgent}
            className="px-3 py-1 rounded-lg text-sm font-body bg-cyan-electric/10 text-cyan-electric hover:bg-cyan-electric/20 transition-colors flex items-center gap-1"
          >
            <Plus size={14} /> Add
          </button>
        </div>

        {/* Active Agent Config */}
        {config.agents[activeAgent] && (
          <div className="p-4 rounded-lg bg-ocean-depth/5 border border-cyan-electric/10">
            <div className="grid grid-cols-2 gap-4">
              <div>
                {renderInput('Name', config.agents[activeAgent].name, v => updateAgent(activeAgent, 'name', v as string), 'text', '', 'Agent 名称（唯一标识）')}
              </div>
              <div>
                {renderInput('Type', config.agents[activeAgent].type, v => updateAgent(activeAgent, 'type', v as string), 'select', '', 'deep=规划+工具, chatmodel=基础对话', ['deep', 'chatmodel', 'planexecute', 'supervisor'])}
              </div>
            </div>
            {renderInput('Description', config.agents[activeAgent].description || '', v => updateAgent(activeAgent, 'description', v as string), 'text', '', 'Agent 描述')}
            <div className="grid grid-cols-2 gap-4">
              <div>
                {renderInput('Workspace', config.agents[activeAgent].workspace, v => updateAgent(activeAgent, 'workspace', v as string), 'text', '', '工作目录（必需）')}
              </div>
              <div>
                {renderInput('Provider', config.agents[activeAgent].provider || '', v => updateAgent(activeAgent, 'provider', v as string), 'select', '', 'LLM 提供商', ['', ...Object.keys(config.providers || {})])}
              </div>
            </div>
            <div className="grid grid-cols-2 gap-4">
              <div>
                {renderInput('Model', config.agents[activeAgent].model || '', v => updateAgent(activeAgent, 'model', v as string), 'text', '', '模型名称')}
              </div>
              <div>
                {renderInput('Max Iteration', config.agents[activeAgent].max_iteration || 10, v => updateAgent(activeAgent, 'max_iteration', v as number), 'number', '', '最大迭代次数')}
              </div>
            </div>
            <div className="grid grid-cols-2 gap-4">
              <div className="flex items-center gap-2 pt-2">
                {renderInput('Streaming', config.agents[activeAgent].streaming ?? true, v => updateAgent(activeAgent, 'streaming', v as boolean), 'checkbox', '', '流式输出')}
              </div>
              <div className="flex items-center gap-2 pt-2">
                {renderInput('Memoria', config.agents[activeAgent].memoria_enabled ?? true, v => updateAgent(activeAgent, 'memoria_enabled', v as boolean), 'checkbox', '', '启用记忆系统')}
              </div>
            </div>
            {renderArrayInput('Tools', config.agents[activeAgent].tools, v => updateAgent(activeAgent, 'tools', v), 'tool1, tool2', '可用工具列表（空=全部可用）')}
            {renderArrayInput('Tools Approval', config.agents[activeAgent].tools_approval, v => updateAgent(activeAgent, 'tools_approval', v), 'tool1, tool2', '需要审批的工具')}
            {renderArrayInput('Sub Agents', config.agents[activeAgent].sub_agents, v => updateAgent(activeAgent, 'sub_agents', v), 'agent1, agent2', '子 Agent 名称')}
          </div>
        )}
      </div>
    )
  }

  // 渲染 Tools 部分
  const renderTools = () => {
    const tools = config?.tools || {}

    return (
      <div className="space-y-4">
        {/* Approval */}
        <div className="p-4 rounded-lg bg-ocean-depth/5 border border-cyan-electric/10">
          <h4 className="font-display text-ocean-deep mb-3">Tool Approval</h4>
          {renderArrayInput('Approval List', tools.approval || [], v => {
            setConfig(prev => prev ? { ...prev, tools: { ...prev.tools || {}, approval: v } } : prev)
          }, 'tool1, tool2', '需要审批的工具名称列表')}
        </div>

        {/* Browser */}
        <div className="p-4 rounded-lg bg-ocean-depth/5 border border-cyan-electric/10">
          <h4 className="font-display text-ocean-deep mb-3">Browser Tool</h4>
          <div className="grid grid-cols-2 gap-4">
            <div className="flex items-center gap-2">
              {renderInput('Enabled', tools.browser?.enabled ?? false, v => {
                setConfig(prev => prev ? { ...prev, tools: { ...prev.tools || {}, browser: { ...prev.tools?.browser || {}, enabled: v as boolean } } } : prev)
              }, 'checkbox', '', '启用浏览器工具')}
            </div>
            <div className="flex items-center gap-2">
              {renderInput('Headless', tools.browser?.headless ?? true, v => {
                setConfig(prev => prev ? { ...prev, tools: { ...prev.tools || {}, browser: { ...prev.tools?.browser || {}, headless: v as boolean } } } : prev)
              }, 'checkbox', '', '无头模式')}
            </div>
          </div>
          <div className="grid grid-cols-2 gap-4">
            {renderInput('Timeout', tools.browser?.timeout || 30, v => {
              setConfig(prev => prev ? { ...prev, tools: { ...prev.tools || {}, browser: { ...prev.tools?.browser || {}, timeout: v as number } } } : prev)
            }, 'number', '', '超时时间（秒）')}
            {renderInput('Relay Mode', tools.browser?.relay_mode || 'auto', v => {
              setConfig(prev => prev ? { ...prev, tools: { ...prev.tools || {}, browser: { ...prev.tools?.browser || {}, relay_mode: v as string } } } : prev)
            }, 'select', '', '连接模式', ['auto', 'direct', 'relay'])}
          </div>
          {renderInput('Relay URL', tools.browser?.relay_url || '', v => {
            setConfig(prev => prev ? { ...prev, tools: { ...prev.tools || {}, browser: { ...prev.tools?.browser || {}, relay_url: v as string } } } : prev)
          }, 'text', '', 'Relay 服务地址')}
        </div>

        {/* Web */}
        <div className="p-4 rounded-lg bg-ocean-depth/5 border border-cyan-electric/10">
          <h4 className="font-display text-ocean-deep mb-3">Web Tool</h4>
          <div className="grid grid-cols-2 gap-4">
            <div className="flex items-center gap-2">
              {renderInput('Enabled', tools.web?.enabled ?? false, v => {
                setConfig(prev => prev ? { ...prev, tools: { ...prev.tools || {}, web: { ...prev.tools?.web || {}, enabled: v as boolean } } } : prev)
              }, 'checkbox', '', '启用 Web 搜索')}
            </div>
            {renderInput('Search Engine', tools.web?.search_engine || 'tavily', v => {
              setConfig(prev => prev ? { ...prev, tools: { ...prev.tools || {}, web: { ...prev.tools?.web || {}, search_engine: v as string } } } : prev)
            }, 'select', '', '搜索引擎', ['tavily', 'serper', 'google'])}
          </div>
          {renderInput('Search API Key', tools.web?.search_api_key || '', v => {
            setConfig(prev => prev ? { ...prev, tools: { ...prev.tools || {}, web: { ...prev.tools?.web || {}, search_api_key: v as string } } } : prev)
          }, 'password', '', '搜索 API Key')}
          {renderInput('Timeout', tools.web?.timeout || 10, v => {
            setConfig(prev => prev ? { ...prev, tools: { ...prev.tools || {}, web: { ...prev.tools?.web || {}, timeout: v as number } } } : prev)
          }, 'number', '', '请求超时（秒）')}
        </div>

        {/* MCP */}
        <div className="p-4 rounded-lg bg-ocean-depth/5 border border-cyan-electric/10">
          <h4 className="font-display text-ocean-deep mb-3">MCP Tools</h4>
          <div className="flex gap-2 mb-4 flex-wrap items-center">
            {(tools.mcp || []).map((mcp, index) => (
              <div key={mcp.name} className="flex items-center">
                <button
                  onClick={() => setActiveMcp(index)}
                  className={`px-3 py-1 rounded-l-lg text-sm font-body transition-colors ${
                    activeMcp === index
                      ? 'bg-cyan-electric/20 text-cyan-electric border border-cyan-electric/40'
                      : 'bg-ocean-depth/5 text-ocean-depth/70 hover:bg-ocean-depth/10 border border-transparent'
                  }`}
                >
                  {mcp.name}
                </button>
                <button
                  onClick={() => removeMcp(index)}
                  className="px-2 py-1 rounded-r-lg text-ocean-depth/40 hover:text-red-500 hover:bg-red-500/10 transition-colors"
                  title="Delete"
                >
                  <Trash2 size={14} />
                </button>
              </div>
            ))}
            <button
              onClick={addMcp}
              className="px-3 py-1 rounded-lg text-sm font-body bg-cyan-electric/10 text-cyan-electric hover:bg-cyan-electric/20 transition-colors flex items-center gap-1"
            >
              <Plus size={14} /> Add
            </button>
          </div>

          {(tools.mcp || [])[activeMcp] && (
            <div className="p-3 rounded-lg bg-ocean-depth/3 border border-cyan-electric/5">
              <div className="grid grid-cols-2 gap-4">
                {renderInput('Name', tools.mcp?.[activeMcp]?.name || '', v => updateMcp(activeMcp, 'name', v as string), 'text', '', 'MCP 名称')}
                {renderInput('Type', tools.mcp?.[activeMcp]?.type || 'stdio', v => updateMcp(activeMcp, 'type', v as string), 'select', '', '连接类型', ['stdio', 'sse'])}
              </div>
              <div className="grid grid-cols-2 gap-4">
                <div className="flex items-center gap-2">
                  {renderInput('Enabled', tools.mcp?.[activeMcp]?.enabled ?? true, v => updateMcp(activeMcp, 'enabled', v as boolean), 'checkbox', '', '启用')}
                </div>
                {renderInput('Init Timeout', tools.mcp?.[activeMcp]?.init_timeout || 30, v => updateMcp(activeMcp, 'init_timeout', v as number), 'number', '', '初始化超时')}
              </div>
              {tools.mcp?.[activeMcp]?.type === 'sse' && (
                renderInput('URL', tools.mcp?.[activeMcp]?.url || '', v => updateMcp(activeMcp, 'url', v as string), 'text', '', 'SSE 连接地址')
              )}
              {tools.mcp?.[activeMcp]?.type === 'stdio' && (
                <>
                  {renderInput('Command', tools.mcp?.[activeMcp]?.command || '', v => updateMcp(activeMcp, 'command', v as string), 'text', '', '执行命令')}
                  {renderArrayInput('Args', tools.mcp?.[activeMcp]?.args, v => updateMcp(activeMcp, 'args', v), 'arg1, arg2', '命令参数')}
                </>
              )}
              {renderArrayInput('Tools', tools.mcp?.[activeMcp]?.tools, v => updateMcp(activeMcp, 'tools', v), 'tool1, tool2', '要加载的工具（空=全部）')}
            </div>
          )}
        </div>
      </div>
    )
  }

  // 渲染 WebSocket 部分
  const renderWebSocket = () => {
    const ws = config?.websocket || {}

    return (
      <div className="p-4 rounded-lg bg-ocean-depth/5 border border-cyan-electric/10">
        <div className="grid grid-cols-2 gap-4">
          <div className="flex items-center gap-2">
            {renderInput('Enabled', ws.enabled ?? true, v => updateValue('websocket', 'enabled', v), 'checkbox', '', '启用 WebSocket')}
          </div>
          {renderInput('Port', ws.port || 8765, v => updateValue('websocket', 'port', v), 'number', '', '端口')}
        </div>
        <div className="grid grid-cols-2 gap-4">
          {renderInput('Host', ws.host || 'localhost', v => updateValue('websocket', 'host', v), 'text', '', '主机地址')}
          {renderInput('Path', ws.path || '/ws', v => updateValue('websocket', 'path', v), 'text', '', '路径')}
        </div>
        {renderInput('Auth Token', ws.auth_token || '', v => updateValue('websocket', 'auth_token', v), 'password', '', '认证 Token（可选）')}
        <div className="grid grid-cols-2 gap-4">
          {renderInput('Read Timeout', ws.read_timeout || 60, v => updateValue('websocket', 'read_timeout', v), 'number', '', '读超时')}
          {renderInput('Write Timeout', ws.write_timeout || 60, v => updateValue('websocket', 'write_timeout', v), 'number', '', '写超时')}
        </div>
      </div>
    )
  }

  // 渲染 Log 部分
  const renderLog = () => {
    const log = config?.log || {}

    return (
      <div className="p-4 rounded-lg bg-ocean-depth/5 border border-cyan-electric/10">
        <div className="grid grid-cols-2 gap-4">
          {renderInput('Level', log.level || 'info', v => updateValue('log', 'level', v), 'select', '', '日志级别', ['debug', 'info', 'warn', 'error'])}
          {renderInput('Max Size', log.max_size || 100, v => updateValue('log', 'max_size', v), 'number', '', '单文件最大 MB')}
        </div>
        {renderInput('File', log.file || '', v => updateValue('log', 'file', v), 'text', '', '日志文件路径（空=stdout）')}
        <div className="grid grid-cols-2 gap-4">
          {renderInput('Max Backups', log.max_backups || 3, v => updateValue('log', 'max_backups', v), 'number', '', '保留文件数')}
          {renderInput('Max Age', log.max_age || 7, v => updateValue('log', 'max_age', v), 'number', '', '保留天数')}
        </div>
        <div className="flex items-center gap-2">
          {renderInput('Compress', log.compress ?? false, v => updateValue('log', 'compress', v), 'checkbox', '', '压缩旧日志')}
        </div>
      </div>
    )
  }

  // 渲染 Scheduler 部分
  const renderScheduler = () => {
    const scheduler = config?.scheduler || {}

    return (
      <div className="p-4 rounded-lg bg-ocean-depth/5 border border-cyan-electric/10">
        <div className="grid grid-cols-2 gap-4">
          <div className="flex items-center gap-2">
            {renderInput('Enabled', scheduler.enabled ?? true, v => updateValue('scheduler', 'enabled', v), 'checkbox', '', '启用调度器')}
          </div>
          <div className="flex items-center gap-2">
            {renderInput('Persist State', scheduler.persist_state ?? true, v => updateValue('scheduler', 'persist_state', v), 'checkbox', '', '持久化状态')}
          </div>
        </div>
        {renderInput('Check Interval', scheduler.check_interval || '1m', v => updateValue('scheduler', 'check_interval', v), 'text', '', '检查间隔（1m, 5m）')}
      </div>
    )
  }

  // 渲染配置分组
  const renderSection = (section: { key: string; label: string; icon: typeof Server; desc: string }) => {
    const isExpanded = expandedSections.has(section.key)
    const Icon = section.icon

    return (
      <div key={section.key} className="mb-4">
        <motion.button
          onClick={() => toggleSection(section.key)}
          className="w-full flex items-center gap-3 px-4 py-3 rounded-lg bg-ocean-depth/5 hover:bg-ocean-depth/10 transition-colors"
          whileHover={{ scale: 1.01 }}
        >
          <Icon size={20} className="text-cyan-electric" />
          <div className="flex-1">
            <span className="font-display text-ocean-deep">{section.label}</span>
            <p className="text-xs text-ocean-depth/50">{section.desc}</p>
          </div>
          {isExpanded ? (
            <ChevronDown size={16} className="text-cyan-electric" />
          ) : (
            <ChevronRight size={16} className="text-cyan-electric" />
          )}
        </motion.button>

        <AnimatePresence>
          {isExpanded && (
            <motion.div
              initial={{ height: 0, opacity: 0 }}
              animate={{ height: 'auto', opacity: 1 }}
              exit={{ height: 0, opacity: 0 }}
              className="overflow-hidden"
            >
              <div className="px-4 py-4 mt-2">
                {section.key === 'providers' && renderProviders()}
                {section.key === 'agents' && renderAgents()}
                {section.key === 'tools' && renderTools()}
                {section.key === 'websocket' && renderWebSocket()}
                {section.key === 'log' && renderLog()}
                {section.key === 'scheduler' && renderScheduler()}
              </div>
            </motion.div>
          )}
        </AnimatePresence>
      </div>
    )
  }

  return (
    <AnimatePresence>
      {isOpen && (
        <motion.div
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          exit={{ opacity: 0 }}
          className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-ocean-depth/40 backdrop-blur-sm"
          onClick={onClose}
        >
          <motion.div
            initial={{ scale: 0.9, opacity: 0 }}
            animate={{ scale: 1, opacity: 1 }}
            exit={{ scale: 0.9, opacity: 0 }}
            className="glass-card-solid w-[90vw] max-w-4xl max-h-[85vh] flex flex-col overflow-hidden"
            onClick={e => e.stopPropagation()}
          >
            {/* Header */}
            <div className="flex items-center justify-between px-6 py-4 border-b border-cyan-electric/15">
              <div className="flex items-center gap-3">
                <Settings size={24} className="text-cyan-electric" />
                <h2 className="font-display font-semibold text-ocean-deep text-lg">Settings</h2>
              </div>
              <motion.button
                onClick={onClose}
                className="btn-glass p-2 rounded-lg"
                whileHover={{ scale: 1.05 }}
                whileTap={{ scale: 0.95 }}
              >
                <X size={20} className="text-cyan-electric" />
              </motion.button>
            </div>

            {/* Content */}
            <div className="flex-1 overflow-y-auto px-6 py-6">
              {isLoading ? (
                <div className="flex items-center justify-center h-full">
                  <RefreshCw size={32} className="text-cyan-electric animate-spin" />
                </div>
              ) : config ? (
                <div className="space-y-2">
                  {configSections.map(renderSection)}
                </div>
              ) : (
                <div className="flex flex-col items-center justify-center h-full">
                  <AlertCircle size={32} className="text-ocean-depth/30" />
                  <p className="text-sm text-ocean-depth/50 mt-2">Failed to load configuration</p>
                </div>
              )}
            </div>

            {/* Footer */}
            <div className="flex items-center justify-between px-6 py-4 border-t border-cyan-electric/15">
              {saveResult && (
                <motion.div
                  initial={{ opacity: 0, x: -10 }}
                  animate={{ opacity: 1, x: 0 }}
                  className={`flex items-center gap-2 ${saveResult.success ? 'text-green-500' : 'text-red-500'}`}
                >
                  {saveResult.success ? <CheckCircle size={18} /> : <AlertCircle size={18} />}
                  <span className="text-sm font-body">{saveResult.message}</span>
                </motion.div>
              )}
              {!saveResult && <div />}
              <div className="flex items-center gap-3">
                <motion.button
                  onClick={loadConfig}
                  className="btn-glass px-4 py-2 rounded-lg flex items-center gap-2"
                  whileHover={{ scale: 1.02 }}
                  whileTap={{ scale: 0.98 }}
                  disabled={isLoading}
                >
                  <RefreshCw size={18} className={`text-cyan-electric ${isLoading ? 'animate-spin' : ''}`} />
                  <span className="text-sm font-body text-cyan-electric">Refresh</span>
                </motion.button>
                <motion.button
                  onClick={handleSave}
                  className="btn-glass px-5 py-2 rounded-lg flex items-center gap-2 bg-cyan-electric/20 border border-cyan-electric/30"
                  whileHover={{ scale: 1.02 }}
                  whileTap={{ scale: 0.98 }}
                  disabled={isSaving || !config}
                >
                  {isSaving ? (
                    <RefreshCw size={18} className="text-cyan-electric animate-spin" />
                  ) : (
                    <Save size={18} className="text-cyan-electric" />
                  )}
                  <span className="text-sm font-body text-cyan-electric font-medium">Save</span>
                </motion.button>
              </div>
            </div>
          </motion.div>
        </motion.div>
      )}
    </AnimatePresence>
  )
}