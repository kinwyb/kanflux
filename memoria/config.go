package memoria

import (
	"errors"
	"time"

	"github.com/kinwyb/kanflux/memoria/types"
)

// Config holds configuration for Memoria service
type Config struct {
	Workspace       string                 `json:"workspace"`
	ScheduleConfig  *ScheduleConfig        `json:"schedule_config"`
	StorageConfig   *types.StorageConfig   `json:"storage_config"`
	ProcessorConfig *types.ProcessorConfig `json:"processor_config"`
	WatchPaths      []types.WatchPath      `json:"watch_paths"`
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

// ApplyOptions applies options to the config
func (c *Config) ApplyOptions(opts ...ConfigOption) *Config {
	for _, opt := range opts {
		opt(c)
	}
	return c
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
	return c.Workspace + "/memoria"
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
