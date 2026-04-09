package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/kinwyb/kanflux/memoria/types"
)

// SummarizerImpl implements the Summarizer interface using ChatModel
type SummarizerImpl struct {
	Model     types.ChatModel
	MaxTokens int
}

// NewSummarizer creates a new summarizer
func NewSummarizer(model types.ChatModel, maxTokens int) *SummarizerImpl {
	if maxTokens <= 0 {
		maxTokens = 500
	}
	return &SummarizerImpl{
		Model:     model,
		MaxTokens: maxTokens,
	}
}

// Summarize generates a summary from content using hall-specific prompts
func (s *SummarizerImpl) Summarize(ctx context.Context, content string, hallType types.HallType, layer types.Layer) (string, error) {
	hallGuidance := GetHallPrompt(string(hallType))
	layerDesc := getLayerDescription(layer)

	prompt := fmt.Sprintf(`%s

Layer: %s
Target length: ~%d tokens

Content:
%s

Provide a concise summary that captures the key information. Start with the summary directly, no preamble.`,
		hallGuidance, layerDesc, getTokenLimit(layer), content)

	return s.Model.GenerateWithSystem(ctx, SystemPrompt, prompt)
}

// ExtractFacts extracts key facts from content
func (s *SummarizerImpl) ExtractFacts(ctx context.Context, content string, userCtx types.UserIdentity) ([]*types.MemoryItem, error) {
	prompt := buildExtractFactsPrompt(content, userCtx)
	response, err := s.Model.GenerateWithSystem(ctx, SystemPrompt, prompt)
	if err != nil {
		return nil, err
	}

	items := parseFactsResponse(response, userCtx)
	return items, nil
}

// Categorize determines the appropriate hall and layer for content
// First tries keyword-based detection, falls back to LLM if needed
func (s *SummarizerImpl) Categorize(ctx context.Context, content string) (types.HallType, types.Layer, error) {
	// Try keyword-based detection first
	hallType, layer := detectByKeywords(content)

	// If confident, return without LLM call
	if hallType != "" {
		return hallType, layer, nil
	}

	// Fall back to LLM categorization
	hallType, layer, err := s.categorizeWithLLM(ctx, content)
	if err != nil {
		return types.HallEvents, types.LayerL2, err
	}
	return hallType, layer, nil
}

// detectByKeywords uses HallKeywords and LayerKeywords for fast categorization
func detectByKeywords(content string) (types.HallType, types.Layer) {
	contentLower := strings.ToLower(content)

	// Score each hall type
	hallScores := make(map[string]int)
	for hall, keywords := range HallKeywords {
		for _, kw := range keywords {
			if strings.Contains(contentLower, strings.ToLower(kw)) {
				hallScores[hall]++
			}
		}
	}

	// Find best hall type
	var bestHall string
	bestScore := 0
	for hall, score := range hallScores {
		if score > bestScore {
			bestScore = score
			bestHall = hall
		}
	}

	// Score layers
	layerScores := make(map[string]int)
	for layer, keywords := range LayerKeywords {
		for _, kw := range keywords {
			if strings.Contains(contentLower, strings.ToLower(kw)) {
				layerScores[layer]++
			}
		}
	}

	// Find best layer
	var bestLayer string
	bestLayerScore := 0
	for layer, score := range layerScores {
		if score > bestLayerScore {
			bestLayerScore = score
			bestLayer = layer
		}
	}

	// Convert to types
	var hallType types.HallType
	switch bestHall {
	case "hall_facts":
		hallType = types.HallFacts
	case "hall_events":
		hallType = types.HallEvents
	case "hall_discoveries":
		hallType = types.HallDiscoveries
	case "hall_preferences":
		hallType = types.HallPreferences
	case "hall_advice":
		hallType = types.HallAdvice
	default:
		return "", types.LayerL2 // Not confident
	}

	var layer types.Layer
	switch bestLayer {
	case "L1":
		layer = types.LayerL1
	case "L3":
		layer = types.LayerL3
	default:
		// Determine layer based on hall type
		if hallType == types.HallFacts || hallType == types.HallPreferences {
			layer = types.LayerL1
		} else {
			layer = types.LayerL2
		}
	}

	// Only return if we have reasonable confidence
	if bestScore >= 1 {
		return hallType, layer
	}
	return "", types.LayerL2
}

