package storage

import (
	"context"
	"fmt"
	"os"

	"github.com/kinwyb/kanflux/knowledgebase/memoria/types"
	"github.com/kinwyb/mempalace-go/pkg/mempalace"
)

// L3SQLiteStore implements L3 storage using SQLite with vector search via mempalace
type L3SQLiteStore struct {
	palace    *mempalace.Palace
	embedder  types.Embedder
	workspace string
}

// NewL3SQLiteStore creates a new L3 SQLite store
func NewL3SQLiteStore(workspace string, embedder types.Embedder) (*L3SQLiteStore, error) {
	if workspace == "" {
		return nil, fmt.Errorf("workspace is required")
	}

	if err := os.MkdirAll(workspace, 0755); err != nil {
		return nil, fmt.Errorf("failed to create workspace: %w", err)
	}

	return &L3SQLiteStore{
		embedder:  embedder,
		workspace: workspace,
	}, nil
}

// Initialize initializes the L3 store
func (s *L3SQLiteStore) Initialize(ctx context.Context) error {
	opts := []mempalace.Option{
		mempalace.WithPalacePath(s.workspace),
	}

	if s.embedder != nil {
		opts = append(opts, mempalace.WithEmbedder(&l3EmbedderAdapter{embedder: s.embedder}))
	}

	palace, err := mempalace.New(ctx, opts...)
	if err != nil {
		return fmt.Errorf("failed to create palace: %w", err)
	}

	s.palace = palace
	return nil
}

// Store stores a memory item with its embedding
func (s *L3SQLiteStore) Store(ctx context.Context, item *types.MemoryItem) error {
	return s.StoreBatch(ctx, []*types.MemoryItem{item})
}

// StoreBatch stores multiple items
func (s *L3SQLiteStore) StoreBatch(ctx context.Context, items []*types.MemoryItem) error {
	if s.palace == nil {
		return fmt.Errorf("L3 store not initialized")
	}

	for _, item := range items {
		// 准备 metadata
		metadata := make(map[string]any)
		if item.Metadata != nil {
			for k, v := range item.Metadata {
				metadata[k] = v
			}
		}
		metadata["summary"] = item.Summary
		metadata["layer"] = "L3"

		doc := mempalace.Document{
			ID:       item.ID,
			Content:  item.Content,
			Wing:     string(item.HallType),
			Room:     item.UserID,
			Source:   item.Source,
			Metadata: metadata,
		}

		if _, err := s.palace.AddDocument(ctx, doc); err != nil {
			return fmt.Errorf("failed to store document %s: %w", item.ID, err)
		}
	}

	return nil
}

// Search performs semantic search
func (s *L3SQLiteStore) Search(ctx context.Context, query string, opts *types.RetrieveOptions) ([]*types.SearchResult, error) {
	if s.palace == nil {
		return nil, fmt.Errorf("L3 store not initialized")
	}

	limit := 10
	if opts != nil && opts.Limit > 0 {
		limit = opts.Limit
	}

	searchOpts := []mempalace.SearchOption{mempalace.WithLimit(limit)}

	if opts != nil {
		if opts.UserID != "" {
			searchOpts = append(searchOpts, mempalace.WithRoom(opts.UserID))
		}
		if len(opts.HallTypes) > 0 {
			searchOpts = append(searchOpts, mempalace.WithWing(string(opts.HallTypes[0])))
		}
	}

	result, err := s.palace.Search(ctx, query, searchOpts...)
	if err != nil {
		return nil, err
	}

	return s.convertSearchResults(result.Results), nil
}

