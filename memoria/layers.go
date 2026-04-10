package memoria

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/kinwyb/kanflux/memoria/llm"
	"github.com/kinwyb/kanflux/memoria/storage"
	"github.com/kinwyb/kanflux/memoria/types"
)

// L1FactsLayer implements the L1 (Preferences) layer
// Only stores user preferences, concise and always loaded
type L1FactsLayer struct {
	storage    types.Storage
	maxTokens  int
	summarizer types.Summarizer // Optional LLM summarizer for compacting
	mu         sync.RWMutex
	cache      []*types.MemoryItem
}

// NewL1FactsLayer creates an L1 layer instance
func NewL1FactsLayer(storage types.Storage, maxTokens int) *L1FactsLayer {
	return &L1FactsLayer{
		storage:   storage,
		maxTokens: maxTokens,
		cache:     make([]*types.MemoryItem, 0),
	}
}

// SetSummarizer sets the summarizer for compacting
func (l *L1FactsLayer) SetSummarizer(s types.Summarizer) {
	l.summarizer = s
}

// Initialize loads existing L1 memories into cache
func (l *L1FactsLayer) Initialize(ctx context.Context) error {
	items, err := l.storage.Retrieve(ctx, &types.RetrieveOptions{
		Layers: []types.Layer{types.LayerL1},
		Limit:  100,
	})
	if err != nil {
		return err
	}

	l.mu.Lock()
	l.cache = items
	l.mu.Unlock()
	return nil
}

// Add adds a new L1 memory item (only preferences)
func (l *L1FactsLayer) Add(ctx context.Context, item *types.MemoryItem) error {
	if item.Layer != types.LayerL1 {
		return fmt.Errorf("invalid layer for L1: expected L1, got %d", item.Layer)
	}

	// L1 only accepts preferences now
	if item.HallType != types.HallPreferences {
		return fmt.Errorf("invalid hall type for L1: expected preferences, got %s (facts should go to L2)", item.HallType)
	}

	l.mu.RLock()
	totalTokens := l.getTotalTokens()
	l.mu.RUnlock()

	if totalTokens+item.Tokens > l.maxTokens {
		if err := l.Compact(ctx); err != nil {
			return fmt.Errorf("failed to compact L1: %w", err)
		}
	}

	if err := l.storage.Store(ctx, item); err != nil {
		return err
	}

	l.mu.Lock()
	l.cache = append(l.cache, item)
	l.mu.Unlock()

	return nil
}

// Retrieve retrieves L1 memories matching criteria
func (l *L1FactsLayer) Retrieve(ctx context.Context, opts *types.RetrieveOptions) ([]*types.MemoryItem, error) {
	if len(opts.Layers) == 0 {
		opts.Layers = []types.Layer{types.LayerL1}
	}
	return l.storage.Retrieve(ctx, opts)
}

// GetAll returns all L1 memories
func (l *L1FactsLayer) GetAll() []*types.MemoryItem {
	l.mu.RLock()
	defer l.mu.RUnlock()
	result := make([]*types.MemoryItem, len(l.cache))
	copy(result, l.cache)
	return result
}

// GetForUser returns L1 memories for a specific user
func (l *L1FactsLayer) GetForUser(userID string) []*types.MemoryItem {
	l.mu.RLock()
	defer l.mu.RUnlock()

	result := make([]*types.MemoryItem, 0)
	for _, item := range l.cache {
		if item.UserID == userID {
			result = append(result, item)
		}
	}
	return result
}

// Compact consolidates memories when approaching token limit
// Uses CompactPrompt if summarizer is available, otherwise removes oldest items
func (l *L1FactsLayer) Compact(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.cache) <= 2 {
		return nil
	}

	// If summarizer is available, use LLM-based compaction
	if l.summarizer != nil {
		return l.compactWithLLM(ctx)
	}

	// Fallback: simple removal of oldest items
	return l.compactSimple(ctx)
}

// compactWithLLM uses CompactPrompt for intelligent compaction
func (l *L1FactsLayer) compactWithLLM(ctx context.Context) error {
	// Collect items to compact (the oldest half)
	itemsToCompact := l.cache[:len(l.cache)/2]
	if len(itemsToCompact) == 0 {
		return nil
	}

	// Extract summaries
	summaries := make([]string, len(itemsToCompact))
	for i, item := range itemsToCompact {
		if item.Summary != "" {
			summaries[i] = item.Summary
		} else {
			summaries[i] = item.Content
		}
	}

	// Use CompactPrompt via summarizer
	if impl, ok := l.summarizer.(*llm.SummarizerImpl); ok {
		compacted, err := impl.CompactMemories(ctx, summaries, l.maxTokens/2)
		if err != nil {
			slog.Warn("LLM compaction failed, falling back to simple", "error", err)
			return l.compactSimple(ctx)
		}

		// Remove old items
		for _, item := range itemsToCompact {
			l.storage.Delete(ctx, item.ID)
		}

		// Create new compacted items
		for _, summary := range compacted {
			newItem := &types.MemoryItem{
				ID:        generateID(),
				HallType:  types.HallFacts,
				Layer:     types.LayerL1,
				Content:   summary,
				Summary:   summary,
				Source:    "compacted",
				Timestamp: time.Now(),
				Tokens:    len(summary) / 4,
			}
			l.storage.Store(ctx, newItem)
			l.cache = append(l.cache, newItem)
		}

		// Remove compacted items from cache
		l.cache = l.cache[len(itemsToCompact):]

		slog.Info("L1 compaction complete",
			"removed", len(itemsToCompact),
			"added", len(compacted))
	}

	return nil
}

