package rag

import (
	"github.com/cloudwego/eino/components/embedding"
)

// KnowledgePath 知识库路径配置
type KnowledgePath struct {
	Path       string   `json:"path"`       // 文件或目录路径
	Extensions []string `json:"extensions"` // 文件扩展名过滤，空表示所有
	Recursive  bool     `json:"recursive"`  // 是否递归子目录
	Exclude    []string `json:"exclude"`    // 排除的模式（glob）
}

// Config RAG 配置
type Config struct {
	Workspace      string            `json:"workspace"`       // 工作区路径
	KnowledgePaths []KnowledgePath   `json:"knowledge_paths"` // 知识库路径列表
	EmbeddingModel string            `json:"embedding_model"` // Embedding 模型名称
	ChunkSize      int               `json:"chunk_size"`      // 分块大小（字符数）
	ChunkOverlap   int               `json:"chunk_overlap"`   // 分块重叠
	TopK           int               `json:"top_k"`           // 检索返回数量
	ScoreThreshold float64           `json:"score_threshold"` // 相关性阈值
	EnableWatcher  bool              `json:"enable_watcher"`  // 启用文件监控
	Embedder       embedding.Embedder                         // Embedding 实例
	StoreType      string            `json:"store_type"`      // 存储类型: "sqlite" (默认) 或 "json"
}

// DefaultConfig 返回默认配置
func DefaultConfig() *Config {
	return &Config{
		ChunkSize:      500,
		ChunkOverlap:   50,
		TopK:           5,
		ScoreThreshold: 0.5,
		EnableWatcher:  true,
	}
}

// RetrieveConfig 检索配置
type RetrieveConfig struct {
	TopK           int
	ScoreThreshold float64
	SourceFilter   string
}

// RetrieveOption 检索选项函数
type RetrieveOption func(*RetrieveConfig)

// WithTopK 设置返回数量
func WithTopK(k int) RetrieveOption {
	return func(c *RetrieveConfig) {
		c.TopK = k
	}
}

// WithScoreThreshold 设置相关性阈值
func WithScoreThreshold(threshold float64) RetrieveOption {
	return func(c *RetrieveConfig) {
		c.ScoreThreshold = threshold
	}
}

// WithSourceFilter 设置源路径过滤
func WithSourceFilter(filter string) RetrieveOption {
	return func(c *RetrieveConfig) {
		c.SourceFilter = filter
	}
}

// ApplyOptions 应用检索选项
func ApplyOptions(opts ...RetrieveOption) *RetrieveConfig {
	cfg := &RetrieveConfig{
		TopK:           5,
		ScoreThreshold: 0.5,
	}
	for _, opt := range opts {
		opt(cfg)
	}
	return cfg
}

// EmbeddingProviderType Embedding Provider 类型
type EmbeddingProviderType string

const (
	EmbeddingProviderOpenAI EmbeddingProviderType = "openai"
	EmbeddingProviderOllama EmbeddingProviderType = "ollama"
)

// EmbeddingConfig Embedding Provider 配置
type EmbeddingConfig struct {
	Provider   EmbeddingProviderType `json:"provider"`    // Provider 类型
	Model      string                `json:"model"`       // 模型名称
	APIKey     string                `json:"api_key"`     // API Key (OpenAI)
	APIBaseURL string                `json:"api_base_url"`// API Base URL
}