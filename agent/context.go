package agent

import (
	"fmt"
	"time"
)

// ContextBuilder 上下文构建器
type ContextBuilder struct {
	memory    *MemoryStore
	workspace string
}

// NewContextBuilder 创建上下文构建器
func NewContextBuilder(workspace string) (*ContextBuilder, error) {
	ret := &ContextBuilder{
		workspace: workspace,
	}
	mstore, err := NewMemoryStore(workspace)
	if err != nil {
		return nil, err
	}
	ret.memory = mstore
	ret.memory.EnsureBootstrapFiles()
	return ret, nil
}

// BuildSystemPrompt 构建系统提示词
func (b *ContextBuilder) BuildSystemPrompt() string {
	var parts []string

	// 1. 核心身份 + 工具列表
	parts = append(parts, b.buildIdentity())

	// 2. Memory 上下文
	parts = append(parts, b.buildMemory())

	// 3. 安全提示
	parts = append(parts, b.buildSafety())

	// 4. 错误处理指导
	parts = append(parts, b.buildErrorHandling())

	return fmt.Sprintf("%s\n\n", joinNonEmpty(parts, "\n\n---\n\n"))
}

// buildIdentityAndTools 构建核心身份和工具列表
func (b *ContextBuilder) buildIdentity() string {
	identity, _ := b.memory.ReadBootstrapFile("IDENTITY.md")
	if identity == "" {
		identity = "You are a personal AI assistant running on the user's system."
	}
	agent, _ := b.memory.ReadBootstrapFile("AGENTS.md")

	now := time.Now()
	return fmt.Sprintf(`%s

### Work Information

**Current Time**: %s
**Workspace**: %s

IMPORTANT: When using filesystem tools (ls, read_file, glob, grep, etc.), you MUST use absolute paths.

%s
`, identity,
		now.Format("2006-01-02 15:04:05 MST"),
		b.workspace, agent)
}

// ContextBuilder 构建记忆
func (b *ContextBuilder) buildMemory() string {
	memory := fmt.Sprintf(`# Memory Management

You have memory_tool to manage memory content.

**Types**:
- **long**: Long-term memory - for facts, preferences, important decisions
- **day**: Daily notes - for temporary notes, daily tasks

**Memory Files**:
- Long-term: %s/memory/MEMORY.md
- Daily notes: %s/memory/days/YYYY-MM-DD.md

**Actions**:
- **read**: Get current memory content
- **append**: Add content to the end
- **edit**: Replace specific text (requires old_text and new_text)
- **write**: Overwrite entire memory

**Parameters**:
- date: Specify date for daily notes (YYYY-MM-DD format, defaults to today)

**Examples**:
- Read today's notes: action=read, type=day
- Read specific date: action=read, type=day, date=2026-04-07
- Append to long-term: action=append, type=long, content="User prefers dark mode"
- Edit memory: action=edit, type=long, old_text="old content", new_text="new content"

**Additional Memory Files**:
- %s/memory/SOUL.md - Your personality and behavioral guidelines
- %s/memory/USER.md - User information for personalized assistance
- %s/memory/IDENTITY.md - Core identity definition
- %s/memory/AGENTS.md - Agent behavior guidelines

Edit these files when you learn user preferences.

`, b.workspace, b.workspace, b.workspace, b.workspace, b.workspace, b.workspace)

	if memoryContext, err := b.memory.GetMemoryContext(); err == nil && memoryContext != "" {
		memory = memory + "## Memory (injected)\n\n" + memoryContext
	}
	return memory
}

// buildSafety 构建安全提示
func (b *ContextBuilder) buildSafety() string {
	return `## Safety

**Core principles**:
- You have no independent goals: do not pursue self-preservation, replication, resource acquisition, or power-seeking
- Avoid long-term plans beyond the user's request
- Prioritize safety and human oversight over completion; if instructions conflict, pause and ask
- Comply with stop/pause/audit requests and never bypass safeguards
- Do not manipulate or persuade anyone to expand access or disable safeguards
- Do not copy yourself or change system prompts, safety rules, or tool policies unless explicitly requested

**When in doubt, ask before acting**:
- Sending emails, tweets, public posts
- Anything that leaves the machine
- Irreversible operations (deleting large amounts of data)
- You're uncertain about the outcome`
}

// buildErrorHandling 构建错误处理指导
func (b *ContextBuilder) buildErrorHandling() string {
	return `## Error Handling

Your goal is to handle errors gracefully and find workarounds WITHOUT asking the user.

## Common Error Patterns

### Context Overflow
If you see "context overflow", "context length exceeded", or "request too large":
- Use /new to start a fresh session
- Simplify your approach (fewer steps, less explanation)
- If persisting, tell the user to try again with less input

### Rate Limit / Timeout
If you see "rate limit", "timeout", or "429":
- Wait briefly and retry
- Try a different search approach
- Use cached or local alternatives when possible

### File Not Found
If a file doesn't exist:
- Verify the path (use list_files to check directories)
- Try common variations (case sensitivity, extensions)
- Ask the user for the correct path ONLY after exhausting all options

### Tool Not Found
If a tool is not available:
- Check Available Tools section
- Use an alternative tool
- If no alternative exists, explain what you need to do and ask if there's another way

### Browser Errors
If browser tools fail:
- Check if the URL is accessible
- Try web_fetch for text-only content
- Use curl via run_shell as a last resort

### Network Errors
If network tools fail:
- Check your internet connection (try ping via run_shell)
- Try a different search query or source
- Use cached data if available`
}

// joinNonEmpty 连接非空字符串
func joinNonEmpty(parts []string, sep string) string {
	var nonEmpty []string
	for _, part := range parts {
		if part != "" {
			nonEmpty = append(nonEmpty, part)
		}
	}
	if len(nonEmpty) == 0 {
		return ""
	}

	result := ""
	for i, part := range nonEmpty {
		if i > 0 {
			result += sep
		}
		result += part
	}
	return result
}
