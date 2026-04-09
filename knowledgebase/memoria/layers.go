package memoria

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/kinwyb/kanflux/knowledgebase/memoria/llm"
	"github.com/kinwyb/kanflux/knowledgebase/memoria/types"
)

// L1FactsLayer implements the L1 (Key Facts) layer
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

// Add adds a new L1 memory item
func (l *L1FactsLayer) Add(ctx context.Context, item *types.MemoryItem) error {
	if item.Layer != types.LayerL1 {
		return fmt.Errorf("invalid layer for L1: expected L1, got %d", item.Layer)
	}

	if item.HallType != types.HallFacts && item.HallType != types.HallPreferences {
		return fmt.Errorf("invalid hall type for L1: expected facts or preferences, got %s", item.HallType)
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
type L2EventsLayer struct {
	storage   types.Storage
	maxTokens int
}

// NewL2EventsLayer creates an L2 layer instance
func NewL2EventsLayer(storage types.Storage, maxTokens int) *L2EventsLayer {
	return &L2EventsLayer{
		storage:   storage,
		maxTokens: maxTokens,
	}
}

// Initialize initializes the L2 layer
func (l *L2EventsLayer) Initialize(ctx context.Context) error {
	return nil
}

// Add adds a new L2 memory item
func (l *L2EventsLayer) Add(ctx context.Context, item *types.MemoryItem) error {
	if item.Layer != types.LayerL2 {
		return fmt.Errorf("invalid layer for L2: expected L2, got %d", item.Layer)
	}

	if item.HallType != types.HallEvents && item.HallType != types.HallDiscoveries {
		return fmt.Errorf("invalid hall type for L2: expected events or discoveries, got %s", item.HallType)
	}

	return l.storage.Store(ctx, item)
}

// Retrieve retrieves L2 memories matching criteria
func (l *L2EventsLayer) Retrieve(ctx context.Context, opts *types.RetrieveOptions) ([]*types.MemoryItem, error) {
	if len(opts.Layers) == 0 {
		opts.Layers = []types.Layer{types.LayerL2}
	}
	return l.storage.Retrieve(ctx, opts)
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
	return l.storage.Retrieve(ctx, opts)
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
	wing string
}

// NewL3RawLayer creates an L3 layer instance
func NewL3RawLayer(kb interface{}) *L3RawLayer {
	return &L3RawLayer{
		wing: "memoria_l3",
	}
}

// Initialize initializes the L3 layer
func (l *L3RawLayer) Initialize(ctx context.Context) error {
	return nil
}

// Add adds raw content to L3
func (l *L3RawLayer) Add(ctx context.Context, item *types.MemoryItem) error {
	if item.Layer != types.LayerL3 {
		return fmt.Errorf("invalid layer for L3: expected L3, got %d", item.Layer)
	}
	return nil
}

// Search performs semantic search
func (l *L3RawLayer) Search(ctx context.Context, query string, opts *types.RetrieveOptions) ([]*types.MemoryItem, error) {
	return []*types.MemoryItem{}, nil
}

// GetAll returns all L3 memories
func (l *L3RawLayer) GetAll() []*types.MemoryItem {
	return nil
}

// GetForUser returns L3 memories for a user
func (l *L3RawLayer) GetForUser(userID string) []*types.MemoryItem {
	return nil
}

// Compact is a no-op for L3
func (l *L3RawLayer) Compact(ctx context.Context) error {
	return nil
}

// Close closes the layer
func (l *L3RawLayer) Close() error {
	return nil
}

func generateID() string {
	return fmt.Sprintf("mem_%d", time.Now().UnixNano())
}