package rag

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode"

	"github.com/cloudwego/eino/components/embedding"
	"github.com/cloudwego/eino/schema"
)

// Retriever 文档检索器
type Retriever struct {
	store     VectorStore
	embedder  embedding.Embedder
	topK      int
	threshold float64
}

// NewRetriever 创建检索器
func NewRetriever(store VectorStore, embedder embedding.Embedder, topK int, threshold float64) *Retriever {
	if topK <= 0 {
		topK = 5
	}
	if threshold <= 0 || threshold > 1 {
		threshold = 0.5
	}
	return &Retriever{
		store:     store,
		embedder:  embedder,
		topK:      topK,
		threshold: threshold,
	}
}

// Retrieve 检索相关文档
func (r *Retriever) Retrieve(ctx context.Context, query string, opts ...RetrieveOption) ([]*schema.Document, error) {
	cfg := ApplyOptions(opts...)

	// 生成查询向量
	queryVector, err := r.embedder.EmbedStrings(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("failed to embed query: %w", err)
	}
	if len(queryVector) == 0 {
		return nil, fmt.Errorf("empty query embedding")
	}

	// 获取所有向量
	vectors := r.store.GetAllVectors()

	// 计算相似度并排序
	results := r.calculateSimilarities(queryVector[0], vectors, cfg)

	// 获取 TopK
	topK := cfg.TopK
	if topK > len(results) {
		topK = len(results)
	}

	// 构建返回文档
	docs := make([]*schema.Document, 0, topK)
	for i := 0; i < topK; i++ {
		result := results[i]
		if result.Score < cfg.ScoreThreshold {
			break
		}

		chunk, ok := r.store.GetChunk(result.ChunkID)
		if !ok {
			continue
		}

		doc := &schema.Document{
			Content:  chunk.Content,
			MetaData: make(map[string]any),
		}
		// 复制元数据
		for k, v := range chunk.Metadata {
			doc.MetaData[k] = v
		}
		doc.MetaData["chunk_id"] = result.ChunkID
		doc.MetaData["document_id"] = chunk.DocumentID
		doc.MetaData["score"] = result.Score

		docs = append(docs, doc)
	}

	return docs, nil
}

// SearchResult 搜索结果
type SearchResult struct {
	ChunkID string
	Score   float64
}

// calculateSimilarities 计算相似度并排序
func (r *Retriever) calculateSimilarities(queryVector []float64, vectors map[string][]float64, cfg *RetrieveConfig) []SearchResult {
	var results []SearchResult

	for chunkID, vector := range vectors {
		score := cosineSimilarity(queryVector, vector)
		if score < cfg.ScoreThreshold {
			continue
		}

		// 应用源路径过滤
		if cfg.SourceFilter != "" {
			chunk, ok := r.store.GetChunk(chunkID)
			if ok {
				sourcePath, _ := chunk.Metadata["source_path"].(string)
				if !strings.Contains(sourcePath, cfg.SourceFilter) {
					continue
				}
			}
		}

		results = append(results, SearchResult{
			ChunkID: chunkID,
			Score:   score,
		})
	}

	// 按分数降序排序
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	return results
}

// cosineSimilarity 计算余弦相似度
func cosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) {
		return 0
	}

	var dotProduct, normA, normB float64
	for i := range a {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}

// HybridRetrieve 混合检索（向量相似度 + 关键词匹配）
func (r *Retriever) HybridRetrieve(ctx context.Context, query string, opts ...RetrieveOption) ([]*schema.Document, error) {
	// 先进行向量检索
	vectorDocs, err := r.Retrieve(ctx, query, opts...)
	if err != nil {
		return nil, err
	}

	// 提取查询关键词
	keywords := extractKeywords(query)

	// 对向量检索结果进行关键词匹配增强
	for _, doc := range vectorDocs {
		textScore := calculateTextScore(doc.Content, keywords)
		vectorScore, _ := doc.MetaData["score"].(float64)

		// 混合分数：向量分数权重 0.7，关键词分数权重 0.3
		doc.MetaData["hybrid_score"] = 0.7*vectorScore + 0.3*textScore
		doc.MetaData["text_score"] = textScore
	}

	// 按混合分数重新排序
	sort.Slice(vectorDocs, func(i, j int) bool {
		scoreI, _ := vectorDocs[i].MetaData["hybrid_score"].(float64)
		scoreJ, _ := vectorDocs[j].MetaData["hybrid_score"].(float64)
		return scoreI > scoreJ
	})

	return vectorDocs, nil
}

// extractKeywords 提取关键词（简单实现）
func extractKeywords(query string) []string {
	// 简单分词：按空格和标点分割
	words := strings.FieldsFunc(query, func(c rune) bool {
		return !unicode.IsLetter(c) && !unicode.IsNumber(c)
	})

	// 过滤停用词（简单实现）
	stopWords := map[string]bool{
		"的": true, "是": true, "在": true, "了": true,
		"the": true, "a": true, "an": true, "is": true, "are": true,
		"was": true, "were": true, "be": true, "been": true,
	}

	var keywords []string
	for _, word := range words {
		word = strings.ToLower(word)
		if len(word) > 1 && !stopWords[word] {
			keywords = append(keywords, word)
		}
	}

	return keywords
}

// calculateTextScore 计算关键词匹配分数
func calculateTextScore(content string, keywords []string) float64 {
	if len(keywords) == 0 {
		return 0
	}

	contentLower := strings.ToLower(content)
	matches := 0

	for _, keyword := range keywords {
		if strings.Contains(contentLower, strings.ToLower(keyword)) {
			matches++
		}
	}

	return float64(matches) / float64(len(keywords))
}