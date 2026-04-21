package handler

import (
	"context"

	"github.com/kinwyb/kanflux/gateway/types"
)

// ConfigServer interface for config operations
type ConfigServer interface {
	Server
	HandleConfigGet(connID string, msgID string) error
	HandleConfigUpdate(connID string, msgID string, payload *types.ConfigUpdatePayload) error
}

// RegisterConfigHandlers registers all config-related handlers
func RegisterConfigHandlers() {
	RegisterFunc(types.MsgTypeConfigGet, handleConfigGet)
	RegisterFunc(types.MsgTypeConfigUpdate, handleConfigUpdate)
}

func handleConfigGet(ctx context.Context, conn Conn, msg *types.WSMessage) error {
	server := conn.GetServer()
	configServer, ok := server.(ConfigServer)
	if !ok {
		return conn.SendMessage(types.MsgTypeConfigGetAck, msg.ID, &types.ConfigGetAckPayload{
			Success: false,
			Error:   "config manager not available",
		})
	}
	return configServer.HandleConfigGet(conn.ID(), msg.ID)
}

func handleConfigUpdate(ctx context.Context, conn Conn, msg *types.WSMessage) error {
	server := conn.GetServer()
	configServer, ok := server.(ConfigServer)
	if !ok {
		return conn.SendMessage(types.MsgTypeConfigUpdateAck, msg.ID, &types.ConfigUpdateAckPayload{
			Success: false,
			Error:   "config manager not available",
		})
	}

	var payload types.ConfigUpdatePayload
	if err := msg.ParsePayload(&payload); err != nil {
		return conn.SendMessage(types.MsgTypeConfigUpdateAck, msg.ID, &types.ConfigUpdateAckPayload{
			Success: false,
			Error:   "invalid payload: " + err.Error(),
		})
	}

	return configServer.HandleConfigUpdate(conn.ID(), msg.ID, &payload)
}