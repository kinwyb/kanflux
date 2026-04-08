package knowledgebase

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// JSONStore implements Store using JSON files.
// It's a simple implementation suitable for small-scale usage or testing.
type JSONStore struct {
	basePath string
	embedder Embedder

	mu      sync.RWMutex
	storage *jsonStorage
}

type jsonStorage struct {
	Documents  map[string]*docEntry `json:"documents"`
	Embeddings map[string][]float32 `json:"embeddings"`
	UpdatedAt  string                `json:"updated_at"`
}

type docEntry struct {
	ID       string         `json:"id"`
	Content  string         `json:"content"`
	Wing     string         `json:"wing"`
	Room     string         `json:"room"`
	Source   string         `json:"source"`
	Metadata map[string]any `json:"metadata"`
}

// NewJSONStore creates a new JSON-based store.
func NewJSONStore(basePath string, embedder Embedder) (*JSONStore, error) {
	if basePath == "" {
		return nil, ErrInvalidConfig
	}

	if err := os.MkdirAll(basePath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create store directory: %w", err)
	}

	store := &JSONStore{
		basePath: basePath,
		embedder: embedder,
		storage: &jsonStorage{
			Documents:  make(map[string]*docEntry),
			Embeddings: make(map[string][]float32),
		},
	}

	// Load existing data
	if err := store.load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to load store: %w", err)
	}

	return store, nil
}

// Initialize prepares the JSON store.
func (s *JSONStore) Initialize(ctx context.Context) error {
	return s.save()
}

// Add stores documents.
func (s *JSONStore) Add(ctx context.Context, docs []*Document) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, doc := range docs {
		entry := &docEntry{
			ID:       doc.ID,
			Content:  doc.Content,
			Wing:     doc.Wing,
			Room:     doc.Room,
			Source:   doc.Source,
			Metadata: doc.Metadata,
		}

		s.storage.Documents[doc.ID] = entry

		// Generate embedding if embedder is available
		if s.embedder != nil {
			embedding, err := s.embedder.Embed(ctx, doc.Content)
			if err == nil {
				s.storage.Embeddings[doc.ID] = embedding
			}
		}
	}

	s.storage.UpdatedAt = time.Now().Format(time.RFC3339)
	return s.save()
}

// Search performs a semantic search.
func (s *JSONStore) Search(ctx context.Context, query string, opts *SearchOptions) ([]*SearchResult, error) {
	// Use SearchByEmbedding if embedder is available
	if s.embedder != nil {
		embedding, err := s.embedder.Embed(ctx, query)
		if err == nil {
			return s.SearchByEmbedding(ctx, embedding, opts)
		}
	}

	// Fallback to keyword search
	return s.keywordSearch(query, opts), nil
}

