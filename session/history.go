package session

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/embedding"
	"github.com/cloudwego/eino/schema"
)

// SearchResult 搜索结果
type SearchResult struct {
	Layer   int     `json:"layer"`   // 层级: 1=每日总结, 2=具体问答
	Date    string  `json:"date"`    // 日期
	Content string  `json:"content"` // 内容
	Score   float64 `json:"score"`   // 相关性分数
	Source  string  `json:"source"`  // 来源描述
}

// ConversationHistory 历史对话管理器
// 两层结构：
// - 第一层：每日问答总结 (days/YYYY-MM-DD.md) - 按日聚合，阈值 0.4
// - 第二层：原始 session 向量索引 (index.json) - 精确问答对，阈值 0.5
type ConversationHistory struct {
	baseDir    string
	indexPath  string
	embedder   embedding.Embedder
	sessionDir string // session 文件目录
	mu         sync.RWMutex

	// 第二层：原始问答向量索引
	vectors      map[string][]float64              // chunkID -> vector
	chunkContent map[string]string                 // chunkID -> content
	chunkMeta    map[string]map[string]interface{} // chunkID -> metadata

	// 缓存：第一层的向量
	dayVectors  map[string][]float64 // 日期 -> 向量
	dayContents map[string]string    // 日期 -> 内容（用于检测变化）
}

// NewConversationHistory 创建历史对话管理器
func NewConversationHistory(sessionDir string, embedder embedding.Embedder) (*ConversationHistory, error) {
	baseDir := filepath.Join(sessionDir, ".kanflux", "sessions", "history")
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create history directory: %w", err)
	}

	daysDir := filepath.Join(baseDir, "days")
	if err := os.MkdirAll(daysDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create days directory: %w", err)
	}

	history := &ConversationHistory{
		baseDir:      baseDir,
		indexPath:    filepath.Join(baseDir, "index.json"),
		embedder:     embedder,
		sessionDir:   filepath.Dir(baseDir), // session 文件目录
		vectors:      make(map[string][]float64),
		chunkContent: make(map[string]string),
		chunkMeta:    make(map[string]map[string]interface{}),
		dayVectors:   make(map[string][]float64),
		dayContents:  make(map[string]string),
	}

	// 加载已有索引
	if err := history.loadIndex(); err != nil && !os.IsNotExist(err) {
		slog.Warn("[History] Failed to load index", "error", err)
	}

	return history, nil
}

// InitializeAsync 异步初始化：处理已有的 session 文件生成历史记录
func (h *ConversationHistory) InitializeAsync() {
	if h.sessionDir == "" {
		return
	}

	go func() {
		ctx := context.Background()
		if err := h.Initialize(ctx); err != nil {
			slog.Warn("[History] Failed to initialize", "error", err)
		}
	}()
}

// Initialize 同步初始化：处理已有的 session 文件生成历史记录
func (h *ConversationHistory) Initialize(ctx context.Context) error {
	if h.sessionDir == "" {
		return nil
	}

	slog.Info("[History] Starting initialization, processing existing sessions...")

	// 列出所有 session 文件
	entries, err := os.ReadDir(h.sessionDir)
	if err != nil {
		if os.IsNotExist(err) {
			slog.Info("[History] No existing sessions directory")
			return nil
		}
		return err
	}

	var sessionFiles []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".jsonl") {
			sessionFiles = append(sessionFiles, entry.Name())
		}
	}

	if len(sessionFiles) == 0 {
		slog.Info("[History] No existing sessions to process")
		return nil
	}

	slog.Info("[History] Found existing sessions", "count", len(sessionFiles))

	processed := 0
	for _, filename := range sessionFiles {
		sessionKey := strings.TrimSuffix(filename, ".jsonl")
		session, err := h.loadSession(sessionKey)
		if err != nil {
			slog.Warn("[History] Failed to load session", "key", sessionKey, "error", err)
			continue
		}

		if len(session.Messages) == 0 {
			continue
		}

		if err := h.ProcessSession(ctx, session); err != nil {
			slog.Warn("[History] Failed to process session", "key", sessionKey, "error", err)
			continue
		}
		processed++
	}

	slog.Info("[History] Finished processing existing sessions", "processed", processed, "total", len(sessionFiles))
	return nil
}

