package memoria

import (
	"context"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/kinwyb/kanflux/memoria/types"
)

// EinoChatModelAdapter wraps Eino's ToolCallingChatModel to implement types.ChatModel
type EinoChatModelAdapter struct {
	model model.ToolCallingChatModel
}

// NewEinoChatModelAdapter creates an adapter for Eino's ChatModel
func NewEinoChatModelAdapter(m model.ToolCallingChatModel) *EinoChatModelAdapter {
	return &EinoChatModelAdapter{model: m}
}

// Generate implements types.ChatModel.Generate
func (a *EinoChatModelAdapter) Generate(ctx context.Context, prompt string) (string, error) {
	msgs := []*schema.Message{
		schema.UserMessage(prompt),
	}
	resp, err := a.model.Generate(ctx, msgs)
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}

// GenerateWithSystem implements types.ChatModel.GenerateWithSystem
func (a *EinoChatModelAdapter) GenerateWithSystem(ctx context.Context, system, prompt string) (string, error) {
	msgs := []*schema.Message{
		schema.SystemMessage(system),
		schema.UserMessage(prompt),
	}
	resp, err := a.model.Generate(ctx, msgs)
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}

// Ensure EinoChatModelAdapter implements types.ChatModel
var _ types.ChatModel = (*EinoChatModelAdapter)(nil)