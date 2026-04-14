package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/cloudwego/eino/adk"
)

// InstructionLoggerMiddleware 记录每次 agent 执行的 Instruction
type InstructionLoggerMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
	workspace string // 工作目录，用于保存 instruction 文件
}

func NewInstructionLoggerMiddleware(workspace string) *InstructionLoggerMiddleware {
	return &InstructionLoggerMiddleware{
		workspace: workspace,
	}
}

func (m *InstructionLoggerMiddleware) BeforeAgent(
	ctx context.Context,
	runCtx *adk.ChatModelAgentContext,
) (context.Context, *adk.ChatModelAgentContext, error) {
	// 从 context 获取 session_id
	sessionID := ""
	if v, ok := adk.GetSessionValue(ctx, "session_id"); ok {
		if s, ok := v.(string); ok {
			sessionID = s
		}
	}

	// 从 context 获取 agent_name
	agentName := ""
	if v, ok := adk.GetSessionValue(ctx, "agent_name"); ok {
		if s, ok := v.(string); ok {
			agentName = s
		}
	}

	// 记录到 slog
	slog.Debug("Agent Instruction",
		"session_id", sessionID,
		"agent_name", agentName,
		"instruction_length", len(runCtx.Instruction))

	// 保存到文件
	if m.workspace != "" && runCtx.Instruction != "" && sessionID != "" {
		m.saveInstructionToFile(sessionID, agentName, runCtx.Instruction)
	}

	return ctx, runCtx, nil
}

func (m *InstructionLoggerMiddleware) saveInstructionToFile(sessionID, agentName, instruction string) {
	// 创建 instruction 日志目录
	dir := filepath.Join(m.workspace, ".kanflux", "instructions")
	if err := os.MkdirAll(dir, 0755); err != nil {
		slog.Warn("Failed to create instruction dir", "error", err)
		return
	}

	// 文件名：sessionID_timestamp.txt
	// 使用 sessionID 的 base 名称避免路径过长
	shortSessionID := filepath.Base(sessionID)
	if len(shortSessionID) > 50 {
		shortSessionID = shortSessionID[:50]
	}
	filename := filepath.Join(dir,
		shortSessionID+"_"+time.Now().Format("20060102_150405")+".txt")

	// 添加元数据头部
	content := fmt.Sprintf("# Agent: %s\n# Session: %s\n# Time: %s\n\n%s",
		agentName, sessionID, time.Now().Format(time.RFC3339), instruction)

	if err := os.WriteFile(filename, []byte(content), 0644); err != nil {
		slog.Warn("Failed to save instruction", "error", err)
	} else {
		slog.Debug("Instruction saved", "file", filename)
	}
}