// categorizeWithLLM uses LLM for categorization
func (s *SummarizerImpl) categorizeWithLLM(ctx context.Context, content string) (types.HallType, types.Layer, error) {
	prompt := buildCategorizePrompt(content)
	response, err := s.Model.GenerateWithSystem(ctx, SystemPrompt, prompt)
	if err != nil {
		return types.HallEvents, types.LayerL2, err
	}

	hallType, layer := parseCategorizeResponse(response)
	return hallType, layer, nil
}

// ProcessChatContent processes chat Q&A content using ChatSummarizePrompt
func (s *SummarizerImpl) ProcessChatContent(ctx context.Context, question, answer string, userCtx types.UserIdentity) ([]*types.MemoryItem, error) {
	prompt := ChatSummarizePrompt(question, answer)
	response, err := s.Model.GenerateWithSystem(ctx, SystemPrompt, prompt)
	if err != nil {
		return nil, err
	}

	// Parse response
	result := parseChatResponse(response, userCtx)
	return result, nil
}

// ProcessChatBatchContent processes multiple Q&A pairs in one LLM call
// This reduces API calls when processing chat history
func (s *SummarizerImpl) ProcessChatBatchContent(ctx context.Context, qaPairs []QAPair, userCtx types.UserIdentity) ([]*types.MemoryItem, error) {
	if len(qaPairs) == 0 {
		return nil, nil
	}

	// Single pair: use the single prompt
	if len(qaPairs) == 1 {
		return s.ProcessChatContent(ctx, qaPairs[0].Question, qaPairs[0].Answer, userCtx)
	}

	prompt := ChatBatchSummarizePrompt(qaPairs)
	response, err := s.Model.GenerateWithSystem(ctx, SystemPrompt, prompt)
	if err != nil {
		return nil, err
	}

	// Parse batch response
	return parseChatBatchResponse(response, userCtx), nil
}

// ProcessFileContent processes file content using FileSummarizePrompt
func (s *SummarizerImpl) ProcessFileContent(ctx context.Context, content, filePath string, userCtx types.UserIdentity) ([]*types.MemoryItem, error) {
	prompt := FileSummarizePrompt(content, filePath)
	response, err := s.Model.GenerateWithSystem(ctx, SystemPrompt, prompt)
	if err != nil {
		return nil, err
	}

	// Parse response
	result := parseFileResponse(response, userCtx)
	return result, nil
}

// CompactMemories compacts multiple memory items using CompactPrompt
func (s *SummarizerImpl) CompactMemories(ctx context.Context, items []string, maxTokens int) ([]string, error) {
	prompt := CompactPrompt(items, maxTokens)
	response, err := s.Model.GenerateWithSystem(ctx, SystemPrompt, prompt)
	if err != nil {
		return nil, err
	}

	// Parse compacted memories
	return parseCompactResponse(response), nil
}

// ProcessContent processes generic content
func (s *SummarizerImpl) ProcessContent(ctx context.Context, content string, userCtx types.UserIdentity) ([]*types.MemoryItem, error) {
	hallType, layer, err := s.Categorize(ctx, content)
	if err != nil {
		return nil, fmt.Errorf("categorize failed: %w", err)
	}

	summary, err := s.Summarize(ctx, content, hallType, layer)
	if err != nil {
		return nil, fmt.Errorf("summarize failed: %w", err)
	}

	item := &types.MemoryItem{
		ID:        generateID(),
		HallType:  hallType,
		Layer:     layer,
		Content:   content,
		Summary:   summary,
		Source:    "chat",
		UserID:    userCtx.GetUserID(),
		Timestamp: time.Now(),
		Tokens:    estimateTokens(summary),
	}

	return []*types.MemoryItem{item}, nil
}

// Helper functions

func buildExtractFactsPrompt(content string, userCtx types.UserIdentity) string {
	return fmt.Sprintf(`Extract key facts from the following content for user %s.

Content:
%s

Return a JSON array of facts. Each fact should be an object with:
- "content": the fact text
- "hall_type": one of hall_facts, hall_preferences
- "importance": 1-5 (5 being most important)

Example format:
[
  {"content": "User prefers dark mode", "hall_type": "hall_preferences", "importance": 3},
  {"content": "Project uses PostgreSQL", "hall_type": "hall_facts", "importance": 4}
]

Return only the JSON array, no other text.`, userCtx.GetUserID(), content)
}