// SearchByEmbedding searches using pre-computed embedding
func (s *L3SQLiteStore) SearchByEmbedding(ctx context.Context, embedding []float32, opts *types.RetrieveOptions) ([]*types.SearchResult, error) {
	if s.palace == nil {
		return nil, fmt.Errorf("L3 store not initialized")
	}

	limit := 10
	if opts != nil && opts.Limit > 0 {
		limit = opts.Limit
	}

	searchOpts := []mempalace.SearchOption{mempalace.WithLimit(limit)}

	if opts != nil {
		if opts.UserID != "" {
			searchOpts = append(searchOpts, mempalace.WithRoom(opts.UserID))
		}
		if len(opts.HallTypes) > 0 {
			searchOpts = append(searchOpts, mempalace.WithWing(string(opts.HallTypes[0])))
		}
	}

	result, err := s.palace.SearchByVector(ctx, embedding, searchOpts...)
	if err != nil {
		return nil, err
	}

	return s.convertSearchResults(result.Results), nil
}

// Delete removes a document by ID
func (s *L3SQLiteStore) Delete(ctx context.Context, id string) error {
	if s.palace == nil {
		return fmt.Errorf("L3 store not initialized")
	}
	return s.palace.Delete(ctx, id)
}

// DeleteBySource removes all documents from a source
func (s *L3SQLiteStore) DeleteBySource(ctx context.Context, source string) error {
	// mempalace 没有直接的 DeleteBySource，需要先搜索再删除
	// 这里简单返回 nil，实际可以扩展
	return nil
}

// DeleteByUser removes all documents for a user
func (s *L3SQLiteStore) DeleteByUser(ctx context.Context, userID string) error {
	if s.palace == nil {
		return fmt.Errorf("L3 store not initialized")
	}
	// mempalace DeleteByRoom 需要 wing 和 room
	// 这里用空 wing 表示删除所有 wing 下的该 room
	return s.palace.DeleteByRoom(ctx, "", userID)
}

// GetStats returns storage statistics
func (s *L3SQLiteStore) GetStats(ctx context.Context) (*L3Stats, error) {
	if s.palace == nil {
		return nil, fmt.Errorf("L3 store not initialized")
	}

	stats, err := s.palace.GetStats(ctx)
	if err != nil {
		return nil, err
	}

	return &L3Stats{
		TotalDocuments: stats.TotalDocuments,
		TotalVectors:   stats.TotalDocuments, // Each document has a vector
		StorageSize:    stats.StorageSize,
	}, nil
}

// Close closes the store
func (s *L3SQLiteStore) Close() error {
	if s.palace != nil {
		return s.palace.Close()
	}
	return nil
}

// ============ Internal ============

type l3EmbedderAdapter struct {
	embedder types.Embedder
}

func (a *l3EmbedderAdapter) Embed(ctx context.Context, text string) ([]float32, error) {
	return a.embedder.Embed(ctx, text)
}

func (a *l3EmbedderAdapter) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	return a.embedder.EmbedBatch(ctx, texts)
}

func (a *l3EmbedderAdapter) Dimension() int {
	return a.embedder.Dimension()
}

func (a *l3EmbedderAdapter) Model() string {
	// Return empty string if Model method not available
	return ""
}

func (s *L3SQLiteStore) convertSearchResults(items []mempalace.ResultItem) []*types.SearchResult {
	results := make([]*types.SearchResult, len(items))
	for i, item := range items {
		// 从 metadata 提取摘要
		summary := ""
		if item.Metadata != nil {
			summary = item.Metadata["summary"]
		}

		results[i] = &types.SearchResult{
			Item: &types.MemoryItem{
				ID:       item.ID,
				Content:  item.Content,
				Summary:  summary,
				HallType: types.HallType(item.Wing),
				UserID:   item.Room,
				Source:   item.Source,
				Metadata: stringMapToAny(item.Metadata),
				Layer:    types.LayerL3,
			},
			Score:     item.Score,
			Layer:     types.LayerL3,
			MatchType: "semantic",
		}
	}
	return results
}

func stringMapToAny(m map[string]string) map[string]any {
	if m == nil {
		return nil
	}
	result := make(map[string]any, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}

// L3Stats for L3 storage statistics
type L3Stats struct {
	TotalDocuments int
	TotalVectors   int
	StorageSize    int64
}