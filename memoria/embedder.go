package memoria

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/components/embedding"
	ollamaEmbedding "github.com/cloudwego/eino-ext/components/embedding/ollama"
	openaiEmbedding "github.com/cloudwego/eino-ext/components/embedding/openai"
	"github.com/kinwyb/kanflux/memoria/types"
)

// EmbedderAdapter 将 eino Embedder 适配为 types.Embedder
type EmbedderAdapter struct {
	embedder   embedding.Embedder
	dimension  int
}

// NewEmbedderAdapter creates a new embedder adapter
func NewEmbedderAdapter(embedder embedding.Embedder, dimension int) *EmbedderAdapter {
	return &EmbedderAdapter{
		embedder:  embedder,
		dimension: dimension,
	}
}

// Embed 实现 types.Embedder 接口
func (a *EmbedderAdapter) Embed(ctx context.Context, text string) ([]float32, error) {
	results, err := a.embedder.EmbedStrings(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("no embedding returned")
	}
	// 转换 float64 到 float32
	result := make([]float32, len(results[0]))
	for i, v := range results[0] {
		result[i] = float32(v)
	}
	return result, nil
}

// EmbedBatch 实现 types.Embedder 接口
func (a *EmbedderAdapter) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	results, err := a.embedder.EmbedStrings(ctx, texts)
	if err != nil {
		return nil, err
	}
	// 转换 float64 到 float32
	float32Results := make([][]float32, len(results))
	for i, r := range results {
		float32Results[i] = make([]float32, len(r))
		for j, v := range r {
			float32Results[i][j] = float32(v)
		}
	}
	return float32Results, nil
}

// Dimension 返回 embedding 维度
func (a *EmbedderAdapter) Dimension() int {
	return a.dimension
}

// NewEmbedderFromConfig 从配置创建 Embedder
func NewEmbedderFromConfig(ctx context.Context, cfg *EmbeddingConfig) (types.Embedder, error) {
	if cfg == nil {
		return nil, fmt.Errorf("embedding config is nil")
	}

	if cfg.Provider == "" || cfg.Model == "" || cfg.APIKey == "" {
		return nil, fmt.Errorf("missing required embedding config: provider=%s, model=%s", cfg.Provider, cfg.Model)
	}

	var embedder embedding.Embedder
	var dimension int

	switch cfg.Provider {
	case "openai", "OpenAI":
		model := cfg.Model
		if model == "" {
			model = "text-embedding-3-small"
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

		e, err := openaiEmbedding.NewEmbedder(ctx, config)
		if err != nil {
			return nil, fmt.Errorf("failed to create OpenAI embedder: %w", err)
		}
		embedder = e
		dimension = 1536 // text-embedding-3-small 默认维度

	case "ollama", "Ollama":
		model := cfg.Model
		if model == "" {
			model = "nomic-embed-text"
		}

		config := &ollamaEmbedding.EmbeddingConfig{
			Model: model,
		}
		if cfg.APIBaseURL != "" {
			config.BaseURL = cfg.APIBaseURL
		}

		e, err := ollamaEmbedding.NewEmbedder(ctx, config)
		if err != nil {
			return nil, fmt.Errorf("failed to create Ollama embedder: %w", err)
		}
		embedder = e
		dimension = 768 // nomic-embed-text 默认维度

	default:
		return nil, fmt.Errorf("unsupported embedding provider: %s", cfg.Provider)
	}

	return NewEmbedderAdapter(embedder, dimension), nil
}