func buildCategorizePrompt(content string) string {
	return fmt.Sprintf(`Categorize the following content for memory storage.

Content:
%s

Return a single line with the hall type and layer, separated by comma.
Format: hall_type,layer
Example: hall_events,L2

Valid hall types: hall_facts, hall_events, hall_discoveries, hall_preferences, hall_advice
Valid layers: L1, L2, L3

Return only the categorization, no explanation.`, content)
}

func getLayerDescription(layer types.Layer) string {
	switch layer {
	case types.LayerL1:
		return "L1 (critical, always loaded)"
	case types.LayerL2:
		return "L2 (events, loaded on demand)"
	case types.LayerL3:
		return "L3 (raw, for deep search)"
	default:
		return "L2"
	}
}

func getTokenLimit(layer types.Layer) int {
	switch layer {
	case types.LayerL1:
		return 50
	case types.LayerL2:
		return 200
	case types.LayerL3:
		return 500
	default:
		return 200
	}
}

// Response parsing functions

func parseFactsResponse(response string, userCtx types.UserIdentity) []*types.MemoryItem {
	response = strings.TrimSpace(response)
	response = cleanCodeBlock(response)

	var facts []struct {
		Content    string `json:"content"`
		HallType   string `json:"hall_type"`
		Importance int    `json:"importance"`
	}

	if err := json.Unmarshal([]byte(response), &facts); err != nil {
		return parsePlainTextFacts(response, userCtx)
	}

	items := make([]*types.MemoryItem, len(facts))
	for i, fact := range facts {
		hallType := types.HallFacts
		if fact.HallType == "hall_preferences" {
			hallType = types.HallPreferences
		}

		layer := types.LayerL1
		if fact.Importance < 4 {
			layer = types.LayerL2
		}

		items[i] = &types.MemoryItem{
			ID:        generateID(),
			HallType:  hallType,
			Layer:     layer,
			Content:   fact.Content,
			Summary:   fact.Content,
			Source:    "extracted",
			UserID:    userCtx.GetUserID(),
			Timestamp: time.Now(),
			Tokens:    estimateTokens(fact.Content),
		}
	}

	return items
}

func parsePlainTextFacts(response string, userCtx types.UserIdentity) []*types.MemoryItem {
	lines := strings.Split(response, "\n")
	items := make([]*types.MemoryItem, 0)

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		items = append(items, &types.MemoryItem{
			ID:        generateID(),
			HallType:  types.HallFacts,
			Layer:     types.LayerL1,
			Content:   line,
			Summary:   line,
			Source:    "extracted",
			UserID:    userCtx.GetUserID(),
			Timestamp: time.Now(),
			Tokens:    estimateTokens(line),
		})
	}

	return items
}

func parseCategorizeResponse(response string) (types.HallType, types.Layer) {
	response = strings.TrimSpace(response)
	response = strings.ToLower(response)

	var hallType types.HallType
	if strings.Contains(response, "facts") {
		hallType = types.HallFacts
	} else if strings.Contains(response, "events") {
		hallType = types.HallEvents
	} else if strings.Contains(response, "discoveries") {
		hallType = types.HallDiscoveries
	} else if strings.Contains(response, "preferences") {
		hallType = types.HallPreferences
	} else if strings.Contains(response, "advice") {
		hallType = types.HallAdvice
	} else {
		hallType = types.HallEvents
	}

	var layer types.Layer
	if strings.Contains(response, "l1") || strings.Contains(response, "critical") {
		layer = types.LayerL1
	} else if strings.Contains(response, "l3") || strings.Contains(response, "raw") {
		layer = types.LayerL3
	} else {
		layer = types.LayerL2
	}

	return hallType, layer
}

