package handler

import (
	"context"

	"github.com/kinwyb/kanflux/gateway/types"
)

// InboundHandler handles inbound messages.
type InboundHandler struct{}

// NewInboundHandler creates a new inbound handler.
func NewInboundHandler() *InboundHandler {
	return &InboundHandler{}
}

// Handle handles inbound messages.
func (h *InboundHandler) Handle(ctx context.Context, conn Conn, wsMsg *types.WSMessage) error {
	var payload types.InboundPayload
	if err := wsMsg.ParsePayload(&payload); err != nil {
		conn.GetLogger().Error("Parse inbound payload error", "error", err)
		SendError(conn, "Invalid inbound payload")
		return err
	}

	// Convert to InboundMessage
	inbound := types.ConvertPayloadToInbound(&payload)
	if inbound.ID == "" {
		inbound.ID = wsMsg.ID
	}

	// Publish to MessageBus
	if err := conn.GetServer().PublishInbound(ctx, inbound); err != nil {
		conn.GetLogger().Error("Publish inbound error", "error", err)
		SendError(conn, "Failed to publish inbound: "+err.Error())
		return err
	}

	conn.GetLogger().Debug("Inbound message published", "channel", inbound.Channel, "chat_id", inbound.ChatID)
	return nil
}

// SendError sends an error message to the connection.
func SendError(conn Conn, message string) {
	errorPayload := types.ErrorPayload{
		Message: message,
	}

	wsMsg, err := types.NewWSMessage(types.MsgTypeError, "", errorPayload)
	if err != nil {
		return
	}

	msgBytes, err := wsMsg.Marshal()
	if err != nil {
		return
	}

	conn.Send(msgBytes)
}

// init registers the inbound handler.
func init() {
	Register(types.MsgTypeInbound, NewInboundHandler())
}