// compactSimple removes oldest items to stay under limit
func (l *L1FactsLayer) compactSimple(ctx context.Context) error {
	targetTokens := l.maxTokens - 20
	currentTokens := l.getTotalTokens()

	itemsToRemove := make([]*types.MemoryItem, 0)
	for i := 0; i < len(l.cache) && currentTokens > targetTokens; i++ {
		itemsToRemove = append(itemsToRemove, l.cache[i])
		currentTokens -= l.cache[i].Tokens
	}

	for _, item := range itemsToRemove {
		l.storage.Delete(ctx, item.ID)
	}

	if len(itemsToRemove) > 0 {
		l.cache = l.cache[len(itemsToRemove):]
	}

	return nil
}

func (l *L1FactsLayer) getTotalTokens() int {
	total := 0
	for _, item := range l.cache {
		total += item.Tokens
	}
	return total
}

// Close closes the layer
func (l *L1FactsLayer) Close() error {
	return nil
}

// ============ L2 Events Layer ============

// L2EventsLayer implements the L2 (Room Memories) layer
// Uses SQLite for storage with semantic search capability
// Also outputs MD files for validation
type L2EventsLayer struct {
	mdStore   types.Storage   // MD file storage for validation
	sqlite    *storage.SQLiteStore // SQLite storage for semantic search
	maxTokens int
}

// NewL2EventsLayer creates an L2 layer instance
func NewL2EventsLayer(mdStore types.Storage, maxTokens int) *L2EventsLayer {
	return &L2EventsLayer{
		mdStore:   mdStore,
		maxTokens: maxTokens,
	}
}

// SetSQLiteStore sets the SQLite store for L2
func (l *L2EventsLayer) SetSQLiteStore(sqlite *storage.SQLiteStore) {
	l.sqlite = sqlite
}

// Initialize initializes the L2 layer
func (l *L2EventsLayer) Initialize(ctx context.Context) error {
	return nil
}

// Add adds a new L2 memory item
// Stores to both SQLite (for semantic search) and MD files (for validation)
func (l *L2EventsLayer) Add(ctx context.Context, item *types.MemoryItem) error {
	if item.Layer != types.LayerL2 {
		return fmt.Errorf("invalid layer for L2: expected L2, got %d", item.Layer)
	}

	// L2 accepts facts, events, and discoveries
	if item.HallType != types.HallFacts && item.HallType != types.HallEvents && item.HallType != types.HallDiscoveries {
		return fmt.Errorf("invalid hall type for L2: expected facts, events or discoveries, got %s", item.HallType)
	}

	// Store to SQLite first (primary storage for semantic search)
	if l.sqlite != nil {
		if err := l.sqlite.Store(ctx, item); err != nil {
			slog.Warn("failed to store L2 to SQLite", "id", item.ID, "error", err)
			// Continue to MD storage even if SQLite fails
		}
	}

	// Also store to MD files for validation
	if l.mdStore != nil {
		if err := l.mdStore.Store(ctx, item); err != nil {
			slog.Warn("failed to store L2 to MD", "id", item.ID, "error", err)
		}
	}

	return nil
}

// Retrieve retrieves L2 memories matching criteria
func (l *L2EventsLayer) Retrieve(ctx context.Context, opts *types.RetrieveOptions) ([]*types.MemoryItem, error) {
	if len(opts.Layers) == 0 {
		opts.Layers = []types.Layer{types.LayerL2}
	}

	// Prefer SQLite for retrieval
	if l.sqlite != nil {
		items, err := l.sqlite.GetByLayer(ctx, 2, opts.UserID, opts.Limit)
		if err != nil {
			slog.Warn("failed to retrieve L2 from SQLite, fallback to MD", "error", err)
		} else if len(items) > 0 {
			return items, nil
		}
	}

	// Fallback to MD storage
	if l.mdStore != nil {
		return l.mdStore.Retrieve(ctx, opts)
	}

	return []*types.MemoryItem{}, nil
}

