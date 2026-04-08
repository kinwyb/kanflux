package knowledgebase

import (
	"context"

	"github.com/cloudwego/eino/components/embedding"
)

// EinoEmbedder adapts Eino's embedding.Embedder to our Embedder interface.
type EinoEmbedder struct {
	embedder embedding.Embedder
	model    string
}

// NewEinoEmbedder creates a new adapter for Eino's embedder.
func NewEinoEmbedder(e embedding.Embedder, model string) *EinoEmbedder {
	return &EinoEmbedder{
		embedder: e,
		model:    model,
	}
}

// Embed generates an embedding for a single text.
func (e *EinoEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	vectors, err := e.embedder.EmbedStrings(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(vectors) == 0 || len(vectors[0]) == 0 {
		return nil, ErrEmbedderNotSet
	}

	return float64SliceToFloat32(vectors[0]), nil
}

// EmbedBatch generates embeddings for multiple texts.
func (e *EinoEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	vectors, err := e.embedder.EmbedStrings(ctx, texts)
	if err != nil {
		return nil, err
	}

	result := make([][]float32, len(vectors))
	for i, v := range vectors {
		result[i] = float64SliceToFloat32(v)
	}

	return result, nil
}

// Dimension returns the embedding dimension.
func (e *EinoEmbedder) Dimension() int {
	return 768 // Default, can be detected from first embedding
}

// Model returns the model name.
func (e *EinoEmbedder) Model() string {
	return e.model
}

// float64SliceToFloat32 converts []float64 to []float32.
func float64SliceToFloat32(src []float64) []float32 {
	if src == nil {
		return nil
	}
	dst := make([]float32, len(src))
	for i, v := range src {
		dst[i] = float32(v)
	}
	return dst
}

// Ensure EinoEmbedder implements Embedder
var _ Embedder = (*EinoEmbedder)(nil)