package memoria

import (
	"context"
	"testing"
	"time"

	"github.com/kinwyb/kanflux/memoria/types"
)

// MockRAGSearcher for testing
type MockRAGSearcher struct {
	results []*types.SearchResult
	err     error
}

func (m *MockRAGSearcher) SearchL3(ctx context.Context, query string, opts *types.RetrieveOptions) ([]*types.MemoryItem, error) {
	items := make([]*types.MemoryItem, len(m.results))
	for i, r := range m.results {
		items[i] = r.Item
	}
	return items, m.err
}

func (m *MockRAGSearcher) Search(ctx context.Context, query string, opts *types.RetrieveOptions) ([]*types.SearchResult, error) {
	return m.results, m.err
}

func TestRAGTool(t *testing.T) {
	t.Run("name and description", func(t *testing.T) {
		searcher := &MockRAGSearcher{}
		tool := NewRAGTool(searcher)

		if tool.Name() != "knowledge_search" {
			t.Errorf("expected name 'knowledge_search', got '%s'", tool.Name())
		}

		if tool.Description() == "" {
			t.Error("description should not be empty")
		}

		params := tool.Parameters()
		if params == nil {
			t.Error("parameters should not be nil")
		}
	})

	t.Run("execute with query", func(t *testing.T) {
		results := []*types.SearchResult{
			{
				Item: &types.MemoryItem{
					ID:         "rag1",
					HallType:   types.HallFacts,
					Layer:      types.LayerL2,
					SourceType: types.SourceTypeChat,
					Summary:    "Use PostgreSQL for main database",
					Source:     "chat/session1.jsonl",
					Timestamp:  time.Now(),
				},
				Score:     0.85,
				MatchType: "semantic",
				Layer:     types.LayerL2,
			},
			{
				Item: &types.MemoryItem{
					ID:         "rag2",
					HallType:   types.HallEvents,
					Layer:      types.LayerL3,
					SourceType: types.SourceTypeFile,
					Summary:    "Performance optimization in config file",
					Source:     "docs/config.md",
					Timestamp:  time.Now(),
				},
				Score:     0.72,
				MatchType: "semantic",
				Layer:     types.LayerL3,
			},
		}
		searcher := &MockRAGSearcher{results: results}
		tool := NewRAGTool(searcher)

		ctx := context.Background()
		params := map[string]interface{}{
			"query":     "database",
			"min_score": 0.5,
		}

		output, err := tool.Execute(ctx, params)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		if output == "" {
			t.Error("output should not be empty")
		}

		// Should show found results
		if !contains(output, "Found") {
			t.Error("output should indicate results found")
		}
	})

	t.Run("execute without query", func(t *testing.T) {
		searcher := &MockRAGSearcher{}
		tool := NewRAGTool(searcher)

		ctx := context.Background()
		params := map[string]interface{}{
			"limit": 5,
		}

		_, err := tool.Execute(ctx, params)
		if err == nil {
			t.Error("expected error for missing query")
		}
	})

	t.Run("execute with source_type filter", func(t *testing.T) {
		results := []*types.SearchResult{
			{
				Item: &types.MemoryItem{
					ID:         "chat_item",
					Layer:      types.LayerL2,
					SourceType: types.SourceTypeChat,
					Summary:    "Chat content",
					Source:     "chat/test",
					Timestamp:  time.Now(),
				},
				Score:     0.9,
				Layer:     types.LayerL2,
				MatchType: "semantic",
			},
		}
		searcher := &MockRAGSearcher{results: results}
		tool := NewRAGTool(searcher)

		ctx := context.Background()
		params := map[string]interface{}{
			"query":       "test",
			"source_type": "chat",
		}

		output, err := tool.Execute(ctx, params)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		// Should show chat source type
		if !contains(output, "chat") {
			t.Error("output should show chat source type")
		}
	})

	t.Run("execute with min_score filter", func(t *testing.T) {
		results := []*types.SearchResult{
			{
				Item: &types.MemoryItem{
					ID:         "low_score",
					Layer:      types.LayerL2,
					SourceType: types.SourceTypeChat,
					Content:    "low relevance",
					Source:     "test",
					Timestamp:  time.Now(),
				},
				Score:     0.3, // Below min_score
				Layer:     types.LayerL2,
				MatchType: "semantic",
			},
		}
		searcher := &MockRAGSearcher{results: results}
		tool := NewRAGTool(searcher)

		ctx := context.Background()
		params := map[string]interface{}{
			"query":     "test",
			"min_score": 0.5, // Filter out score 0.3
		}

		output, err := tool.Execute(ctx, params)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		if !contains(output, "No matches") {
			t.Error("should show no results message for low scores")
		}
	})

	t.Run("quick search method", func(t *testing.T) {
		searcher := &MockRAGSearcher{results: []*types.SearchResult{}}
		tool := NewRAGTool(searcher)

		ctx := context.Background()
		results, err := tool.QuickSearch(ctx, "test", 10)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if results == nil {
			t.Error("results should not be nil")
		}
	})
}