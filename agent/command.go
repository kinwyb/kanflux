package agent

import (
	"context"
	"strings"

	"github.com/kinwyb/kanflux/bus"
)

// CommandResult 命令处理结果
type CommandResult struct {
	Content     string                 // 响应内容
	Media       []bus.Media            // 响应媒体
	SkipAgent   bool                   // 是否跳过 agent 循环
	Error       error                  // 错误信息
	ShouldReply bool                   // 是否需要回复（发布到 outbound）
	Metadata    map[string]interface{} // 元数据，可携带额外信息如新 chatID
}

// CommandMetadataKeys 元数据键名常量
const (
	MetadataKeyNewChatID = "new_chat_id" // 新会话 ID
	MetadataKeyClearChat = "clear_chat"  // 是否清空会话
)

// CommandHandler 命令处理器
type CommandHandler interface {
	// Handle 处理命令
	// ctx: 上下文
	// cmd: 命令名称（不含斜杠，如 "help"）
	// args: 命令参数（空格分隔后的参数列表）
	// msg: 原始入站消息
	Handle(ctx context.Context, cmd string, args []string, msg *bus.InboundMessage) *CommandResult
}

// CommandHandlerFunc 命令处理函数类型
type CommandHandlerFunc func(ctx context.Context, cmd string, args []string, msg *bus.InboundMessage) *CommandResult

// Handle 实现 CommandHandler 接口
func (f CommandHandlerFunc) Handle(ctx context.Context, cmd string, args []string, msg *bus.InboundMessage) *CommandResult {
	return f(ctx, cmd, args, msg)
}

// ParseCommand 解析消息中的命令
// 返回: 命令名称（不含斜杠）、参数列表、是否是命令
func ParseCommand(content string) (cmd string, args []string, isCommand bool) {
	content = trimLeadingWhitespace(content)

	if len(content) == 0 || content[0] != '/' {
		return "", nil, false
	}

	// 找到第一个空格的位置
	spaceIdx := findFirstSpace(content)

	if spaceIdx == -1 {
		// 没有空格，整个内容就是命令
		cmd = content[1:]
		return cmd, nil, true
	}

	// 命令是从斜杠后到第一个空格
	cmd = content[1:spaceIdx]

	// 参数是空格后的内容
	remaining := content[spaceIdx+1:]
	args = parseArgs(remaining)

	return cmd, args, true
}

// trimLeadingWhitespace 移除前导空白字符
func trimLeadingWhitespace(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] != ' ' && s[i] != '\t' && s[i] != '\n' && s[i] != '\r' {
			return s[i:]
		}
	}
	return ""
}

// findFirstSpace 找到第一个空格的位置
func findFirstSpace(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == ' ' || s[i] == '\t' {
			return i
		}
	}
	return -1
}

// parseArgs 解析参数，支持引号包裹
func parseArgs(s string) []string {
	var args []string
	var current strings.Builder
	inQuote := false
	quoteChar := byte(0)

	for i := 0; i < len(s); i++ {
		c := s[i]

		switch {
		case inQuote:
			if c == quoteChar {
				inQuote = false
				quoteChar = 0
			} else {
				current.WriteByte(c)
			}
		case c == '"' || c == '\'':
			inQuote = true
			quoteChar = c
		case c == ' ' || c == '\t':
			if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
		default:
			current.WriteByte(c)
		}
	}

	if current.Len() > 0 {
		args = append(args, current.String())
	}

	return args
}