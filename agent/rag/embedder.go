package rag

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/components/embedding"
	"github.com/kinwyb/kanflux/providers"
)

// EmbedderConfig Embedder 创建配置
type EmbedderConfig struct {
	Provider   string // provider 类型: openai, ollama
	Model      string // embedding 模型名称
	APIKey     string // API Key
	APIBaseURL string // API Base URL
}

// CreateEmbedder 创建 Embedder 实例
func CreateEmbedder(ctx context.Context, cfg *EmbedderConfig) (embedding.Embedder, error) {
	if cfg == nil {
		return nil, fmt.Errorf("embedder config is nil")
	}

	// 确定 provider 类型
	providerType := providers.EmbeddingProviderOpenAI
	if cfg.Provider != "" {
		providerLower := providers.EmbeddingProviderOpenAI
		if providerLower == "ollama" || cfg.Provider == "ollama" {
			providerType = providers.EmbeddingProviderOllama
		}
	}

	// 默认模型
	model := cfg.Model
	if model == "" {
		model = providers.DefaultEmbeddingModel(providerType)
	}

	return providers.NewEmbedder(ctx, &providers.EmbeddingConfig{
		Provider:   providerType,
		Model:      model,
		APIKey:     cfg.APIKey,
		APIBaseURL: cfg.APIBaseURL,
	})
}