package rag

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// VectorStore 向量存储接口
// 实现 this interface 可以支持不同的向量数据库后端
type VectorStore interface {
	// 文档操作
	AddDocument(doc *DocumentInfo)
	RemoveDocument(docID string)
	GetDocument(docID string) (*DocumentInfo, bool)
	GetDocumentByPath(path string) (*DocumentInfo, bool)

	// 分块操作
	AddChunk(chunk *ChunkInfo)
	GetChunk(chunkID string) (*ChunkInfo, bool)
	GetAllChunks() []*ChunkInfo

	// 向量操作
	SetVector(chunkID string, vector []float64)
	GetVector(chunkID string) ([]float64, bool)
	GetAllVectors() map[string][]float64

	// 元数据操作
	SetMetadata(metadata *Metadata)
	GetMetadata() *Metadata

	// 生命周期
	Load() error
	Save() error
	Clear() error
	Close() error
	GetStats() *Stats
}

// StoreType 存储类型
type StoreType string

const (
	StoreTypeJSON   StoreType = "json"   // 本地 JSON 文件存储
	StoreTypeRedis  StoreType = "redis"  // Redis 向量存储
	StoreTypeMilvus StoreType = "milvus" // Milvus 向量存储
)

// StoreConfig 存储配置
type StoreConfig struct {
	Type      StoreType              // 存储类型
	Workspace string                 // 工作区路径（JSON 存储使用）
	Options   map[string]interface{} // 额外配置选项
}

// NewVectorStore 创建向量存储实例
func NewVectorStore(cfg *StoreConfig) (VectorStore, error) {
	switch cfg.Type {
	case StoreTypeJSON, "": // 默认使用 JSON 存储
		return NewJSONStore(cfg.Workspace)
	case StoreTypeRedis:
		return nil, fmt.Errorf("redis store not implemented yet, use json store instead")
	case StoreTypeMilvus:
		return nil, fmt.Errorf("milvus store not implemented yet, use json store instead")
	default:
		return nil, fmt.Errorf("unsupported store type: %s", cfg.Type)
	}
}

// JSONStore 本地 JSON 文件存储实现
type JSONStore struct {
	index        *IndexStore
	metadata     *Metadata
	indexPath    string
	metadataPath string
	mu           sync.RWMutex
}

// NewJSONStore 创建 JSON 存储实例
func NewJSONStore(workspace string) (*JSONStore, error) {
	baseDir := filepath.Join(workspace, ".kanflux", "knowledge")
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create knowledge directory: %w", err)
	}

	store := &JSONStore{
		indexPath:    filepath.Join(baseDir, "index.json"),
		metadataPath: filepath.Join(baseDir, "metadata.json"),
		index:        NewIndexStore(),
		metadata:     &Metadata{CreatedAt: 0},
	}

	// 尝试加载已有数据
	if err := store.Load(); err != nil {
		// 如果文件不存在，忽略错误
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("failed to load existing index: %w", err)
		}
	}

	return store, nil
}

// Load 加载索引数据
func (s *JSONStore) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 加载索引
	indexData, err := os.ReadFile(s.indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			s.index = NewIndexStore()
			return nil
		}
		return err
	}

	if err := json.Unmarshal(indexData, &s.index); err != nil {
		return fmt.Errorf("failed to unmarshal index: %w", err)
	}

	// 加载元数据
	metadataData, err := os.ReadFile(s.metadataPath)
	if err != nil {
		if os.IsNotExist(err) {
			s.metadata = &Metadata{CreatedAt: 0}
			return nil
		}
		return err
	}

	if err := json.Unmarshal(metadataData, &s.metadata); err != nil {
		return fmt.Errorf("failed to unmarshal metadata: %w", err)
	}

	return nil
}

// Save 保存索引数据
func (s *JSONStore) Save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// 保存索引
	indexData, err := json.MarshalIndent(s.index, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal index: %w", err)
	}

	if err := os.WriteFile(s.indexPath, indexData, 0644); err != nil {
		return fmt.Errorf("failed to write index: %w", err)
	}

	// 更新统计信息
	s.metadata.TotalDocuments = len(s.index.Documents)
	s.metadata.TotalChunks = len(s.index.Chunks)

	// 保存元数据
	metadataData, err := json.MarshalIndent(s.metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	if err := os.WriteFile(s.metadataPath, metadataData, 0644); err != nil {
		return fmt.Errorf("failed to write metadata: %w", err)
	}

	return nil
}

// AddDocument 添加文档
func (s *JSONStore) AddDocument(doc *DocumentInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.index.AddDocument(doc)
}

// RemoveDocument 移除文档
func (s *JSONStore) RemoveDocument(docID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.index.RemoveDocument(docID)
}

// GetDocument 获取文档
func (s *JSONStore) GetDocument(docID string) (*DocumentInfo, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	doc, ok := s.index.Documents[docID]
	return doc, ok
}

// GetDocumentByPath 通过路径获取文档
func (s *JSONStore) GetDocumentByPath(path string) (*DocumentInfo, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	docID, ok := s.index.PathHashes[path]
	if !ok {
		return nil, false
	}
	doc, ok := s.index.Documents[docID]
	return doc, ok
}

// AddChunk 添加分块
func (s *JSONStore) AddChunk(chunk *ChunkInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.index.AddChunk(chunk)
}

// GetChunk 获取分块
func (s *JSONStore) GetChunk(chunkID string) (*ChunkInfo, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	chunk, ok := s.index.Chunks[chunkID]
	return chunk, ok
}

// SetVector 设置向量
func (s *JSONStore) SetVector(chunkID string, vector []float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.index.SetVector(chunkID, vector)
}

// GetVector 获取向量
func (s *JSONStore) GetVector(chunkID string) ([]float64, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	vector, ok := s.index.Vectors[chunkID]
	return vector, ok
}

// GetAllChunks 获取所有分块
func (s *JSONStore) GetAllChunks() []*ChunkInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	chunks := make([]*ChunkInfo, 0, len(s.index.Chunks))
	for _, chunk := range s.index.Chunks {
		chunks = append(chunks, chunk)
	}
	return chunks
}

// GetAllVectors 获取所有向量及其分块ID
func (s *JSONStore) GetAllVectors() map[string][]float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	// 返回副本
	vectors := make(map[string][]float64, len(s.index.Vectors))
	for id, v := range s.index.Vectors {
		vectors[id] = v
	}
	return vectors
}

// GetStats 获取统计信息
func (s *JSONStore) GetStats() *Stats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.index.GetStats(s.indexPath)
}

// SetMetadata 设置元数据
func (s *JSONStore) SetMetadata(metadata *Metadata) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.metadata = metadata
}

// GetMetadata 获取元数据
func (s *JSONStore) GetMetadata() *Metadata {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.metadata
}

// Clear 清空所有数据
func (s *JSONStore) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.index = NewIndexStore()
	s.metadata = &Metadata{CreatedAt: 0}
	return s.Save()
}

// Close 关闭存储
func (s *JSONStore) Close() error {
	return s.Save()
}

// 确保实现接口
var _ VectorStore = (*JSONStore)(nil)