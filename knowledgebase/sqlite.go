package knowledgebase

import (
	"context"
	"fmt"
	"os"

	"github.com/kinwyb/mempalace-go/pkg/mempalace"
	mpvector "github.com/kinwyb/mempalace-go/pkg/vector"
)

// SQLiteStore implements Store using mempalace-go's SQLite backend.
// It provides hybrid search (FTS5 + vector) and L0-L3 layer support.
type SQLiteStore struct {
	palace   *mempalace.Palace
	embedder Embedder
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
func (s *SQLiteStore) Add(ctx context.Context, docs []*Document) error {
	if s.palace == nil {
		return ErrStoreNotInitialized
	}

	for _, doc := range docs {
		opts := []mempalace.AddOption{
			mempalace.WithWingForAdd(doc.Wing),
			mempalace.WithRoomForAdd(doc.Room),
			mempalace.WithSource(doc.Source),
		}

		if len(doc.Metadata) > 0 {
			opts = append(opts, mempalace.WithMetadata(doc.Metadata))
		}

		if _, err := s.palace.Add(ctx, doc.Content, opts...); err != nil {
			return fmt.Errorf("failed to add document %s: %w", doc.ID, err)
		}
	}

	return nil
}

// Search performs a semantic search.
func (s *SQLiteStore) Search(ctx context.Context, query string, opts *SearchOptions) ([]*SearchResult, error) {
	if s.palace == nil {
		return nil, ErrStoreNotInitialized
	}

	palaceOpts := []mempalace.SearchOption{}
	if opts.Wing != "" {
		palaceOpts = append(palaceOpts, mempalace.WithWing(opts.Wing))
	}
	if opts.Room != "" {
		palaceOpts = append(palaceOpts, mempalace.WithRoom(opts.Room))
	}
	if opts.Limit > 0 {
		palaceOpts = append(palaceOpts, mempalace.WithLimit(opts.Limit))
	}

	result, err := s.palace.Search(ctx, query, palaceOpts...)
	if err != nil {
		return nil, err
	}

	return convertSearchResults(result.Results), nil
}

// SearchByEmbedding performs a search using a pre-computed embedding.
func (s *SQLiteStore) SearchByEmbedding(ctx context.Context, embedding []float32, opts *SearchOptions) ([]*SearchResult, error) {
	if s.palace == nil {
		return nil, ErrStoreNotInitialized
	}

	// mempalace doesn't have direct SearchByVector, so we use Search
	// In practice, the palace.Search will use embeddings internally
	// For now, return empty - this can be enhanced
	return []*SearchResult{}, nil
}

// Get retrieves a document by ID.
func (s *SQLiteStore) Get(ctx context.Context, id string) (*Document, error) {
	if s.palace == nil {
		return nil, ErrStoreNotInitialized
	}

	doc, err := s.palace.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if doc == nil {
		return nil, ErrDocumentNotFound
	}

	return convertDocument(doc), nil
}

// Delete removes a document.
func (s *SQLiteStore) Delete(ctx context.Context, id string) error {
	if s.palace == nil {
		return ErrStoreNotInitialized
	}
	return s.palace.Delete(ctx, id)
}

// DeleteByWing removes all documents in a wing.
func (s *SQLiteStore) DeleteByWing(ctx context.Context, wing string) error {
	if s.palace == nil {
		return ErrStoreNotInitialized
	}
	return s.palace.DeleteByWing(ctx, wing)
}

// DeleteByRoom removes all documents in a wing/room.
func (s *SQLiteStore) DeleteByRoom(ctx context.Context, wing, room string) error {
	if s.palace == nil {
		return ErrStoreNotInitialized
	}
	return s.palace.DeleteByRoom(ctx, wing, room)
}

// Count returns the total number of documents.
func (s *SQLiteStore) Count(ctx context.Context) (int, error) {
	if s.palace == nil {
		return 0, ErrStoreNotInitialized
	}
	stats, err := s.palace.GetStats(ctx)
	if err != nil {
		return 0, err
	}
	return stats.TotalDocuments, nil
}

// Stats returns statistics.
func (s *SQLiteStore) Stats(ctx context.Context) (*Stats, error) {
	if s.palace == nil {
		return nil, ErrStoreNotInitialized
	}

	stats, err := s.palace.GetStats(ctx)
	if err != nil {
		return nil, err
	}

	return &Stats{
		TotalDocuments: stats.TotalDocuments,
		TotalWings:     stats.TotalWings,
		TotalRooms:     stats.TotalRooms,
		StorageSize:    stats.StorageSize,
		StoreType:      StoreTypeSQLite,
	}, nil
}

// Close closes the store.
func (s *SQLiteStore) Close() error {
	if s.palace != nil {
		return s.palace.Close()
	}
	return nil
}

// ============ Adapters ============

// embedderAdapter adapts our Embedder to mempalace's Embedder interface.
type embedderAdapter struct {
	embedder Embedder
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
	return "knowledgebase"
}

// ============ Converters ============

func convertSearchResults(items []mempalace.ResultItem) []*SearchResult {
	results := make([]*SearchResult, len(items))
	for i, item := range items {
		results[i] = &SearchResult{
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

func convertDocument(doc *mempalace.Document) *Document {
	return &Document{
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

// Ensure SQLiteStore implements Store
var _ Store = (*SQLiteStore)(nil)

// Ensure embedderAdapter implements mempalace's Embedder
var _ interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
	Dimension() int
	Model() string
} = (*embedderAdapter)(nil)

// Reference mempalace types to ensure compatibility
var _ = mpvector.Document{}