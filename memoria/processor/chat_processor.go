package processor

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

	"github.com/kinwyb/kanflux/memoria/llm"
	"github.com/kinwyb/kanflux/memoria/types"
)

// ChatProcessor processes chat history for memory extraction
type ChatProcessor struct {
	*BaseProcessor
	sessionDir string
}

// NewChatProcessor creates a chat history processor
func NewChatProcessor(summarizer types.Summarizer, config *ProcessorConfig, sessionDir string) *ChatProcessor {
	return &ChatProcessor{
		BaseProcessor: NewBaseProcessor(summarizer, config),
		sessionDir:    sessionDir,
	}
}

// Name returns the processor name
func (p *ChatProcessor) Name() string {
	return "chat_processor"
}

// Process processes a single chat source
func (p *ChatProcessor) Process(ctx context.Context, source string, content string, userCtx types.UserIdentity) (*types.ProcessingResult, error) {
	result := &types.ProcessingResult{
		Items:       make([]*types.MemoryItem, 0),
		LayerCounts: make(map[types.Layer]int),
		HallCounts:  make(map[types.HallType]int),
	}

	messages := p.parseJSONL(content)
	if len(messages) == 0 {
		return result, nil
	}

	qaPairs := p.extractQAPairs(messages)

	// Process QA pairs in batches to reduce LLM calls
	batchSize := p.Config.MaxBatchSize
	if batchSize <= 0 {
		batchSize = 5 // Default: 5 QA pairs per LLM call
	}

	for i := 0; i < len(qaPairs); i += batchSize {
		end := i + batchSize
		if end > len(qaPairs) {
			end = len(qaPairs)
		}

		batch := qaPairs[i:end]
		items, err := p.processQABatch(ctx, batch, source, userCtx)
		if err != nil {
			result.Errors = append(result.Errors, err)
			continue
		}

		for _, item := range items {
			result.Items = append(result.Items, item)
			result.LayerCounts[item.Layer]++
			result.HallCounts[item.HallType]++
		}
	}

	return result, nil
}

// ProcessBatch processes multiple session files
func (p *ChatProcessor) ProcessBatch(ctx context.Context, items []types.ProcessItem) (*types.ProcessingResult, error) {
	result := &types.ProcessingResult{
		Items:       make([]*types.MemoryItem, 0),
		LayerCounts: make(map[types.Layer]int),
		HallCounts:  make(map[types.HallType]int),
	}

	if p.Config.EnableParallel {
		return p.processBatchParallel(ctx, items)
	}

	for _, item := range items {
		r, err := p.Process(ctx, item.Source, item.Content, item.UserCtx)
		if err != nil {
			result.Errors = append(result.Errors, err)
			continue
		}
		p.mergeResults(result, r)
	}

	return result, nil
}

func (p *ChatProcessor) processBatchParallel(ctx context.Context, items []types.ProcessItem) (*types.ProcessingResult, error) {
	result := &types.ProcessingResult{
		Items:       make([]*types.MemoryItem, 0),
		LayerCounts: make(map[types.Layer]int),
		HallCounts:  make(map[types.HallType]int),
	}

	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, item := range items {
		wg.Add(1)
		go func(item types.ProcessItem) {
			defer wg.Done()
			r, err := p.Process(ctx, item.Source, item.Content, item.UserCtx)
			if err != nil {
				mu.Lock()
				result.Errors = append(result.Errors, err)
				mu.Unlock()
				return
			}
			mu.Lock()
			p.mergeResults(result, r)
			mu.Unlock()
		}(item)
	}

	wg.Wait()
	return result, nil
}

// ScanSessions scans the session directory for new/modified files
func (p *ChatProcessor) ScanSessions(ctx context.Context, since time.Time) ([]types.ProcessItem, error) {
	files, err := filepath.Glob(p.sessionDir + "/*.jsonl")
	if err != nil {
		return nil, fmt.Errorf("failed to scan session directory: %w", err)
	}

	items := make([]types.ProcessItem, 0)
	for _, file := range files {
		info, err := os.Stat(file)
		if err != nil {
			continue
		}

		if info.ModTime().Before(since) {
			continue
		}

		content, err := os.ReadFile(file)
		if err != nil {
			slog.Warn("Failed to read session file", "file", file, "error", err)
			continue
		}

		sessionKey := filepath.Base(file)
		sessionKey = strings.TrimSuffix(sessionKey, ".jsonl")
		userCtx := parseSessionKey(sessionKey)

		items = append(items, types.ProcessItem{
			Source:    file,
			Content:   string(content),
			UserCtx:   userCtx,
			Timestamp: info.ModTime(),
		})
	}

	return items, nil
}

func parseSessionKey(sessionKey string) *types.DefaultUserIdentity {
	parts := strings.Split(sessionKey, ":")
	if len(parts) < 3 {
		return &types.DefaultUserIdentity{UserID: sessionKey}
	}
	return &types.DefaultUserIdentity{
		UserID:    parts[0] + ":" + parts[1] + ":" + parts[2],
		Channel:   parts[0],
		AccountID: parts[1],
		ChatID:    parts[2],
	}
}

func (p *ChatProcessor) parseJSONL(content string) []ChatMessage {
	lines := strings.Split(content, "\n")
	messages := make([]ChatMessage, 0)

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "{\"_type\":\"metadata\"") {
			continue
		}

		var msg ChatMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}

		if msg.Role != "" && msg.Content != "" {
			messages = append(messages, msg)
		}
	}

	return messages
}

