// Package types defines public interfaces and types for the knowledge base.
package types

import "context"

// Document represents a document to be stored.
type Document struct {
	ID       string         // Unique identifier
	Content  string         // Document content
	Wing     string         // Top-level category (e.g., "history", "rag")
	Room     string         // Sub-level category (e.g., "daily", "files")
	Source   string         // Source path or identifier
	Metadata map[string]any // Additional metadata
}

// SearchResult represents a search result.
type SearchResult struct {
	ID       string         // Document ID
	Content  string         // Matched content
	Score    float64        // Relevance score (0-1)
	Wing     string         // Wing of the document
	Room     string         // Room of the document
	Source   string         // Source of the document
	Metadata map[string]any // Additional metadata
}

// SearchOptions configures search behavior.
type SearchOptions struct {
	Wing      string  // Filter by wing
	Room      string  // Filter by room
	Limit     int     // Max results to return (default: 10)
	Threshold float64 // Min relevance score (default: 0.3)
}

// Stats holds statistics about the store.
type Stats struct {
	TotalDocuments int    // Total number of documents
	TotalWings     int    // Number of wings
	TotalRooms     int    // Number of rooms
	StorageSize    int64  // Storage size in bytes
	StoreType      string // Type of backend store
}

// Store is the interface for storage backends.
type Store interface {
	Initialize(ctx context.Context) error
	Add(ctx context.Context, docs []*Document) error
	Search(ctx context.Context, query string, opts *SearchOptions) ([]*SearchResult, error)
	SearchByEmbedding(ctx context.Context, embedding []float32, opts *SearchOptions) ([]*SearchResult, error)
	Get(ctx context.Context, id string) (*Document, error)
	Delete(ctx context.Context, id string) error
	DeleteByWing(ctx context.Context, wing string) error
	DeleteByRoom(ctx context.Context, wing, room string) error
	Count(ctx context.Context) (int, error)
	Stats(ctx context.Context) (*Stats, error)
	Close() error
}

// Embedder is the interface for generating text embeddings.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
	Dimension() int
	Model() string
}

// Store type constants
const (
	StoreTypeSQLite = "sqlite"
	StoreTypeJSON   = "json"
)