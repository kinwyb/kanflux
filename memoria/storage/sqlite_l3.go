package storage

import (
	"context"
	"fmt"
	"os"

	"github.com/kinwyb/kanflux/memoria/types"
)

// SQLiteStore implements storage for L1/L2/L3 layers using SQLite with vector search
// L1: only store document, no embedding
// L2: store document + embedding + output MD file for validation
// L3: store document + embedding
type SQLiteStore struct {
	store     *SQLiteVectorStore
	embedder  types.Embedder
	workspace string
}

// NewSQLiteStore creates a new SQLite store for all layers
func NewSQLiteStore(workspace string, embedder types.Embedder) (*SQLiteStore, error) {
	if workspace == "" {
		return nil, fmt.Errorf("workspace is required")
	}

	if err := os.MkdirAll(workspace, 0755); err != nil {
		return nil, fmt.Errorf("failed to create workspace: %w", err)
	}

	return &SQLiteStore{
		embedder:  embedder,
		workspace: workspace,
	}, nil
}

// Initialize initializes the SQLite store
func (s *SQLiteStore) Initialize(ctx context.Context) error {
	dbPath := s.workspace + "/memoria.db"
	store, err := NewSQLiteVectorStore(dbPath, s.embedder, s.workspace)
	if err != nil {
		return fmt.Errorf("failed to create sqlite vector store: %w", err)
	}

	if err := store.Initialize(ctx); err != nil {
		return fmt.Errorf("failed to initialize sqlite vector store: %w", err)
	}

	s.store = store
	return nil
}

// Store stores a memory item
func (s *SQLiteStore) Store(ctx context.Context, item *types.MemoryItem) error {
	return s.StoreBatch(ctx, []*types.MemoryItem{item})
}

// StoreBatch stores multiple items
func (s *SQLiteStore) StoreBatch(ctx context.Context, items []*types.MemoryItem) error {
	if s.store == nil {
		return fmt.Errorf("store not initialized")
	}

	docs := make([]Document, len(items))
	for i, item := range items {
		docs[i] = *NewDocumentFromMemoryItem(item)
	}

	return s.store.Add(ctx, docs)
}

// Search performs semantic search (L2 preferred, then L3)
func (s *SQLiteStore) Search(ctx context.Context, query string, opts *types.RetrieveOptions) ([]*types.SearchResult, error) {
	if s.store == nil {
		return nil, fmt.Errorf("store not initialized")
	}

	searchOpts := s.convertRetrieveOptions(opts)

	results, err := s.store.Search(ctx, query, searchOpts)
	if err != nil {
		return nil, err
	}

	return s.convertResults(results), nil
}

// KeywordSearch performs FTS5 keyword search
func (s *SQLiteStore) KeywordSearch(ctx context.Context, query string, opts *types.RetrieveOptions) ([]*types.SearchResult, error) {
	if s.store == nil {
		return nil, fmt.Errorf("store not initialized")
	}

	searchOpts := s.convertRetrieveOptions(opts)

	results, err := s.store.KeywordSearch(ctx, query, searchOpts)
	if err != nil {
		return nil, err
	}

	return s.convertResults(results), nil
}

// SearchByEmbedding searches using pre-computed embedding
func (s *SQLiteStore) SearchByEmbedding(ctx context.Context, embedding []float32, opts *types.RetrieveOptions) ([]*types.SearchResult, error) {
	if s.store == nil {
		return nil, fmt.Errorf("store not initialized")
	}

	searchOpts := s.convertRetrieveOptions(opts)

	results, err := s.store.SearchByVector(ctx, embedding, searchOpts)
	if err != nil {
		return nil, err
	}

	return s.convertResults(results), nil
}

