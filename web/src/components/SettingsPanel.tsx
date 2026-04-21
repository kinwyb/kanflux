import { useState, useEffect } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import { Settings, X, Save, RefreshCw, AlertCircle, CheckCircle } from 'lucide-react'
import { useWebSocketContext } from '../contexts/WebSocketContext'
import type { ConfigGetAckPayload } from '../types'

interface SettingsPanelProps {
  isOpen: boolean
  onClose: () => void
}

export default function SettingsPanel({ isOpen, onClose }: SettingsPanelProps) {
  const { connectionState, fetchConfig, updateConfig } = useWebSocketContext()
  const [configText, setConfigText] = useState<string>('')
  const [isLoading, setIsLoading] = useState(false)
  const [isSaving, setIsSaving] = useState(false)
  const [saveResult, setSaveResult] = useState<{ success: boolean; message: string } | null>(null)
  const [parseError, setParseError] = useState<string | null>(null)

  // Load config when panel opens
  useEffect(() => {
    if (isOpen && connectionState === 'connected') {
      loadConfig()
    }
  }, [isOpen, connectionState])

  const loadConfig = async () => {
    setIsLoading(true)
    setSaveResult(null)
    setParseError(null)
    try {
      const response: ConfigGetAckPayload = await fetchConfig()
      if (response.success && response.config) {
        setConfigText(JSON.stringify(response.config, null, 2))
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
    // Validate JSON first
    try {
      JSON.parse(configText)
      setParseError(null)
    } catch (error) {
      setParseError('Invalid JSON format: ' + (error instanceof Error ? error.message : 'Unknown error'))
      return
    }

    setIsSaving(true)
    setSaveResult(null)
    try {
      const parsedConfig = JSON.parse(configText)
      const response = await updateConfig(parsedConfig)
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

  const handleTextChange = (text: string) => {
    setConfigText(text)
    setSaveResult(null)
    // Try to parse and clear error if valid
    try {
      JSON.parse(text)
      setParseError(null)
    } catch {
      // Don't show error while typing, only on save
    }
  }

  return (
    <AnimatePresence>
      {isOpen && (
        <motion.div
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          exit={{ opacity: 0 }}
          className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-ocean-depth/20 backdrop-blur-sm"
          onClick={onClose}
        >
          <motion.div
            initial={{ scale: 0.9, opacity: 0 }}
            animate={{ scale: 1, opacity: 1 }}
            exit={{ scale: 0.9, opacity: 0 }}
            className="glass-card-solid max-w-2xl w-full max-h-[85vh] flex flex-col overflow-hidden"
            onClick={e => e.stopPropagation()}
          >
            {/* Header */}
            <div className="flex items-center justify-between px-4 py-3 border-b border-cyan-electric/15">
              <div className="flex items-center gap-3">
                <Settings size={20} className="text-cyan-electric" />
                <h2 className="font-display font-semibold text-ocean-deep">Settings</h2>
              </div>
              <motion.button
                onClick={onClose}
                className="btn-glass p-2 rounded-lg"
                whileHover={{ scale: 1.05 }}
                whileTap={{ scale: 0.95 }}
              >
                <X size={16} className="text-cyan-electric" />
              </motion.button>
            </div>

            {/* Content */}
            <div className="flex-1 overflow-hidden px-4 py-4">
              {isLoading ? (
                <div className="flex items-center justify-center h-full">
                  <RefreshCw size={24} className="text-cyan-electric animate-spin" />
                </div>
              ) : (
                <div className="h-full flex flex-col">
                  <label className="text-sm font-body text-ocean-depth/70 mb-2">
                    Configuration (JSON format)
                  </label>
                  <textarea
                    value={configText}
                    onChange={e => handleTextChange(e.target.value)}
                    className="flex-1 w-full p-3 rounded-lg bg-ocean-depth/5 border border-cyan-electric/20 text-ocean-deep font-mono text-sm resize-none focus:outline-none focus:border-cyan-electric/40"
                    placeholder="Loading configuration..."
                    spellCheck={false}
                  />
                  {parseError && (
                    <div className="mt-2 flex items-center gap-2 text-red-500">
                      <AlertCircle size={16} />
                      <span className="text-xs font-body">{parseError}</span>
                    </div>
                  )}
                </div>
              )}
            </div>

            {/* Footer */}
            <div className="flex items-center justify-between px-4 py-3 border-t border-cyan-electric/15">
              {saveResult && (
                <div className={`flex items-center gap-2 ${saveResult.success ? 'text-green-500' : 'text-red-500'}`}>
                  {saveResult.success ? <CheckCircle size={16} /> : <AlertCircle size={16} />}
                  <span className="text-xs font-body">{saveResult.message}</span>
                </div>
              )}
              {!saveResult && <div />}
              <div className="flex items-center gap-3">
                <motion.button
                  onClick={loadConfig}
                  className="btn-glass px-4 py-2 rounded-lg"
                  whileHover={{ scale: 1.02 }}
                  whileTap={{ scale: 0.98 }}
                  disabled={isLoading}
                >
                  <RefreshCw size={16} className={`text-cyan-electric ${isLoading ? 'animate-spin' : ''}`} />
                </motion.button>
                <motion.button
                  onClick={handleSave}
                  className="btn-glass px-4 py-2 rounded-lg flex items-center gap-2 bg-cyan-electric/20"
                  whileHover={{ scale: 1.02 }}
                  whileTap={{ scale: 0.98 }}
                  disabled={isSaving || !configText}
                >
                  {isSaving ? (
                    <RefreshCw size={16} className="text-cyan-electric animate-spin" />
                  ) : (
                    <Save size={16} className="text-cyan-electric" />
                  )}
                  <span className="text-sm font-body text-cyan-electric">Save</span>
                </motion.button>
              </div>
            </div>
          </motion.div>
        </motion.div>
      )}
    </AnimatePresence>
  )
}