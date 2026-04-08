package knowledgebase

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	mpvector "github.com/kinwyb/mempalace-go/pkg/vector"
)

// SQLiteStore implements Store using mempalace-go's SQLite backend.
// It provides hybrid search (FTS5 + vector) and L0-L3 layer support.
type SQLiteStore struct {
	store     *mpvector.SQLiteStore
	embedder  Embedder
	workspace string
}

// NewSQLiteStore creates a new SQLite-based store.
func NewSQLiteStore(workspace string, embedder Embedder) (*SQLiteStore, error) {
	if workspace == "" {
		return nil, ErrInvalidConfig
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

	// Create embedder adapter if available
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
func (s *SQLiteStore) Add(ctx context.Context, docs []*Document) error {
	if s.store == nil {
		return ErrStoreNotInitialized
	}

	mpDocs := make([]mpvector.Document, len(docs))
	for i, doc := range docs {
		metadata := make(map[string]any)
		if doc.Metadata != nil {
			for k, v := range doc.Metadata {
				metadata[k] = v
			}
		}
		// Store wing/room/source in metadata (required by vector.SQLiteStore)
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

// Search performs a hybrid search (FTS5 + semantic).
func (s *SQLiteStore) Search(ctx context.Context, query string, opts *SearchOptions) ([]*SearchResult, error) {
	if s.store == nil {
		return nil, ErrStoreNotInitialized
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
func (s *SQLiteStore) SearchByEmbedding(ctx context.Context, embedding []float32, opts *SearchOptions) ([]*SearchResult, error) {
	if s.store == nil {
		return nil, ErrStoreNotInitialized
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
func (s *SQLiteStore) Get(ctx context.Context, id string) (*Document, error) {
	if s.store == nil {
		return nil, ErrStoreNotInitialized
	}

	doc, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if doc.ID == "" {
		return nil, ErrDocumentNotFound
	}

	return convertVectorDocument(doc), nil
}

// Delete removes a document.
func (s *SQLiteStore) Delete(ctx context.Context, id string) error {
	if s.store == nil {
		return ErrStoreNotInitialized
	}
	return s.store.Delete(ctx, id)
}

// DeleteByWing removes all documents in a wing.
func (s *SQLiteStore) DeleteByWing(ctx context.Context, wing string) error {
	if s.store == nil {
		return ErrStoreNotInitialized
	}
	return s.store.DeleteByWing(ctx, wing)
}

// DeleteByRoom removes all documents in a wing/room.
func (s *SQLiteStore) DeleteByRoom(ctx context.Context, wing, room string) error {
	if s.store == nil {
		return ErrStoreNotInitialized
	}
	return s.store.DeleteByRoom(ctx, wing, room)
}

// Count returns the total number of documents.
func (s *SQLiteStore) Count(ctx context.Context) (int, error) {
	if s.store == nil {
		return 0, ErrStoreNotInitialized
	}
	return s.store.Count(ctx)
}

// Stats returns statistics.
func (s *SQLiteStore) Stats(ctx context.Context) (*Stats, error) {
	if s.store == nil {
		return nil, ErrStoreNotInitialized
	}

	storeStats, err := s.store.GetStats(ctx)
	if err != nil {
		return nil, err
	}

	return &Stats{
		TotalDocuments: storeStats.TotalDocuments,
		TotalWings:     storeStats.TotalWings,
		TotalRooms:     storeStats.TotalRooms,
		StorageSize:    storeStats.StorageSize,
		StoreType:      StoreTypeSQLite,
	}, nil
}

// Close closes the store.
func (s *SQLiteStore) Close() error {
	if s.store != nil {
		return s.store.Close()
	}
	return nil
}

// ============ Adapters ============

// embedderAdapter adapts our Embedder to vector.Embedder interface.
type embedderAdapter struct {
	embedder Embedder
}

func (a *embedderAdapter) Embed(ctx context.Context, text string) ([]float32, error) {
	return a.embedder.Embed(ctx, text)
}

func (a *embedderAdapter) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	return a.embedder.EmbedBatch(ctx, texts)
}

// ============ Converters ============

func convertVectorSearchResults(items []mpvector.SearchResult) []*SearchResult {
	results := make([]*SearchResult, len(items))
	for i, item := range items {
		results[i] = &SearchResult{
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

func convertVectorDocument(doc *mpvector.Document) *Document {
	return &Document{
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

// Ensure SQLiteStore implements Store
var _ Store = (*SQLiteStore)(nil)

// Ensure embedderAdapter implements vector.Embedder
var _ mpvector.Embedder = (*embedderAdapter)(nil)