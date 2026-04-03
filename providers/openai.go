package providers

import (
	"context"
	"errors"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
)

// NewOpenAI 初始化openai模型
func NewOpenAI(ctx context.Context, baseURL string, modelName string, apiKey string) (model.ToolCallingChatModel, error) {
	if apiKey == "" {
		return nil, errors.New("openai: empty api key")
	}
	return openai.NewChatModel(ctx, &openai.ChatModelConfig{
		APIKey:          apiKey,
		Model:           modelName,
		BaseURL:         baseURL,
		ByAzure:         false,
		ReasoningEffort: openai.ReasoningEffortLevelHigh,
	})
}
