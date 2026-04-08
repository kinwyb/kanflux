// Package knowledgebase provides a unified knowledge storage and retrieval module.
// It defines public interfaces that can be implemented by various backends
// (SQLite, JSON, etc.) and provides a facade for easy usage.
package knowledgebase

import (
	"context"
	"fmt"
)

// Document represents a document to be stored in the knowledge base.
type Document struct {
	ID       string         // Unique identifier
	Content  string         // Document content
	Wing     string         // Top-level category (e.g., "history", "rag")
	Room     string         // Sub-level category (e.g., "daily", "files")
	Source   string         // Source path or identifier
	Metadata map[string]any // Additional metadata
}

// SearchResult represents a search result from the knowledge base.
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

// Stats holds statistics about the knowledge base.
type Stats struct {
	TotalDocuments int    // Total number of documents
	TotalWings     int    // Number of wings
	TotalRooms     int    // Number of rooms
	StorageSize    int64  // Storage size in bytes
	StoreType      string // Type of backend store
}

// Store is the interface for knowledge base storage backends.
// Implementations can use SQLite, JSON files, or other storage systems.
type Store interface {
	// Initialize prepares the storage backend.
	Initialize(ctx context.Context) error

	// Add stores documents in the knowledge base.
	Add(ctx context.Context, docs []*Document) error

	// Search performs a semantic search.
	Search(ctx context.Context, query string, opts *SearchOptions) ([]*SearchResult, error)

	// SearchByEmbedding performs a search using a pre-computed embedding.
	SearchByEmbedding(ctx context.Context, embedding []float32, opts *SearchOptions) ([]*SearchResult, error)

	// Get retrieves a document by ID.
	Get(ctx context.Context, id string) (*Document, error)

	// Delete removes a document by ID.
	Delete(ctx context.Context, id string) error

	// DeleteByWing removes all documents in a wing.
	DeleteByWing(ctx context.Context, wing string) error

	// DeleteByRoom removes all documents in a wing/room.
	DeleteByRoom(ctx context.Context, wing, room string) error

	// Count returns the total number of documents.
	Count(ctx context.Context) (int, error)

	// Stats returns statistics about the store.
	Stats(ctx context.Context) (*Stats, error)

	// Close releases resources.
	Close() error
}

// Embedder is the interface for generating text embeddings.
type Embedder interface {
	// Embed generates an embedding for a single text.
	Embed(ctx context.Context, text string) ([]float32, error)

	// EmbedBatch generates embeddings for multiple texts.
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)

	// Dimension returns the embedding vector dimension.
	Dimension() int
}

// KnowledgeBase is the main facade for knowledge storage and retrieval.
// It combines a Store and an Embedder to provide a unified interface.
type KnowledgeBase struct {
	store    Store
	embedder Embedder
	config   *Config
}

// New creates a new KnowledgeBase with the given configuration.
func New(cfg *Config) (*KnowledgeBase, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	store, err := createStore(cfg)
	if err != nil {
		return nil, err
	}

	if err := store.Initialize(context.Background()); err != nil {
		return nil, err
	}

	kb := &KnowledgeBase{
		store:    store,
		embedder: cfg.Embedder,
		config:   cfg,
	}

	return kb, nil
}

// createStore creates a store based on configuration.
func createStore(cfg *Config) (Store, error) {
	switch cfg.StoreType {
	case StoreTypeJSON:
		return NewJSONStore(cfg.Workspace, cfg.Embedder)
	case StoreTypeSQLite:
		fallthrough
	default:
		return NewSQLiteStore(cfg.Workspace, cfg.Embedder)
	}
}

// Add stores content in the knowledge base.
func (kb *KnowledgeBase) Add(ctx context.Context, content string, opts ...AddOption) (*AddResult, error) {
	cfg := &addConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	doc := &Document{
		ID:       generateID(cfg.wing, cfg.room, content),
		Content:  content,
		Wing:     cfg.wing,
		Room:     cfg.room,
		Source:   cfg.source,
		Metadata: cfg.metadata,
	}

	if err := kb.store.Add(ctx, []*Document{doc}); err != nil {
		return nil, err
	}

	return &AddResult{
		ID:     doc.ID,
		Wing:   doc.Wing,
		Room:   doc.Room,
		Source: doc.Source,
	}, nil
}

