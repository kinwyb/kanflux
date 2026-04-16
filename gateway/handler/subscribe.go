package handler

import (
	"context"

	"github.com/kinwyb/kanflux/gateway/types"
)

// SubscribeHandler handles subscription requests.
type SubscribeHandler struct{}

// NewSubscribeHandler creates a new subscribe handler.
func NewSubscribeHandler() *SubscribeHandler {
	return &SubscribeHandler{}
}

// Handle handles subscription requests.
func (h *SubscribeHandler) Handle(_ context.Context, conn Conn, wsMsg *types.WSMessage) error {
	var payload types.SubscribePayload
	if err := wsMsg.ParsePayload(&payload); err != nil {
		conn.GetLogger().Error("Parse subscribe payload error", "error", err)
		SendError(conn, "Invalid subscribe payload")
		return err
	}

	// Update subscription
	conn.GetServer().UpdateSubscription(conn.ID(), payload.Channels, payload.ChatIDs)

	conn.GetLogger().Debug("Subscription updated", "channels", payload.Channels, "chat_ids", payload.ChatIDs)
	return nil
}

// init registers the subscribe handler.
func init() {
	Register(types.MsgTypeSubscribe, NewSubscribeHandler())
}