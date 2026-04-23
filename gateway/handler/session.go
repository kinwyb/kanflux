// Package handler provides WebSocket message handlers for the gateway.
package handler

import (
	"context"
	"strings"
	"time"
	"unicode"

	"github.com/kinwyb/kanflux/gateway/types"
	"github.com/kinwyb/kanflux/session"
)

// SessionListHandler 处理 session 列表请求
type SessionListHandler struct{}

// NewSessionListHandler 创建 session 列表处理器
func NewSessionListHandler() *SessionListHandler {
	return &SessionListHandler{}
}

// Handle 处理 session 列表请求
func (h *SessionListHandler) Handle(ctx context.Context, conn Conn, msg *types.WSMessage) error {
	// 解析请求
	var payload types.SessionListPayload
	if err := msg.ParsePayload(&payload); err != nil {
		return h.sendError(conn, msg.ID, "解析请求失败: "+err.Error())
	}

	// 获取 SessionManager
	sessionMgr := conn.GetServer().GetSessionManager()
	if sessionMgr == nil {
		return h.sendError(conn, msg.ID, "SessionManager 未初始化")
	}

	// 查询 session 元数据
	var metas []*session.SessionMeta
	var err error

	if payload.DateStart != "" && payload.DateEnd != "" {
		// 按日期范围查询
		start, err1 := time.Parse("2006-01-02", payload.DateStart)
		end, err2 := time.Parse("2006-01-02", payload.DateEnd)
		if err1 != nil || err2 != nil {
			return h.sendError(conn, msg.ID, "日期格式错误，应为 YYYY-MM-DD")
		}
		// 包含结束日期的整天
		end = end.Add(24 * time.Hour)
		metas, err = sessionMgr.GetMetaByDateRange(start, end)
	} else {
		// 获取所有 session
		metas, err = sessionMgr.ListMeta()
	}

	if err != nil {
		return h.sendError(conn, msg.ID, "查询 session 失败: "+err.Error())
	}

	// 转换为 payload 格式
	sessions := make([]*types.SessionMetaPayload, 0, len(metas))
	for _, meta := range metas {
		sessions = append(sessions, &types.SessionMetaPayload{
			Key:          meta.Key,
			CreatedAt:    meta.CreatedAt.Format(time.RFC3339),
			UpdatedAt:    meta.UpdatedAt.Format(time.RFC3339),
			MessageCount: meta.MessageCount,
			InstrCount:   meta.InstrCount,
			Metadata:     meta.Metadata,
		})
	}

	// 发送响应
	ack := &types.SessionListAckPayload{
		Success:  true,
		Sessions: sessions,
	}
	return conn.SendMessage(types.MsgTypeSessionListAck, msg.ID, ack)
}

// sendError 发送错误响应
func (h *SessionListHandler) sendError(conn Conn, msgID string, errMsg string) error {
	ack := &types.SessionListAckPayload{
		Success: false,
		Error:   errMsg,
	}
	return conn.SendMessage(types.MsgTypeSessionListAck, msgID, ack)
}

// SessionGetHandler 处理 session 内容请求
type SessionGetHandler struct{}

// NewSessionGetHandler 创建 session 内容处理器
func NewSessionGetHandler() *SessionGetHandler {
	return &SessionGetHandler{}
}

