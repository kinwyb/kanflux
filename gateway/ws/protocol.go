// Package ws provides WebSocket server and client for kanflux gateway.
// WebSocket service acts as a message hub between TUI/Web clients and the backend services.
package ws

import (
	"github.com/kinwyb/kanflux/gateway/types"
)

// Re-export types from gateway/types for convenience
type MessageType = types.MessageType
type WSMessage = types.WSMessage
type InboundPayload = types.InboundPayload
type OutboundPayload = types.OutboundPayload
type ChatEventPayload = types.ChatEventPayload
type ToolEventInfoPayload = types.ToolEventInfoPayload
type LogEventPayload = types.LogEventPayload
type SubscribePayload = types.SubscribePayload
type MediaPayload = types.MediaPayload
type ErrorPayload = types.ErrorPayload
type HeartbeatPayload = types.HeartbeatPayload
type ControlPayload = types.ControlPayload
type ControlAckPayload = types.ControlAckPayload
// 定时任务相关
type TaskListPayload = types.TaskListPayload
type TaskAddPayload = types.TaskAddPayload
type TaskUpdatePayload = types.TaskUpdatePayload
type TaskRemovePayload = types.TaskRemovePayload
type TaskTriggerPayload = types.TaskTriggerPayload
type TaskStatusPayload = types.TaskStatusPayload
type SchedulePayload = types.SchedulePayload
type TargetPayload = types.TargetPayload
type ContentPayload = types.ContentPayload
type TaskListAckPayload = types.TaskListAckPayload
type TaskDetailPayload = types.TaskDetailPayload
type TaskStatePayload = types.TaskStatePayload
type TaskAddAckPayload = types.TaskAddAckPayload
type TaskUpdateAckPayload = types.TaskUpdateAckPayload
type TaskRemoveAckPayload = types.TaskRemoveAckPayload
type TaskTriggerAckPayload = types.TaskTriggerAckPayload
type TaskStatusAckPayload = types.TaskStatusAckPayload

// InboundMessage = types.InboundMessage
type InboundMessage = types.InboundMessage
type OutboundMessage = types.OutboundMessage
type ChatEvent = types.ChatEvent
type ToolEventInfo = types.ToolEventInfo
type LogEvent = types.LogEvent
type Media = types.Media

// Re-export constants
const (
	MsgTypeInbound      = types.MsgTypeInbound
	MsgTypeSubscribe    = types.MsgTypeSubscribe
	MsgTypeHeartbeat    = types.MsgTypeHeartbeat
	MsgTypeControl      = types.MsgTypeControl
	MsgTypeSessionList  = types.MsgTypeSessionList
	MsgTypeSessionGet   = types.MsgTypeSessionGet
	// 定时任务
	MsgTypeTaskList     = types.MsgTypeTaskList
	MsgTypeTaskAdd      = types.MsgTypeTaskAdd
	MsgTypeTaskUpdate   = types.MsgTypeTaskUpdate
	MsgTypeTaskRemove   = types.MsgTypeTaskRemove
	MsgTypeTaskTrigger  = types.MsgTypeTaskTrigger
	MsgTypeTaskStatus   = types.MsgTypeTaskStatus
	// 响应
	MsgTypeOutbound        = types.MsgTypeOutbound
	MsgTypeChatEvent       = types.MsgTypeChatEvent
	MsgTypeLogEvent        = types.MsgTypeLogEvent
	MsgTypeHeartbeatAck    = types.MsgTypeHeartbeatAck
	MsgTypeControlAck      = types.MsgTypeControlAck
	MsgTypeError           = types.MsgTypeError
	MsgTypeSessionListAck  = types.MsgTypeSessionListAck
	MsgTypeSessionGetAck   = types.MsgTypeSessionGetAck
	// 定时任务响应
	MsgTypeTaskListAck    = types.MsgTypeTaskListAck
	MsgTypeTaskAddAck     = types.MsgTypeTaskAddAck
	MsgTypeTaskUpdateAck  = types.MsgTypeTaskUpdateAck
	MsgTypeTaskRemoveAck  = types.MsgTypeTaskRemoveAck
	MsgTypeTaskTriggerAck = types.MsgTypeTaskTriggerAck
	MsgTypeTaskStatusAck  = types.MsgTypeTaskStatusAck
	ControlActionShutdown = types.ControlActionShutdown
	ControlActionStatus   = types.ControlActionStatus
)

// Re-export functions
var (
	NewWSMessage            = types.NewWSMessage
	ParseWSMessage          = types.ParseWSMessage
	ConvertInboundToPayload = types.ConvertInboundToPayload
	ConvertOutboundToPayload = types.ConvertOutboundToPayload
	ConvertChatEventToPayload = types.ConvertChatEventToPayload
	ConvertToolEventInfoPayload = types.ConvertToolEventInfoPayload
	ConvertLogEventToPayload = types.ConvertLogEventToPayload
	ConvertPayloadToInbound = types.ConvertPayloadToInbound
	ConvertPayloadToOutbound = types.ConvertPayloadToOutbound
	ConvertPayloadToChatEvent = types.ConvertPayloadToChatEvent
	ConvertPayloadToToolEventInfo = types.ConvertPayloadToToolEventInfo
)