// SearchByLayer performs search within specific layers
func (s *SQLiteStore) SearchByLayer(ctx context.Context, query string, layers []int, opts *types.RetrieveOptions) ([]*types.SearchResult, error) {
	if s.store == nil {
		return nil, fmt.Errorf("store not initialized")
	}

	searchOpts := s.convertRetrieveOptions(opts)
	searchOpts.Layers = layers

	results, err := s.store.Search(ctx, query, searchOpts)
	if err != nil {
		return nil, err
	}

	return s.convertResults(results), nil
}

// GetByLayer retrieves documents by layer
func (s *SQLiteStore) GetByLayer(ctx context.Context, layer int, userID string, limit int) ([]*types.MemoryItem, error) {
	if s.store == nil {
		return nil, fmt.Errorf("store not initialized")
	}

	docs, err := s.store.GetByLayer(ctx, layer, userID, limit)
	if err != nil {
		return nil, err
	}

	items := make([]*types.MemoryItem, len(docs))
	for i, doc := range docs {
		items[i] = doc.ToMemoryItem()
	}

	return items, nil
}

// Delete removes a document by ID
func (s *SQLiteStore) Delete(ctx context.Context, id string) error {
	if s.store == nil {
		return fmt.Errorf("store not initialized")
	}
	return s.store.Delete(ctx, id)
}

// DeleteByUser removes all documents for a user
func (s *SQLiteStore) DeleteByUser(ctx context.Context, userID string) error {
	if s.store == nil {
		return fmt.Errorf("store not initialized")
	}
	return s.store.DeleteByUserID(ctx, userID)
}

// DeleteBySource removes all documents from a source
func (s *SQLiteStore) DeleteBySource(ctx context.Context, source string) error {
	// TODO: implement DeleteBySource if needed
	return nil
}

// GetStats returns storage statistics
func (s *SQLiteStore) GetStats(ctx context.Context) (*StoreStats, error) {
	if s.store == nil {
		return nil, fmt.Errorf("store not initialized")
	}
	return s.store.GetStats(ctx)
}

// Close closes the store
func (s *SQLiteStore) Close() error {
	if s.store != nil {
		return s.store.Close()
	}
	return nil
}

// GetVectorStore returns the underlying vector store (for advanced usage)
func (s *SQLiteStore) GetVectorStore() *SQLiteVectorStore {
	return s.store
}

// ============ Internal Helper Methods ============

func (s *SQLiteStore) convertRetrieveOptions(opts *types.RetrieveOptions) *SearchOptions {
	searchOpts := DefaultSearchOptions()

	if opts == nil {
		return searchOpts
	}

	if opts.Limit > 0 {
		searchOpts.Limit = opts.Limit
	}

	if opts.UserID != "" {
		searchOpts.UserID = opts.UserID
	}

	// No HallType filtering - search all types together
	// searchOpts.HallType is left empty

	if opts.SourceType != "" {
		searchOpts.SourceType = string(opts.SourceType)
	}

	// Set layers from opts, default to L2 + L3
	if len(opts.Layers) > 0 {
		searchOpts.Layers = make([]int, len(opts.Layers))
		for i, l := range opts.Layers {
			searchOpts.Layers[i] = int(l)
		}
	} else {
		searchOpts.Layers = []int{2, 3}
	}
	searchOpts.PreferLayer = 2

	return searchOpts
}

func (s *SQLiteStore) convertResults(results []SearchResult) []*types.SearchResult {
	converted := make([]*types.SearchResult, len(results))
	for i, r := range results {
		converted[i] = r.ToSearchResult()
	}
	return converted
}

// ============ Backward Compatibility ============

// L3SQLiteStore is an alias for backward compatibility
type L3SQLiteStore = SQLiteStore

// NewL3SQLiteStore creates a new L3 SQLite store (backward compatibility)
func NewL3SQLiteStore(workspace string, embedder types.Embedder) (*L3SQLiteStore, error) {
	return NewSQLiteStore(workspace, embedder)
}

// L3Stats is an alias for backward compatibility
type L3Stats = StoreStats