// ProcessSession 处理 session，生成历史记录
func (h *ConversationHistory) ProcessSession(ctx context.Context, session *Session) error {
	if len(session.Messages) == 0 {
		return nil
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	slog.Debug("[History] Processing session", "key", session.Key, "messages", len(session.Messages))

	// 1. 提取问答对
	qaPairs := h.extractQAPairs(session.Messages)
	if len(qaPairs) == 0 {
		return nil
	}

	// 2. 获取日期
	date := session.CreatedAt.Format("2006-01-02")

	// 3. 保存每日问答总结（第二层）
	if err := h.appendDaySummary(date, session.Key, qaPairs); err != nil {
		slog.Warn("[History] Failed to save day summary", "error", err)
	}

	// 4. 生成向量并存储到索引（第二层）
	if h.embedder != nil {
		if err := h.indexQAPairs(ctx, date, session.Key, qaPairs); err != nil {
			slog.Warn("[History] Failed to index QA pairs", "error", err)
		}
	}

	// 5. 清除缓存（下次搜索时会重新生成）
	h.clearCache()

	return h.saveIndex()
}

// clearCache 清除向量缓存
func (h *ConversationHistory) clearCache() {
	h.dayVectors = make(map[string][]float64)
	h.dayContents = make(map[string]string)
}

// Search 搜索历史对话（两层检索）
func (h *ConversationHistory) Search(ctx context.Context, query string, topK int) (string, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.embedder == nil {
		return "History search is not available (no embedder).", nil
	}

	// 生成查询向量
	queryVectors, err := h.embedder.EmbedStrings(ctx, []string{query})
	if err != nil {
		return "", fmt.Errorf("failed to embed query: %w", err)
	}
	if len(queryVectors) == 0 {
		return "Failed to generate query vector.", nil
	}
	queryVector := queryVectors[0]

	var allResults []SearchResult

	// 第一层：搜索每日问答总结
	layer1Results := h.searchLayer1(ctx, queryVector)
	allResults = append(allResults, layer1Results...)

	// 第二层：搜索原始问答索引
	layer2Results := h.searchLayer2(queryVector)
	allResults = append(allResults, layer2Results...)

	if len(allResults) == 0 {
		return "No relevant conversation history found.", nil
	}

	// 按分数排序
	sortResultsByScore(allResults)

	// 限制数量
	if len(allResults) > topK {
		allResults = allResults[:topK]
	}

	// 格式化输出
	return h.formatResults(allResults), nil
}

// searchLayer1 搜索第一层：每日问答总结
func (h *ConversationHistory) searchLayer1(ctx context.Context, queryVector []float64) []SearchResult {
	var results []SearchResult

	days, err := h.ListDays()
	if err != nil {
		return results
	}

	// 按日期倒序，优先搜索最近的
	sortDaysDesc(days)

	for _, date := range days {
		dayContent, err := h.GetDaySummary(date)
		if err != nil || dayContent == "" {
			continue
		}

		// 检查缓存
		cachedContent, cached := h.dayContents[date]
		if !cached || cachedContent != dayContent {
			// 生成向量
			vectors, err := h.embedder.EmbedStrings(ctx, []string{dayContent})
			if err != nil || len(vectors) == 0 {
				continue
			}
			h.dayVectors[date] = vectors[0]
			h.dayContents[date] = dayContent
		}

		// 计算相似度
		dayVector, ok := h.dayVectors[date]
		if !ok {
			continue
		}

		score := cosineSimilarity(queryVector, dayVector)
		if score > 0.4 { // 第一层阈值
			results = append(results, SearchResult{
				Layer:   1,
				Date:    date,
				Content: truncateText(dayContent, 800),
				Score:   score,
				Source:  fmt.Sprintf("日期: %s 的对话总结", date),
			})
		}
	}

	return results
}

// searchLayer2 搜索第二层：原始问答索引
func (h *ConversationHistory) searchLayer2(queryVector []float64) []SearchResult {
	var results []SearchResult

	for chunkID, vector := range h.vectors {
		score := cosineSimilarity(queryVector, vector)
		if score > 0.5 { // 第二层阈值，精确匹配
			content, ok := h.chunkContent[chunkID]
			if !ok {
				continue
			}
			meta := h.chunkMeta[chunkID]
			date := ""
			if d, ok := meta["date"].(string); ok {
				date = d
			}

			results = append(results, SearchResult{
				Layer:   2,
				Date:    date,
				Content: content,
				Score:   score,
				Source:  fmt.Sprintf("具体问答 (%s)", date),
			})
		}
	}

	return results
}

// formatResults 格式化搜索结果
func (h *ConversationHistory) formatResults(results []SearchResult) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d relevant records:\n\n", len(results)))

	for i, r := range results {
		layerName := getLayerName(r.Layer)
		sb.WriteString(fmt.Sprintf("**[%d] %s** (Score: %.2f)\n", i+1, layerName, r.Score))
		if r.Date != "" {
			sb.WriteString(fmt.Sprintf("Date: %s\n", r.Date))
		}
		sb.WriteString(fmt.Sprintf("%s\n\n", r.Content))
	}

	return sb.String()
}

