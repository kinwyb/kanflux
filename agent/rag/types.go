package rag

import (
	"fmt"
	"time"
)

// DocumentInfo 文档信息
type DocumentInfo struct {
	ID         string         `json:"id"`          // 文档唯一ID (基于路径哈希)
	SourcePath string         `json:"source_path"` // 源文件路径
	Content    string         `json:"content"`     // 文档内容（可选存储）
	Metadata   map[string]any `json:"metadata"`    // 元数据
	ModTime    int64          `json:"mod_time"`    // 文件修改时间 (Unix timestamp)
	ChunkIDs   []string       `json:"chunk_ids"`   // 分块ID列表
}

// ChunkInfo 分块信息
type ChunkInfo struct {
	ID        string         `json:"id"`         // 分块唯一ID
	DocumentID string        `json:"document_id"`// 所属文档ID
	Content    string        `json:"content"`    // 分块内容
	Metadata   map[string]any `json:"metadata"`   // 元数据
	StartPos   int           `json:"start_pos"`  // 在原文中的起始位置
	EndPos     int           `json:"end_pos"`    // 在原文中的结束位置
}

// IndexStore 索引存储结构
type IndexStore struct {
	Documents  map[string]*DocumentInfo `json:"documents"`  // 文档索引 (docID -> DocumentInfo)
	Chunks     map[string]*ChunkInfo    `json:"chunks"`     // 分块索引 (chunkID -> ChunkInfo)
	Vectors    map[string][]float64     `json:"vectors"`    // 向量存储 (chunkID -> vector)
	PathHashes map[string]string        `json:"path_hashes"`// 路径哈希映射 (sourcePath -> docID)
	Version    int                      `json:"version"`    // 版本号
	UpdatedAt  int64                    `json:"updated_at"` // 最后更新时间
}

// Metadata 索引元数据
type Metadata struct {
	KnowledgePaths []KnowledgePath `json:"knowledge_paths"` // 监控的知识库路径配置
	TotalDocuments int             `json:"total_documents"` // 文档总数
	TotalChunks    int             `json:"total_chunks"`    // 分块总数
	CreatedAt      int64           `json:"created_at"`      // 创建时间
	UpdatedAt      int64           `json:"updated_at"`      // 更新时间
}

// Stats 统计信息
type Stats struct {
	TotalDocuments  int    `json:"total_documents"`
	TotalChunks     int    `json:"total_chunks"`
	TotalVectors    int    `json:"total_vectors"`
	TotalPaths      int    `json:"total_paths"`
	LastUpdateTime  int64  `json:"last_update_time"`
	IndexPath       string `json:"index_path"`
}

// NewIndexStore 创建新的索引存储
func NewIndexStore() *IndexStore {
	return &IndexStore{
		Documents:  make(map[string]*DocumentInfo),
		Chunks:     make(map[string]*ChunkInfo),
		Vectors:    make(map[string][]float64),
		PathHashes: make(map[string]string),
		Version:    1,
		UpdatedAt:  time.Now().Unix(),
	}
}

// AddDocument 添加文档
func (s *IndexStore) AddDocument(doc *DocumentInfo) {
	s.Documents[doc.ID] = doc
	s.PathHashes[doc.SourcePath] = doc.ID
	s.UpdatedAt = time.Now().Unix()
}

// RemoveDocument 移除文档及其分块
func (s *IndexStore) RemoveDocument(docID string) {
	doc, ok := s.Documents[docID]
	if !ok {
		return
	}

	// 移除分块和向量
	for _, chunkID := range doc.ChunkIDs {
		delete(s.Chunks, chunkID)
		delete(s.Vectors, chunkID)
	}

	// 移除文档
	delete(s.Documents, docID)
	delete(s.PathHashes, doc.SourcePath)
	s.UpdatedAt = time.Now().Unix()
}

// AddChunk 添加分块
func (s *IndexStore) AddChunk(chunk *ChunkInfo) {
	s.Chunks[chunk.ID] = chunk
	s.UpdatedAt = time.Now().Unix()
}

// SetVector 设置向量
func (s *IndexStore) SetVector(chunkID string, vector []float64) {
	s.Vectors[chunkID] = vector
	s.UpdatedAt = time.Now().Unix()
}

// GetDocumentByPath 通过路径获取文档ID
func (s *IndexStore) GetDocumentByPath(path string) (string, bool) {
	docID, ok := s.PathHashes[path]
	return docID, ok
}

// GetStats 获取统计信息
func (s *IndexStore) GetStats(indexPath string) *Stats {
	return &Stats{
		TotalDocuments: len(s.Documents),
		TotalChunks:    len(s.Chunks),
		TotalVectors:   len(s.Vectors),
		TotalPaths:     len(s.PathHashes),
		LastUpdateTime: s.UpdatedAt,
		IndexPath:      indexPath,
	}
}

// GenerateDocumentID 根据路径生成文档ID
func GenerateDocumentID(path string) string {
	return fmt.Sprintf("doc_%x", hashString(path))
}

// GenerateChunkID 生成分块ID
func GenerateChunkID(docID string, index int) string {
	return fmt.Sprintf("%s_chunk_%d", docID, index)
}

// hashString 简单字符串哈希
func hashString(s string) uint32 {
	h := uint32(0)
	for _, c := range s {
		h = h*31 + uint32(c)
	}
	return h
}