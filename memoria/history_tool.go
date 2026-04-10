// Package memoria provides memory query tools for the Memoria system.
// This file implements the history_tool for keyword-based search across chat memories.
package memoria

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/kinwyb/kanflux/memoria/types"
)

// HistoryTool provides keyword-based search across all layers (L1/L2/L3)
// but only for chat-sourced content (user memories, decisions, preferences).
// Use this tool when you need quick keyword lookup for historical memories.
type HistoryTool struct {
	searcher HistorySearcher
}

// HistorySearcher is the interface for memory search
type HistorySearcher interface {
	// Search performs hierarchical search across all layers
	Search(ctx context.Context, query string, opts *types.RetrieveOptions) ([]*types.SearchResult, error)
	GetL1Facts(userID string) []*types.MemoryItem
	GetL2Recent(ctx context.Context, userID string, days int) ([]*types.MemoryItem, error)
}

// NewHistoryTool creates a new history query tool
func NewHistoryTool(searcher HistorySearcher) *HistoryTool {
	return &HistoryTool{
		searcher: searcher,
	}
}

// Name returns the tool name
func (t *HistoryTool) Name() string {
	return "history_query"
}

// Description returns the tool description with optimized prompts
func (t *HistoryTool) Description() string {
	return `Search chat history only.

Combines keyword and semantic matching automatically.
Use for finding past conversations and user memories.

**Parameters**:
- query: Search terms or description
- limit: Max results (default: 10)
- days_back: Limit time range in days (default: 30)`
}

// Parameters returns the JSON Schema parameter definition
func (t *HistoryTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "Search query. Keywords or natural language both work.",
			},
			"days_back": map[string]interface{}{
				"type":        "integer",
				"description": "Limit search to recent days. Default: 30.",
				"default":     30,
			},
			"limit": map[string]interface{}{
				"type":        "integer",
				"description": "Max results. Default: 10.",
				"default":     10,
			},
			"user_id": map[string]interface{}{
				"type":        "string",
				"description": "Optional. Filter by user/session ID.",
			},
		},
		"required": []string{"query"},
	}
}

// Execute performs the history search
func (t *HistoryTool) Execute(ctx context.Context, params map[string]interface{}) (string, error) {
	query, ok := params["query"].(string)
	if !ok || query == "" {
		return "", fmt.Errorf("query parameter is required")
	}

	slog.Debug("HistoryTool execute started", "query", query)

	opts := &types.RetrieveOptions{
		Query:      query,
		SourceType: types.SourceTypeChat, // Only search chat content
	}

	// Parse optional parameters
	if limit, ok := params["limit"].(int); ok && limit > 0 {
		opts.Limit = limit
	} else {
		opts.Limit = 10
	}

	if userID, ok := params["user_id"].(string); ok && userID != "" {
		opts.UserID = userID
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

	// Perform search
	startTime := time.Now()
	results, err := t.searcher.Search(ctx, query, opts)
	if err != nil {
		slog.Error("HistoryTool search failed", "query", query, "error", err)
		return "", fmt.Errorf("search failed: %w", err)
	}

	slog.Info("HistoryTool search completed",
		"query", query,
		"results", len(results),
		"days_back", daysBack,
		"duration", time.Since(startTime).Milliseconds())

	if len(results) == 0 {
		return formatNoResults(query, daysBack), nil
	}

	return formatHistoryResults(results, query), nil
}

// formatHistoryResults formats search results for display
func formatHistoryResults(results []*types.SearchResult, query string) string {
	var builder strings.Builder

	builder.WriteString(fmt.Sprintf("Found %d chat memories for '%s':\n", len(results), query))

	for _, r := range results {
		builder.WriteString(fmt.Sprintf("[%.2f] ", r.Score))

		content := r.Item.Summary
		if content == "" {
			content = r.Item.Content
			if len(content) > 100 {
				content = content[:100] + "..."
			}
		}
		builder.WriteString(fmt.Sprintf("%s (%s)\n", content, r.Item.Timestamp.Format("01-02")))
	}

	return builder.String()
}

// formatNoResults formats a message when no results found
func formatNoResults(query string, daysBack int) string {
	return fmt.Sprintf("No chat memories for '%s' (last %d days).\n"+
		"Try: different keywords or increase days_back.",
		query, daysBack)
}