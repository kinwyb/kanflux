// Package memoria provides memory query tools for the Memoria system.
// This file implements the history_tool for keyword-based search across chat memories.
package memoria

import (
	"context"
	"fmt"
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
	return `Keyword search for user memories (chat history only).

**What it searches**:
- **Source**: Chat conversations only (no files)
- **Layers**: L1 + L2 + L3 (key facts, events, raw chat history)
- **Method**: Keyword matching (fast, exact)

**Layer Contents**:
- **L1**: Critical decisions, user preferences, must-remember facts
- **L2**: Session events, milestones, discoveries
- **L3**: Full chat history for deep keyword search

**Use Cases**:
- Find previous decisions: "database choice", "auth method"
- Look up user preferences: "coding style", "testing preference"
- Recall session events: "debug session", "deployment"
- Search chat history: "we discussed", "you mentioned"

**When to Use**:
- You know specific keywords to search
- Need fast results
- Looking for user-specific memories

**Time Filtering**: Use "days_back" to limit search time range.

**Difference from knowledge_search**:
- history_query: Keyword search, chat only, L1+L2+L3
- knowledge_search: Semantic search, chat+files, L2+L3
- Use history_query for quick keyword lookup in conversations
- Use knowledge_search for conceptual search or file content`
}

// Parameters returns the JSON Schema parameter definition
func (t *HistoryTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "Search query. Use keywords for best results. Examples: 'database choice', 'auth preference', 'last session'",
			},
			"hall_types": map[string]interface{}{
				"type":        "array",
				"description": "Optional. Filter by hall types: 'hall_facts', 'hall_events', 'hall_discoveries', 'hall_preferences', 'hall_advice'",
				"items": map[string]interface{}{
					"type": "string",
					"enum": []string{"hall_facts", "hall_events", "hall_discoveries", "hall_preferences", "hall_advice"},
				},
			},
			"days_back": map[string]interface{}{
				"type":        "integer",
				"description": "Optional. Limit search to recent days. Default: 30. Use 7 for very recent, 90 for broader search.",
				"default":     30,
			},
			"limit": map[string]interface{}{
				"type":        "integer",
				"description": "Maximum results to return. Default: 10.",
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

	opts := &types.RetrieveOptions{
		Query:      query,
		SourceType: types.SourceTypeChat, // Only search chat content
		SearchMode: types.SearchModeKeyword,
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

	// Parse hall_types
	if hallTypesRaw, ok := params["hall_types"].([]interface{}); ok {
		opts.HallTypes = make([]types.HallType, 0, len(hallTypesRaw))
		for _, ht := range hallTypesRaw {
			if htStr, ok := ht.(string); ok {
				opts.HallTypes = append(opts.HallTypes, types.HallType(htStr))
			}
		}
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
	results, err := t.searcher.Search(ctx, query, opts)
	if err != nil {
		return "", fmt.Errorf("search failed: %w", err)
	}

	if len(results) == 0 {
		return formatNoResults(query, daysBack), nil
	}

	return formatHistoryResults(results, query), nil
}

// formatHistoryResults formats search results for display
func formatHistoryResults(results []*types.SearchResult, query string) string {
	var builder strings.Builder

	builder.WriteString(fmt.Sprintf("Found %d chat memories matching '%s':\n\n", len(results), query))

	for i, r := range results {
		layerName := "L1"
		if r.Layer == types.LayerL2 {
			layerName = "L2"
		} else if r.Layer == types.LayerL3 {
			layerName = "L3"
		}

		hallName := strings.Replace(string(r.Item.HallType), "hall_", "", 1)

		builder.WriteString(fmt.Sprintf("**[%s/%s] Score: %.2f (%s)**\n",
			layerName, hallName, r.Score, r.MatchType))

		if r.Item.Summary != "" {
			builder.WriteString(fmt.Sprintf("%s\n", r.Item.Summary))
		} else {
			// Truncate content if too long
			content := r.Item.Content
			if len(content) > 200 {
				content = content[:200] + "..."
			}
			builder.WriteString(fmt.Sprintf("%s\n", content))
		}

		builder.WriteString(fmt.Sprintf("Source: %s | Time: %s\n",
			r.Item.Source, r.Item.Timestamp.Format("2006-01-02 15:04")))

		if i < len(results)-1 {
			builder.WriteString("\n---\n")
		}
	}

	return builder.String()
}

// formatNoResults formats a message when no results found
func formatNoResults(query string, daysBack int) string {
	return fmt.Sprintf("No chat memories found for '%s' in the last %d days.\n\n"+
		"Suggestions:\n"+
		"- Try different keywords or broader terms\n"+
		"- Increase days_back for older memories (current: %d)\n"+
		"- Remove hall_types filter to search all categories\n"+
		"- Try knowledge_search for file content or semantic search",
		query, daysBack, daysBack)
}