// LoadRecent loads recent L2 memories
func (l *L2EventsLayer) LoadRecent(ctx context.Context, userID string, days int) ([]*types.MemoryItem, error) {
	now := time.Now()
	opts := &types.RetrieveOptions{
		Layers: []types.Layer{types.LayerL2},
		UserID: userID,
		TimeRange: &types.TimeRange{
			Start: now.AddDate(0, 0, -days),
			End:   now,
		},
		Limit: 100,
	}
	return l.Retrieve(ctx, opts)
}

// GetAll returns all L2 memories
func (l *L2EventsLayer) GetAll() []*types.MemoryItem {
	items, _ := l.Retrieve(context.Background(), &types.RetrieveOptions{
		Layers: []types.Layer{types.LayerL2},
		Limit:  100,
	})
	return items
}

// GetForUser returns L2 memories for a specific user
func (l *L2EventsLayer) GetForUser(userID string) []*types.MemoryItem {
	items, _ := l.Retrieve(context.Background(), &types.RetrieveOptions{
		Layers: []types.Layer{types.LayerL2},
		UserID: userID,
		Limit:  50,
	})
	return items
}

// Compact is a no-op for L2
func (l *L2EventsLayer) Compact(ctx context.Context) error {
	return nil
}

// Close closes the layer
func (l *L2EventsLayer) Close() error {
	return nil
}

// ============ L3 Raw Layer ============

// L3RawLayer implements the L3 (Deep Search) layer
type L3RawLayer struct {
	store     *storage.SQLiteStore
	embedder  types.Embedder
	workspace string
}

// NewL3RawLayer creates an L3 layer instance
func NewL3RawLayer(workspace string, embedder types.Embedder) *L3RawLayer {
	return &L3RawLayer{
		workspace: workspace,
		embedder:  embedder,
	}
}

// SetSQLiteStore sets the SQLite store for L3 (shares with L2)
func (l *L3RawLayer) SetSQLiteStore(store *storage.SQLiteStore) {
	l.store = store
}

// Initialize initializes the L3 layer
func (l *L3RawLayer) Initialize(ctx context.Context) error {
	if l.workspace == "" {
		return nil // No workspace, skip initialization
	}

	// If store already set (shared with L2), skip creation
	if l.store != nil {
		return nil
	}

	store, err := storage.NewSQLiteStore(l.workspace, l.embedder)
	if err != nil {
		return fmt.Errorf("failed to create L3 store: %w", err)
	}

	if err := store.Initialize(ctx); err != nil {
		return fmt.Errorf("failed to initialize L3 store: %w", err)
	}

	l.store = store
	return nil
}

// Add adds raw content to L3 with embedding
func (l *L3RawLayer) Add(ctx context.Context, item *types.MemoryItem) error {
	if l.store == nil {
		return fmt.Errorf("L3 store not initialized")
	}

	if item.Layer != types.LayerL3 {
		return fmt.Errorf("invalid layer for L3: expected L3, got %d", item.Layer)
	}

	return l.store.Store(ctx, item)
}

// Search performs semantic search in L3
func (l *L3RawLayer) Search(ctx context.Context, query string, opts *types.RetrieveOptions) ([]*types.MemoryItem, error) {
	if l.store == nil {
		return []*types.MemoryItem{}, nil
	}

	results, err := l.store.Search(ctx, query, opts)
	if err != nil {
		return nil, err
	}

	items := make([]*types.MemoryItem, len(results))
	for i, r := range results {
		items[i] = r.Item
	}

	return items, nil
}

// SearchWithScores performs semantic search with similarity scores
func (l *L3RawLayer) SearchWithScores(ctx context.Context, query string, opts *types.RetrieveOptions) ([]*types.SearchResult, error) {
	if l.store == nil {
		return []*types.SearchResult{}, nil
	}

	return l.store.Search(ctx, query, opts)
}

// GetAll returns all L3 memories (not recommended for L3, use Search instead)
func (l *L3RawLayer) GetAll() []*types.MemoryItem {
	return nil // L3 is for search, not enumeration
}

// GetForUser returns L3 memories for a user (not recommended, use Search)
func (l *L3RawLayer) GetForUser(userID string) []*types.MemoryItem {
	return nil // L3 is for semantic search, not user filtering
}

// Compact is a no-op for L3
func (l *L3RawLayer) Compact(ctx context.Context) error {
	return nil
}

// Close closes the L3 layer
func (l *L3RawLayer) Close() error {
	if l.store != nil {
		return l.store.Close()
	}
	return nil
}

// DeleteBySource removes all memories from a source file
func (l *L3RawLayer) DeleteBySource(ctx context.Context, source string) error {
	if l.store == nil {
		return nil
	}
	return l.store.DeleteBySource(ctx, source)
}

// GetStore returns the underlying L3 store
func (l *L3RawLayer) GetStore() *storage.SQLiteStore {
	return l.store
}

func generateID() string {
	return fmt.Sprintf("mem_%d", time.Now().UnixNano())
}