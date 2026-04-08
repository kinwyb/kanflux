package store

import (
	"context"
	"fmt"
	"os"

	"github.com/kinwyb/kanflux/knowledgebase/types"
	"github.com/kinwyb/mempalace-go/pkg/mempalace"
)

// SQLiteStore implements types.Store using mempalace.Palace.
type SQLiteStore struct {
	palace    *mempalace.Palace
	embedder  types.Embedder
	workspace string
}

// NewSQLite creates a new SQLite-based store using mempalace.Palace.
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
	opts := []mempalace.Option{
		mempalace.WithPalacePath(s.workspace),
	}

	if s.embedder != nil {
		opts = append(opts, mempalace.WithEmbedder(&embedderAdapter{embedder: s.embedder}))
	}

	palace, err := mempalace.New(ctx, opts...)
	if err != nil {
		return fmt.Errorf("failed to create palace: %w", err)
	}

	s.palace = palace
	return nil
}

// Add stores documents.
func (s *SQLiteStore) Add(ctx context.Context, docs []*types.Document) error {
	if s.palace == nil {
		return types.ErrStoreNotInitialized
	}

	for _, doc := range docs {
		mDoc := mempalace.Document{
			ID:       doc.ID,
			Content:  doc.Content,
			Wing:     doc.Wing,
			Room:     doc.Room,
			Source:   doc.Source,
			Metadata: doc.Metadata,
		}

		if _, err := s.palace.AddDocument(ctx, mDoc); err != nil {
			return fmt.Errorf("failed to add document %s: %w", doc.ID, err)
		}
	}

	return nil
}

// Search performs a semantic search.
func (s *SQLiteStore) Search(ctx context.Context, query string, opts *types.SearchOptions) ([]*types.SearchResult, error) {
	if s.palace == nil {
		return nil, types.ErrStoreNotInitialized
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = 10
	}

	searchOpts := []mempalace.SearchOption{mempalace.WithLimit(limit)}
	if opts.Wing != "" {
		searchOpts = append(searchOpts, mempalace.WithWing(opts.Wing))
	}
	if opts.Room != "" {
		searchOpts = append(searchOpts, mempalace.WithRoom(opts.Room))
	}

	result, err := s.palace.Search(ctx, query, searchOpts...)
	if err != nil {
		return nil, err
	}

	return convertSearchResults(result.Results), nil
}

// SearchByEmbedding performs a search using a pre-computed embedding.
func (s *SQLiteStore) SearchByEmbedding(ctx context.Context, embedding []float32, opts *types.SearchOptions) ([]*types.SearchResult, error) {
	if s.palace == nil {
		return nil, types.ErrStoreNotInitialized
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = 10
	}

	searchOpts := []mempalace.SearchOption{mempalace.WithLimit(limit)}
	if opts.Wing != "" {
		searchOpts = append(searchOpts, mempalace.WithWing(opts.Wing))
	}
	if opts.Room != "" {
		searchOpts = append(searchOpts, mempalace.WithRoom(opts.Room))
	}

	result, err := s.palace.SearchByVector(ctx, embedding, searchOpts...)
	if err != nil {
		return nil, err
	}

	return convertSearchResults(result.Results), nil
}

// Get retrieves a document by ID.
func (s *SQLiteStore) Get(ctx context.Context, id string) (*types.Document, error) {
	if s.palace == nil {
		return nil, types.ErrStoreNotInitialized
	}

	doc, err := s.palace.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if doc == nil {
		return nil, types.ErrDocumentNotFound
	}

	return convertDocument(doc), nil
}

// Delete removes a document.
func (s *SQLiteStore) Delete(ctx context.Context, id string) error {
	if s.palace == nil {
		return types.ErrStoreNotInitialized
	}
	return s.palace.Delete(ctx, id)
}

// DeleteByWing removes all documents in a wing.
func (s *SQLiteStore) DeleteByWing(ctx context.Context, wing string) error {
	if s.palace == nil {
		return types.ErrStoreNotInitialized
	}
	return s.palace.DeleteByWing(ctx, wing)
}

// DeleteByRoom removes all documents in a wing/room.
func (s *SQLiteStore) DeleteByRoom(ctx context.Context, wing, room string) error {
	if s.palace == nil {
		return types.ErrStoreNotInitialized
	}
	return s.palace.DeleteByRoom(ctx, wing, room)
}

// Count returns the total number of documents.
func (s *SQLiteStore) Count(ctx context.Context) (int, error) {
	if s.palace == nil {
		return 0, types.ErrStoreNotInitialized
	}
	stats, err := s.palace.GetStats(ctx)
	if err != nil {
		return 0, err
	}
	return stats.TotalDocuments, nil
}

// Stats returns statistics.
func (s *SQLiteStore) Stats(ctx context.Context) (*types.Stats, error) {
	if s.palace == nil {
		return nil, types.ErrStoreNotInitialized
	}

	stats, err := s.palace.GetStats(ctx)
	if err != nil {
		return nil, err
	}

	return &types.Stats{
		TotalDocuments: stats.TotalDocuments,
		TotalWings:     stats.TotalWings,
		TotalRooms:     stats.TotalRooms,
		StorageSize:    stats.StorageSize,
		StoreType:      types.StoreTypeSQLite,
	}, nil
}

// Close closes the store.
func (s *SQLiteStore) Close() error {
	if s.palace != nil {
		return s.palace.Close()
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

func (a *embedderAdapter) Dimension() int {
	return a.embedder.Dimension()
}

func (a *embedderAdapter) Model() string {
	return a.embedder.Model()
}

func convertSearchResults(items []mempalace.ResultItem) []*types.SearchResult {
	results := make([]*types.SearchResult, len(items))
	for i, item := range items {
		results[i] = &types.SearchResult{
			ID:       item.ID,
			Content:  item.Content,
			Score:    item.Score,
			Wing:     item.Wing,
			Room:     item.Room,
			Source:   item.Source,
			Metadata: convertMetadata(item.Metadata),
		}
	}
	return results
}

func convertDocument(doc *mempalace.Document) *types.Document {
	return &types.Document{
		ID:       doc.ID,
		Content:  doc.Content,
		Wing:     doc.Wing,
		Room:     doc.Room,
		Source:   doc.Source,
		Metadata: doc.Metadata,
	}
}

func convertMetadata(m map[string]string) map[string]any {
	if m == nil {
		return nil
	}
	result := make(map[string]any, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}

// Ensure SQLiteStore implements types.Store
var _ types.Store = (*SQLiteStore)(nil)