package knowledgebase

import "errors"

// Store type constants
const (
	StoreTypeSQLite = "sqlite" // SQLite + FTS5 (default, recommended)
	StoreTypeJSON   = "json"   // JSON file (simple, no dependencies)
)

// Config holds configuration for the knowledge base.
type Config struct {
	// Workspace is the directory for storing data.
	Workspace string

	// Embedder generates text embeddings.
	Embedder Embedder

	// StoreType specifies the storage backend: "sqlite" (default) or "json".
	StoreType string

	// ChunkSize is the text chunk size for indexing (default: 500).
	ChunkSize int

	// ChunkOverlap is the overlap between chunks (default: 50).
	ChunkOverlap int

	// DefaultLimit is the default max search results (default: 10).
	DefaultLimit int

	// DefaultThreshold is the default min relevance score (default: 0.3).
	DefaultThreshold float64
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
func WithEmbedder(e Embedder) ConfigOption {
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