// Handle 处理 session 内容请求
func (h *SessionGetHandler) Handle(ctx context.Context, conn Conn, msg *types.WSMessage) error {
	// 解析请求
	var payload types.SessionGetPayload
	if err := msg.ParsePayload(&payload); err != nil {
		return h.sendError(conn, msg.ID, "解析请求失败: "+err.Error())
	}

	if payload.Key == "" {
		return h.sendError(conn, msg.ID, "session key 不能为空")
	}

	// 获取 SessionManager
	sessionMgr := conn.GetServer().GetSessionManager()
	if sessionMgr == nil {
		return h.sendError(conn, msg.ID, "SessionManager 未初始化")
	}

	// 获取 session
	sess, err := sessionMgr.GetOrCreate(payload.Key)
	if err != nil {
		return h.sendError(conn, msg.ID, "获取 session 失败: "+err.Error())
	}

	// 获取元数据
	meta := sess.GetMeta()

	// 获取消息历史（加载完整数据）
	history := sess.GetHistory(0) // 0 表示获取全部

	// 获取 instructions
	instructions := sess.GetInstructions()

	// 转换消息为 payload 格式
	msgs := make([]*types.MessagePayload, 0, len(history))
	for _, m := range history {
		msgPayload := &types.MessagePayload{
			Role:       string(m.Role),
			Content:    strings.TrimLeftFunc(m.Content, unicode.IsSpace),
			ToolCallID: m.ToolCallID,
			Name:       m.Name,
			Reasoning:  strings.TrimLeftFunc(m.ReasoningContent, unicode.IsSpace),
		}
		if m.Extra != nil {
			msgPayload.ID = m.Extra["req_id"].(string)
		}
		// 转换 ToolCalls
		if len(m.ToolCalls) > 0 {
			msgPayload.ToolCalls = make([]*types.ToolCallPayload, 0, len(m.ToolCalls))
			for _, tc := range m.ToolCalls {
				msgPayload.ToolCalls = append(msgPayload.ToolCalls, &types.ToolCallPayload{
					ID:   tc.ID,
					Type: tc.Type,
					Function: &types.ToolFunctionPayload{
						Name:      tc.Function.Name,
						Arguments: tc.Function.Arguments,
					},
				})
			}
		}
		msgs = append(msgs, msgPayload)
	}

	// 转换 instructions 为 payload 格式
	instrs := make([]*types.InstructionPayload, 0, len(instructions))
	for _, instr := range instructions {
		instrs = append(instrs, &types.InstructionPayload{
			AgentName: instr.AgentName,
			Content:   instr.Content,
			Timestamp: instr.Timestamp.Format(time.RFC3339),
		})
	}

	// 发送响应
	ack := &types.SessionGetAckPayload{
		Success:      true,
		Key:          meta.Key,
		CreatedAt:    meta.CreatedAt.Format(time.RFC3339),
		UpdatedAt:    meta.UpdatedAt.Format(time.RFC3339),
		Metadata:     meta.Metadata,
		Messages:     msgs,
		Instructions: instrs,
	}
	return conn.SendMessage(types.MsgTypeSessionGetAck, msg.ID, ack)
}

// sendError 发送错误响应
func (h *SessionGetHandler) sendError(conn Conn, msgID string, errMsg string) error {
	ack := &types.SessionGetAckPayload{
		Success: false,
		Error:   errMsg,
	}
	return conn.SendMessage(types.MsgTypeSessionGetAck, msgID, ack)
}

// 注册 handlers
func init() {
	Register(types.MsgTypeSessionList, NewSessionListHandler())
	Register(types.MsgTypeSessionGet, NewSessionGetHandler())
	Register(types.MsgTypeSessionDelete, NewSessionDeleteHandler())
}

// SessionDeleteHandler 处理 session 删除请求
type SessionDeleteHandler struct{}

// NewSessionDeleteHandler 创建 session 删除处理器
func NewSessionDeleteHandler() *SessionDeleteHandler {
	return &SessionDeleteHandler{}
}

// Handle 处理 session 删除请求
func (h *SessionDeleteHandler) Handle(ctx context.Context, conn Conn, msg *types.WSMessage) error {
	// 解析请求
	var payload types.SessionDeletePayload
	if err := msg.ParsePayload(&payload); err != nil {
		return h.sendError(conn, msg.ID, "解析请求失败: "+err.Error())
	}

	if payload.Key == "" {
		return h.sendError(conn, msg.ID, "session key 不能为空")
	}

	// 获取 SessionManager
	sessionMgr := conn.GetServer().GetSessionManager()
	if sessionMgr == nil {
		return h.sendError(conn, msg.ID, "SessionManager 未初始化")
	}

	// 删除 session
	err := sessionMgr.Delete(payload.Key)
	if err != nil {
		return h.sendError(conn, msg.ID, "删除 session 失败: "+err.Error())
	}

	// 发送成功响应
	ack := &types.SessionDeleteAckPayload{
		Success: true,
		Key:     payload.Key,
	}
	return conn.SendMessage(types.MsgTypeSessionDeleteAck, msg.ID, ack)
}

// sendError 发送错误响应
func (h *SessionDeleteHandler) sendError(conn Conn, msgID string, errMsg string) error {
	ack := &types.SessionDeleteAckPayload{
		Success: false,
		Error:   errMsg,
	}
	return conn.SendMessage(types.MsgTypeSessionDeleteAck, msgID, ack)
}
