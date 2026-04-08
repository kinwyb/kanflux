package session

import (
	"context"
	"fmt"
	"strings"

	"github.com/kinwyb/kanflux/agent/tools"
)

// HistorySearchTool 历史对话检索工具
type HistorySearchTool struct {
	history *ConversationHistory
}

// NewHistorySearchTool 创建历史对话检索工具
func NewHistorySearchTool(history *ConversationHistory) *HistorySearchTool {
	return &HistorySearchTool{
		history: history,
	}
}

// Name 返回工具名称
func (t *HistorySearchTool) Name() string {
	return "history_search"
}

// Description 返回工具描述
func (t *HistorySearchTool) Description() string {
	return `Search past conversations to find relevant information.

**When to use this tool**:
- When the user asks about something discussed in previous sessions
- To recall user preferences, decisions, or information shared before
- To avoid asking the user to repeat information they've already provided
- When you need context from past conversations

**Parameters**:
- query: Required. Natural language search query.
- top_k: Optional. Number of results to return (default: 5).

**Returns**: Relevant Q&A pairs from past conversations with dates.

**Note**: For important information you want to remember permanently, use memory_tool instead.`
}

// Parameters 返回参数定义
func (t *HistorySearchTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "Required. Natural language search query.",
			},
			"top_k": map[string]interface{}{
				"type":        "integer",
				"description": "Optional. Number of results to return (default: 5).",
				"default":     5,
			},
		},
		"required": []string{"query"},
	}
}

// Execute 执行工具
func (t *HistorySearchTool) Execute(ctx context.Context, params map[string]interface{}) (string, error) {
	query, ok := params["query"].(string)
	if !ok || query == "" {
		return "", fmt.Errorf("query is required")
	}

	topK := 5
	if v, ok := params["top_k"].(int); ok && v > 0 {
		topK = v
	}
	if v, ok := params["top_k"].(float64); ok && v > 0 {
		topK = int(v)
	}

	if t.history == nil {
		return "History search is not available.", nil
	}

	return t.history.Search(ctx, query, topK)
}

// Ensure HistorySearchTool implements Tool interface
var _ tools.Tool = (*HistorySearchTool)(nil)

// GetHistoryStatsTool 获取历史统计工具
type GetHistoryStatsTool struct {
	history *ConversationHistory
}

// NewGetHistoryStatsTool 创建获取历史统计工具
func NewGetHistoryStatsTool(history *ConversationHistory) *GetHistoryStatsTool {
	return &GetHistoryStatsTool{
		history: history,
	}
}

// Name 返回工具名称
func (t *GetHistoryStatsTool) Name() string {
	return "history_stats"
}

// Description 返回工具描述
func (t *GetHistoryStatsTool) Description() string {
	return `Get statistics about conversation history.

Returns:
- Total number of indexed documents
- Number of rooms (days) with recorded conversations
- Storage size`
}

// Parameters 返回参数定义
func (t *GetHistoryStatsTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}

// Execute 执行工具
func (t *GetHistoryStatsTool) Execute(ctx context.Context, params map[string]interface{}) (string, error) {
	if t.history == nil {
		return "History is not available.", nil
	}

	stats := t.history.GetStats()

	var sb strings.Builder
	sb.WriteString("Conversation History Statistics:\n")
	sb.WriteString(fmt.Sprintf("- Total Documents: %v\n", stats["total_documents"]))
	sb.WriteString(fmt.Sprintf("- Total Rooms (Days): %v\n", stats["total_rooms"]))
	sb.WriteString(fmt.Sprintf("- Storage Size: %v bytes\n", stats["storage_size"]))

	return sb.String(), nil
}

// Ensure GetHistoryStatsTool implements Tool interface
var _ tools.Tool = (*GetHistoryStatsTool)(nil)