// ChatMessage represents a chat message from JSONL
type ChatMessage struct {
	Role         string         `json:"role"`
	Content      string         `json:"content"`
	MultiContent []ContentPart  `json:"multi_content,omitempty"`
	ToolCalls    []ToolCall     `json:"tool_calls,omitempty"`
	Name         string         `json:"name,omitempty"`
	Timestamp    string         `json:"timestamp,omitempty"`
}

// ContentPart represents a part of multi-content message
type ContentPart struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// ToolCall represents a tool call in a message
type ToolCall struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// QAPair represents a question-answer pair
type QAPair struct {
	Question string
	Answer   string
	Time     time.Time
}

// extractQAPairs extracts question-answer pairs from messages
// It skips tool messages and finds the final assistant response without tool calls
func (p *ChatProcessor) extractQAPairs(messages []ChatMessage) []QAPair {
	var pairs []QAPair
	var currentQuestion string
	var questionTime time.Time

	for i := 0; i < len(messages); i++ {
		msg := messages[i]

		switch msg.Role {
		case "user":
			// User message: save previous QA pair, start new question
			if currentQuestion != "" {
				// Find the last assistant message without tool calls as answer
				answer := p.findFinalAnswer(messages, i-1)
				if answer != "" {
					pairs = append(pairs, QAPair{
						Question: currentQuestion,
						Answer:   answer,
						Time:     questionTime,
					})
				}
			}
			currentQuestion = p.extractMessageText(msg)
			if msg.Timestamp != "" {
				if t, err := time.Parse(time.RFC3339, msg.Timestamp); err == nil {
					questionTime = t
				}
			} else {
				questionTime = time.Now()
			}

		case "assistant":
			// Assistant message: continue processing, answer is determined at next user message or end
			// Skip here, answer will be found by findFinalAnswer

		case "tool":
			// Tool message: skip
		}
	}

	// Handle the last QA pair
	if currentQuestion != "" {
		answer := p.findFinalAnswer(messages, len(messages)-1)
		if answer != "" {
			pairs = append(pairs, QAPair{
				Question: currentQuestion,
				Answer:   answer,
				Time:     questionTime,
			})
		}
	}

	return pairs
}

// findFinalAnswer finds the last assistant message without tool calls from startPos backwards
func (p *ChatProcessor) findFinalAnswer(messages []ChatMessage, startPos int) string {
	for i := startPos; i >= 0; i-- {
		msg := messages[i]
		if msg.Role == "assistant" && len(msg.ToolCalls) == 0 {
			text := p.extractMessageText(msg)
			if text != "" {
				return text
			}
		}
		// Stop at user message to avoid crossing to previous QA
		if msg.Role == "user" {
			break
		}
	}
	return ""
}

// extractMessageText extracts text content from a message
func (p *ChatProcessor) extractMessageText(msg ChatMessage) string {
	if msg.Content != "" {
		return msg.Content
	}
	if len(msg.MultiContent) > 0 {
		var texts []string
		for _, part := range msg.MultiContent {
			if part.Type == "text" && part.Text != "" {
				texts = append(texts, part.Text)
			}
		}
		return strings.Join(texts, "\n")
	}
	return ""
}

// processQA uses ChatSummarizePrompt for processing single QA pair
func (p *ChatProcessor) processQA(ctx context.Context, qa QAPair, source string, userCtx types.UserIdentity) ([]*types.MemoryItem, error) {
	// Use the specialized ProcessChatContent which uses ChatSummarizePrompt
	summarizer := llm.NewSummarizer(p.Summarizer.(*llm.SummarizerImpl).Model, p.Config.MaxBatchSize)
	items, err := summarizer.ProcessChatContent(ctx, qa.Question, qa.Answer, userCtx)
	if err != nil {
		// Fallback to generic ProcessContent
		content := qa.Question + "\n\n" + qa.Answer
		items, err = summarizer.ProcessContent(ctx, content, userCtx)
		if err != nil {
			return nil, err
		}
	}

	for _, item := range items {
		item.Source = source
		item.Timestamp = qa.Time
	}

	return items, nil
}

// processQABatch processes multiple QA pairs in one LLM call
func (p *ChatProcessor) processQABatch(ctx context.Context, qaPairs []QAPair, source string, userCtx types.UserIdentity) ([]*types.MemoryItem, error) {
	summarizer := llm.NewSummarizer(p.Summarizer.(*llm.SummarizerImpl).Model, p.Config.MaxBatchSize)

	// Convert local QAPair to llm.QAPair
	llmPairs := make([]llm.QAPair, len(qaPairs))
	for i, qa := range qaPairs {
		llmPairs[i] = llm.QAPair{
			Question: qa.Question,
			Answer:   qa.Answer,
		}
	}

	items, err := summarizer.ProcessChatBatchContent(ctx, llmPairs, userCtx)
	if err != nil {
		// Fallback: process individually
		result := make([]*types.MemoryItem, 0)
		for _, qa := range qaPairs {
			singleItems, singleErr := p.processQA(ctx, qa, source, userCtx)
			if singleErr != nil {
				continue
			}
			result = append(result, singleItems...)
		}
		return result, nil
	}

	// Set source and use first QA pair's timestamp as approximation
	for _, item := range items {
		item.Source = source
		if len(qaPairs) > 0 {
			item.Timestamp = qaPairs[0].Time
		}
	}

	return items, nil
}

func (p *ChatProcessor) mergeResults(dst, src *types.ProcessingResult) {
	dst.Items = append(dst.Items, src.Items...)
	for layer, count := range src.LayerCounts {
		dst.LayerCounts[layer] += count
	}
	for hall, count := range src.HallCounts {
		dst.HallCounts[hall] += count
	}
	dst.Errors = append(dst.Errors, src.Errors...)
}
