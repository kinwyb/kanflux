package agent

import (
	"time"

	"github.com/cloudwego/eino/adk"
)

type EventType string

const (
	EventMessageStart       EventType = "message_start"
	EventMessageUpdate      EventType = "message_update"
	EventMessageEnd         EventType = "message_end"
	EventToolStart          EventType = "tool_start"
	EventToolEnd            EventType = "tool_end"
	EventInterrupt          EventType = "interrupt"
	EventUnregisterCallback EventType = "unregister_callback"
)

type Event struct {
	Type      EventType              `json:"type"`
	Message   adk.Message            `json:"message,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
	Timestamp int64                  `json:"timestamp"`
}

// NewEvent creates a new event with current timestamp
func NewEvent(eventType EventType) *Event {
	return &Event{
		Type:      eventType,
		Timestamp: time.Now().UnixMilli(),
	}
}

// WithMessage adds message to the event
func (e *Event) WithMessage(msg adk.Message) *Event {
	e.Message = msg
	return e
}

// WithMetadata adds metadata to the event
func (e *Event) WithMetadata(metadata map[string]interface{}) *Event {
	e.Metadata = metadata
	return e
}

// EventCallback 事件回调函数
type EventCallback func(event *Event)
