// Package handler provides WebSocket message handlers for the gateway.
package handler

import (
	"context"

	"github.com/kinwyb/kanflux/gateway/types"
	"github.com/kinwyb/kanflux/session"
)

// Conn defines the interface for WebSocket connection that handlers need.
type Conn interface {
	// Send sends raw bytes to the connection
	Send(msg []byte)
	// SendMessage sends a typed WebSocket message
	SendMessage(typ types.MessageType, id string, payload interface{}) error
	// GetLogger returns the connection's logger
	GetLogger() Logger
	// GetServer returns the server interface
	GetServer() Server
	// ID returns the connection ID
	ID() string
}

// Server defines the interface for WebSocket server that handlers need.
type Server interface {
	// PublishInbound publishes an inbound message to the message bus
	PublishInbound(ctx context.Context, msg *types.InboundMessage) error
	// UpdateSubscription updates the connection's subscription
	UpdateSubscription(connID string, channels, chatIDs []string)
	// GetCommandHandler returns a custom command handler for the action
	GetCommandHandler(action string) Handler
	// TriggerShutdown triggers the server shutdown
	TriggerShutdown()
	// GetStatus returns the server status
	GetStatus() map[string]interface{}
	// Context returns the server's context
	Context() context.Context
	// GetSessionManager returns the session manager
	GetSessionManager() *session.Manager
}

// Logger defines the interface for logging that handlers need.
type Logger interface {
	Debug(msg string, args ...interface{})
	Info(msg string, args ...interface{})
	Warn(msg string, args ...interface{})
	Error(msg string, args ...interface{})
}

// Handler defines the interface for handling WebSocket messages.
type Handler interface {
	Handle(ctx context.Context, conn Conn, msg *types.WSMessage) error
}

// HandlerFunc is an adapter to allow using ordinary functions as handlers.
type HandlerFunc func(ctx context.Context, conn Conn, msg *types.WSMessage) error

// Handle implements the Handler interface.
func (f HandlerFunc) Handle(ctx context.Context, conn Conn, msg *types.WSMessage) error {
	return f(ctx, conn, msg)
}

// Registry manages handler registration by message type.
type Registry struct {
	handlers map[types.MessageType]Handler
}

// NewRegistry creates a new handler registry.
func NewRegistry() *Registry {
	return &Registry{
		handlers: make(map[types.MessageType]Handler),
	}
}

// Register registers a handler for a message type.
func (r *Registry) Register(msgType types.MessageType, h Handler) {
	r.handlers[msgType] = h
}

// RegisterFunc registers a handler function for a message type.
func (r *Registry) RegisterFunc(msgType types.MessageType, f HandlerFunc) {
	r.handlers[msgType] = f
}

// Get returns the handler for a message type.
func (r *Registry) Get(msgType types.MessageType) Handler {
	return r.handlers[msgType]
}

// Handle dispatches a message to the registered handler.
// Returns an error if no handler is registered for the message type.
func (r *Registry) Handle(ctx context.Context, conn Conn, msg *types.WSMessage) error {
	h := r.handlers[msg.Type]
	if h == nil {
		return ErrNoHandler{Type: msg.Type}
	}
	return h.Handle(ctx, conn, msg)
}

// ErrNoHandler is returned when no handler is registered for a message type.
type ErrNoHandler struct {
	Type types.MessageType
}

func (e ErrNoHandler) Error() string {
	return "no handler registered for message type: " + string(e.Type)
}

// DefaultRegistry is the global default handler registry.
var DefaultRegistry = NewRegistry()

// Register registers a handler in the default registry.
func Register(msgType types.MessageType, h Handler) {
	DefaultRegistry.Register(msgType, h)
}

// RegisterFunc registers a handler function in the default registry.
func RegisterFunc(msgType types.MessageType, f HandlerFunc) {
	DefaultRegistry.RegisterFunc(msgType, f)
}

// Get returns the handler from the default registry.
func Get(msgType types.MessageType) Handler {
	return DefaultRegistry.Get(msgType)
}

// Handle handles a message using the default registry.
func Handle(ctx context.Context, conn Conn, msg *types.WSMessage) error {
	return DefaultRegistry.Handle(ctx, conn, msg)
}