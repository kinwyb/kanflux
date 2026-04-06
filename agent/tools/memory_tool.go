package tools

import (
	"context"
	"fmt"
	"time"
)

//// MemoryTool memory 搜索工具
//type MemoryTool struct {
//	searchManager memory.MemorySearchManager
//	name          string
//}
//
//// NewMemoryTool 创建 memory 搜索工具
//func NewMemoryTool(searchManager memory.MemorySearchManager) *MemoryTool {
//	return &MemoryTool{
//		searchManager: searchManager,
//		name:          "memory_search",
//	}
//}
//
//// Name 返回工具名称
//func (t *MemoryTool) Name() string {
//	return t.name
//}
//
//// Description 返回工具描述
//func (t *MemoryTool) Description() string {
//	return "Search semantic memory for relevant information about past conversations, facts, and context."
//}
//
//// Parameters 返回参数定义
//func (t *MemoryTool) Parameters() map[string]interface{} {
//	return map[string]interface{}{
//		"type": "object",
//		"properties": map[string]interface{}{
//			"query": map[string]interface{}{
//				"type":        "string",
//				"description": "Search query",
//			},
//			"limit": map[string]interface{}{
//				"type":        "integer",
//				"description": "Maximum number of results",
//				"default":     6,
//			},
//		},
//		"required": []string{"query"},
//	}
//}
//
//// Execute 执行工具
//func (t *MemoryTool) Execute(ctx context.Context, params map[string]interface{}) (string, error) {
//	query, ok := params["query"].(string)
//	if !ok {
//		return "", fmt.Errorf("query is required and must be a string")
//	}
//
//	limit := 6
//	if l, ok := params["limit"].(float64); ok {
//		limit = int(l)
//	}
//
//	opts := memory.DefaultSearchOptions()
//	opts.Limit = limit
//
//	results, err := t.searchManager.Search(ctx, query, opts)
//	if err != nil {
//		return "", fmt.Errorf("memory search failed: %w", err)
//	}
//
//	return formatSearchResults(query, results), nil
//}
//
//// formatSearchResults 格式化搜索结果
//func formatSearchResults(query string, results []*memory.SearchResult) string {
//	if len(results) == 0 {
//		return fmt.Sprintf("No results found for: %s", query)
//	}
//
//	var output string
//	output += fmt.Sprintf("Found %d result(s) for: %s\n\n", len(results), query)
//
//	for i, result := range results {
//		output += fmt.Sprintf("[%d] Score: %.2f\n", i+1, result.Score)
//		if result.Source != "" {
//			output += fmt.Sprintf("    Source: %s\n", result.Source)
//		}
//		if result.Type != "" {
//			output += fmt.Sprintf("    Type: %s\n", result.Type)
//		}
//		if result.Metadata.FilePath != "" {
//			output += fmt.Sprintf("    File: %s", result.Metadata.FilePath)
//			if result.Metadata.LineNumber > 0 {
//				output += fmt.Sprintf(":%d", result.Metadata.LineNumber)
//			}
//			output += "\n"
//		}
//
//		// 文本内容
//		text := result.Text
//		maxLen := 300
//		if len(text) > maxLen {
//			text = text[:maxLen] + "..."
//		}
//		output += fmt.Sprintf("    Content: %s\n\n", text)
//	}
//
//	return output
//}
//
//// MemoryAddTool memory 添加工具
//type MemoryAddTool struct {
//	searchManager memory.MemorySearchManager
//	name          string
//}
//
//// NewMemoryAddTool 创建 memory 添加工具
//func NewMemoryAddTool(searchManager memory.MemorySearchManager) *MemoryAddTool {
//	return &MemoryAddTool{
//		searchManager: searchManager,
//		name:          "memory_add",
//	}
//}
//
//// Name 返回工具名称
//func (t *MemoryAddTool) Name() string {
//	return t.name
//}
//
//// Description 返回工具描述
//func (t *MemoryAddTool) Description() string {
//	return "Add information to memory for future reference. Only works with builtin backend."
//}
//
//// Parameters 返回参数定义
//func (t *MemoryAddTool) Parameters() map[string]interface{} {
//	return map[string]interface{}{
//		"type": "object",
//		"properties": map[string]interface{}{
//			"text": map[string]interface{}{
//				"type":        "string",
//				"description": "The text content to store",
//			},
//			"source": map[string]interface{}{
//				"type":        "string",
//				"description": "Source of the memory (longterm, session, daily)",
//				"default":     "session",
//			},
//			"type": map[string]interface{}{
//				"type":        "string",
//				"description": "Type of memory (fact, preference, context, conversation)",
//				"default":     "fact",
//			},
//		},
//		"required": []string{"text"},
//	}
//}
//
//// Execute 执行工具
//func (t *MemoryAddTool) Execute(ctx context.Context, params map[string]interface{}) (string, error) {
//	text, ok := params["text"].(string)
//	if !ok || text == "" {
//		return "", fmt.Errorf("text is required and must be a non-empty string")
//	}
//
//	sourceStr := "session"
//	if s, ok := params["source"].(string); ok {
//		sourceStr = s
//	}
//
//	typeStr := "fact"
//	if typ, ok := params["type"].(string); ok {
//		typeStr = typ
//	}
//
//	source := memory.MemorySource(sourceStr)
//	memType := memory.MemoryType(typeStr)
//
//	metadata := memory.MemoryMetadata{}
//
//	if err := t.searchManager.Add(ctx, text, source, memType, metadata); err != nil {
//		return "", fmt.Errorf("failed to add memory: %w", err)
//	}
//
//	return "Memory added successfully", nil
//}

