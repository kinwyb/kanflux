// Package memoria provides memory query tools for the Memoria system.
// This file implements the unified memories_tool that combines history_query and rag_tool.
package memoria

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kinwyb/kanflux/memoria/types"
)

// MemoriesTool provides unified memory search with intelligent mode selection.
// It combines keyword search (history) and semantic search (RAG) into one tool,
// reducing context usage while maintaining comprehensive search capabilities.
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
	return `Search memories with intelligent mode selection.

**Search Modes**:
- **keyword** (default): Fast keyword matching, chat-only, L1+L2+L3
- **semantic**: Deep semantic search, chat+files, L2+L3

**Layer Contents**:
- **L1**: Critical decisions, user preferences (always loaded)
- **L2**: Session events, milestones, file summaries
- **L3**: Full chat history and file content (vector indexed)

**Quick Guide**:
| Need | Mode | Source | Example |
|------|------|--------|---------|
| Find decisions/preferences | keyword | chat | "database choice" |
| Search chat history | keyword | chat | "we discussed auth" |
| Search files/concepts | semantic | all | "performance patterns" |
| Semantic/conceptual search | semantic | all | "optimization approaches" |

**Parameters by Mode**:
- keyword: Use hall_types, days_back for filtering
- semantic: Use min_score for relevance threshold

**Tips**:
- Start with keyword for known terms
- Use semantic when keywords don't match
- Combine source_type filter ("chat"/"file") for targeted search`
}

