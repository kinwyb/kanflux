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
	return `Deep search across L2 + L3 knowledge (summaries + raw content).

**Search Strategy**:
1. semantic search first (vector similarity, understands meaning)
2. keyword search fallback (FTS5, exact matching)
3. Layer order: L2 (summaries) → L3 (full content)

**What it searches**:
- **Layers**: L2 + L3 only (not L1)
- **Sources**: Chat conversations + File content
- **Types**: All hall types (facts, events, discoveries, etc.)

**When to Use**:
- Looking for conceptually related information
- Keyword search didn't find what you need
- Searching across both conversations and documents
- Don't know exact keywords

**Tips for Better Results**:
- Use natural language queries
- Describe what you're looking for conceptually
- Example: "performance optimization" finds related discussions

**Difference from history_query**:
- history_query: Chat only, L2+L3, semantic+keyword
- knowledge_search: Chat+files, L2+L3, semantic+keyword
- Use history_query for chat-only quick search
- Use knowledge_search for comprehensive search including files`
}

// Parameters returns the JSON Schema parameter definition
func (t *RAGTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "Semantic search query. Use natural language for best results. The system understands meaning, not just keywords.",
			},
			"limit": map[string]interface{}{
				"type":        "integer",
				"description": "Maximum results to return. Default: 10.",
				"default":     10,
			},
			"min_score": map[string]interface{}{
				"type":        "number",
				"description": "Minimum similarity score (0-1). Default: 0.5. Higher values return more relevant results.",
				"default":     0.5,
			},
			"source_type": map[string]interface{}{
				"type":        "string",
				"description": "Optional. Filter by source: 'chat' for conversations only, 'file' for documents only. Default: search all sources.",
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

	builder.WriteString(fmt.Sprintf("Search for '%s' found %d items:\n\n", query, len(results)))

	for i, r := range results {
		layerName := "L2"
		if r.Layer == types.LayerL3 {
			layerName = "L3"
		}

		sourceType := "chat"
		if r.Item.SourceType == types.SourceTypeFile {
			sourceType = "file"
		}

		builder.WriteString(fmt.Sprintf("**[%s/%s] Score: %.2f (%s)**\n",
			layerName, sourceType, r.Score, r.MatchType))

		// Show content based on layer
		if r.Item.Summary != "" {
			builder.WriteString(fmt.Sprintf("%s\n", r.Item.Summary))
		} else {
			content := r.Item.Content
			if len(content) > 300 {
				content = content[:300] + "..."
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

// formatNoRAGResults formats a message when no semantic matches found
func formatNoRAGResults(query string, minScore float64) string {
	return fmt.Sprintf("No matches found for '%s' (min score: %.2f).\n\n"+
		"Suggestions:\n"+
		"- Try a more descriptive query with context\n"+
		"- Lower min_score threshold (current: %.2f)\n"+
		"- Use different terminology or phrasing\n"+
		"- Try history_query for chat-only search",
		query, minScore, minScore)
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