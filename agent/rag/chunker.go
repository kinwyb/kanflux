package rag

import (
	"strings"
	"unicode"
)

// Chunker 文档分块器
type Chunker struct {
	chunkSize    int
	chunkOverlap int
}

// NewChunker 创建分块器
func NewChunker(chunkSize, chunkOverlap int) *Chunker {
	if chunkSize <= 0 {
		chunkSize = 500
	}
	if chunkOverlap < 0 {
		chunkOverlap = 0
	}
	if chunkOverlap >= chunkSize {
		chunkOverlap = chunkSize / 10
	}
	return &Chunker{
		chunkSize:    chunkSize,
		chunkOverlap: chunkOverlap,
	}
}

// Chunk 分块文档
func (c *Chunker) Chunk(content string) []*ChunkInfo {
	if content == "" {
		return nil
	}

	var chunks []*ChunkInfo
	start := 0
	chunkIndex := 0
	docID := "" // 会在调用时设置

	for start < len(content) {
		end := start + c.chunkSize
		if end > len(content) {
			end = len(content)
		}

		// 尝试在句子边界处切分
		if end < len(content) {
			boundary := findBoundary(content, end, min(50, c.chunkSize/10))
			if boundary > start {
				end = boundary
			}
		}

		chunkContent := strings.TrimSpace(content[start:end])
		if chunkContent != "" {
			chunk := &ChunkInfo{
				ID:        GenerateChunkID(docID, chunkIndex),
				Content:   chunkContent,
				StartPos:  start,
				EndPos:    end,
				Metadata:  make(map[string]any),
			}
			chunks = append(chunks, chunk)
			chunkIndex++
		}

		// 下一块的起始位置（考虑重叠）
		nextStart := end - c.chunkOverlap
		if nextStart >= end {
			nextStart = end
		}
		// 跳过空白区域
		for nextStart < len(content) && unicode.IsSpace(rune(content[nextStart])) {
			nextStart++
		}
		if nextStart <= start {
			nextStart = end
		}
		start = nextStart
	}

	return chunks
}

// ChunkWithDocID 分块文档并设置文档ID
func (c *Chunker) ChunkWithDocID(content, docID string) []*ChunkInfo {
	chunks := c.Chunk(content)
	for i, chunk := range chunks {
		chunk.ID = GenerateChunkID(docID, i)
		chunk.DocumentID = docID
	}
	return chunks
}

// findBoundary 在指定位置附近查找合适的切分边界
func findBoundary(content string, pos, lookback int) int {
	start := pos - lookback
	if start < 0 {
		start = 0
	}

	// 从 pos 向前查找边界字符
	// 优先级：换行符 > 句号/问号/感叹号 > 分号/冒号 > 其他标点
	boundaryChars := []struct {
		char   rune
		priority int
	}{
		{'\n', 5},
		{'。', 4}, {'？', 4}, {'！', 4}, {'.', 4}, {'?', 4}, {'!', 4},
		{'；', 3}, {'；', 3}, {';', 3}, {':', 3},
		{'，', 2}, {'、', 2}, {',', 2},
	}

	bestBoundary := -1
	bestPriority := 0

	for i := pos; i >= start; i-- {
		char := rune(content[i])
		for _, bc := range boundaryChars {
			if char == bc.char && bc.priority > bestPriority {
				bestBoundary = i + 1 // 切分点在边界字符之后
				bestPriority = bc.priority
				if bestPriority >= 4 {
					return bestBoundary
				}
			}
		}
	}

	// 如果没找到优先边界，查找空格
	for i := pos; i >= start; i-- {
		if unicode.IsSpace(rune(content[i])) {
			return i + 1
		}
	}

	return bestBoundary
}

// min 返回较小值
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}