// Parameters returns the JSON Schema parameter definition
func (t *MemoriesTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "Search query. For keyword mode: use exact terms. For semantic: use natural language.",
			},
			"mode": map[string]interface{}{
				"type":        "string",
				"description": "Search mode: 'keyword' for fast exact matching (chat only), 'semantic' for deep concept search (chat+files). Default: keyword.",
				"default":     "keyword",
				"enum":        []string{"keyword", "semantic"},
			},
			"source_type": map[string]interface{}{
				"type":        "string",
				"description": "Filter by source: 'chat' for conversations, 'file' for documents. Default: all sources (semantic mode), chat only (keyword mode).",
				"enum":        []string{"chat", "file"},
			},
			"limit": map[string]interface{}{
				"type":        "integer",
				"description": "Max results. Default: 10.",
				"default":     10,
			},
			// Keyword mode specific
			"hall_types": map[string]interface{}{
				"type":        "array",
				"description": "Keyword mode only. Filter by category: 'facts', 'events', 'discoveries', 'preferences', 'advice'.",
				"items": map[string]interface{}{
					"type": "string",
					"enum": []string{"facts", "events", "discoveries", "preferences", "advice"},
				},
			},
			"days_back": map[string]interface{}{
				"type":        "integer",
				"description": "Keyword mode only. Limit to recent days. Default: 30.",
				"default":     30,
			},
			// Semantic mode specific
			"min_score": map[string]interface{}{
				"type":        "number",
				"description": "Semantic mode only. Minimum similarity (0-1). Default: 0.5. Higher = more relevant.",
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

// Execute performs the unified memory search
func (t *MemoriesTool) Execute(ctx context.Context, params map[string]interface{}) (string, error) {
	query, ok := params["query"].(string)
	if !ok || query == "" {
		return "", fmt.Errorf("query parameter is required")
	}

	// Determine search mode
	mode := "keyword"
	if m, ok := params["mode"].(string); ok && m == "semantic" {
		mode = "semantic"
	}

	// Build search options based on mode
	opts := &types.RetrieveOptions{
		Query: query,
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

	// Mode-specific configuration
	if mode == "keyword" {
		opts.SearchMode = types.SearchModeKeyword
		opts.SourceType = types.SourceTypeChat // Keyword mode defaults to chat
		opts.Layers = []types.Layer{types.LayerL1, types.LayerL2, types.LayerL3}

		// Allow source_type override (but warn it's unusual for keyword)
		if st, ok := params["source_type"].(string); ok && st == "file" {
			opts.SourceType = types.SourceTypeFile
		}

		// Parse hall_types (convert short names to full names)
		if hallTypesRaw, ok := params["hall_types"].([]interface{}); ok {
			opts.HallTypes = make([]types.HallType, 0, len(hallTypesRaw))
			for _, ht := range hallTypesRaw {
				if htStr, ok := ht.(string); ok {
					fullName := "hall_" + htStr
					opts.HallTypes = append(opts.HallTypes, types.HallType(fullName))
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

		return t.executeKeywordSearch(ctx, query, opts, daysBack)
	}

	// Semantic mode
	opts.SearchMode = types.SearchModeSemantic
	opts.Layers = []types.Layer{types.LayerL2, types.LayerL3}

	// Parse source_type filter
	if st, ok := params["source_type"].(string); ok && st != "" {
		opts.SourceType = types.SourceType(st)
	}

	minScore := 0.5
	if ms, ok := params["min_score"].(float64); ok && ms > 0 {
		minScore = ms
	}

	return t.executeSemanticSearch(ctx, query, opts, minScore)
}

// executeKeywordSearch performs keyword-based search (history_query style)
func (t *MemoriesTool) executeKeywordSearch(ctx context.Context, query string, opts *types.RetrieveOptions, daysBack int) (string, error) {
	results, err := t.searcher.Search(ctx, query, opts)
	if err != nil {
		return "", fmt.Errorf("keyword search failed: %w", err)
	}

	if len(results) == 0 {
		return formatNoKeywordResults(query, daysBack), nil
	}

	return formatKeywordResults(results, query), nil
}

// executeSemanticSearch performs semantic search (rag_tool style)
func (t *MemoriesTool) executeSemanticSearch(ctx context.Context, query string, opts *types.RetrieveOptions, minScore float64) (string, error) {
	results, err := t.searcher.Search(ctx, query, opts)
	if err != nil {
		return "", fmt.Errorf("semantic search failed: %w", err)
	}

	// Filter by minimum score
	filtered := make([]*types.SearchResult, 0)
	for _, r := range results {
		if r.Score >= minScore {
			filtered = append(filtered, r)
		}
	}

	if len(filtered) == 0 {
		return formatNoSemanticResults(query, minScore), nil
	}

	return formatSemanticResults(filtered, query), nil
}

// formatKeywordResults formats keyword search results (compact format)
func formatKeywordResults(results []*types.SearchResult, query string) string {
	var builder strings.Builder

	builder.WriteString(fmt.Sprintf("Found %d memories for '%s':\n", len(results), query))

	for _, r := range results {
		layerName := "L1"
		if r.Layer == types.LayerL2 {
			layerName = "L2"
		} else if r.Layer == types.LayerL3 {
			layerName = "L3"
		}

		hallName := strings.Replace(string(r.Item.HallType), "hall_", "", 1)

		builder.WriteString(fmt.Sprintf("[%s/%s] ", layerName, hallName))

		// Show summary (compact)
		if r.Item.Summary != "" {
			content := r.Item.Summary
			if len(content) > 100 {
				content = content[:100] + "..."
			}
			builder.WriteString(content)
		} else {
			content := r.Item.Content
			if len(content) > 80 {
				content = content[:80] + "..."
			}
			builder.WriteString(content)
		}

		builder.WriteString(fmt.Sprintf(" (%s)\n", r.Item.Timestamp.Format("01-02")))
	}

	return builder.String()
}

// formatSemanticResults formats semantic search results (compact format)
func formatSemanticResults(results []*types.SearchResult, query string) string {
	var builder strings.Builder

	builder.WriteString(fmt.Sprintf("Semantic search '%s': %d results\n", query, len(results)))

	for _, r := range results {
		layerName := "L2"
		if r.Layer == types.LayerL3 {
			layerName = "L3"
		}

		sourceType := "chat"
		if r.Item.SourceType == types.SourceTypeFile {
			sourceType = "file"
		}

		builder.WriteString(fmt.Sprintf("[%s/%s %.2f] ", layerName, sourceType, r.Score))

		// Show content (compact)
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

// formatNoKeywordResults formats no results for keyword mode
func formatNoKeywordResults(query string, daysBack int) string {
	return fmt.Sprintf("No memories for '%s' (last %d days).\n"+
		"Try: different keywords, increase days_back, or use mode='semantic'",
		query, daysBack)
}

// formatNoSemanticResults formats no results for semantic mode
func formatNoSemanticResults(query string, minScore float64) string {
	return fmt.Sprintf("No semantic matches for '%s' (min %.2f).\n"+
		"Try: natural language query, lower min_score, or use mode='keyword'",
		query, minScore)
}

// QuickSearch provides a simplified search interface for internal use
// It automatically selects the best mode based on query characteristics
func (t *MemoriesTool) QuickSearch(ctx context.Context, query string, limit int) ([]*types.SearchResult, error) {
	// Simple heuristic: short queries with known terms use keyword
	if len(strings.Fields(query)) <= 3 && !strings.ContainsAny(query, "?") {
		opts := &types.RetrieveOptions{
			Query:      query,
			SearchMode: types.SearchModeKeyword,
			Layers:     []types.Layer{types.LayerL1, types.LayerL2, types.LayerL3},
			SourceType: types.SourceTypeChat,
			Limit:      limit,
		}
		return t.searcher.Search(ctx, query, opts)
	}

	// Longer or question-like queries use semantic
	opts := &types.RetrieveOptions{
		Query:      query,
		SearchMode: types.SearchModeSemantic,
		Layers:     []types.Layer{types.LayerL2, types.LayerL3},
		Limit:      limit,
	}
	return t.searcher.Search(ctx, query, opts)
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