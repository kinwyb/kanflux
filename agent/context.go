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

{user_preferences}

%s
`, identity,
		now.Format("2006-01-02 15:04:05 MST"),
		b.workspace, agent)
}

// ContextBuilder 构建记忆
func (b *ContextBuilder) buildMemory() string {
	memory := `# 🧠 Memory Management (Minimalist)

You use memory_tool to manage memory. 

**Core Rules**:
1. **Context**: Long-term memory is already in your context. **Never** use read for type: "long".
2. **On-Demand**: Use read only for type: "day" to retrieve specific daily notes or tasks.
3. **Storage**: Use append to save new facts to long or daily logs to day.

### **Actions**
1. **read**: Strictly for type: "day" only.
2. **append**: Add new information to the end of long or day.
3. **edit**: Replace existing text. Requires old_text and new_text.
4. **write**: Overwrite the entire file content.

### **Parameters**
- **action**: read, append, edit, write
- **type**: "day" (default) or "long"
- **date**: For day type only (YYYY-MM-DD, defaults to today)
- **content**: Required for append / write.
- **old_text** / **new_text**: Required for edit.

### **Examples**
* **Save Fact**: {"action": "append", "type": "long", "content": "User prefers minimalist tools."}
* **Check Tasks**: {"action": "read", "type": "day"}
* **Log Work**: {"action": "append", "type": "day", "content": "Updated memory logic."}

`

	if memoryContext, err := b.memory.GetMemoryContext(); err == nil && memoryContext != "" {
		memory = memory + "## Current Memory Content\n\n" + memoryContext
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
