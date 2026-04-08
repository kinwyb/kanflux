package rag

import (
	"context"

	"github.com/cloudwego/eino/components/embedding"
	"github.com/kinwyb/kanflux/providers"
)

// EmbedderConfig Embedder 配置
type EmbedderConfig struct {
	Provider   string `json:"provider"`     // Provider 类型: openai, ollama
	Model      string `json:"model"`        // 模型名称
	APIKey     string `json:"api_key"`      // API Key
	APIBaseURL string `json:"api_base_url"` // API Base URL
}

// CreateEmbedder 创建 Embedder 实例
func CreateEmbedder(ctx context.Context, cfg *EmbedderConfig) (embedding.Embedder, error) {
	if cfg == nil {
		return nil, nil
	}

	providerType := providers.EmbeddingProviderOpenAI
	if cfg.Provider == "ollama" {
		providerType = providers.EmbeddingProviderOllama
	}

	return providers.NewEmbedder(ctx, &providers.EmbeddingConfig{
		Provider:   providerType,
		Model:      cfg.Model,
		APIKey:     cfg.APIKey,
		APIBaseURL: cfg.APIBaseURL,
	})
}