// Package knowledgebase provides a unified knowledge storage and retrieval module.
// It re-exports types from the types package and provides a facade for easy usage.
package knowledgebase

import (
	"context"
	"fmt"

	"github.com/kinwyb/kanflux/knowledgebase/store"
	"github.com/kinwyb/kanflux/knowledgebase/types"
)

// Re-export types for convenience
type (
	Document     = types.Document
	SearchResult = types.SearchResult
	SearchOptions = types.SearchOptions
	Stats        = types.Stats
	Store        = types.Store
	Embedder     = types.Embedder
)

// Re-export constants
const (
	StoreTypeSQLite = types.StoreTypeSQLite
	StoreTypeJSON   = types.StoreTypeJSON
)

// Re-export errors
var (
	ErrEmptyContent        = types.ErrEmptyContent
	ErrDocumentNotFound    = types.ErrDocumentNotFound
	ErrInvalidConfig       = types.ErrInvalidConfig
	ErrStoreNotInitialized = types.ErrStoreNotInitialized
	ErrEmbedderNotSet      = types.ErrEmbedderNotSet
)

// KnowledgeBase is the main facade for knowledge storage and retrieval.
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

	var s Store
	var err error

	switch cfg.StoreType {
	case StoreTypeJSON:
		s, err = store.NewJSON(cfg.Workspace, cfg.Embedder)
	default:
		s, err = store.NewSQLite(cfg.Workspace, cfg.Embedder)
	}

	if err != nil {
		return nil, err
	}

	if err := s.Initialize(context.Background()); err != nil {
		return nil, err
	}

	return &KnowledgeBase{
		store:    s,
		embedder: cfg.Embedder,
		config:   cfg,
	}, nil
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

type AddOption func(*addConfig)

type addConfig struct {
	wing     string
	room     string
	source   string
	metadata map[string]any
}

func WithWing(wing string) AddOption     { return func(c *addConfig) { c.wing = wing } }
func WithRoom(room string) AddOption     { return func(c *addConfig) { c.room = room } }
func WithSource(source string) AddOption { return func(c *addConfig) { c.source = source } }
func WithMetadata(meta map[string]any) AddOption {
	return func(c *addConfig) { c.metadata = meta }
}

type SearchOption func(*searchOpts)

type searchOpts struct {
	wing      string
	room      string
	limit     int
	threshold float64
}

func WithWingFilter(wing string) SearchOption      { return func(o *searchOpts) { o.wing = wing } }
func WithRoomFilter(room string) SearchOption      { return func(o *searchOpts) { o.room = room } }
func WithLimit(limit int) SearchOption             { return func(o *searchOpts) { o.limit = limit } }
func WithThreshold(threshold float64) SearchOption { return func(o *searchOpts) { o.threshold = threshold } }

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