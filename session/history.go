package session

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/embedding"
	"github.com/cloudwego/eino/schema"
	"github.com/kinwyb/kanflux/knowledgebase"
)

// SearchResult 搜索结果
type SearchResult struct {
	Date    string  `json:"date"`    // 日期
	Content string  `json:"content"` // 内容
	Score   float64 `json:"score"`   // 相关性分数
	Source  string  `json:"source"`  // 来源描述
}

// HistoryConfig 历史记录配置
type HistoryConfig struct {
	SessionDir string            `json:"session_dir"` // session 文件目录
	StoreType  string            `json:"store_type"`  // 存储类型: "sqlite" (默认) 或 "json"
	Embedder   embedding.Embedder `json:"-"`           // Embedder 实例
}

// ConversationHistory 历史对话管理器
type ConversationHistory struct {
	kb         *knowledgebase.KnowledgeBase
	embedder   embedding.Embedder
	sessionDir string
}

// NewConversationHistory 创建历史对话管理器
func NewConversationHistory(sessionDir string, embedder embedding.Embedder) (*ConversationHistory, error) {
	return NewConversationHistoryWithConfig(&HistoryConfig{
		SessionDir: sessionDir,
		Embedder:   embedder,
	})
}

// NewConversationHistoryWithConfig 使用配置创建历史对话管理器
func NewConversationHistoryWithConfig(cfg *HistoryConfig) (*ConversationHistory, error) {
	if cfg.SessionDir == "" {
		return nil, fmt.Errorf("session directory is required")
	}

	// 知识库存储在 session 目录的父目录下的 history 子目录
	// 例如：sessions 在 <workspace>/.kanflux/sessions，知识库在 <workspace>/.kanflux/history
	workspace := filepath.Join(filepath.Dir(cfg.SessionDir), "history")
	if err := os.MkdirAll(workspace, 0755); err != nil {
		return nil, fmt.Errorf("failed to create history directory: %w", err)
	}

	// 创建 KnowledgeBase
	kbCfg := knowledgebase.DefaultConfig()
	kbCfg.Workspace = workspace
	kbCfg.StoreType = cfg.StoreType

	if cfg.Embedder != nil {
		kbCfg.Embedder = knowledgebase.NewEinoEmbedder(cfg.Embedder, "text-embedding")
	}

	kb, err := knowledgebase.New(kbCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create knowledge base: %w", err)
	}

	history := &ConversationHistory{
		kb:         kb,
		embedder:   cfg.Embedder,
		sessionDir: cfg.SessionDir,
	}

	return history, nil
}

// SetEmbedder 设置 embedder（用于延迟初始化）
func (h *ConversationHistory) SetEmbedder(embedder embedding.Embedder) {
	h.embedder = embedder
}

// InitializeAsync 异步初始化
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

// Initialize 同步初始化
func (h *ConversationHistory) Initialize(ctx context.Context) error {
	if h.sessionDir == "" {
		return nil
	}

	slog.Info("[History] Starting initialization, processing existing sessions...")

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

	slog.Debug("[History] Processing session", "key", session.Key, "messages", len(session.Messages))

	qaPairs := h.extractQAPairs(session.Messages)
	if len(qaPairs) == 0 {
		return nil
	}

	date := session.CreatedAt.Format("2006-01-02")
	content := h.formatQAPairs(qaPairs)

	_, err := h.kb.Add(ctx, content,
		knowledgebase.WithWing("history"),
		knowledgebase.WithRoom(date),
		knowledgebase.WithSource(session.Key),
		knowledgebase.WithMetadata(map[string]any{
			"session_key": session.Key,
			"date":        date,
			"type":        "qa_pairs",
		}),
	)

	if err != nil {
		slog.Warn("[History] Failed to store in knowledge base", "error", err)
		return err
	}

	slog.Debug("[History] Session processed", "key", session.Key, "date", date)
	return nil
}

