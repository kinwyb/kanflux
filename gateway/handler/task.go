package handler

import (
	"context"

	"github.com/kinwyb/kanflux/gateway/types"
)

// TaskServer interface for task operations
type TaskServer interface {
	Server
	HandleTaskList(connID string, msgID string) error
	HandleTaskAdd(connID string, msgID string, payload *types.TaskAddPayload) error
	HandleTaskUpdate(connID string, msgID string, payload *types.TaskUpdatePayload) error
	HandleTaskRemove(connID string, msgID string, payload *types.TaskRemovePayload) error
	HandleTaskTrigger(connID string, msgID string, payload *types.TaskTriggerPayload) error
	HandleTaskStatus(connID string, msgID string, payload *types.TaskStatusPayload) error
}

// RegisterTaskHandlers registers all task-related handlers
func RegisterTaskHandlers() {
	RegisterFunc(types.MsgTypeTaskList, handleTaskList)
	RegisterFunc(types.MsgTypeTaskAdd, handleTaskAdd)
	RegisterFunc(types.MsgTypeTaskUpdate, handleTaskUpdate)
	RegisterFunc(types.MsgTypeTaskRemove, handleTaskRemove)
	RegisterFunc(types.MsgTypeTaskTrigger, handleTaskTrigger)
	RegisterFunc(types.MsgTypeTaskStatus, handleTaskStatus)
}

func handleTaskList(ctx context.Context, conn Conn, msg *types.WSMessage) error {
	server := conn.GetServer()
	taskServer, ok := server.(TaskServer)
	if !ok {
		return conn.SendMessage(types.MsgTypeTaskListAck, msg.ID, &types.TaskListAckPayload{
			Success: false,
			Error:   "task scheduler not available",
		})
	}
	return taskServer.HandleTaskList(conn.ID(), msg.ID)
}

func handleTaskAdd(ctx context.Context, conn Conn, msg *types.WSMessage) error {
	server := conn.GetServer()
	taskServer, ok := server.(TaskServer)
	if !ok {
		return conn.SendMessage(types.MsgTypeTaskAddAck, msg.ID, &types.TaskAddAckPayload{
			Success: false,
			Error:   "task scheduler not available",
		})
	}

	var payload types.TaskAddPayload
	if err := msg.ParsePayload(&payload); err != nil {
		return conn.SendMessage(types.MsgTypeTaskAddAck, msg.ID, &types.TaskAddAckPayload{
			Success: false,
			Error:   "invalid payload: " + err.Error(),
		})
	}

	return taskServer.HandleTaskAdd(conn.ID(), msg.ID, &payload)
}

func handleTaskUpdate(ctx context.Context, conn Conn, msg *types.WSMessage) error {
	server := conn.GetServer()
	taskServer, ok := server.(TaskServer)
	if !ok {
		return conn.SendMessage(types.MsgTypeTaskUpdateAck, msg.ID, &types.TaskUpdateAckPayload{
			Success: false,
			Error:   "task scheduler not available",
		})
	}

	var payload types.TaskUpdatePayload
	if err := msg.ParsePayload(&payload); err != nil {
		return conn.SendMessage(types.MsgTypeTaskUpdateAck, msg.ID, &types.TaskUpdateAckPayload{
			Success: false,
			Error:   "invalid payload: " + err.Error(),
		})
	}

	return taskServer.HandleTaskUpdate(conn.ID(), msg.ID, &payload)
}

func handleTaskRemove(ctx context.Context, conn Conn, msg *types.WSMessage) error {
	server := conn.GetServer()
	taskServer, ok := server.(TaskServer)
	if !ok {
		return conn.SendMessage(types.MsgTypeTaskRemoveAck, msg.ID, &types.TaskRemoveAckPayload{
			Success: false,
			Error:   "task scheduler not available",
		})
	}

	var payload types.TaskRemovePayload
	if err := msg.ParsePayload(&payload); err != nil {
		return conn.SendMessage(types.MsgTypeTaskRemoveAck, msg.ID, &types.TaskRemoveAckPayload{
			Success: false,
			Error:   "invalid payload: " + err.Error(),
		})
	}

	return taskServer.HandleTaskRemove(conn.ID(), msg.ID, &payload)
}

func handleTaskTrigger(ctx context.Context, conn Conn, msg *types.WSMessage) error {
	server := conn.GetServer()
	taskServer, ok := server.(TaskServer)
	if !ok {
		return conn.SendMessage(types.MsgTypeTaskTriggerAck, msg.ID, &types.TaskTriggerAckPayload{
			Success: false,
			Error:   "task scheduler not available",
		})
	}

	var payload types.TaskTriggerPayload
	if err := msg.ParsePayload(&payload); err != nil {
		return conn.SendMessage(types.MsgTypeTaskTriggerAck, msg.ID, &types.TaskTriggerAckPayload{
			Success: false,
			Error:   "invalid payload: " + err.Error(),
		})
	}

	return taskServer.HandleTaskTrigger(conn.ID(), msg.ID, &payload)
}

func handleTaskStatus(ctx context.Context, conn Conn, msg *types.WSMessage) error {
	server := conn.GetServer()
	taskServer, ok := server.(TaskServer)
	if !ok {
		return conn.SendMessage(types.MsgTypeTaskStatusAck, msg.ID, &types.TaskStatusAckPayload{
			Success: false,
			Error:   "task scheduler not available",
		})
	}

	var payload types.TaskStatusPayload
	if err := msg.ParsePayload(&payload); err != nil {
		return conn.SendMessage(types.MsgTypeTaskStatusAck, msg.ID, &types.TaskStatusAckPayload{
			Success: false,
			Error:   "invalid payload: " + err.Error(),
		})
	}

	return taskServer.HandleTaskStatus(conn.ID(), msg.ID, &payload)
}