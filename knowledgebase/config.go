package knowledgebase

import (
	"errors"

	"github.com/kinwyb/kanflux/knowledgebase/types"
)

// Config holds configuration for the knowledge base.
type Config struct {
	Workspace        string         // Directory for storing data
	Embedder         types.Embedder // Embedder for semantic search
	StoreType        string         // Storage backend: "sqlite" (default) or "json"
	ChunkSize        int            // Text chunk size for indexing (default: 500)
	ChunkOverlap     int            // Overlap between chunks (default: 50)
	DefaultLimit     int            // Default max search results (default: 10)
	DefaultThreshold float64        // Default min relevance score (default: 0.3)
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		StoreType:        StoreTypeSQLite,
		ChunkSize:        500,
		ChunkOverlap:     50,
		DefaultLimit:     10,
		DefaultThreshold: 0.3,
	}
}

// Validate checks if the configuration is valid.
func (c *Config) Validate() error {
	if c.Workspace == "" {
		return errors.New("workspace is required")
	}
	return nil
}

// ConfigOption is a function that modifies Config.
type ConfigOption func(*Config)

// WithWorkspace sets the workspace directory.
func WithWorkspace(path string) ConfigOption {
	return func(c *Config) { c.Workspace = path }
}

// WithEmbedder sets the embedder.
func WithEmbedder(e types.Embedder) ConfigOption {
	return func(c *Config) { c.Embedder = e }
}

// WithStoreType sets the storage backend type.
func WithStoreType(storeType string) ConfigOption {
	return func(c *Config) { c.StoreType = storeType }
}

// WithChunkSize sets the chunk size and overlap.
func WithChunkSize(size, overlap int) ConfigOption {
	return func(c *Config) {
		c.ChunkSize = size
		c.ChunkOverlap = overlap
	}
}

// WithSearchDefaults sets the default search parameters.
func WithSearchDefaults(limit int, threshold float64) ConfigOption {
	return func(c *Config) {
		c.DefaultLimit = limit
		c.DefaultThreshold = threshold
	}
}

// ApplyOptions applies options to the config.
func (c *Config) ApplyOptions(opts ...ConfigOption) *Config {
	for _, opt := range opts {
		opt(c)
	}
	return c
}