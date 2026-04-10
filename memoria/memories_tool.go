// Package memoria provides memory query tools for the Memoria system.
// This file implements the unified memories_tool that combines history_query and rag_tool.
package memoria

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/kinwyb/kanflux/memoria/types"
)

// MemoriesTool provides unified memory search combining keyword and semantic matching.
// It searches across all sources (chat + files) with automatic search strategy selection.
type MemoriesTool struct {
	searcher MemoriesSearcher
}

// MemoriesSearcher is the combined interface for all memory search operations
type MemoriesSearcher interface {
	// Search performs hierarchical search across all layers
	Search(ctx context.Context, query string, opts *types.RetrieveOptions) ([]*types.SearchResult, error)
	// GetL1Facts returns L1 facts for a user (always loaded, key memories)
	GetL1Facts(userID string) []*types.MemoryItem
	// GetL2Recent returns recent L2 events for a user
	GetL2Recent(ctx context.Context, userID string, days int) ([]*types.MemoryItem, error)
	// SearchL3 performs semantic search in L3 layer
	SearchL3(ctx context.Context, query string, opts *types.RetrieveOptions) ([]*types.MemoryItem, error)
}

// NewMemoriesTool creates a new unified memories search tool
func NewMemoriesTool(searcher MemoriesSearcher) *MemoriesTool {
	return &MemoriesTool{
		searcher: searcher,
	}
}

// Name returns the tool name
func (t *MemoriesTool) Name() string {
	return "memories"
}

// Description returns the unified tool description
func (t *MemoriesTool) Description() string {
	return `Search all memories (chat + files).

Search combines keyword matching and semantic understanding automatically.
Use natural language or keywords - both work well.

**Parameters**:
- query: Search terms or natural language description
- source_type: Filter by "chat" or "file" (optional)
- limit: Max results (default: 10)
- days_back: Limit time range in days (optional)
- min_score: Minimum relevance threshold (optional)`
}

// Parameters returns the JSON Schema parameter definition
func (t *MemoriesTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "Search query. Use keywords or natural language - both work.",
			},
			"source_type": map[string]interface{}{
				"type":        "string",
				"description": "Filter by source: 'chat' for conversations, 'file' for documents. Default: all sources.",
				"enum":        []string{"chat", "file"},
			},
			"limit": map[string]interface{}{
				"type":        "integer",
				"description": "Max results. Default: 10.",
				"default":     10,
			},
			"days_back": map[string]interface{}{
				"type":        "integer",
				"description": "Limit to recent days. Default: 30.",
				"default":     30,
			},
			"min_score": map[string]interface{}{
				"type":        "number",
				"description": "Minimum relevance threshold (0-1). Default: 0.5.",
				"default":     0.5,
			},
			"user_id": map[string]interface{}{
				"type":        "string",
				"description": "Optional. Filter by user/session ID.",
			},
		},
		"required": []string{"query"},
	}
}

// Execute performs unified memory search
func (t *MemoriesTool) Execute(ctx context.Context, params map[string]interface{}) (string, error) {
	query, ok := params["query"].(string)
	if !ok || query == "" {
		return "", fmt.Errorf("query parameter is required")
	}

	slog.Debug("MemoriesTool execute started", "query", query)

	opts := &types.RetrieveOptions{
		Query: query,
		Layers: []types.Layer{types.LayerL1, types.LayerL2, types.LayerL3},
	}

	// Parse common parameters
	if limit, ok := params["limit"].(int); ok && limit > 0 {
		opts.Limit = limit
	} else {
		opts.Limit = 10
	}

	if userID, ok := params["user_id"].(string); ok && userID != "" {
		opts.UserID = userID
	}

	// Parse source_type filter
	if st, ok := params["source_type"].(string); ok && st != "" {
		opts.SourceType = types.SourceType(st)
	}

	// Parse days_back for time range
	daysBack := 30
	if db, ok := params["days_back"].(int); ok && db > 0 {
		daysBack = db
	}
	opts.TimeRange = &types.TimeRange{
		Start: time.Now().AddDate(0, 0, -daysBack),
		End:   time.Now(),
	}

	// Parse min_score threshold
	minScore := 0.5
	if ms, ok := params["min_score"].(float64); ok && ms > 0 {
		minScore = ms
	}

	startTime := time.Now()

	// Perform unified search (combines keyword + semantic)
	results, err := t.searcher.Search(ctx, query, opts)
	if err != nil {
		slog.Error("MemoriesTool search failed", "query", query, "error", err)
		return "", fmt.Errorf("search failed: %w", err)
	}

	// Filter by minimum score
	filtered := make([]*types.SearchResult, 0)
	for _, r := range results {
		if r.Score >= minScore {
			filtered = append(filtered, r)
		}
	}

	slog.Info("MemoriesTool search completed",
		"query", query,
		"total_results", len(results),
		"filtered_results", len(filtered),
		"duration", time.Since(startTime).Milliseconds())

	if len(filtered) == 0 {
		return formatMemoriesNoResults(query, daysBack, minScore), nil
	}

	return formatMemoriesResults(filtered, query), nil
}

// formatMemoriesResults formats search results in compact format
func formatMemoriesResults(results []*types.SearchResult, query string) string {
	var builder strings.Builder

	builder.WriteString(fmt.Sprintf("Found %d for '%s':\n", len(results), query))

	for _, r := range results {
		sourceType := "chat"
		if r.Item.SourceType == types.SourceTypeFile {
			sourceType = "file"
		}

		builder.WriteString(fmt.Sprintf("[%s %.2f] ", sourceType, r.Score))

		content := r.Item.Summary
		if content == "" {
			content = r.Item.Content
		}
		if len(content) > 100 {
			content = content[:100] + "..."
		}
		builder.WriteString(content)

		builder.WriteString(fmt.Sprintf(" (%s)\n", r.Item.Timestamp.Format("01-02")))
	}

	return builder.String()
}

// formatMemoriesNoResults formats message when no matches found
func formatMemoriesNoResults(query string, daysBack int, minScore float64) string {
	return fmt.Sprintf("No matches for '%s' (days: %d, score: %.2f).\n"+
		"Try: different keywords, broader time range, or lower min_score.",
		query, daysBack, minScore)
}

// QuickSearch provides a simplified search interface for internal use
func (t *MemoriesTool) QuickSearch(ctx context.Context, query string, limit int) ([]*types.SearchResult, error) {
	slog.Debug("QuickSearch started", "query", query, "limit", limit)

	opts := &types.RetrieveOptions{
		Query:  query,
		Layers: []types.Layer{types.LayerL1, types.LayerL2, types.LayerL3},
		Limit:  limit,
	}
	results, err := t.searcher.Search(ctx, query, opts)
	if err == nil {
		slog.Debug("QuickSearch completed", "results", len(results))
	}
	return results, err
}

// GetL1FactsShortcut provides quick access to L1 facts without search
func (t *MemoriesTool) GetL1FactsShortcut(userID string) string {
	items := t.searcher.GetL1Facts(userID)
	if len(items) == 0 {
		return "No key facts stored."
	}

	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("%d key facts:\n", len(items)))
	for _, item := range items {
		builder.WriteString(fmt.Sprintf("- %s\n", item.Summary))
	}
	return builder.String()
}