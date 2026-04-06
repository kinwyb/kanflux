package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// MemoryStore 记忆存储
type MemoryStore struct {
	baseDIR string
}

// NewMemoryStore 创建记忆存储
func NewMemoryStore(workspace string) (*MemoryStore, error) {
	baseDir := filepath.Join(workspace, "memory")
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, err
	}
	return &MemoryStore{
		baseDIR: baseDir,
	}, nil
}

// ReadToday 读取今日笔记
func (m *MemoryStore) ReadToday() (string, error) {
	path, _ := m.todyFilePath()

	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}

	return string(content), nil
}

// AppendToday 追加到今日笔记
func (m *MemoryStore) AppendToday(content string) error {
	// 确保目录存在
	path, err := m.todyFilePath()
	if err != nil {
		return err
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	// 如果文件不为空，添加换行
	if info, err := file.Stat(); err == nil && info.Size() > 0 {
		if _, err := file.WriteString("\n\n"); err != nil {
			return err
		}
	}

	// 写入内容
	if _, err := file.WriteString(content); err != nil {
		return err
	}

	return nil
}

func (m *MemoryStore) todyFilePath() (string, error) {
	// 确保目录存在
	memoryDir := filepath.Join(m.baseDIR, "days")
	if err := os.MkdirAll(memoryDir, 0755); err != nil {
		return "", err
	}

	// 追加内容
	today := time.Now().Format("2006-01-02")
	path := filepath.Join(memoryDir, today+".md")
	return path, nil
}

// ReadLongTerm 读取长期记忆
func (m *MemoryStore) ReadLongTerm() (string, error) {
	path := m.longTermFilePath()

	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}

	return string(content), nil
}

func (m *MemoryStore) longTermFilePath() string {
	return filepath.Join(m.baseDIR, "MEMORY.md")
}

// AppendLongTerm 追加到长期记忆
func (m *MemoryStore) AppendLongTerm(content string) error {

	path := m.longTermFilePath()

	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	// 如果文件不为空，添加换行
	if info, err := file.Stat(); err == nil && info.Size() > 0 {
		if _, err := file.WriteString("\n\n"); err != nil {
			return err
		}
	}

	// 写入内容
	if _, err := file.WriteString(content); err != nil {
		return err
	}

	return nil
}

// GetMemoryContext 获取格式化的记忆上下文
func (m *MemoryStore) GetMemoryContext() (string, error) {
	var parts []string

	// 读取长期记忆
	longTerm, err := m.ReadLongTerm()
	if err != nil {
		return "", err
	}
	if longTerm != "" {
		parts = append(parts, "## Long-term Memory\n\n"+longTerm)
	}

	// 读取今日笔记
	today, err := m.ReadToday()
	if err != nil {
		return "", err
	}
	if today != "" {
		parts = append(parts, "## Today's Notes\n\n"+today)
	}

	if len(parts) == 0 {
		return "", nil
	}

	return strings.Join(parts, "\n\n---\n\n"), nil
}

// ReadBootstrapFile 读取 bootstrap 文件
func (m *MemoryStore) ReadBootstrapFile(filename string) (string, error) {
	path := filepath.Join(m.baseDIR, filename)

	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}

	return string(content), nil
}

// EnsureBootstrapFiles 确保 bootstrap 文件存在
func (m *MemoryStore) EnsureBootstrapFiles() error {

	// bootstrap 文件列表
	bootstrapFiles := []string{
		"IDENTITY.md",
		"AGENTS.md",
		"SOUL.md",
		"USER.md",
	}

	for _, filename := range bootstrapFiles {
		path := filepath.Join(m.baseDIR, filename)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			// 创建默认内容
			var defaultContent string
			switch filename {
			case "IDENTITY.md":
				defaultContent = `# Identity

You are **KanFlux**, a personal AI assistant running on the user's system.
You are NOT a passive chat bot. You are a **DOER** that executes tasks directly.
Your mission: complete user requests using all available means, minimizing human intervention.
`
			case "AGENTS.md":
				defaultContent = `### Task Complexity Guidelines

- **Simple tasks**: Use tools directly
- **Moderate tasks**: Use tools, narrate key steps
- **Complex/Long tasks**: Consider spawning a sub-agent. Completion is push-based: it will auto-announce when done
- **For long waits**: Avoid rapid poll loops. Use run_shell with background mode, or process(action=poll, timeout=<ms>)

### Skill-First Workflow (HIGHEST PRIORITY)

1. **ALWAYS check the Skills section first** before using any other tools
2. If a matching skill is found, use the use_skill tool with the skill name
3. If no matching skill: use built-in tools
4. Only after checking skills should you proceed with built-in tools

### Core Rules

- For ANY search request ("search for", "find", "google search", etc.): IMMEDIATELY call web_search tool. DO NOT provide manual instructions or advice.
- When the user asks for information: USE YOUR TOOLS to get it. Do NOT explain how to get it.
- DO NOT tell the user "I cannot" or "here's how to do it yourself". ACTUALLY DO IT with tools.
- If you have tools available for a task, use them. No permission needed for safe operations.
- **NEVER HALLUCINATE SEARCH RESULTS**: When presenting search results, ONLY use the exact data returned by the tool. If no results were found, clearly state that no results were found.
- When a tool fails: analyze the error, try an alternative approach WITHOUT asking the user unless absolutely necessary.
`
			case "SOUL.md":
				defaultContent = `# Agent Soul

This file defines the agent's personality, behavior patterns, and core principles.

## File Location

This file is located at: <workspace>/memory/SOUL.md

You can edit this file to update your personality and behavioral guidelines.

## Purpose

- Store learned preferences about how the user likes to work
- Record behavioral guidelines that improve over time
- Define the agent's character and interaction style

## When to Update

You should proactively update this file when:
- The user expresses preferences about your behavior (e.g., "be more concise", "explain in detail")
- You discover patterns that improve the user's experience
- The user gives feedback about your responses
- You learn what works best for this specific user

This file evolves with your interactions to better serve the user.
`
			case "USER.md":
				defaultContent = `# User Information

This file contains information about the user that helps you provide personalized assistance.

## File Location

This file is located at: <workspace>/memory/USER.md

You can edit this file to store and update user-related information.

## Purpose

- Store relevant facts about the user (role, expertise, preferences)
- Remember context about ongoing projects or goals
- Track user-specific knowledge that improves future interactions

## When to Update

You should proactively update this file when:
- The user shares personal information relevant to your assistance
- You learn about the user's role, skills, or domain expertise
- The user mentions preferences, constraints, or working styles
- New project context or goals emerge that should be remembered

This file helps you understand the user's perspective and tailor responses accordingly.
`
			}

			if err := os.WriteFile(path, []byte(defaultContent), 0644); err != nil {
				return fmt.Errorf("failed to create bootstrap file %s: %w", filename, err)
			}
		}
	}

	return nil
}
