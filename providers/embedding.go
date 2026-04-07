package providers

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/components/embedding"
	ollamaEmbedding "github.com/cloudwego/eino-ext/components/embedding/ollama"
	openaiEmbedding "github.com/cloudwego/eino-ext/components/embedding/openai"
)

// EmbeddingProviderType Embedding Provider 类型
type EmbeddingProviderType string

const (
	EmbeddingProviderOpenAI EmbeddingProviderType = "openai"
	EmbeddingProviderOllama EmbeddingProviderType = "ollama"
)

// EmbeddingConfig Embedding 配置
type EmbeddingConfig struct {
	Provider   EmbeddingProviderType
	Model      string
	APIKey     string
	APIBaseURL string
}

// NewEmbedder 创建 Embedder
func NewEmbedder(ctx context.Context, cfg *EmbeddingConfig) (embedding.Embedder, error) {
	if cfg == nil {
		return nil, fmt.Errorf("embedding config is nil")
	}

	switch cfg.Provider {
	case EmbeddingProviderOpenAI:
		return newOpenAIEmbedder(ctx, cfg)
	case EmbeddingProviderOllama:
		return newOllamaEmbedder(ctx, cfg)
	default:
		return nil, fmt.Errorf("unsupported embedding provider: %s", cfg.Provider)
	}
}

// newOpenAIEmbedder 创建 OpenAI Embedder
func newOpenAIEmbedder(ctx context.Context, cfg *EmbeddingConfig) (embedding.Embedder, error) {
	model := cfg.Model
	if model == "" {
		model = "text-embedding-3-small" // 默认模型
	}

	config := &openaiEmbedding.EmbeddingConfig{
		Model: model,
	}

	if cfg.APIKey != "" {
		config.APIKey = cfg.APIKey
	}
	if cfg.APIBaseURL != "" {
		config.BaseURL = cfg.APIBaseURL
	}

	return openaiEmbedding.NewEmbedder(ctx, config)
}

// newOllamaEmbedder 创建 Ollama Embedder
func newOllamaEmbedder(ctx context.Context, cfg *EmbeddingConfig) (embedding.Embedder, error) {
	model := cfg.Model
	if model == "" {
		model = "nomic-embed-text" // 默认模型
	}

	config := &ollamaEmbedding.EmbeddingConfig{
		Model: model,
	}

	if cfg.APIBaseURL != "" {
		config.BaseURL = cfg.APIBaseURL
	}

	return ollamaEmbedding.NewEmbedder(ctx, config)
}

// DefaultEmbeddingModel 返回默认的 Embedding 模型名称
func DefaultEmbeddingModel(provider EmbeddingProviderType) string {
	switch provider {
	case EmbeddingProviderOpenAI:
		return "text-embedding-3-small"
	case EmbeddingProviderOllama:
		return "nomic-embed-text"
	default:
		return ""
	}
}