// MemoryTool 统一记忆工具，支持长期记忆和每日笔记的读写
type MemoryTool struct {
	memoryStore interface {
		AppendLongTerm(content string) error
		AppendDay(date, content string) error
		ReplaceLongTerm(content string) error
		ReplaceDay(date, content string) error
		ReadLongTerm() (string, error)
		ReadDay(date string) (string, error)
	}
	name string
}

// NewMemoryTool 创建统一记忆工具
func NewMemoryTool(memoryStore interface {
	AppendLongTerm(content string) error
	AppendDay(date, content string) error
	ReplaceLongTerm(content string) error
	ReplaceDay(date, content string) error
	ReadLongTerm() (string, error)
	ReadDay(date string) (string, error)
}) *MemoryTool {
	return &MemoryTool{
		memoryStore: memoryStore,
		name:        "memory_tool",
	}
}

// Name 返回工具名称
func (t *MemoryTool) Name() string {
	return t.name
}

// Description 返回工具描述
func (t *MemoryTool) Description() string {
	return "Manage memory content. Supports long-term memory (MEMORY.md) and daily notes. " +
		"Use action='read' to retrieve content, action='append' to add content, or action='replace' to overwrite. " +
		"For daily notes, use type='day' with optional 'date' parameter (format: YYYY-MM-DD, defaults to today). " +
		"IMPORTANT: When action is 'append' or 'replace', the 'content' parameter is REQUIRED."
}

// Parameters 返回参数定义
func (t *MemoryTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"description": "Action to perform: 'read' to retrieve content, 'append' to add content, 'replace' to overwrite entire file",
				"enum":        []string{"read", "append", "replace"},
				"default":     "append",
			},
			"type": map[string]interface{}{
				"type":        "string",
				"description": "Memory type: 'long' for long-term memory (MEMORY.md), 'day' for daily notes",
				"enum":        []string{"long", "day"},
				"default":     "long",
			},
			"date": map[string]interface{}{
				"type":        "string",
				"description": "Date for daily notes in YYYY-MM-DD format (e.g., '2026-04-07'). Only used when type='day'. Defaults to today if not specified.",
			},
			"content": map[string]interface{}{
				"type":        "string",
				"description": "REQUIRED for 'append' and 'replace' actions: The content to write to memory. Example: 'User prefers dark mode in the editor'. NOT needed for 'read' action.",
			},
		},
		"required": []string{},
	}
}

// Execute 执行工具
func (t *MemoryTool) Execute(ctx context.Context, params map[string]interface{}) (string, error) {
	action := "append"
	if act, ok := params["action"].(string); ok && act != "" {
		action = act
	}

	memType := "long"
	if typ, ok := params["type"].(string); ok && typ != "" {
		memType = typ
	}

	// 获取日期参数（仅用于 daily notes）
	date := ""
	if d, ok := params["date"].(string); ok && d != "" {
		date = d
	} else if memType == "day" {
		date = time.Now().Format("2006-01-02")
	}

	content, _ := params["content"].(string)

	switch memType {
	case "day":
		return t.executeDayAction(action, date, content)
	case "long":
		fallthrough
	default:
		return t.executeLongAction(action, content)
	}
}

// executeDayAction 执行每日笔记操作
func (t *MemoryTool) executeDayAction(action, date, content string) (string, error) {
	if date == "" {
		date = time.Now().Format("2006-01-02")
	}

	switch action {
	case "read":
		result, err := t.memoryStore.ReadDay(date)
		if err != nil {
			return "", fmt.Errorf("failed to read daily notes for %s: %w", date, err)
		}
		if result == "" {
			return fmt.Sprintf("No notes found for %s", date), nil
		}
		return fmt.Sprintf("Notes for %s:\n\n%s", date, result), nil

	case "replace":
		if content == "" {
			return "", fmt.Errorf("content is required for 'replace' action")
		}
		if err := t.memoryStore.ReplaceDay(date, content); err != nil {
			return "", fmt.Errorf("failed to replace daily notes for %s: %w", date, err)
		}
		return fmt.Sprintf("Replaced daily notes for %s", date), nil

	case "append":
		if content == "" {
			return "", fmt.Errorf("content is required for 'append' action")
		}
		if err := t.memoryStore.AppendDay(date, content); err != nil {
			return "", fmt.Errorf("failed to append to daily notes for %s: %w", date, err)
		}
		return fmt.Sprintf("Added to daily notes for %s", date), nil

	default:
		return "", fmt.Errorf("unknown action: %s", action)
	}
}

// executeLongAction 执行长期记忆操作
func (t *MemoryTool) executeLongAction(action, content string) (string, error) {
	switch action {
	case "read":
		result, err := t.memoryStore.ReadLongTerm()
		if err != nil {
			return "", fmt.Errorf("failed to read long-term memory: %w", err)
		}
		if result == "" {
			return "Long-term memory is empty", nil
		}
		return fmt.Sprintf("Long-term memory:\n\n%s", result), nil

	case "replace":
		if content == "" {
			return "", fmt.Errorf("content is required for 'replace' action")
		}
		if err := t.memoryStore.ReplaceLongTerm(content); err != nil {
			return "", fmt.Errorf("failed to replace long-term memory: %w", err)
		}
		return "Replaced long-term memory", nil

	case "append":
		if content == "" {
			return "", fmt.Errorf("content is required for 'append' action")
		}
		if err := t.memoryStore.AppendLongTerm(content); err != nil {
			return "", fmt.Errorf("failed to append to long-term memory: %w", err)
		}
		return "Added to long-term memory", nil

	default:
		return "", fmt.Errorf("unknown action: %s", action)
	}
}