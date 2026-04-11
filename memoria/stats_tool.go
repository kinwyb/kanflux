package memoria

import (
	"context"
	"fmt"
	"strings"

	"github.com/kinwyb/kanflux/agent/tools"
)

// StatsTool 统计信息工具
type StatsTool struct {
	memoria *Memoria
}

// NewStatsTool 创建统计信息工具
func NewStatsTool(memoria *Memoria) *StatsTool {
	return &StatsTool{
		memoria: memoria,
	}
}

// Name 返回工具名称
func (t *StatsTool) Name() string {
	return "memory_stats"
}

// Description 返回工具描述
func (t *StatsTool) Description() string {
	return `Get statistics about the memory system.

**Returns**:
- L1 items count (user preferences, always loaded)
- L2 items count (facts, events, discoveries)
- L3 items count (raw content for semantic search)
- Storage information
- Knowledge paths configuration

**When to use**:
- To understand the current state of your memory system
- To verify if knowledge files have been indexed
- To check storage health`
}

// Parameters 返回参数定义
func (t *StatsTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}

// Execute 执行工具
func (t *StatsTool) Execute(ctx context.Context, params map[string]interface{}) (string, error) {
	if t.memoria == nil {
		return "Memory system is not available.", nil
	}

	stats := t.memoria.GetStats()

	var sb strings.Builder
	sb.WriteString("## Memory System Statistics\n\n")

	// L1 统计
	l1Items := stats["l1_items"]
	if l1Items != nil {
		sb.WriteString(fmt.Sprintf("**L1 (Preferences)**: %v items\n", l1Items))
		sb.WriteString("  - Always loaded, ~120 tokens total\n")
		sb.WriteString("  - Stores user preferences and critical decisions\n\n")
	}

	// L2 统计
	l2Items := stats["l2_items"]
	if l2Items != nil {
		sb.WriteString(fmt.Sprintf("**L2 (Events)**: %v items\n", l2Items))
		sb.WriteString("  - Semantic + keyword search\n")
		sb.WriteString("  - Stores facts, events, discoveries, advice\n\n")
	}

	// L3 统计
	l3Items := stats["l3_items"]
	if l3Items != nil {
		sb.WriteString(fmt.Sprintf("**L3 (Raw Content)**: %v items\n", l3Items))
		sb.WriteString("  - Full semantic search\n")
		sb.WriteString("  - Raw conversation and file content\n\n")
	}

	// 工作空间
	workspace := stats["workspace"]
	if workspace != nil {
		sb.WriteString(fmt.Sprintf("**Workspace**: %v\n", workspace))
	}

	// Memoria 目录
	memoriaDir := t.memoria.GetMemoriaDir()
	sb.WriteString(fmt.Sprintf("**Storage Path**: %s\n", memoriaDir))

	// 知识路径配置
	cfg := t.memoria.GetConfig()
	if cfg != nil && len(cfg.GetAllWatchPaths()) > 0 {
		sb.WriteString("\n**Knowledge Paths**:\n")
		for i, wp := range cfg.GetAllWatchPaths() {
			sb.WriteString(fmt.Sprintf("  %d. %s (extensions: %v, recursive: %v)\n",
				i+1, wp.Path, wp.Extensions, wp.Recursive))
		}
	}

	return sb.String(), nil
}

// Ensure StatsTool implements Tool interface
var _ tools.Tool = (*StatsTool)(nil)