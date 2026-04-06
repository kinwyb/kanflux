package tools

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// MemoryTool 统一记忆工具，支持长期记忆和每日笔记的读写
type MemoryTool struct {
	memoryStore interface {
		AppendLongTerm(content string) error
		AppendDay(date, content string) error
		WriteLongTerm(content string) error
		WriteDay(date, content string) error
		ReadLongTerm() (string, error)
		ReadDay(date string) (string, error)
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
	return `Manage memory content (long-term memory or daily notes).

Actions:
- read: Get current memory content
- append: Add content to the end
- edit: Replace specific text (requires old_text and new_text)
- write: Overwrite entire memory with new content

For daily notes, use type='day' with optional 'date' (YYYY-MM-DD, defaults to today).`
}

// Parameters 返回参数定义
func (t *MemoryTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"description": "Action: 'read', 'append', 'edit', or 'write'",
				"enum":        []string{"read", "append", "edit", "write"},
			},
			"type": map[string]interface{}{
				"type":        "string",
				"description": "Memory type: 'long' or 'day'",
				"enum":        []string{"long", "day"},
				"default":     "long",
			},
			"date": map[string]interface{}{
				"type":        "string",
				"description": "Date for daily notes (YYYY-MM-DD). Only for type='day'. Defaults to today.",
			},
			"content": map[string]interface{}{
				"type":        "string",
				"description": "Content for 'append' or 'write' action",
			},
			"old_text": map[string]interface{}{
				"type":        "string",
				"description": "Text to replace (for 'edit' action)",
			},
			"new_text": map[string]interface{}{
				"type":        "string",
				"description": "New text (for 'edit' action)",
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
	if memType == "day" {
		current, err = t.memoryStore.ReadDay(date)
	} else {
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
	if memType == "day" {
		if current == "" {
			return fmt.Sprintf("No notes for %s", date), nil
		}
		return fmt.Sprintf("Notes for %s:\n%s", date, current), nil
	}
	if current == "" {
		return "Long-term memory is empty", nil
	}
	return fmt.Sprintf("Long-term memory:\n%s", current), nil
}

// handleAppend 处理追加操作
func (t *MemoryTool) handleAppend(memType, date, current, content string) (string, error) {
	if content == "" {
		return "", fmt.Errorf("content is required for 'append' action")
	}

	if memType == "day" {
		if err := t.memoryStore.AppendDay(date, content); err != nil {
			return "", err
		}
		return fmt.Sprintf("Added to notes for %s", date), nil
	}
	if err := t.memoryStore.AppendLongTerm(content); err != nil {
		return "", err
	}
	return "Added to long-term memory", nil
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

	if memType == "day" {
		if err := t.memoryStore.WriteDay(date, newContent); err != nil {
			return "", err
		}
		return fmt.Sprintf("Updated notes for %s", date), nil
	}
	if err := t.memoryStore.WriteLongTerm(newContent); err != nil {
		return "", err
	}
	return "Updated long-term memory", nil
}

// handleWrite 处理写入操作
func (t *MemoryTool) handleWrite(memType, date, content string) (string, error) {
	if content == "" {
		return "", fmt.Errorf("content is required for 'write' action")
	}

	if memType == "day" {
		if err := t.memoryStore.WriteDay(date, content); err != nil {
			return "", err
		}
		return fmt.Sprintf("Wrote notes for %s", date), nil
	}
	if err := t.memoryStore.WriteLongTerm(content); err != nil {
		return "", err
	}
	return "Wrote long-term memory", nil
}