// getLayerName 获取层级名称
func getLayerName(layer int) string {
	switch layer {
	case 1:
		return "每日总结"
	case 2:
		return "具体问答"
	default:
		return "未知来源"
	}
}

// sortResultsByScore 按分数排序
func sortResultsByScore(results []SearchResult) {
	n := len(results)
	for i := 0; i < n-1; i++ {
		for j := 0; j < n-i-1; j++ {
			if results[j].Score < results[j+1].Score {
				results[j], results[j+1] = results[j+1], results[j]
			}
		}
	}
}

// ============ 其他辅助方法 ============

// extractQAPairs 提取问答对
func (h *ConversationHistory) extractQAPairs(messages []adk.Message) []QAPair {
	var pairs []QAPair
	var currentQuestion string
	var currentAnswer string

	for i := 0; i < len(messages); i++ {
		msg := messages[i]

		switch msg.Role {
		case schema.User:
			if currentQuestion != "" && currentAnswer != "" {
				pairs = append(pairs, QAPair{
					Question: currentQuestion,
					Answer:   currentAnswer,
				})
			}
			currentQuestion = h.extractText(msg)
			currentAnswer = ""

		case schema.Assistant:
			if len(msg.ToolCalls) == 0 {
				currentAnswer = h.extractText(msg)
			} else {
				for j := i + 1; j < len(messages); j++ {
					if messages[j].Role == schema.Assistant && len(messages[j].ToolCalls) == 0 {
						currentAnswer = h.extractText(messages[j])
						i = j
						break
					}
					if messages[j].Role == schema.User {
						break
					}
				}
			}
		}
	}

	if currentQuestion != "" && currentAnswer != "" {
		pairs = append(pairs, QAPair{
			Question: currentQuestion,
			Answer:   currentAnswer,
		})
	}

	return pairs
}

// QAPair 问答对
type QAPair struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

// extractText 提取消息文本
func (h *ConversationHistory) extractText(msg adk.Message) string {
	if msg == nil {
		return ""
	}
	if msg.Content != "" {
		return msg.Content
	}
	var texts []string
	for _, part := range msg.MultiContent {
		if part.Type == "text" && part.Text != "" {
			texts = append(texts, part.Text)
		}
	}
	return strings.Join(texts, "\n")
}

// appendDaySummary 追加每日问答总结到 md 文件
func (h *ConversationHistory) appendDaySummary(date, sessionKey string, qaPairs []QAPair) error {
	dayFile := filepath.Join(h.baseDir, "days", date+".md")

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Session: %s\n\n", sessionKey))

	for i, qa := range qaPairs {
		q := truncateText(qa.Question, 200)
		a := truncateText(qa.Answer, 500)
		sb.WriteString(fmt.Sprintf("### Q%d\n%s\n\n**A**: %s\n\n", i+1, q, a))
	}
	sb.WriteString("---\n\n")

	f, err := os.OpenFile(dayFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.WriteString(sb.String())
	return err
}

// indexQAPairs 将问答对生成向量索引
func (h *ConversationHistory) indexQAPairs(ctx context.Context, date, sessionKey string, qaPairs []QAPair) error {
	for i, qa := range qaPairs {
		chunkID := fmt.Sprintf("%s_%s_%d", date, sessionKey, i)
		content := fmt.Sprintf("问：%s\n答：%s", qa.Question, qa.Answer)

		vectors, err := h.embedder.EmbedStrings(ctx, []string{content})
		if err != nil {
			continue
		}

		if len(vectors) > 0 {
			h.vectors[chunkID] = vectors[0]
			h.chunkContent[chunkID] = content
			h.chunkMeta[chunkID] = map[string]interface{}{
				"date":        date,
				"session_key": sessionKey,
				"index":       i,
			}
		}
	}

	return nil
}

// GetDaySummary 获取指定日期的问答总结
func (h *ConversationHistory) GetDaySummary(date string) (string, error) {
	dayFile := filepath.Join(h.baseDir, "days", date+".md")
	data, err := os.ReadFile(dayFile)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}

// ListDays 列出所有有记录的日期
func (h *ConversationHistory) ListDays() ([]string, error) {
	daysDir := filepath.Join(h.baseDir, "days")
	entries, err := os.ReadDir(daysDir)
	if err != nil {
		return nil, err
	}

	var days []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
			days = append(days, strings.TrimSuffix(entry.Name(), ".md"))
		}
	}
	return days, nil
}

