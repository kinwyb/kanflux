package handler

import (
	"context"
	"time"

	"github.com/kinwyb/kanflux/gateway/types"
)

// HeartbeatHandler handles heartbeat messages.
type HeartbeatHandler struct{}

// NewHeartbeatHandler creates a new heartbeat handler.
func NewHeartbeatHandler() *HeartbeatHandler {
	return &HeartbeatHandler{}
}

// Handle handles heartbeat messages.
func (h *HeartbeatHandler) Handle(_ context.Context, conn Conn, wsMsg *types.WSMessage) error {
	var payload types.HeartbeatPayload
	if err := wsMsg.ParsePayload(&payload); err != nil {
		payload.Timestamp = time.Now().UnixMilli()
	}

	// Send heartbeat response
	ackPayload := types.HeartbeatPayload{
		Timestamp: time.Now().UnixMilli(),
	}

	ackMsg, err := types.NewWSMessage(types.MsgTypeHeartbeatAck, wsMsg.ID, ackPayload)
	if err != nil {
		return err
	}

	msgBytes, err := ackMsg.Marshal()
	if err != nil {
		return err
	}

	conn.Send(msgBytes)
	return nil
}

// init registers the heartbeat handler.
func init() {
	Register(types.MsgTypeHeartbeat, NewHeartbeatHandler())
}