// Package memoria provides memory query tools for the Memoria system.
// This file tests the unified memories_tool.
package memoria

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kinwyb/kanflux/memoria/types"
)

// MockMemoriesSearcher implements MemoriesSearcher for testing
type MockMemoriesSearcher struct {
	l1Items []*types.MemoryItem
	results []*types.SearchResult
}

func (m *MockMemoriesSearcher) Search(ctx context.Context, query string, opts *types.RetrieveOptions) ([]*types.SearchResult, error) {
	return m.results, nil
}

func (m *MockMemoriesSearcher) GetL1Facts(userID string) []*types.MemoryItem {
	return m.l1Items
}

func (m *MockMemoriesSearcher) GetL2Recent(ctx context.Context, userID string, days int) ([]*types.MemoryItem, error) {
	return m.l1Items, nil
}

func (m *MockMemoriesSearcher) SearchL3(ctx context.Context, query string, opts *types.RetrieveOptions) ([]*types.MemoryItem, error) {
	return nil, nil
}

func TestMemoriesTool_Name(t *testing.T) {
	searcher := &MockMemoriesSearcher{}
	tool := NewMemoriesTool(searcher)

	if tool.Name() != "memories" {
		t.Errorf("Expected name 'memories', got '%s'", tool.Name())
	}
}

func TestMemoriesTool_Description(t *testing.T) {
	searcher := &MockMemoriesSearcher{}
	tool := NewMemoriesTool(searcher)

	desc := tool.Description()
	if desc == "" {
		t.Error("Description should not be empty")
	}

	// Check that description mentions key features
	if !strings.Contains(desc, "memories") {
		t.Error("Description should mention 'memories'")
	}
}

func TestMemoriesTool_Parameters(t *testing.T) {
	searcher := &MockMemoriesSearcher{}
	tool := NewMemoriesTool(searcher)

	params := tool.Parameters()

	// Check required parameter
	if params["required"] == nil {
		t.Error("Parameters should have 'required' field")
	}

	// Check properties exist
	props := params["properties"].(map[string]interface{})
	expectedProps := []string{"query", "source_type", "limit", "days_back", "min_score", "user_id"}
	for _, prop := range expectedProps {
		if props[prop] == nil {
			t.Errorf("Parameters should have '%s' property", prop)
		}
	}
}

func TestMemoriesTool_ExecuteWithQuery(t *testing.T) {
	searcher := &MockMemoriesSearcher{
		results: []*types.SearchResult{
			{
				Item: &types.MemoryItem{
					ID:         "1",
					HallType:   types.HallFacts,
					Layer:      types.LayerL1,
					SourceType: types.SourceTypeChat,
					Content:    "User prefers dark mode",
					Summary:    "User prefers dark mode",
					Timestamp:  time.Now(),
				},
				Score:     0.9,
				Layer:     types.LayerL1,
				MatchType: "keyword",
			},
		},
	}
	tool := NewMemoriesTool(searcher)

	params := map[string]interface{}{
		"query": "dark mode",
	}

	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Errorf("Execute failed: %v", err)
	}

	if result == "" {
		t.Error("Result should not be empty")
	}

	if !strings.Contains(result, "dark mode") {
		t.Error("Result should contain query term")
	}
}

func TestMemoriesTool_ExecuteWithNoResults(t *testing.T) {
	searcher := &MockMemoriesSearcher{
		results: []*types.SearchResult{},
	}
	tool := NewMemoriesTool(searcher)

	params := map[string]interface{}{
		"query": "nonexistent",
	}

	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Errorf("Execute failed: %v", err)
	}

	// Should return no results message
	if !strings.Contains(result, "No matches") {
		t.Error("Should return no matches message")
	}
}

func TestMemoriesTool_ExecuteMissingQuery(t *testing.T) {
	searcher := &MockMemoriesSearcher{}
	tool := NewMemoriesTool(searcher)

	params := map[string]interface{}{}

	_, err := tool.Execute(context.Background(), params)
	if err == nil {
		t.Error("Should return error for missing query")
	}
}

func TestMemoriesTool_QuickSearch(t *testing.T) {
	searcher := &MockMemoriesSearcher{
		results: []*types.SearchResult{
			{
				Item: &types.MemoryItem{
					ID:        "3",
					Content:   "test content",
					Timestamp: time.Now(),
				},
				Score: 0.8,
			},
		},
	}
	tool := NewMemoriesTool(searcher)

	results, err := tool.QuickSearch(context.Background(), "test", 5)
	if err != nil {
		t.Errorf("QuickSearch failed: %v", err)
	}

	if len(results) != 1 {
		t.Errorf("Expected 1 result, got %d", len(results))
	}
}

func TestMemoriesTool_GetL1FactsShortcut(t *testing.T) {
	searcher := &MockMemoriesSearcher{
		l1Items: []*types.MemoryItem{
			{
				ID:        "l1-1",
				HallType:  types.HallFacts,
				Layer:     types.LayerL1,
				Summary:   "User prefers Go for backend",
				Timestamp: time.Now(),
			},
		},
	}
	tool := NewMemoriesTool(searcher)

	result := tool.GetL1FactsShortcut("user1")
	if result == "" {
		t.Error("GetL1FactsShortcut should return non-empty result")
	}

	if !strings.Contains(result, "Go") {
		t.Error("Result should contain L1 fact content")
	}
}

func TestMemoriesTool_ScoreFiltering(t *testing.T) {
	searcher := &MockMemoriesSearcher{
		results: []*types.SearchResult{
			{
				Item: &types.MemoryItem{
					ID:        "high",
					Content:   "high relevance",
					Timestamp: time.Now(),
				},
				Score: 0.8,
			},
			{
				Item: &types.MemoryItem{
					ID:        "low",
					Content:   "low relevance",
					Timestamp: time.Now(),
				},
				Score: 0.3,
			},
		},
	}
	tool := NewMemoriesTool(searcher)

	params := map[string]interface{}{
		"query":     "test",
		"min_score": 0.5,
	}

	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Errorf("Execute failed: %v", err)
	}

	// Should only include high score result (1 result)
	if !strings.Contains(result, "Found 1") {
		t.Error("Should filter out low score results, expected 'Found 1'")
	}
}

func TestMemoriesTool_SourceTypeFilter(t *testing.T) {
	searcher := &MockMemoriesSearcher{
		results: []*types.SearchResult{
			{
				Item: &types.MemoryItem{
					ID:         "file1",
					SourceType: types.SourceTypeFile,
					Content:    "file content",
					Timestamp:  time.Now(),
				},
				Score: 0.8,
			},
		},
	}
	tool := NewMemoriesTool(searcher)

	params := map[string]interface{}{
		"query":       "test",
		"source_type": "file",
	}

	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Errorf("Execute failed: %v", err)
	}

	// Should show file source
	if !strings.Contains(result, "file") {
		t.Error("Result should indicate file source type")
	}
}