func parseChatResponse(response string, userCtx types.UserIdentity) []*types.MemoryItem {
	response = strings.TrimSpace(response)
	response = cleanCodeBlock(response)

	var result struct {
		HallType string   `json:"hall_type"`
		Layer    string   `json:"layer"`
		Summary  string   `json:"summary"`
		Keywords []string `json:"keywords"`
	}

	if err := json.Unmarshal([]byte(response), &result); err != nil {
		// Fall back to simple parsing
		return []*types.MemoryItem{{
			ID:        generateID(),
			HallType:  types.HallEvents,
			Layer:     types.LayerL2,
			Content:   response,
			Summary:   response,
			Source:    "chat",
			UserID:    userCtx.GetUserID(),
			Timestamp: time.Now(),
			Tokens:    estimateTokens(response),
		}}
	}

	hallType := types.HallEvents
	switch result.HallType {
	case "hall_facts":
		hallType = types.HallFacts
	case "hall_preferences":
		hallType = types.HallPreferences
	case "hall_discoveries":
		hallType = types.HallDiscoveries
	case "hall_advice":
		hallType = types.HallAdvice
	}

	layer := types.LayerL2
	if result.Layer == "L1" {
		layer = types.LayerL1
	} else if result.Layer == "L3" {
		layer = types.LayerL3
	}

	return []*types.MemoryItem{{
		ID:        generateID(),
		HallType:  hallType,
		Layer:     layer,
		Content:   result.Summary,
		Summary:   result.Summary,
		Source:    "chat",
		UserID:    userCtx.GetUserID(),
		Timestamp: time.Now(),
		Tokens:    estimateTokens(result.Summary),
		Metadata:  map[string]any{"keywords": result.Keywords},
	}}
}

func parseChatBatchResponse(response string, userCtx types.UserIdentity) []*types.MemoryItem {
	response = strings.TrimSpace(response)
	response = cleanCodeBlock(response)

	// Handle empty array case
	if response == "[]" || response == "" {
		return nil
	}

	var items []struct {
		QAIndex  int      `json:"qa_index"`
		HallType string   `json:"hall_type"`
		Layer    string   `json:"layer"`
		Summary  string   `json:"summary"`
		Keywords []string `json:"keywords"`
	}

	if err := json.Unmarshal([]byte(response), &items); err != nil {
		// Fall back to single parsing if batch fails
		return parseChatResponse(response, userCtx)
	}

	result := make([]*types.MemoryItem, 0, len(items))
	for _, item := range items {
		if item.Summary == "" {
			continue
		}

		hallType := types.HallEvents
		switch item.HallType {
		case "hall_facts":
			hallType = types.HallFacts
		case "hall_preferences":
			hallType = types.HallPreferences
		case "hall_discoveries":
			hallType = types.HallDiscoveries
		case "hall_advice":
			hallType = types.HallAdvice
		}

		layer := types.LayerL2
		if item.Layer == "L1" {
			layer = types.LayerL1
		} else if item.Layer == "L3" {
			layer = types.LayerL3
		}

		result = append(result, &types.MemoryItem{
			ID:        generateID(),
			HallType:  hallType,
			Layer:     layer,
			Content:   item.Summary,
			Summary:   item.Summary,
			Source:    "chat",
			UserID:    userCtx.GetUserID(),
			Timestamp: time.Now(),
			Tokens:    estimateTokens(item.Summary),
			Metadata:  map[string]any{"keywords": item.Keywords, "qa_index": item.QAIndex},
		})
	}

	return result
}

func parseFileResponse(response string, userCtx types.UserIdentity) []*types.MemoryItem {
	// Similar to parseChatResponse
	return parseChatResponse(response, userCtx)
}

func parseCompactResponse(response string) []string {
	response = strings.TrimSpace(response)
	response = cleanCodeBlock(response)

	var items []struct {
		Summary  string `json:"summary"`
		HallType string `json:"hall_type"`
	}

	if err := json.Unmarshal([]byte(response), &items); err != nil {
		// Fall back to line parsing
		lines := strings.Split(response, "\n")
		result := make([]string, 0)
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				result = append(result, line)
			}
		}
		return result
	}

	result := make([]string, len(items))
	for i, item := range items {
		result[i] = item.Summary
	}
	return result
}

func cleanCodeBlock(s string) string {
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}

func generateID() string {
	return fmt.Sprintf("mem_%d", time.Now().UnixNano())
}

func estimateTokens(text string) int {
	return len(text) / 4
}