// GetStats 获取统计信息
func (h *ConversationHistory) GetStats() map[string]interface{} {
	h.mu.RLock()
	defer h.mu.RUnlock()

	days, _ := h.ListDays()

	return map[string]interface{}{
		"indexed_chunks": len(h.vectors),
		"days_with_data": len(days),
	}
}

// loadIndex 加载向量索引
func (h *ConversationHistory) loadIndex() error {
	data, err := os.ReadFile(h.indexPath)
	if err != nil {
		return err
	}

	index := struct {
		Vectors      map[string][]float64              `json:"vectors"`
		ChunkContent map[string]string                 `json:"chunk_content"`
		ChunkMeta    map[string]map[string]interface{} `json:"chunk_meta"`
	}{
		Vectors:      make(map[string][]float64),
		ChunkContent: make(map[string]string),
		ChunkMeta:    make(map[string]map[string]interface{}),
	}

	if err := json.Unmarshal(data, &index); err != nil {
		return err
	}

	h.vectors = index.Vectors
	h.chunkContent = index.ChunkContent
	h.chunkMeta = index.ChunkMeta
	return nil
}

// saveIndex 保存向量索引
func (h *ConversationHistory) saveIndex() error {
	index := struct {
		Vectors      map[string][]float64              `json:"vectors"`
		ChunkContent map[string]string                 `json:"chunk_content"`
		ChunkMeta    map[string]map[string]interface{} `json:"chunk_meta"`
	}{
		Vectors:      h.vectors,
		ChunkContent: h.chunkContent,
		ChunkMeta:    h.chunkMeta,
	}

	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(h.indexPath, data, 0644)
}

// cosineSimilarity 计算余弦相似度
func cosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
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

	return dotProduct / (sqrt(normA) * sqrt(normB))
}

// truncateText 截断文本
func truncateText(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}
	return text[:maxLen] + "..."
}

// sqrt 简单平方根
func sqrt(x float64) float64 {
	if x <= 0 {
		return 0
	}
	z := x
	for i := 0; i < 10; i++ {
		z = z - (z*z-x)/(2*z)
	}
	return z
}

// sortDaysDesc 按日期倒序排列
func sortDaysDesc(days []string) {
	n := len(days)
	for i := 0; i < n-1; i++ {
		for j := 0; j < n-i-1; j++ {
			if days[j] < days[j+1] {
				days[j], days[j+1] = days[j+1], days[j]
			}
		}
	}
}

// loadSession 加载 session 文件
func (h *ConversationHistory) loadSession(key string) (*Session, error) {
	// 将 key 中的特殊字符替换为下划线（与 manager.go sessionPath 保持一致）
	safeKey := strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|' {
			return '_'
		}
		return r
	}, key)

	filePath := filepath.Join(h.sessionDir, safeKey+".jsonl")

	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	session := &Session{
		Key:       key,
		Messages:  []adk.Message{},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Metadata:  make(map[string]interface{}),
	}

	decoder := json.NewDecoder(file)
	for decoder.More() {
		var raw map[string]interface{}
		if err := decoder.Decode(&raw); err != nil {
			return nil, err
		}

		// 检查是否为元数据行
		if msgType, ok := raw["_type"].(string); ok && msgType == "metadata" {
			if createdAt, ok := raw["created_at"].(string); ok {
				session.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
			}
			if updatedAt, ok := raw["updated_at"].(string); ok {
				session.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
			}
			if metadata, ok := raw["metadata"].(map[string]interface{}); ok {
				session.Metadata = metadata
			}
		} else {
			// 消息行
			data, _ := json.Marshal(raw)
			var msg adk.Message
			if err := json.Unmarshal(data, &msg); err != nil {
				return nil, err
			}
			session.Messages = append(session.Messages, msg)
		}
	}

	return session, nil
}