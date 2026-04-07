package rag

import (
	"context"
	"fmt"
	"strings"

	"github.com/kinwyb/kanflux/agent/tools"
)

// KnowledgeToolInterface 知识检索工具接口
type KnowledgeToolInterface interface {
	Name() string
	Description() string
	Parameters() map[string]interface{}
	Execute(ctx context.Context, params map[string]interface{}) (string, error)
}

// KnowledgeTool 知识检索工具
type KnowledgeTool struct {
	manager *RAGManager
}

// NewKnowledgeTool 创建知识检索工具
func NewKnowledgeTool(manager *RAGManager) *KnowledgeTool {
	return &KnowledgeTool{
		manager: manager,
	}
}

// Name 返回工具名称
func (t *KnowledgeTool) Name() string {
	return "knowledge_search"
}

// Description 返回工具描述
func (t *KnowledgeTool) Description() string {
	return `Search the knowledge base for relevant information.

**Usage**:
- Use this tool to find relevant documentation, code examples, or knowledge from indexed files.
- Returns top-K most relevant chunks based on semantic similarity.
- Supports filtering by source path patterns.

**Parameters**:
- query: Required. The search query in natural language.
- top_k: Optional. Number of results to return (default: 5).
- source_filter: Optional. Filter results by source path pattern (e.g., "docs/", "*.go").
- score_threshold: Optional. Minimum relevance score (default: 0.5).`
}

// Parameters 返回参数定义
func (t *KnowledgeTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "Required. The search query in natural language.",
			},
			"top_k": map[string]interface{}{
				"type":        "integer",
				"description": "Optional. Number of results to return (default: 5).",
				"default":     5,
			},
			"source_filter": map[string]interface{}{
				"type":        "string",
				"description": "Optional. Filter results by source path pattern.",
			},
			"score_threshold": map[string]interface{}{
				"type":        "number",
				"description": "Optional. Minimum relevance score (default: 0.5).",
				"default":     0.5,
			},
		},
		"required": []string{"query"},
	}
}

// Execute 执行工具
func (t *KnowledgeTool) Execute(ctx context.Context, params map[string]interface{}) (string, error) {
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

	scoreThreshold := 0.5
	if v, ok := params["score_threshold"].(float64); ok && v > 0 {
		scoreThreshold = v
	}

	sourceFilter, _ := params["source_filter"].(string)

	// 构建检索选项
	opts := []RetrieveOption{
		WithTopK(topK),
		WithScoreThreshold(scoreThreshold),
	}
	if sourceFilter != "" {
		opts = append(opts, WithSourceFilter(sourceFilter))
	}

	// 执行检索
	results, err := t.manager.Retrieve(ctx, query, opts...)
	if err != nil {
		return "", fmt.Errorf("failed to search knowledge base: %w", err)
	}

	if len(results) == 0 {
		return "No relevant knowledge found.", nil
	}

	// 格式化输出
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d relevant documents:\n\n", len(results)))

	for i, result := range results {
		sb.WriteString(fmt.Sprintf("**[%d] Score: %.2f**\n", i+1, result.Score))
		sb.WriteString(fmt.Sprintf("Source: %s\n", result.SourcePath))
		sb.WriteString(fmt.Sprintf("Content:\n%s\n", result.Content))
		sb.WriteString("---\n\n")
	}

	return sb.String(), nil
}

// 实现 ApprovalPrompter 接口（可选）
// KnowledgeToolApprovalPrompter 自定义审批提示
type KnowledgeToolApprovalPrompter struct{}

// ApprovalPrompt 返回自定义审批提示
func (p *KnowledgeToolApprovalPrompter) ApprovalPrompt(argsJSON string) string {
	return "Search knowledge base with query: " + argsJSON + ". Approve?"
}

// Ensure KnowledgeTool implements Tool interface
var _ tools.Tool = (*KnowledgeTool)(nil)