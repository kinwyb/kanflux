package memoria

import (
	"context"
	"testing"
	"time"

	"github.com/kinwyb/kanflux/memoria/types"
)

// MockHistorySearcher for testing
type MockHistorySearcher struct {
	results []*types.SearchResult
	err     error
}

func (m *MockHistorySearcher) Search(ctx context.Context, query string, opts *types.RetrieveOptions) ([]*types.SearchResult, error) {
	return m.results, m.err
}

func (m *MockHistorySearcher) GetL1Facts(userID string) []*types.MemoryItem {
	return nil
}

func (m *MockHistorySearcher) GetL2Recent(ctx context.Context, userID string, days int) ([]*types.MemoryItem, error) {
	return nil, nil
}

func TestHistoryTool(t *testing.T) {
	t.Run("name and description", func(t *testing.T) {
		searcher := &MockHistorySearcher{}
		tool := NewHistoryTool(searcher)

		if tool.Name() != "history_query" {
			t.Errorf("expected name 'history_query', got '%s'", tool.Name())
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
					ID:       "test1",
					HallType: types.HallFacts,
					Layer:    types.LayerL1,
					Summary:  "Use PostgreSQL for database",
					Source:   "session1",
					Timestamp: time.Now(),
				},
				Score:     0.9,
				MatchType: "keyword",
			},
		}
		searcher := &MockHistorySearcher{results: results}
		tool := NewHistoryTool(searcher)

		ctx := context.Background()
		params := map[string]interface{}{
			"query": "database",
			"limit": 10,
		}

		output, err := tool.Execute(ctx, params)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		if output == "" {
			t.Error("output should not be empty")
		}

		if !contains(output, "database") {
			t.Error("output should contain query term")
		}
	})

	t.Run("execute without query", func(t *testing.T) {
		searcher := &MockHistorySearcher{}
		tool := NewHistoryTool(searcher)

		ctx := context.Background()
		params := map[string]interface{}{
			"limit": 10,
		}

		_, err := tool.Execute(ctx, params)
		if err == nil {
			t.Error("expected error for missing query")
		}
	})

	t.Run("execute with no results", func(t *testing.T) {
		searcher := &MockHistorySearcher{results: []*types.SearchResult{}}
		tool := NewHistoryTool(searcher)

		ctx := context.Background()
		params := map[string]interface{}{
			"query": "nonexistent",
		}

		output, err := tool.Execute(ctx, params)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		if !contains(output, "No chat memories") {
			t.Error("should show no results message")
		}
	})

	t.Run("execute with hall_types filter", func(t *testing.T) {
		searcher := &MockHistorySearcher{results: []*types.SearchResult{}}
		tool := NewHistoryTool(searcher)

		ctx := context.Background()
		params := map[string]interface{}{
			"query":      "test",
			"hall_types": []interface{}{"hall_facts", "hall_events"},
		}

		_, err := tool.Execute(ctx, params)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}