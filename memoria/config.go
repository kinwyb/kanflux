package memoria

import (
	"errors"
	"time"

	"github.com/kinwyb/kanflux/config"
	"github.com/kinwyb/kanflux/memoria/types"
)

// EmbeddingConfig 配置 L3 层的 Embedder
type EmbeddingConfig struct {
	Provider   string `json:"provider"`    // openai, ollama
	Model      string `json:"model"`       // embedding model name
	APIKey     string `json:"api_key"`     // API key
	APIBaseURL string `json:"api_base_url"` // API base URL
}

// Config holds configuration for Memoria service
type Config struct {
	// Enabled 是否启用 Memoria 记忆系统（默认 true）
	Enabled         bool                   `json:"enabled"`
	Workspace       string                 `json:"workspace"`
	ScheduleConfig  *ScheduleConfig        `json:"schedule_config"`
	StorageConfig   *types.StorageConfig   `json:"storage_config"`
	ProcessorConfig *types.ProcessorConfig `json:"processor_config"`
	WatchPaths      []types.WatchPath      `json:"watch_paths"`
	// KnowledgePaths 知识库路径配置（从主配置继承）
	KnowledgePaths  []config.KnowledgePathConfig `json:"knowledge_paths"`
	// Embedding 配置（用于 L3 语义搜索）
	Embedding       *EmbeddingConfig `json:"embedding"`
	// InitialScan 初始化时是否扫描（默认 true）
	InitialScan     bool `json:"initial_scan"`
}

// ScheduleConfig for periodic scheduling
type ScheduleConfig struct {
	Enabled         bool          `json:"enabled"`
	ChatInterval    time.Duration `json:"chat_interval"`
	FileInterval    time.Duration `json:"file_interval"`
	CleanupInterval time.Duration `json:"cleanup_interval"`
}

// DefaultConfig returns default configuration
func DefaultConfig() *Config {
	return &Config{
		Enabled: true, // 默认启用 Memoria
		ScheduleConfig: &ScheduleConfig{
			Enabled:         true,
			ChatInterval:    5 * time.Minute,
			FileInterval:    10 * time.Minute,
			CleanupInterval: 1 * time.Hour,
		},
		StorageConfig: &types.StorageConfig{
			MaxL1Tokens:  120,
			MaxL2Tokens:  500,
			DateFormat:   "2006-01-02",
			EnableBackup: true,
			BackupDir:    "backup",
		},
		ProcessorConfig: &types.ProcessorConfig{
			MaxBatchSize:   50,
			Timeout:        30 * time.Second,
			EnableParallel: true,
			MaxRetries:     3,
		},
		InitialScan: true, // 默认初始化时扫描
	}
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	if c.Workspace == "" {
		return errors.New("workspace is required")
	}
	if c.ScheduleConfig == nil {
		c.ScheduleConfig = DefaultConfig().ScheduleConfig
	}
	if c.StorageConfig == nil {
		c.StorageConfig = DefaultConfig().StorageConfig
	}
	if c.ProcessorConfig == nil {
		c.ProcessorConfig = DefaultConfig().ProcessorConfig
	}
	return nil
}

// ConfigOption is a function that modifies Config
type ConfigOption func(*Config)

// WithWorkspace sets the workspace directory
func WithWorkspace(path string) ConfigOption {
	return func(c *Config) { c.Workspace = path }
}

// WithScheduleConfig sets the schedule config
func WithScheduleConfig(sc *ScheduleConfig) ConfigOption {
	return func(c *Config) { c.ScheduleConfig = sc }
}

// WithStorageConfig sets the storage config
func WithStorageConfig(sc *types.StorageConfig) ConfigOption {
	return func(c *Config) { c.StorageConfig = sc }
}

// WithProcessorConfig sets the processor config
func WithProcessorConfig(pc *types.ProcessorConfig) ConfigOption {
	return func(c *Config) { c.ProcessorConfig = pc }
}

// WithWatchPaths sets the watch paths
func WithWatchPaths(paths []types.WatchPath) ConfigOption {
	return func(c *Config) { c.WatchPaths = paths }
}

