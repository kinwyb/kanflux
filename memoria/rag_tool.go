// Package memoria provides memory query tools for the Memoria system.
// This file implements the rag_tool for semantic search over all content.
package memoria

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/kinwyb/kanflux/memoria/types"
)

// RAGTool provides semantic search across L2 and L3 layers for all content
// (both chat and file sources). Use this tool for deep, comprehensive search.
type RAGTool struct {
	searcher RAGSearcher
}

// RAGSearcher is the interface for semantic memory search
type RAGSearcher interface {
	SearchL3(ctx context.Context, query string, opts *types.RetrieveOptions) ([]*types.MemoryItem, error)
	Search(ctx context.Context, query string, opts *types.RetrieveOptions) ([]*types.SearchResult, error)
}

// NewRAGTool creates a new RAG query tool
func NewRAGTool(searcher RAGSearcher) *RAGTool {
	return &RAGTool{
		searcher: searcher,
	}
}

// Name returns the tool name
func (t *RAGTool) Name() string {
	return "knowledge_search"
}

// Description returns the tool description with optimized prompts
func (t *RAGTool) Description() string {
	return `Deep search across all knowledge (chat + files).

Combines keyword and semantic matching automatically.
Use for comprehensive search when you need broader context.

**Parameters**:
- query: Search terms or natural language description
- limit: Max results (default: 10)
- min_score: Minimum relevance threshold (default: 0.5)
- source_type: Filter by "chat" or "file" (optional)`
}

// Parameters returns the JSON Schema parameter definition
func (t *RAGTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "Search query. Keywords or natural language both work well.",
			},
			"limit": map[string]interface{}{
				"type":        "integer",
				"description": "Max results. Default: 10.",
				"default":     10,
			},
			"min_score": map[string]interface{}{
				"type":        "number",
				"description": "Minimum relevance threshold (0-1). Default: 0.5.",
				"default":     0.5,
			},
			"source_type": map[string]interface{}{
				"type":        "string",
				"description": "Filter by source: 'chat' or 'file'. Default: all sources.",
				"enum":        []string{"chat", "file"},
			},
		},
		"required": []string{"query"},
	}
}

// Execute performs the semantic search across all layers
func (t *RAGTool) Execute(ctx context.Context, params map[string]interface{}) (string, error) {
	query, ok := params["query"].(string)
	if !ok || query == "" {
		return "", fmt.Errorf("query parameter is required")
	}

	slog.Debug("RAGTool execute started", "query", query)

	opts := &types.RetrieveOptions{
		Query:      query,
		SearchMode: types.SearchModeSemantic,
		Layers:     []types.Layer{types.LayerL2, types.LayerL3}, // L2 + L3 only
	}

	// Parse optional parameters
	if limit, ok := params["limit"].(int); ok && limit > 0 {
		opts.Limit = limit
	} else {
		opts.Limit = 10
	}

	minScore := 0.5
	if ms, ok := params["min_score"].(float64); ok && ms > 0 {
		minScore = ms
	}

	// Parse source_type filter
	if sourceType, ok := params["source_type"].(string); ok && sourceType != "" {
		opts.SourceType = types.SourceType(sourceType)
	}

	// Perform semantic search across all layers
	startTime := time.Now()
	results, err := t.searcher.Search(ctx, query, opts)
	if err != nil {
		slog.Error("RAGTool search failed", "query", query, "error", err)
		return "", fmt.Errorf("semantic search failed: %w", err)
	}

	// Filter by minimum score
	filteredResults := make([]*types.SearchResult, 0)
	for _, r := range results {
		if r.Score >= minScore {
			filteredResults = append(filteredResults, r)
		}
	}

	slog.Info("RAGTool search completed",
		"query", query,
		"total_results", len(results),
		"filtered_results", len(filteredResults),
		"min_score", minScore,
		"duration", time.Since(startTime).Milliseconds())

	if len(filteredResults) == 0 {
		return formatNoRAGResults(query, minScore), nil
	}

	return formatRAGResults(filteredResults, query), nil
}

// formatRAGResults formats semantic search results
func formatRAGResults(results []*types.SearchResult, query string) string {
	var builder strings.Builder

	builder.WriteString(fmt.Sprintf("Found %d for '%s':\n", len(results), query))

	for i, r := range results {
		sourceType := "chat"
		if r.Item.SourceType == types.SourceTypeFile {
			sourceType = "file"
		}

		builder.WriteString(fmt.Sprintf("[%s %.2f] ", sourceType, r.Score))

		content := r.Item.Summary
		if content == "" {
			content = r.Item.Content
			if len(content) > 100 {
				content = content[:100] + "..."
			}
		}
		builder.WriteString(fmt.Sprintf("%s (%s)\n", content, r.Item.Timestamp.Format("01-02")))

		if i < len(results)-1 && len(results) <= 10 {
			builder.WriteString("")
		}
	}

	return builder.String()
}

// formatNoRAGResults formats a message when no semantic matches found
func formatNoRAGResults(query string, minScore float64) string {
	return fmt.Sprintf("No matches for '%s' (min score: %.2f).\n"+
		"Try: different keywords, lower min_score.",
		query, minScore)
}

// QuickSearch performs a combined L1/L2/L3 search with automatic layer selection
// This is a convenience method for tools that want all results
func (t *RAGTool) QuickSearch(ctx context.Context, query string, limit int) ([]*types.SearchResult, error) {
	opts := &types.RetrieveOptions{
		Query:      query,
		Limit:      limit,
		SearchMode: types.SearchModeSemantic,
		Layers:     []types.Layer{types.LayerL2, types.LayerL3},
	}
	return t.searcher.Search(ctx, query, opts)
}