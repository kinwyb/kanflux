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

	// Check that description mentions both modes
	if !strings.Contains(desc, "keyword") {
		t.Error("Description should mention 'keyword' mode")
	}
	if !strings.Contains(desc, "semantic") {
		t.Error("Description should mention 'semantic' mode")
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
	expectedProps := []string{"query", "mode", "source_type", "limit", "hall_types", "days_back", "min_score"}
	for _, prop := range expectedProps {
		if props[prop] == nil {
			t.Errorf("Parameters should have '%s' property", prop)
		}
	}
}

func TestMemoriesTool_ExecuteKeywordMode(t *testing.T) {
	searcher := &MockMemoriesSearcher{
		results: []*types.SearchResult{
			{
				Item: &types.MemoryItem{
					ID:        "1",
					HallType:  types.HallFacts,
					Layer:     types.LayerL1,
					SourceType: types.SourceTypeChat,
					Content:   "User prefers dark mode",
					Summary:   "User prefers dark mode",
					Timestamp: time.Now(),
				},
				Score:     0.9,
				Layer:     types.LayerL1,
				MatchType: "keyword",
			},
		},
	}
	tool := NewMemoriesTool(searcher)

	params := map[string]interface{}{
		"query":    "dark mode",
		"mode":     "keyword",
		"days_back": 30,
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

func TestMemoriesTool_ExecuteSemanticMode(t *testing.T) {
	searcher := &MockMemoriesSearcher{
		results: []*types.SearchResult{
			{
				Item: &types.MemoryItem{
					ID:        "2",
					HallType:  types.HallEvents,
					Layer:     types.LayerL3,
					SourceType: types.SourceTypeFile,
					Content:   "Performance optimization guide discussing caching strategies",
					Summary:   "Caching strategies for performance",
					Timestamp: time.Now(),
				},
				Score:     0.75,
				Layer:     types.LayerL3,
				MatchType: "semantic",
			},
		},
	}
	tool := NewMemoriesTool(searcher)

	params := map[string]interface{}{
		"query":     "performance optimization approaches",
		"mode":      "semantic",
		"min_score": 0.5,
	}

	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Errorf("Execute failed: %v", err)
	}

	if result == "" {
		t.Error("Result should not be empty")
	}

	// Check format includes semantic indicators
	if !strings.Contains(result, "L3") {
		t.Error("Result should show layer L3")
	}
}

func TestMemoriesTool_ExecuteDefaultMode(t *testing.T) {
	searcher := &MockMemoriesSearcher{
		results: []*types.SearchResult{},
	}
	tool := NewMemoriesTool(searcher)

	// Without mode parameter, should default to keyword
	params := map[string]interface{}{
		"query": "test",
	}

	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Errorf("Execute failed: %v", err)
	}

	// Should return no results message for keyword mode
	if !strings.Contains(result, "last 30 days") {
		t.Error("Default mode should be keyword (shows days_back info)")
	}
}

func TestMemoriesTool_ExecuteMissingQuery(t *testing.T) {
	searcher := &MockMemoriesSearcher{}
	tool := NewMemoriesTool(searcher)

	params := map[string]interface{}{
		"mode": "keyword",
	}

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

func TestMemoriesTool_HallTypesConversion(t *testing.T) {
	// Test that short hall type names are converted correctly
	searcher := &MockMemoriesSearcher{}
	tool := NewMemoriesTool(searcher)

	params := map[string]interface{}{
		"query":     "test",
		"mode":      "keyword",
		"hall_types": []interface{}{"facts", "events"},
	}

	// Execute should handle hall_types conversion
	_, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Errorf("Execute failed with hall_types: %v", err)
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
		"mode":      "semantic",
		"min_score": 0.5,
	}

	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Errorf("Execute failed: %v", err)
	}

	// Should only include high score result
	if strings.Contains(result, "1 results") {
		// Correct - filtered to 1 result
	} else if strings.Contains(result, "2 results") {
		t.Error("Should filter out low score results")
	}
}