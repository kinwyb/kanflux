package tools

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// MemoryTool 统一记忆工具，支持长期记忆、每日笔记和灵魂记忆的读写
type MemoryTool struct {
	memoryStore interface {
		AppendLongTerm(content string) error
		AppendDay(date, content string) error
		WriteLongTerm(content string) error
		WriteDay(date, content string) error
		ReadLongTerm() (string, error)
		ReadDay(date string) (string, error)
		AppendSoul(content string) error
		WriteSoul(content string) error
		ReadSoul() (string, error)
	}
	name string
}

// NewMemoryTool 创建统一记忆工具
func NewMemoryTool(memoryStore interface {
	AppendLongTerm(content string) error
	AppendDay(date, content string) error
	WriteLongTerm(content string) error
	WriteDay(date, content string) error
	ReadLongTerm() (string, error)
	ReadDay(date string) (string, error)
	AppendSoul(content string) error
	WriteSoul(content string) error
	ReadSoul() (string, error)
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
	return `Provides CRUD-like operations on memory files. Manages three distinct layers:
1. "long": Persistent facts and user preferences (MEMORY.md).
2. "day": Date-specific logs and task trackers (days/YYYY-MM-DD.md).
3. "soul": Core personality and behavioral protocols (SOUL.md).

## Parameters
- **action** (required): 
    - "read": Retrieves the current content of the specified memory type.
    - "append": Adds "content" to the end of the file. (Use for new entries).
    - "edit": Replaces "old_text" with "new_text". (Requires exact match).
    - "write": Overwrites the entire file with "content".
- **type** (optional): "long", "day", or "soul". (Default: "long").
- **date** (optional): Specific date for "day" type (YYYY-MM-DD). Defaults to current date.
- **content**: Required for "append" and "write" actions.
- **old_text**: Required for "edit". Must match existing file content exactly.
- **new_text**: Required for "edit".

## Constraints & Behavior
- **Initial Writes**: Use "append" or "write" if a file is empty or does not exist. "edit" will fail in these cases.
- **Context Awareness**: "long" and "soul" data are typically pre-loaded in the prompt context; use "read" primarily for synchronizing "day" records.
- **Precision**: "old_text" must be a literal string match from the file to ensure successful "edit" operations.`
}

// Parameters 返回参数定义
func (t *MemoryTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"description": "Required. Action: 'read', 'append', 'edit', or 'write'",
				"enum":        []string{"read", "append", "edit", "write"},
			},
			"type": map[string]interface{}{
				"type":        "string",
				"description": "Memory type: 'long', 'day', or 'soul' (default: 'long')",
				"enum":        []string{"long", "day", "soul"},
				"default":     "long",
			},
			"date": map[string]interface{}{
				"type":        "string",
				"description": "Date for daily notes (YYYY-MM-DD format). Only used when type='day'. Defaults to today.",
			},
			"content": map[string]interface{}{
				"type":        "string",
				"description": "Required for 'append' and 'write'. The text content to write.",
			},
			"old_text": map[string]interface{}{
				"type":        "string",
				"description": "Required for 'edit'. Must match existing text in file exactly. Edit fails if text not found.",
			},
			"new_text": map[string]interface{}{
				"type":        "string",
				"description": "Required for 'edit'. The replacement text.",
			},
		},
		"required": []string{"action"},
	}
}

// Execute 执行工具
func (t *MemoryTool) Execute(ctx context.Context, params map[string]interface{}) (string, error) {
	action, _ := params["action"].(string)
	if action == "" {
		return "", fmt.Errorf("action is required")
	}

	memType, _ := params["type"].(string)
	if memType == "" {
		memType = "long"
	}

	date, _ := params["date"].(string)
	if memType == "day" && date == "" {
		date = time.Now().Format("2006-01-02")
	}

	// 获取当前内容
	var current string
	var err error
	switch memType {
	case "day":
		current, err = t.memoryStore.ReadDay(date)
	case "soul":
		current, err = t.memoryStore.ReadSoul()
	default:
		current, err = t.memoryStore.ReadLongTerm()
	}
	if err != nil {
		return "", err
	}

	// 执行操作
	switch action {
	case "read":
		return t.handleRead(memType, date, current)

	case "append":
		content, _ := params["content"].(string)
		return t.handleAppend(memType, date, current, content)

	case "edit":
		oldText, _ := params["old_text"].(string)
		newText, _ := params["new_text"].(string)
		return t.handleEdit(memType, date, current, oldText, newText)

	case "write":
		content, _ := params["content"].(string)
		return t.handleWrite(memType, date, content)

	default:
		return "", fmt.Errorf("unknown action: %s", action)
	}
}

// handleRead 处理读取操作
func (t *MemoryTool) handleRead(memType, date, current string) (string, error) {
	switch memType {
	case "day":
		if current == "" {
			return fmt.Sprintf("No notes for %s", date), nil
		}
		return fmt.Sprintf("Notes for %s:\n%s", date, current), nil
	case "soul":
		if current == "" {
			return "Soul memory is empty", nil
		}
		return fmt.Sprintf("Soul memory:\n%s", current), nil
	default:
		if current == "" {
			return "Long-term memory is empty", nil
		}
		return fmt.Sprintf("Long-term memory:\n%s", current), nil
	}
}

// handleAppend 处理追加操作
func (t *MemoryTool) handleAppend(memType, date, current, content string) (string, error) {
	if content == "" {
		return "", fmt.Errorf("content is required for 'append' action")
	}

	switch memType {
	case "day":
		if err := t.memoryStore.AppendDay(date, content); err != nil {
			return "", err
		}
		return fmt.Sprintf("Added to notes for %s", date), nil
	case "soul":
		if err := t.memoryStore.AppendSoul(content); err != nil {
			return "", err
		}
		return "Added to soul memory", nil
	default:
		if err := t.memoryStore.AppendLongTerm(content); err != nil {
			return "", err
		}
		return "Added to long-term memory", nil
	}
}

// handleEdit 处理编辑操作
func (t *MemoryTool) handleEdit(memType, date, current, oldText, newText string) (string, error) {
	if oldText == "" {
		return "", fmt.Errorf("old_text is required for 'edit' action")
	}

	if !strings.Contains(current, oldText) {
		return "", fmt.Errorf("text not found: %s", oldText)
	}

	newContent := strings.ReplaceAll(current, oldText, newText)

	switch memType {
	case "day":
		if err := t.memoryStore.WriteDay(date, newContent); err != nil {
			return "", err
		}
		return fmt.Sprintf("Updated notes for %s", date), nil
	case "soul":
		if err := t.memoryStore.WriteSoul(newContent); err != nil {
			return "", err
		}
		return "Updated soul memory", nil
	default:
		if err := t.memoryStore.WriteLongTerm(newContent); err != nil {
			return "", err
		}
		return "Updated long-term memory", nil
	}
}

// handleWrite 处理写入操作
func (t *MemoryTool) handleWrite(memType, date, content string) (string, error) {
	if content == "" {
		return "", fmt.Errorf("content is required for 'write' action")
	}

	switch memType {
	case "day":
		if err := t.memoryStore.WriteDay(date, content); err != nil {
			return "", err
		}
		return fmt.Sprintf("Wrote notes for %s", date), nil
	case "soul":
		if err := t.memoryStore.WriteSoul(content); err != nil {
			return "", err
		}
		return "Wrote soul memory", nil
	default:
		if err := t.memoryStore.WriteLongTerm(content); err != nil {
			return "", err
		}
		return "Wrote long-term memory", nil
	}
}
