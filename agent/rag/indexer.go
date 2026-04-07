package rag

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/cloudwego/eino/components/embedding"
)

// Indexer 文档索引器
type Indexer struct {
	store    VectorStore
	chunker  *Chunker
	embedder embedding.Embedder
	mu       sync.Mutex
}

// NewIndexer 创建索引器
func NewIndexer(store VectorStore, chunker *Chunker, embedder embedding.Embedder) *Indexer {
	return &Indexer{
		store:    store,
		chunker:  chunker,
		embedder: embedder,
	}
}

// IndexDocument 索引单个文档
func (i *Indexer) IndexDocument(ctx context.Context, doc *DocumentInfo) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	// 检查是否已存在且未修改
	existingDoc, exists := i.store.GetDocumentByPath(doc.SourcePath)
	if exists && existingDoc.ModTime == doc.ModTime {
		return nil
	}

	// 如果文档已存在但已修改，先删除旧的
	if exists {
		i.store.RemoveDocument(existingDoc.ID)
	}

	// 分块
	chunks := i.chunker.ChunkWithDocID(doc.Content, doc.ID)
	if len(chunks) == 0 {
		return nil
	}

	// 设置分块的元数据
	for _, chunk := range chunks {
		chunk.Metadata["source_path"] = doc.SourcePath
		chunk.Metadata["extension"] = doc.Metadata["extension"]
		chunk.Metadata["filename"] = doc.Metadata["filename"]
	}

	// 添加文档到存储（不含 content，节省空间）
	docInfo := &DocumentInfo{
		ID:         doc.ID,
		SourcePath: doc.SourcePath,
		Metadata:   doc.Metadata,
		ModTime:    doc.ModTime,
		ChunkIDs:   make([]string, len(chunks)),
	}
	for idx, chunk := range chunks {
		docInfo.ChunkIDs[idx] = chunk.ID
	}
	i.store.AddDocument(docInfo)

	// 批量生成向量
	chunkContents := make([]string, len(chunks))
	for idx, chunk := range chunks {
		chunkContents[idx] = chunk.Content
		i.store.AddChunk(chunk)
	}

	// 批量 Embedding
	vectors, err := i.batchEmbed(ctx, chunkContents)
	if err != nil {
		return fmt.Errorf("failed to embed document chunks: %w", err)
	}

	// 存储向量
	for idx, vector := range vectors {
		i.store.SetVector(chunks[idx].ID, vector)
	}

	return nil
}

// batchEmbed 批量生成向量（分批调用避免 API 限制）
func (i *Indexer) batchEmbed(ctx context.Context, texts []string) ([][]float64, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	// 分批处理，每批最多 100 个文本（OpenAI embedding API 限制）
	batchSize := 100
	var allVectors [][]float64

	for start := 0; start < len(texts); start += batchSize {
		end := start + batchSize
		if end > len(texts) {
			end = len(texts)
		}

		batch := texts[start:end]

		// Eino Embedding 接口调用
		embeddings, err := i.embedder.EmbedStrings(ctx, batch)
		if err != nil {
			return nil, fmt.Errorf("embedding batch %d failed: %w", start/batchSize+1, err)
		}

		// 检查返回数量是否匹配
		if len(embeddings) != len(batch) {
			return nil, fmt.Errorf("embedding count mismatch in batch %d: expected %d, got %d", start/batchSize+1, len(batch), len(embeddings))
		}

		// 添加到结果中
		for idx, emb := range embeddings {
			if len(emb) == 0 {
				return nil, fmt.Errorf("empty vector returned in batch %d at index %d", start/batchSize+1, idx)
			}
			allVectors = append(allVectors, emb)
		}
	}

	return allVectors, nil
}

// RemoveDocument 移除文档索引
func (i *Indexer) RemoveDocument(docID string) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	i.store.RemoveDocument(docID)
	return nil
}

// IndexDocuments 批量索引文档
func (i *Indexer) IndexDocuments(ctx context.Context, docs []*DocumentInfo) error {
	slog.Info("[RAG] Starting batch index", "doc_count", len(docs))

	var errors []string
	for idx, doc := range docs {
		slog.Debug("[RAG] Indexing document", "progress", fmt.Sprintf("%d/%d", idx+1, len(docs)), "path", doc.SourcePath)
		if err := i.IndexDocument(ctx, doc); err != nil {
			slog.Error("[RAG] Document indexing failed", "path", doc.SourcePath, "error", err)
			errors = append(errors, fmt.Sprintf("%s: %v", doc.SourcePath, err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("some documents failed to index: %s", errors)
	}

	// 保存索引
	if err := i.store.Save(); err != nil {
		return fmt.Errorf("failed to save index: %w", err)
	}

	slog.Info("[RAG] Batch index completed", "docs", len(docs), "chunks", i.store.GetStats().TotalChunks)
	return nil
}

// ReindexAll 重新索引所有文档
func (i *Indexer) ReindexAll(ctx context.Context, docs []*DocumentInfo) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	// 清空现有索引
	i.store.Clear()

	// 重新索引
	for _, doc := range docs {
		chunks := i.chunker.ChunkWithDocID(doc.Content, doc.ID)
		if len(chunks) == 0 {
			continue
		}

		// 设置元数据
		for _, chunk := range chunks {
			chunk.Metadata["source_path"] = doc.SourcePath
		}

		docInfo := &DocumentInfo{
			ID:         doc.ID,
			SourcePath: doc.SourcePath,
			Metadata:   doc.Metadata,
			ModTime:    doc.ModTime,
			ChunkIDs:   make([]string, len(chunks)),
		}
		for idx, chunk := range chunks {
			docInfo.ChunkIDs[idx] = chunk.ID
			i.store.AddChunk(chunk)
		}
		i.store.AddDocument(docInfo)

		// 生成向量
		chunkContents := make([]string, len(chunks))
		for idx, chunk := range chunks {
			chunkContents[idx] = chunk.Content
		}

		vectors, err := i.batchEmbed(ctx, chunkContents)
		if err != nil {
			slog.Error("[RAG] Failed to embed in reindex", "path", doc.SourcePath, "error", err)
			continue
		}

		for idx, vector := range vectors {
			i.store.SetVector(chunks[idx].ID, vector)
		}
	}

	return i.store.Save()
}

// GetStats 获取统计信息
func (i *Indexer) GetStats() *Stats {
	return i.store.GetStats()
}