// Search 搜索历史对话
func (h *ConversationHistory) Search(ctx context.Context, query string, topK int) (string, error) {
	results, err := h.kb.Search(ctx, query,
		knowledgebase.WithWingFilter("history"),
		knowledgebase.WithLimit(topK),
	)
	if err != nil {
		return "", err
	}

	if len(results) == 0 {
		return "No relevant conversation history found.", nil
	}

	return h.formatResults(results), nil
}

// GetStats 获取统计信息
func (h *ConversationHistory) GetStats() map[string]interface{} {
	stats, err := h.kb.Stats(context.Background())
	if err != nil {
		return map[string]interface{}{"error": err.Error()}
	}

	return map[string]interface{}{
		"total_documents": stats.TotalDocuments,
		"total_wings":     stats.TotalWings,
		"total_rooms":     stats.TotalRooms,
		"storage_size":    stats.StorageSize,
		"store_type":      stats.StoreType,
	}
}

// Close 关闭历史管理器
func (h *ConversationHistory) Close() error {
	return h.kb.Close()
}

// ============ 辅助方法 ============

func (h *ConversationHistory) formatQAPairs(qaPairs []QAPair) string {
	var sb strings.Builder

	for i, qa := range qaPairs {
		q := truncateText(qa.Question, 200)
		a := truncateText(qa.Answer, 500)
		sb.WriteString(fmt.Sprintf("### Q%d\n%s\n\n**A**: %s\n\n", i+1, q, a))
	}

	return sb.String()
}

func (h *ConversationHistory) formatResults(results []*knowledgebase.SearchResult) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d relevant records:\n\n", len(results)))

	for i, r := range results {
		sb.WriteString(fmt.Sprintf("**[%d]** (Score: %.2f)\n", i+1, r.Score))
		if r.Room != "" {
			sb.WriteString(fmt.Sprintf("Date: %s\n", r.Room))
		}
		content := truncateText(r.Content, 800)
		sb.WriteString(fmt.Sprintf("%s\n\n", content))
	}

	return sb.String()
}

func (h *ConversationHistory) extractQAPairs(messages []adk.Message) []QAPair {
	var pairs []QAPair
	var currentQuestion string

	for i := 0; i < len(messages); i++ {
		msg := messages[i]

		switch msg.Role {
		case schema.User:
			// 用户消息：保存之前的问答对，开始新的问题
			if currentQuestion != "" {
				// 向前找最后一个无 ToolCalls 的 Assistant 作为答案
				answer := h.findFinalAnswer(messages, i-1)
				if answer != "" {
					pairs = append(pairs, QAPair{
						Question: currentQuestion,
						Answer:   answer,
					})
				}
			}
			currentQuestion = h.extractText(msg)

		case schema.Assistant:
			// Assistant 消息：继续处理，不在这里设置答案
			// 答案在下一个 User 消息或结束时确定

		case schema.Tool:
			// Tool 消息：跳过
		}
	}

	// 处理最后一个问答对
	if currentQuestion != "" {
		answer := h.findFinalAnswer(messages, len(messages)-1)
		if answer != "" {
			pairs = append(pairs, QAPair{
				Question: currentQuestion,
				Answer:   answer,
			})
		}
	}

	return pairs
}

// findFinalAnswer 从指定位置向前找最后一个无 ToolCalls 的 Assistant 消息
func (h *ConversationHistory) findFinalAnswer(messages []adk.Message, startPos int) string {
	for i := startPos; i >= 0; i-- {
		msg := messages[i]
		if msg.Role == schema.Assistant && len(msg.ToolCalls) == 0 {
			text := h.extractText(msg)
			if text != "" {
				return text
			}
		}
		// 遇到 User 消息就停止，避免跨越到前一个问答
		if msg.Role == schema.User {
			break
		}
	}
	return ""
}

// QAPair 问答对
type QAPair struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

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

func truncateText(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}
	return text[:maxLen] + "..."
}

func (h *ConversationHistory) loadSession(key string) (*Session, error) {
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