// Package store provides storage backend implementations for the knowledge base.
package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kinwyb/kanflux/knowledgebase/types"
	mpvector "github.com/kinwyb/mempalace-go/pkg/vector"
)

// SQLiteStore implements types.Store using mempalace-go's SQLite backend.
type SQLiteStore struct {
	store     *mpvector.SQLiteStore
	embedder  types.Embedder
	workspace string
}

// NewSQLite creates a new SQLite-based store.
func NewSQLite(workspace string, embedder types.Embedder) (types.Store, error) {
	if workspace == "" {
		return nil, types.ErrInvalidConfig
	}

	if err := os.MkdirAll(workspace, 0755); err != nil {
		return nil, fmt.Errorf("failed to create workspace: %w", err)
	}

	return &SQLiteStore{
		embedder:  embedder,
		workspace: workspace,
	}, nil
}

// Initialize prepares the SQLite store.
func (s *SQLiteStore) Initialize(ctx context.Context) error {
	dbPath := filepath.Join(s.workspace, "knowledge.db")

	var mpEmbedder mpvector.Embedder
	if s.embedder != nil {
		mpEmbedder = &embedderAdapter{embedder: s.embedder}
	}

	store, err := mpvector.NewSQLiteStore(dbPath, mpEmbedder)
	if err != nil {
		return fmt.Errorf("failed to create vector store: %w", err)
	}

	if err := store.Initialize(ctx); err != nil {
		return fmt.Errorf("failed to initialize vector store: %w", err)
	}

	s.store = store
	return nil
}

// Add stores documents.
func (s *SQLiteStore) Add(ctx context.Context, docs []*types.Document) error {
	if s.store == nil {
		return types.ErrStoreNotInitialized
	}

	mpDocs := make([]mpvector.Document, len(docs))
	for i, doc := range docs {
		metadata := make(map[string]any)
		if doc.Metadata != nil {
			for k, v := range doc.Metadata {
				metadata[k] = v
			}
		}
		metadata["wing"] = doc.Wing
		metadata["room"] = doc.Room
		metadata["source_file"] = doc.Source

		mpDocs[i] = mpvector.Document{
			ID:       doc.ID,
			Content:  doc.Content,
			Metadata: metadata,
		}
	}

	return s.store.Add(ctx, mpDocs)
}

// Search performs a hybrid search.
func (s *SQLiteStore) Search(ctx context.Context, query string, opts *types.SearchOptions) ([]*types.SearchResult, error) {
	if s.store == nil {
		return nil, types.ErrStoreNotInitialized
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = 10
	}

	results, err := s.store.Search(ctx, query, opts.Wing, opts.Room, limit)
	if err != nil {
		return nil, err
	}

	return convertVectorSearchResults(results), nil
}

// SearchByEmbedding performs a search using a pre-computed embedding.
func (s *SQLiteStore) SearchByEmbedding(ctx context.Context, embedding []float32, opts *types.SearchOptions) ([]*types.SearchResult, error) {
	if s.store == nil {
		return nil, types.ErrStoreNotInitialized
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = 10
	}

	results, err := s.store.SearchByVector(ctx, embedding, opts.Wing, opts.Room, limit)
	if err != nil {
		return nil, err
	}

	return convertVectorSearchResults(results), nil
}

// Get retrieves a document by ID.
func (s *SQLiteStore) Get(ctx context.Context, id string) (*types.Document, error) {
	if s.store == nil {
		return nil, types.ErrStoreNotInitialized
	}

	doc, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if doc.ID == "" {
		return nil, types.ErrDocumentNotFound
	}

	return convertVectorDocument(doc), nil
}

// Delete removes a document.
func (s *SQLiteStore) Delete(ctx context.Context, id string) error {
	if s.store == nil {
		return types.ErrStoreNotInitialized
	}
	return s.store.Delete(ctx, id)
}

// DeleteByWing removes all documents in a wing.
func (s *SQLiteStore) DeleteByWing(ctx context.Context, wing string) error {
	if s.store == nil {
		return types.ErrStoreNotInitialized
	}
	return s.store.DeleteByWing(ctx, wing)
}

// DeleteByRoom removes all documents in a wing/room.
func (s *SQLiteStore) DeleteByRoom(ctx context.Context, wing, room string) error {
	if s.store == nil {
		return types.ErrStoreNotInitialized
	}
	return s.store.DeleteByRoom(ctx, wing, room)
}

// Count returns the total number of documents.
func (s *SQLiteStore) Count(ctx context.Context) (int, error) {
	if s.store == nil {
		return 0, types.ErrStoreNotInitialized
	}
	return s.store.Count(ctx)
}

// Stats returns statistics.
func (s *SQLiteStore) Stats(ctx context.Context) (*types.Stats, error) {
	if s.store == nil {
		return nil, types.ErrStoreNotInitialized
	}

	storeStats, err := s.store.GetStats(ctx)
	if err != nil {
		return nil, err
	}

	return &types.Stats{
		TotalDocuments: storeStats.TotalDocuments,
		TotalWings:     storeStats.TotalWings,
		TotalRooms:     storeStats.TotalRooms,
		StorageSize:    storeStats.StorageSize,
		StoreType:      types.StoreTypeSQLite,
	}, nil
}

// Close closes the store.
func (s *SQLiteStore) Close() error {
	if s.store != nil {
		return s.store.Close()
	}
	return nil
}

// ============ Internal ============

type embedderAdapter struct {
	embedder types.Embedder
}

func (a *embedderAdapter) Embed(ctx context.Context, text string) ([]float32, error) {
	return a.embedder.Embed(ctx, text)
}

func (a *embedderAdapter) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	return a.embedder.EmbedBatch(ctx, texts)
}

func convertVectorSearchResults(items []mpvector.SearchResult) []*types.SearchResult {
	results := make([]*types.SearchResult, len(items))
	for i, item := range items {
		results[i] = &types.SearchResult{
			ID:       item.ID,
			Content:  item.Content,
			Score:    item.Score,
			Wing:     getMetadataString(item.Metadata, "wing"),
			Room:     getMetadataString(item.Metadata, "room"),
			Source:   getMetadataString(item.Metadata, "source_file"),
			Metadata: item.Metadata,
		}
	}
	return results
}

func convertVectorDocument(doc *mpvector.Document) *types.Document {
	return &types.Document{
		ID:       doc.ID,
		Content:  doc.Content,
		Wing:     getMetadataString(doc.Metadata, "wing"),
		Room:     getMetadataString(doc.Metadata, "room"),
		Source:   getMetadataString(doc.Metadata, "source_file"),
		Metadata: doc.Metadata,
	}
}

func getMetadataString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
		return fmt.Sprintf("%v", v)
	}
	return ""
}

// Ensure SQLiteStore implements types.Store
var _ types.Store = (*SQLiteStore)(nil)

// Ensure embedderAdapter implements mpvector.Embedder
var _ mpvector.Embedder = (*embedderAdapter)(nil)