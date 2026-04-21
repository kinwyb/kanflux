package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
)

// Manager 配置管理器，负责配置的读写和验证
type Manager struct {
	configPath string
	config     *Config
	mu         sync.RWMutex
	logger     *slog.Logger
}

// NewManager 创建配置管理器
func NewManager(configPath string) (*Manager, error) {
	cfg, err := Load(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}
	return &Manager{
		configPath: configPath,
		config:     cfg,
		logger:     slog.Default().With("component", "config-manager"),
	}, nil
}

// GetConfig 获取当前配置
func (m *Manager) GetConfig() *Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config
}

// GetConfigJSON 获取配置的 JSON 格式
func (m *Manager) GetConfigJSON() ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return json.MarshalIndent(m.config, "", "  ")
}

// UpdateConfig 更新配置
func (m *Manager) UpdateConfig(newConfigJSON []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 1. 验证新配置格式
	var newConfig Config
	if err := json.Unmarshal(newConfigJSON, &newConfig); err != nil {
		return fmt.Errorf("invalid config format: %w", err)
	}

	// 2. 验证配置内容
	if err := m.validateConfig(&newConfig); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}

	// 3. 备份当前配置文件
	if err := m.backupConfig(); err != nil {
		m.logger.Warn("failed to backup config", "error", err)
	}

	// 4. 原子写入新配置文件
	if err := m.atomicWriteFile(m.configPath, newConfigJSON); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	// 5. 更新内存中的配置
	m.config = &newConfig

	m.logger.Info("config updated successfully")
	return nil
}

// validateConfig 验证配置内容
func (m *Manager) validateConfig(cfg *Config) error {
	// 验证 default_provider 是否存在
	if cfg.DefaultProvider != "" {
		if _, ok := cfg.Providers[cfg.DefaultProvider]; !ok {
			return fmt.Errorf("default_provider '%s' not found in providers", cfg.DefaultProvider)
		}
	}

	// 验证 agents
	for _, agent := range cfg.Agents {
		if agent.Name == "" {
			return fmt.Errorf("agent name is required")
		}
		if agent.Workspace == "" {
			return fmt.Errorf("agent '%s': workspace is required", agent.Name)
		}
		// 验证 agent 的 provider 引用
		if agent.Provider != "" {
			if _, ok := cfg.Providers[agent.Provider]; !ok {
				return fmt.Errorf("agent '%s': provider '%s' not found", agent.Name, agent.Provider)
			}
		}
	}

	return nil
}

// backupConfig 备份当前配置
func (m *Manager) backupConfig() error {
	backupPath := m.configPath + ".bak"
	currentJSON, err := json.MarshalIndent(m.config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config for backup: %w", err)
	}
	return os.WriteFile(backupPath, currentJSON, 0644)
}

// atomicWriteFile 原子写入文件
func (m *Manager) atomicWriteFile(path string, data []byte) error {
	tempPath := path + ".tmp"
	if err := os.WriteFile(tempPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}
	// 确保临时文件写入成功后再重命名
	if err := os.Rename(tempPath, path); err != nil {
		// 尝试清理临时文件
		os.Remove(tempPath)
		return fmt.Errorf("failed to rename temp file: %w", err)
	}
	return nil
}

// Reload 重新加载配置文件
func (m *Manager) Reload() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cfg, err := Load(m.configPath)
	if err != nil {
		return fmt.Errorf("failed to reload config: %w", err)
	}
	m.config = cfg
	m.logger.Info("config reloaded successfully")
	return nil
}

// GetConfigPath 获取配置文件路径
func (m *Manager) GetConfigPath() string {
	return m.configPath
}