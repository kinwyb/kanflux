package handler

import (
	"context"

	"github.com/kinwyb/kanflux/gateway/types"
)

// ControlHandler handles control messages.
type ControlHandler struct{}

// NewControlHandler creates a new control handler.
func NewControlHandler() *ControlHandler {
	return &ControlHandler{}
}

// Handle handles control messages.
func (h *ControlHandler) Handle(ctx context.Context, conn Conn, wsMsg *types.WSMessage) error {
	var payload types.ControlPayload
	if err := wsMsg.ParsePayload(&payload); err != nil {
		conn.GetLogger().Error("Parse control payload error", "error", err)
		SendControlAck(conn, wsMsg.ID, payload.Action, false, "Invalid control payload")
		return err
	}

	// Check if there's a custom command handler
	customHandler := conn.GetServer().GetCommandHandler(payload.Action)
	if customHandler != nil {
		// Use custom handler
		if err := customHandler.Handle(ctx, conn, wsMsg); err != nil {
			SendControlAck(conn, wsMsg.ID, payload.Action, false, err.Error())
			return err
		}
		return nil
	}

	// Default handling logic
	switch payload.Action {
	case types.ControlActionShutdown:
		conn.GetLogger().Info("Received shutdown control message")
		// Send response
		SendControlAck(conn, wsMsg.ID, types.ControlActionShutdown, true, "Gateway shutting down...")
		// Trigger server shutdown
		conn.GetServer().TriggerShutdown()

	case types.ControlActionStatus:
		// Send status response
		status := conn.GetServer().GetStatus()
		SendControlAckWithData(conn, wsMsg.ID, types.ControlActionStatus, true, "Gateway is running", status)

	default:
		SendControlAck(conn, wsMsg.ID, payload.Action, false, "Unknown control action: "+payload.Action)
	}

	return nil
}

// SendControlAck sends a control message acknowledgment.
func SendControlAck(conn Conn, id, action string, success bool, message string) {
	ackPayload := types.ControlAckPayload{
		Action:  action,
		Success: success,
		Message: message,
	}

	ackMsg, err := types.NewWSMessage(types.MsgTypeControlAck, id, ackPayload)
	if err != nil {
		return
	}

	msgBytes, err := ackMsg.Marshal()
	if err != nil {
		return
	}

	conn.Send(msgBytes)
}

// SendControlAckWithData sends a control message acknowledgment with additional data.
func SendControlAckWithData(conn Conn, id, action string, success bool, message string, data map[string]interface{}) {
	ackPayload := types.ControlAckPayload{
		Action:  action,
		Success: success,
		Message: message,
	}

	ackMsg, err := types.NewWSMessage(types.MsgTypeControlAck, id, ackPayload)
	if err != nil {
		return
	}

	msgBytes, err := ackMsg.Marshal()
	if err != nil {
		return
	}

	conn.Send(msgBytes)
}

// init registers the control handler.
func init() {
	Register(types.MsgTypeControl, NewControlHandler())
}