// AddDocument stores a document in the knowledge base.
func (kb *KnowledgeBase) AddDocument(ctx context.Context, doc *Document) error {
	if doc.ID == "" {
		doc.ID = generateID(doc.Wing, doc.Room, doc.Content)
	}
	return kb.store.Add(ctx, []*Document{doc})
}

// Search performs a search in the knowledge base.
func (kb *KnowledgeBase) Search(ctx context.Context, query string, opts ...SearchOption) ([]*SearchResult, error) {
	opt := &searchOpts{}
	for _, o := range opts {
		o(opt)
	}

	options := &SearchOptions{
		Wing:      opt.wing,
		Room:      opt.room,
		Limit:     opt.limit,
		Threshold: opt.threshold,
	}

	if options.Limit <= 0 {
		options.Limit = 10
	}
	if options.Threshold <= 0 {
		options.Threshold = 0.3
	}

	// If we have an embedder, use semantic search
	if kb.embedder != nil {
		embedding, err := kb.embedder.Embed(ctx, query)
		if err == nil {
			return kb.store.SearchByEmbedding(ctx, embedding, options)
		}
	}

	return kb.store.Search(ctx, query, options)
}

// Get retrieves a document by ID.
func (kb *KnowledgeBase) Get(ctx context.Context, id string) (*Document, error) {
	return kb.store.Get(ctx, id)
}

// Delete removes a document by ID.
func (kb *KnowledgeBase) Delete(ctx context.Context, id string) error {
	return kb.store.Delete(ctx, id)
}

// DeleteByWing removes all documents in a wing.
func (kb *KnowledgeBase) DeleteByWing(ctx context.Context, wing string) error {
	return kb.store.DeleteByWing(ctx, wing)
}

// DeleteByRoom removes all documents in a wing/room.
func (kb *KnowledgeBase) DeleteByRoom(ctx context.Context, wing, room string) error {
	return kb.store.DeleteByRoom(ctx, wing, room)
}

// Stats returns statistics about the knowledge base.
func (kb *KnowledgeBase) Stats(ctx context.Context) (*Stats, error) {
	return kb.store.Stats(ctx)
}

// Close closes the knowledge base.
func (kb *KnowledgeBase) Close() error {
	return kb.store.Close()
}

// Store returns the underlying store (for advanced usage).
func (kb *KnowledgeBase) Store() Store {
	return kb.store
}

// ============ Options ============

// AddOption is a functional option for Add operations.
type AddOption func(*addConfig)

type addConfig struct {
	wing     string
	room     string
	source   string
	metadata map[string]any
}

// WithWing sets the wing for the document.
func WithWing(wing string) AddOption {
	return func(c *addConfig) { c.wing = wing }
}

// WithRoom sets the room for the document.
func WithRoom(room string) AddOption {
	return func(c *addConfig) { c.room = room }
}

// WithSource sets the source for the document.
func WithSource(source string) AddOption {
	return func(c *addConfig) { c.source = source }
}

// WithMetadata sets metadata for the document.
func WithMetadata(meta map[string]any) AddOption {
	return func(c *addConfig) { c.metadata = meta }
}

// SearchOption is a functional option for Search operations.
type SearchOption func(*searchOpts)

type searchOpts struct {
	wing      string
	room      string
	limit     int
	threshold float64
}

// WithWingFilter filters by wing.
func WithWingFilter(wing string) SearchOption {
	return func(o *searchOpts) { o.wing = wing }
}

// WithRoomFilter filters by room.
func WithRoomFilter(room string) SearchOption {
	return func(o *searchOpts) { o.room = room }
}

// WithLimit sets the max results.
func WithLimit(limit int) SearchOption {
	return func(o *searchOpts) { o.limit = limit }
}

// WithThreshold sets the min relevance score.
func WithThreshold(threshold float64) SearchOption {
	return func(o *searchOpts) { o.threshold = threshold }
}

// AddResult represents the result of an Add operation.
type AddResult struct {
	ID     string
	Wing   string
	Room   string
	Source string
}

// ============ Helpers ============

func generateID(wing, room, content string) string {
	return fmt.Sprintf("doc_%s_%s_%x", wing, room, hashString(content))
}

func hashString(s string) uint32 {
	h := uint32(0)
	for _, c := range s {
		h = h*31 + uint32(c)
	}
	return h
}

// GenerateDocumentID generates a unique document ID.
func GenerateDocumentID(path string) string {
	return fmt.Sprintf("doc_%x", hashString(path))
}