// WithKnowledgePaths sets the knowledge paths from main config
func WithKnowledgePaths(paths []config.KnowledgePathConfig) ConfigOption {
	return func(c *Config) { c.KnowledgePaths = paths }
}

// WithInitialScan sets whether to scan on initialization
func WithInitialScan(scan bool) ConfigOption {
	return func(c *Config) { c.InitialScan = scan }
}

// WithEmbedding sets the embedding config for L3 layer
func WithEmbedding(cfg *EmbeddingConfig) ConfigOption {
	return func(c *Config) { c.Embedding = cfg }
}

// ApplyOptions applies options to the config
func (c *Config) ApplyOptions(opts ...ConfigOption) *Config {
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// WithEnabled sets whether Memoria is enabled
func WithEnabled(enabled bool) ConfigOption {
	return func(c *Config) { c.Enabled = enabled }
}

// NewConfig creates a new config with options
func NewConfig(opts ...ConfigOption) *Config {
	c := DefaultConfig()
	return c.ApplyOptions(opts...)
}

// GetDefaultWatchPaths returns default watch paths for a workspace
func GetDefaultWatchPaths(workspace string) []types.WatchPath {
	return []types.WatchPath{
		{
			Path:       workspace + "/.kanflux/sessions/",
			Extensions: []string{".jsonl"},
			Recursive:  false,
		},
		{
			Path:       workspace + "/docs/",
			Extensions: []string{".md", ".txt"},
			Recursive:  true,
		},
		{
			Path:       workspace + "/memory/",
			Extensions: []string{".md"},
			Recursive:  true,
		},
	}
}

// GetMemoriaDir returns the memoria storage directory
func (c *Config) GetMemoriaDir() string {
	return c.Workspace + "/.kanflux/memoria"
}

// GetL1Dir returns the L1 storage directory
func (c *Config) GetL1Dir() string {
	return c.GetMemoriaDir() + "/l1"
}

// GetL2Dir returns the L2 storage directory
func (c *Config) GetL2Dir() string {
	return c.GetMemoriaDir() + "/l2"
}

// GetBackupDir returns the backup directory
func (c *Config) GetBackupDir() string {
	return c.GetMemoriaDir() + "/" + c.StorageConfig.BackupDir
}

// GetMetadataDir returns the metadata directory
func (c *Config) GetMetadataDir() string {
	return c.GetMemoriaDir() + "/metadata"
}

// GetSessionDir returns the session directory
func (c *Config) GetSessionDir() string {
	if c.ProcessorConfig.SessionDir != "" {
		return c.ProcessorConfig.SessionDir
	}
	return c.Workspace + "/.kanflux/sessions"
}

// GetKnowledgeWatchPaths converts KnowledgePathConfig to WatchPath format
func (c *Config) GetKnowledgeWatchPaths() []types.WatchPath {
	result := make([]types.WatchPath, 0, len(c.KnowledgePaths))

	for _, kp := range c.KnowledgePaths {
		// 处理相对路径：如果不是绝对路径，则相对于 workspace
		path := kp.Path
		if path[0] != '/' && path[0] != '~' {
			path = c.Workspace + "/" + path
		}

		// 转换扩展名格式：添加 "."
		extensions := make([]string, len(kp.Extensions))
		for i, ext := range kp.Extensions {
			if ext[0] != '.' {
				extensions[i] = "." + ext
			} else {
				extensions[i] = ext
			}
		}

		result = append(result, types.WatchPath{
			Path:       path,
			Extensions: extensions,
			Recursive:  kp.Recursive,
			Exclude:    kp.Exclude,
		})
	}

	return result
}

// GetAllWatchPaths returns combined watch paths (KnowledgePaths + default paths)
func (c *Config) GetAllWatchPaths() []types.WatchPath {
	result := c.GetKnowledgeWatchPaths()

	// 如果没有配置知识库路径，使用默认路径
	if len(result) == 0 {
		result = GetDefaultWatchPaths(c.Workspace)
	}

	return result
}