// SearchByEmbedding performs a search using a pre-computed embedding.
func (s *JSONStore) SearchByEmbedding(ctx context.Context, embedding []float32, opts *SearchOptions) ([]*SearchResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	limit := opts.Limit
	if limit <= 0 {
		limit = 10
	}

	var results []*SearchResult

	for id, entry := range s.storage.Documents {
		// Apply filters
		if opts.Wing != "" && entry.Wing != opts.Wing {
			continue
		}
		if opts.Room != "" && entry.Room != opts.Room {
			continue
		}

		// Get embedding
		docEmbedding, ok := s.storage.Embeddings[id]
		if !ok {
			continue
		}

		// Calculate similarity
		score := cosineSimilarity(embedding, docEmbedding)

		// Apply threshold
		if opts.Threshold > 0 && score < opts.Threshold {
			continue
		}

		results = append(results, &SearchResult{
			ID:       id,
			Content:  entry.Content,
			Score:    score,
			Wing:     entry.Wing,
			Room:     entry.Room,
			Source:   entry.Source,
			Metadata: entry.Metadata,
		})
	}

	// Sort by score descending
	sortByScore(results)

	if len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

// keywordSearch performs a simple keyword-based search.
func (s *JSONStore) keywordSearch(query string, opts *SearchOptions) []*SearchResult {
	s.mu.RLock()
	defer s.mu.RUnlock()

	limit := opts.Limit
	if limit <= 0 {
		limit = 10
	}

	var results []*SearchResult

	for id, entry := range s.storage.Documents {
		// Apply filters
		if opts.Wing != "" && entry.Wing != opts.Wing {
			continue
		}
		if opts.Room != "" && entry.Room != opts.Room {
			continue
		}

		// Simple keyword match
		if containsIgnoreCase(entry.Content, query) {
			results = append(results, &SearchResult{
				ID:       id,
				Content:  entry.Content,
				Score:    0.5, // Base score for keyword match
				Wing:     entry.Wing,
				Room:     entry.Room,
				Source:   entry.Source,
				Metadata: entry.Metadata,
			})
		}
	}

	if len(results) > limit {
		results = results[:limit]
	}

	return results
}

// Get retrieves a document by ID.
func (s *JSONStore) Get(ctx context.Context, id string) (*Document, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, ok := s.storage.Documents[id]
	if !ok {
		return nil, ErrDocumentNotFound
	}

	return &Document{
		ID:       entry.ID,
		Content:  entry.Content,
		Wing:     entry.Wing,
		Room:     entry.Room,
		Source:   entry.Source,
		Metadata: entry.Metadata,
	}, nil
}

// Delete removes a document.
func (s *JSONStore) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.storage.Documents, id)
	delete(s.storage.Embeddings, id)

	return s.save()
}

// DeleteByWing removes all documents in a wing.
func (s *JSONStore) DeleteByWing(ctx context.Context, wing string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for id, entry := range s.storage.Documents {
		if entry.Wing == wing {
			delete(s.storage.Documents, id)
			delete(s.storage.Embeddings, id)
		}
	}

	return s.save()
}

// DeleteByRoom removes all documents in a wing/room.
func (s *JSONStore) DeleteByRoom(ctx context.Context, wing, room string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for id, entry := range s.storage.Documents {
		if entry.Wing == wing && entry.Room == room {
			delete(s.storage.Documents, id)
			delete(s.storage.Embeddings, id)
		}
	}

	return s.save()
}

// Count returns the total number of documents.
func (s *JSONStore) Count(ctx context.Context) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.storage.Documents), nil
}

// Stats returns statistics.
func (s *JSONStore) Stats(ctx context.Context) (*Stats, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	wings := make(map[string]bool)
	rooms := make(map[string]bool)

	for _, entry := range s.storage.Documents {
		wings[entry.Wing] = true
		rooms[entry.Wing+"/"+entry.Room] = true
	}

	// Get storage size
	var size int64
	info, err := os.Stat(filepath.Join(s.basePath, "store.json"))
	if err == nil {
		size = info.Size()
	}

	return &Stats{
		TotalDocuments: len(s.storage.Documents),
		TotalWings:     len(wings),
		TotalRooms:     len(rooms),
		StorageSize:    size,
		StoreType:      StoreTypeJSON,
	}, nil
}

// Close closes the store.
func (s *JSONStore) Close() error {
	return s.save()
}

// ============ Persistence ============

func (s *JSONStore) load() error {
	data, err := os.ReadFile(filepath.Join(s.basePath, "store.json"))
	if err != nil {
		return err
	}

	return json.Unmarshal(data, s.storage)
}

func (s *JSONStore) save() error {
	s.storage.UpdatedAt = time.Now().Format(time.RFC3339)

	data, err := json.MarshalIndent(s.storage, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(s.basePath, "store.json"), data, 0644)
}

// ============ Helpers ============

func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}

	var dotProduct, normA, normB float64
	for i := range a {
		dotProduct += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}

func containsIgnoreCase(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && containsLower(lower(s), lower(substr))))
}

func containsLower(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func lower(s string) string {
	b := make([]byte, len(s))
	for i := range s {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}

func sortByScore(results []*SearchResult) {
	for i := 0; i < len(results)-1; i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].Score > results[i].Score {
				results[i], results[j] = results[j], results[i]
			}
		}
	}
}

// Ensure JSONStore implements Store
var _ Store = (*JSONStore)(nil)