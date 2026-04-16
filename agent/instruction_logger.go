package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/kinwyb/kanflux/session"
)

// InstructionLoggerMiddleware 记录每次 agent 执行的 Instruction
type InstructionLoggerMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
	workspace      string           // 工作目录，用于保存 instruction 文件
	sessionManager *session.Manager // Session 管理器
}

func NewInstructionLoggerMiddleware(workspace string, sessionManager *session.Manager) *InstructionLoggerMiddleware {
	return &InstructionLoggerMiddleware{
		workspace:      workspace,
		sessionManager: sessionManager,
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

	// 手工替换 SessionValues 占位符
	instruction := runCtx.Instruction
	sessionValues := adk.GetSessionValues(ctx)
	if sessionValues != nil {
		for key, value := range sessionValues {
			placeholder := "{" + key + "}"
			if strings.Contains(instruction, placeholder) {
				instruction = strings.ReplaceAll(instruction, placeholder, fmt.Sprintf("%v", value))
			}
		}
	}

	// 记录到 slog
	slog.Debug("Agent Instruction",
		"session_id", sessionID,
		"agent_name", agentName,
		"instruction_length", len(instruction))

	// 记录到 Session 文件（优先）
	if m.sessionManager != nil && instruction != "" && sessionID != "" {
		m.recordInstructionToSession(sessionID, agentName, instruction)
	} else if m.workspace != "" && instruction != "" && sessionID != "" {
		// 向后兼容：如果没有 sessionManager，使用旧方式
		m.saveInstructionToFile(sessionID, agentName, instruction)
	}

	return ctx, runCtx, nil
}

// recordInstructionToSession 将 instruction 记录到 Session 文件
func (m *InstructionLoggerMiddleware) recordInstructionToSession(sessionID, agentName, instruction string) {
	// 获取或创建 session
	sess, err := m.sessionManager.GetOrCreate(sessionID)
	if err != nil {
		slog.Warn("Failed to get session for instruction logging", "error", err, "session_id", sessionID)
		return
	}

	// 创建 instruction entry
	entry := session.InstructionEntry{
		AgentName: agentName,
		Content:   instruction,
		Timestamp: time.Now(),
	}

	// 添加 instruction（带去重）
	added := sess.AddInstruction(entry)
	if !added {
		slog.Debug("Instruction skipped (duplicate)", "session_id", sessionID)
		return
	}

	// 保存 session
	if err := m.sessionManager.Save(sess); err != nil {
		slog.Warn("Failed to save instruction to session", "error", err, "session_id", sessionID)
	} else {
		slog.Debug("Instruction recorded to session",
			"session_id", sessionID,
			"agent_name", agentName,
			"instruction_hash", entry.ContentHash)
	}
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