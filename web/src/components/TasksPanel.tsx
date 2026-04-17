import { useState, useEffect } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import {
  Clock,
  Plus,
  Trash2,
  Play,
  RefreshCw,
  Wifi,
  WifiOff,
  X,
  Check,
  ToggleLeft,
  ToggleRight,
  Edit3,
} from 'lucide-react'
import { format } from 'date-fns'
import { useWebSocket } from '../hooks/useWebSocket'
import type { TaskDetail, TaskConfig } from '../types'

export default function TasksPanel() {
  const {
    connectionState,
    fetchTaskList,
    addTask,
    updateTask,
    removeTask,
    triggerTask,
  } = useWebSocket()
  const [tasks, setTasks] = useState<TaskDetail[]>([])
  const [isLoading, setIsLoading] = useState(false)
  const [showAddModal, setShowAddModal] = useState(false)
  const [editingTask, setEditingTask] = useState<TaskDetail | null>(null)
  const [formData, setFormData] = useState<TaskConfig>({
    id: '',
    name: '',
    enabled: true,
    schedule: { cron: '0 9 * * *' },
    target: { channel: 'tui', account_id: 'default', chat_id: 'main', agent_name: '' },
    content: { prompt: '' },
  })

  const isConnected = connectionState === 'connected'

  // Fetch tasks on mount and when connection state changes
  useEffect(() => {
    if (isConnected) {
      handleRefresh()
    }
  }, [connectionState])

  const handleRefresh = async () => {
    setIsLoading(true)
    const taskList = await fetchTaskList()
    setTasks(taskList)
    setIsLoading(false)
  }

  const handleAddTask = async () => {
    if (!formData.id || !formData.name || !formData.content.prompt) {
      return
    }
    setIsLoading(true)
    const result = await addTask(formData)
    if (result.success) {
      setShowAddModal(false)
      resetForm()
      await handleRefresh()
    } else {
      console.error('Add task failed:', result.error)
    }
    setIsLoading(false)
  }

  const handleUpdateTask = async () => {
    if (!editingTask) return
    setIsLoading(true)
    const result = await updateTask(editingTask.id, {
      name: formData.name,
      enabled: formData.enabled,
      schedule: formData.schedule,
      target: formData.target,
      content: formData.content,
    })
    if (result.success) {
      setEditingTask(null)
      resetForm()
      await handleRefresh()
    } else {
      console.error('Update task failed:', result.error)
    }
    setIsLoading(false)
  }

  const handleRemoveTask = async (id: string) => {
    setIsLoading(true)
    const result = await removeTask(id)
    if (result.success) {
      await handleRefresh()
    } else {
      console.error('Remove task failed:', result.error)
    }
    setIsLoading(false)
  }

  const handleTriggerTask = async (id: string) => {
    setIsLoading(true)
    const result = await triggerTask(id)
    if (result.success) {
      await handleRefresh()
    } else {
      console.error('Trigger task failed:', result.error)
    }
    setIsLoading(false)
  }

  const handleToggleEnabled = async (task: TaskDetail) => {
    setIsLoading(true)
    const result = await updateTask(task.id, { enabled: !task.enabled })
    if (result.success) {
      await handleRefresh()
    } else {
      console.error('Toggle enabled failed:', result.error)
    }
    setIsLoading(false)
  }

  const resetForm = () => {
    setFormData({
      id: '',
      name: '',
      enabled: true,
      schedule: { cron: '0 9 * * *' },
      target: { channel: 'tui', account_id: 'default', chat_id: 'main', agent_name: '' },
      content: { prompt: '' },
    })
  }

  const openEditModal = (task: TaskDetail) => {
    setEditingTask(task)
    setFormData({
      id: task.id,
      name: task.name,
      description: task.description,
      enabled: task.enabled,
      schedule: task.schedule,
      target: task.target,
      content: task.content,
    })
  }

  // Cron expression examples
  const cronExamples = [
    { label: '每天 9:00', value: '0 9 * * *' },
    { label: '每小时', value: '0 * * * *' },
    { label: '每 30 分钟', value: '*/30 * * * *' },
    { label: '工作日 9:00', value: '0 9 * * 1-5' },
    { label: '每天 18:00', value: '0 18 * * *' },
  ]

  // Channel options
  const channelOptions = ['tui', 'wxcom', 'telegram', 'feishu', 'cli', 'discord', 'slack']

  return (
    <div className="glass-card-solid h-full flex flex-col overflow-hidden">
      {/* Header */}
      <div className="flex items-center justify-between px-4 py-3 border-b border-cyan-electric/15">
        <div className="flex items-center gap-3">
          <Clock size={18} className="text-cyan-electric" />
          <h2 className="font-display font-semibold text-ocean-deep">Scheduled Tasks</h2>
          {isConnected ? (
            <Wifi size={14} className="text-green-500" />
          ) : (
            <WifiOff size={14} className="text-red-500" />
          )}
          <span className="text-xs text-ocean-depth/50 font-body">
            {tasks.length} tasks
          </span>
        </div>
        <div className="flex items-center gap-2">
          <motion.button
            onClick={() => {
              resetForm()
              setShowAddModal(true)
            }}
            className="btn-glass px-3 py-2 rounded-lg flex items-center gap-2"
            whileHover={{ scale: 1.05 }}
            whileTap={{ scale: 0.95 }}
            disabled={!isConnected}
          >
            <Plus size={16} className="text-cyan-electric" />
            <span className="text-xs font-body text-cyan-electric">Add</span>
          </motion.button>
          <motion.button
            onClick={handleRefresh}
            className="btn-glass p-2 rounded-lg"
            whileHover={{ scale: 1.05 }}
            whileTap={{ scale: 0.95 }}
            disabled={isLoading || !isConnected}
          >
            <RefreshCw size={16} className={`text-cyan-electric ${isLoading ? 'animate-spin' : ''}`} />
          </motion.button>
        </div>
      </div>

      {/* Connection Status Warning */}
      {!isConnected && (
        <div className="px-4 py-2 bg-yellow-500/10 border-b border-yellow-500/20">
          <p className="text-xs text-yellow-600 font-body flex items-center gap-2">
            <WifiOff size={14} />
            Connecting to gateway...
          </p>
        </div>
      )}

      {/* Task List */}
      <div className="flex-1 overflow-y-auto px-4 py-3">
        {tasks.length === 0 ? (
          <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            className="flex flex-col items-center justify-center h-full text-center"
          >
            <Clock size={32} className="text-ocean-depth/30 mb-3" />
            <p className="text-sm text-ocean-depth/50 font-body">
              {isConnected ? 'No tasks configured' : 'Waiting for connection...'}
            </p>
            {isConnected && (
              <motion.button
                onClick={() => {
                  resetForm()
                  setShowAddModal(true)
                }}
                className="mt-4 btn-glass px-4 py-2 rounded-lg flex items-center gap-2"
                whileHover={{ scale: 1.05 }}
                whileTap={{ scale: 0.95 }}
              >
                <Plus size={16} className="text-cyan-electric" />
                <span className="text-sm font-body text-cyan-electric">Add Task</span>
              </motion.button>
            )}
          </motion.div>
        ) : (
          <div className="space-y-3">
            {tasks.map((task) => (
              <motion.div
                key={task.id}
                initial={{ opacity: 0, y: 10 }}
                animate={{ opacity: 1, y: 0 }}
                className="glass-card p-4"
              >
                <div className="flex items-start justify-between gap-4">
                  {/* Task Info */}
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2 mb-2">
                      <span className="font-mono text-sm text-cyan-electric font-medium">
                        {task.id}
                      </span>
                      <span
                        className={`px-2 py-0.5 rounded text-xs font-body ${
                          task.enabled
                            ? 'bg-green-500/20 text-green-600'
                            : 'bg-ocean-depth/20 text-ocean-depth/50'
                        }`}
                      >
                        {task.enabled ? 'Enabled' : 'Disabled'}
                      </span>
                      {task.is_running && (
                        <span className="px-2 py-0.5 rounded text-xs font-body bg-blue-500/20 text-blue-600">
                          Running
                        </span>
                      )}
                    </div>
                    <h3 className="font-display font-semibold text-ocean-deep mb-1">
                      {task.name}
                    </h3>
                    <div className="flex items-center gap-4 text-xs text-ocean-depth/60 font-body">
                      <span className="flex items-center gap-1">
                        <Clock size={12} className="text-cyan-electric/70" />
                        {task.schedule.cron}
                      </span>
                      <span>
                        → {task.target.channel}:{task.target.chat_id}
                      </span>
                      {task.target.agent_name && (
                        <span className="text-cyan-electric/70">
                          Agent: {task.target.agent_name}
                        </span>
                      )}
                    </div>
                    {/* Next/Last Run */}
                    <div className="mt-2 flex items-center gap-4 text-xs text-ocean-depth/50 font-body">
                      {task.next_run && (
                        <span>
                          Next: {format(new Date(task.next_run), 'MM/dd HH:mm')}
                        </span>
                      )}
                      {task.last_run && (
                        <span>
                          Last: {format(new Date(task.last_run), 'MM/dd HH:mm')}
                        </span>
                      )}
                      {task.state && (
                        <span className="text-ocean-depth/40">
                          ✓{task.state.success_count} ✗{task.state.fail_count}
                        </span>
                      )}
                    </div>
                    {/* Prompt Preview */}
                    <div className="mt-2 p-2 bg-ocean-depth/10 rounded text-xs font-mono text-ocean-depth/70 line-clamp-2">
                      {task.content.prompt}
                    </div>
                  </div>

                  {/* Actions */}
                  <div className="flex items-center gap-1 shrink-0">
                    {/* Toggle Enabled */}
                    <motion.button
                      onClick={() => handleToggleEnabled(task)}
                      className="btn-glass p-2 rounded-lg"
                      whileHover={{ scale: 1.05 }}
                      whileTap={{ scale: 0.95 }}
                      title={task.enabled ? 'Disable' : 'Enable'}
                    >
                      {task.enabled ? (
                        <ToggleRight size={18} className="text-green-500" />
                      ) : (
                        <ToggleLeft size={18} className="text-ocean-depth/50" />
                      )}
                    </motion.button>
                    {/* Trigger */}
                    <motion.button
                      onClick={() => handleTriggerTask(task.id)}
                      className="btn-glass p-2 rounded-lg"
                      whileHover={{ scale: 1.05 }}
                      whileTap={{ scale: 0.95 }}
                      title="Trigger now"
                    >
                      <Play size={18} className="text-cyan-electric" />
                    </motion.button>
                    {/* Edit */}
                    <motion.button
                      onClick={() => openEditModal(task)}
                      className="btn-glass p-2 rounded-lg"
                      whileHover={{ scale: 1.05 }}
                      whileTap={{ scale: 0.95 }}
                      title="Edit"
                    >
                      <Edit3 size={18} className="text-ocean-depth/70" />
                    </motion.button>
                    {/* Delete */}
                    <motion.button
                      onClick={() => handleRemoveTask(task.id)}
                      className="btn-glass p-2 rounded-lg"
                      whileHover={{ scale: 1.05 }}
                      whileTap={{ scale: 0.95 }}
                      title="Delete"
                    >
                      <Trash2 size={18} className="text-red-500/70" />
                    </motion.button>
                  </div>
                </div>
              </motion.div>
            ))}
          </div>
        )}
      </div>

      {/* Add/Edit Task Modal */}
      <AnimatePresence>
        {(showAddModal || editingTask) && (
          <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-ocean-depth/20 backdrop-blur-sm"
            onClick={() => {
              setShowAddModal(false)
              setEditingTask(null)
            }}
          >
            <motion.div
              initial={{ scale: 0.9, opacity: 0 }}
              animate={{ scale: 1, opacity: 1 }}
              exit={{ scale: 0.9, opacity: 0 }}
              className="glass-card-solid max-w-lg w-full max-h-[80vh] flex flex-col overflow-hidden"
              onClick={(e) => e.stopPropagation()}
            >
              {/* Modal Header */}
              <div className="flex items-center justify-between px-4 py-3 border-b border-cyan-electric/15">
                <h3 className="font-display font-semibold text-ocean-deep">
                  {editingTask ? 'Edit Task' : 'Add New Task'}
                </h3>
                <motion.button
                  onClick={() => {
                    setShowAddModal(false)
                    setEditingTask(null)
                  }}
                  className="btn-glass p-2 rounded-lg"
                  whileHover={{ scale: 1.05 }}
                  whileTap={{ scale: 0.95 }}
                >
                  <X size={16} className="text-cyan-electric" />
                </motion.button>
              </div>

              {/* Form */}
              <div className="flex-1 overflow-y-auto px-4 py-4 space-y-4">
                {/* ID */}
                <div>
                  <label className="block text-xs font-body text-ocean-depth/60 mb-1">
                    Task ID *
                  </label>
                  <input
                    type="text"
                    value={formData.id}
                    onChange={(e) => setFormData({ ...formData, id: e.target.value })}
                    className="input-glass w-full text-sm py-2"
                    placeholder="e.g. morning_reminder"
                    disabled={editingTask !== null}
                  />
                </div>

                {/* Name */}
                <div>
                  <label className="block text-xs font-body text-ocean-depth/60 mb-1">
                    Name *
                  </label>
                  <input
                    type="text"
                    value={formData.name}
                    onChange={(e) => setFormData({ ...formData, name: e.target.value })}
                    className="input-glass w-full text-sm py-2"
                    placeholder="e.g. Morning Reminder"
                  />
                </div>

                {/* Enabled */}
                <div className="flex items-center gap-3">
                  <label className="text-xs font-body text-ocean-depth/60">Enabled</label>
                  <motion.button
                    onClick={() => setFormData({ ...formData, enabled: !formData.enabled })}
                    className="btn-glass p-2 rounded-lg"
                    whileTap={{ scale: 0.9 }}
                  >
                    {formData.enabled ? (
                      <ToggleRight size={20} className="text-green-500" />
                    ) : (
                      <ToggleLeft size={20} className="text-ocean-depth/50" />
                    )}
                  </motion.button>
                </div>

                {/* Cron */}
                <div>
                  <label className="block text-xs font-body text-ocean-depth/60 mb-1">
                    Cron Expression *
                  </label>
                  <input
                    type="text"
                    value={formData.schedule.cron}
                    onChange={(e) =>
                      setFormData({
                        ...formData,
                        schedule: { ...formData.schedule, cron: e.target.value },
                      })
                    }
                    className="input-glass w-full text-sm py-2 font-mono"
                    placeholder="0 9 * * *"
                  />
                  {/* Cron Examples */}
                  <div className="mt-2 flex flex-wrap gap-2">
                    {cronExamples.map((ex) => (
                      <motion.button
                        key={ex.value}
                        onClick={() =>
                          setFormData({
                            ...formData,
                            schedule: { ...formData.schedule, cron: ex.value },
                          })
                        }
                        className="btn-glass px-2 py-1 rounded text-xs font-body"
                        whileHover={{ scale: 1.02 }}
                        whileTap={{ scale: 0.98 }}
                      >
                        {ex.label}
                      </motion.button>
                    ))}
                  </div>
                </div>

                {/* Channel */}
                <div>
                  <label className="block text-xs font-body text-ocean-depth/60 mb-1">
                    Channel *
                  </label>
                  <select
                    value={formData.target.channel}
                    onChange={(e) =>
                      setFormData({
                        ...formData,
                        target: { ...formData.target, channel: e.target.value },
                      })
                    }
                    className="input-glass w-full text-sm py-2"
                  >
                    {channelOptions.map((ch) => (
                      <option key={ch} value={ch}>
                        {ch}
                      </option>
                    ))}
                  </select>
                </div>

                {/* Account ID */}
                <div>
                  <label className="block text-xs font-body text-ocean-depth/60 mb-1">
                    Account ID
                  </label>
                  <input
                    type="text"
                    value={formData.target.account_id || ''}
                    onChange={(e) =>
                      setFormData({
                        ...formData,
                        target: { ...formData.target, account_id: e.target.value },
                      })
                    }
                    className="input-glass w-full text-sm py-2"
                    placeholder="default"
                  />
                </div>

                {/* Chat ID */}
                <div>
                  <label className="block text-xs font-body text-ocean-depth/60 mb-1">
                    Chat ID *
                  </label>
                  <input
                    type="text"
                    value={formData.target.chat_id}
                    onChange={(e) =>
                      setFormData({
                        ...formData,
                        target: { ...formData.target, chat_id: e.target.value },
                      })
                    }
                    className="input-glass w-full text-sm py-2"
                    placeholder="main"
                  />
                </div>

                {/* Agent Name */}
                <div>
                  <label className="block text-xs font-body text-ocean-depth/60 mb-1">
                    Agent Name (optional)
                  </label>
                  <input
                    type="text"
                    value={formData.target.agent_name || ''}
                    onChange={(e) =>
                      setFormData({
                        ...formData,
                        target: { ...formData.target, agent_name: e.target.value },
                      })
                    }
                    className="input-glass w-full text-sm py-2"
                    placeholder="assistant"
                  />
                </div>

                {/* Prompt */}
                <div>
                  <label className="block text-xs font-body text-ocean-depth/60 mb-1">
                    Prompt *
                  </label>
                  <textarea
                    value={formData.content.prompt}
                    onChange={(e) =>
                      setFormData({
                        ...formData,
                        content: { ...formData.content, prompt: e.target.value },
                      })
                    }
                    className="input-glass w-full text-sm py-2 min-h-[100px] resize-y"
                    placeholder="Enter the prompt that the agent will process..."
                  />
                </div>
              </div>

              {/* Footer */}
              <div className="flex items-center justify-end gap-3 px-4 py-3 border-t border-cyan-electric/15">
                <motion.button
                  onClick={() => {
                    setShowAddModal(false)
                    setEditingTask(null)
                  }}
                  className="btn-glass px-4 py-2 rounded-lg"
                  whileHover={{ scale: 1.02 }}
                  whileTap={{ scale: 0.98 }}
                >
                  <span className="text-sm font-body text-ocean-depth/70">Cancel</span>
                </motion.button>
                <motion.button
                  onClick={editingTask ? handleUpdateTask : handleAddTask}
                  className="btn-glass px-4 py-2 rounded-lg flex items-center gap-2 bg-cyan-electric/20"
                  whileHover={{ scale: 1.02 }}
                  whileTap={{ scale: 0.98 }}
                  disabled={isLoading || !formData.id || !formData.name || !formData.content.prompt}
                >
                  {isLoading ? (
                    <RefreshCw size={16} className="text-cyan-electric animate-spin" />
                  ) : (
                    <Check size={16} className="text-cyan-electric" />
                  )}
                  <span className="text-sm font-body text-cyan-electric">
                    {editingTask ? 'Update' : 'Add'}
                  </span>
                </motion.button>
              </div>
            </motion.div>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  )
}