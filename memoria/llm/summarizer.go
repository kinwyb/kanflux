package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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
		// L1 only for preferences (concise, always loaded)
		// L2 for facts, events, discoveries
		if hallType == types.HallPreferences {
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
// Returns MemoryItems for L1/L2 (summary) and optionally L3 (raw content)
func (s *SummarizerImpl) ProcessChatContent(ctx context.Context, question, answer string, userCtx types.UserIdentity) ([]*types.MemoryItem, error) {
	prompt := ChatSummarizePrompt(question, answer)
	response, err := s.Model.GenerateWithSystem(ctx, SystemPrompt, prompt)
	if err != nil {
		return nil, err
	}

	// Parse response
	result := parseChatResponse(response, userCtx)

	// Add L3 item with raw content if we have L1/L2 items
	if len(result) > 0 {
		rawContent := "Q: " + question + "\n\nA: " + answer
		l3Item := &types.MemoryItem{
			ID:         generateID(),
			HallType:   types.HallEvents,
			Layer:      types.LayerL3,
			SourceType: types.SourceTypeChat,
			Content:    rawContent,
			Summary:    "", // L3 doesn't need summary
			Source:     "chat",
			UserID:     userCtx.GetUserID(),
			Timestamp:  time.Now(),
			Tokens:     estimateTokens(rawContent),
		}
		result = append(result, l3Item)
	}

	return result, nil
}

// ProcessChatBatchContent processes multiple Q&A pairs in one LLM call
// This reduces API calls when processing chat history
// Returns MemoryItems for L1/L2 (summaries) and L3 (raw content)
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
	result := parseChatBatchResponse(response, userCtx)

	// Add L3 items with raw content for each Q&A pair
	for _, qa := range qaPairs {
		rawContent := "Q: " + qa.Question + "\n\nA: " + qa.Answer
		l3Item := &types.MemoryItem{
			ID:         generateID(),
			HallType:   types.HallEvents,
			Layer:      types.LayerL3,
			SourceType: types.SourceTypeChat,
			Content:    rawContent,
			Summary:    "",
			Source:     "chat",
			UserID:     userCtx.GetUserID(),
			Timestamp:  time.Now(),
			Tokens:     estimateTokens(rawContent),
		}
		result = append(result, l3Item)
	}

	return result, nil
}

// ProcessFileContent processes file content using FileSimplePrompt
// Returns MemoryItems for L2 (summary) and optionally L3 (raw content)
// Files are stored without complex HallType classification
func (s *SummarizerImpl) ProcessFileContent(ctx context.Context, content, filePath string, userCtx types.UserIdentity) ([]*types.MemoryItem, error) {
	prompt := FileSimplePrompt(content, filePath)
	response, err := s.Model.GenerateWithSystem(ctx, SystemPrompt, prompt)
	if err != nil {
		return nil, err
	}

	// Parse response
	result := parseSimpleFileResponse(response, userCtx)

	// Set source type and file path
	for _, item := range result {
		item.Source = filePath
		item.SourceType = types.SourceTypeFile
		item.Layer = types.LayerL2 // Files default to L2 for summary
	}

	return result, nil
}

// ProcessFileContentRaw creates an L3 item for raw file content
// This is called alongside ProcessFileContent to store full content for semantic search
func (s *SummarizerImpl) ProcessFileContentRaw(ctx context.Context, content, filePath string, userCtx types.UserIdentity) *types.MemoryItem {
	// Estimate tokens for the raw content
	estTokens := estimateTokens(content)

	return &types.MemoryItem{
		ID:         generateID(),
		HallType:   types.HallDiscoveries, // Files use discoveries (knowledge found)
		Layer:      types.LayerL3,
		SourceType: types.SourceTypeFile,
		Content:    content,
		Summary:    "", // L3 doesn't need summary, content is the raw text
		Source:     filePath,
		UserID:     userCtx.GetUserID(),
		Timestamp:  time.Now(),
		Tokens:     estTokens,
	}
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
		ID:         generateID(),
		HallType:   hallType,
		Layer:      layer,
		SourceType: types.SourceTypeChat,
		Content:    content,
		Summary:    summary,
		Source:     "chat",
		UserID:     userCtx.GetUserID(),
		Timestamp:  time.Now(),
		Tokens:     estimateTokens(summary),
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
			ID:         generateID(),
			HallType:   hallType,
			Layer:      layer,
			SourceType: types.SourceTypeChat,
			Content:    fact.Content,
			Summary:    fact.Content,
			Source:     "extracted",
			UserID:     userCtx.GetUserID(),
			Timestamp:  time.Now(),
			Tokens:     estimateTokens(fact.Content),
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
			ID:         generateID(),
			HallType:   types.HallFacts,
			Layer:      types.LayerL1,
			SourceType: types.SourceTypeChat,
			Content:    line,
			Summary:    line,
			Source:     "extracted",
			UserID:     userCtx.GetUserID(),
			Timestamp:  time.Now(),
			Tokens:     estimateTokens(line),
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

	// Check if response is a JSON array - redirect to batch parser
	if strings.HasPrefix(response, "[") {
		return parseChatBatchResponse(response, userCtx)
	}

	var result struct {
		HallType string   `json:"hall_type"`
		Layer    string   `json:"layer"`
		Summary  string   `json:"summary"`
		Keywords []string `json:"keywords"`
	}

	if err := json.Unmarshal([]byte(response), &result); err != nil {
		// JSON parsing failed, try to extract useful content
		// Don't store raw JSON as summary - it's not useful for search
		slog.Warn("Failed to parse chat response as JSON", "response_preview", truncateString(response, 100))
		return nil
	}

	if result.Summary == "" {
		return nil
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

	// Determine layer based on hall type
	// L1 only for preferences, L2 for facts/events/discoveries/advice
	layer := types.LayerL2
	if hallType == types.HallPreferences {
		layer = types.LayerL1
	}

	// Only L3 needs Content, L1/L2 only need Summary
	content := ""
	if layer == types.LayerL3 {
		content = result.Summary
	}

	return []*types.MemoryItem{{
		ID:         generateID(),
		HallType:   hallType,
		Layer:      layer,
		SourceType: types.SourceTypeChat,
		Content:    content,
		Summary:    result.Summary,
		Source:     "chat",
		UserID:     userCtx.GetUserID(),
		Timestamp:  time.Now(),
		Tokens:     estimateTokens(result.Summary),
		Metadata:   map[string]any{"keywords": result.Keywords},
	}}
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func parseChatBatchResponse(response string, userCtx types.UserIdentity) []*types.MemoryItem {
	response = strings.TrimSpace(response)
	response = cleanCodeBlock(response)

	// Handle empty array case
	if response == "[]" || response == "" {
		return nil
	}

	// Try to parse as array first
	var rawItems []map[string]interface{}
	if err := json.Unmarshal([]byte(response), &rawItems); err != nil {
		// Not an array, try single object
		return parseChatResponse(response, userCtx)
	}

	result := make([]*types.MemoryItem, 0, len(rawItems))
	for _, rawItem := range rawItems {
		// Extract fields from map
		hallTypeStr, _ := rawItem["hall_type"].(string)
		summary, _ := rawItem["summary"].(string)

		if summary == "" {
			continue
		}

		// Extract keywords
		var keywords []string
		if kw, ok := rawItem["keywords"].([]interface{}); ok {
			for _, k := range kw {
				if ks, ok := k.(string); ok {
					keywords = append(keywords, ks)
				}
			}
		}

		// Extract qa_index
		qaIndex := 0
		if qi, ok := rawItem["qa_index"].(float64); ok {
			qaIndex = int(qi)
		}

		hallType := types.HallEvents
		switch hallTypeStr {
		case "hall_facts":
			hallType = types.HallFacts
		case "hall_preferences":
			hallType = types.HallPreferences
		case "hall_discoveries":
			hallType = types.HallDiscoveries
		case "hall_advice":
			hallType = types.HallAdvice
		}

		// Determine layer based on hall type
		// L1 only for preferences, L2 for facts/events/discoveries/advice
		layer := types.LayerL2
		if hallType == types.HallPreferences {
			layer = types.LayerL1
		}

		result = append(result, &types.MemoryItem{
			ID:         generateID(),
			HallType:   hallType,
			Layer:      layer,
			SourceType: types.SourceTypeChat,
			Content:    "", // Only L3 needs Content
			Summary:    summary,
			Source:     "chat",
			UserID:     userCtx.GetUserID(),
			Timestamp:  time.Now(),
			Tokens:     estimateTokens(summary),
			Metadata:   map[string]any{"keywords": keywords, "qa_index": qaIndex},
		})
	}

	return result
}

func parseFileResponse(response string, userCtx types.UserIdentity) []*types.MemoryItem {
	// Similar to parseChatResponse
	return parseChatResponse(response, userCtx)
}

// parseSimpleFileResponse parses the simplified file response (no HallType)
// Files are stored as hall_discoveries in L2 (knowledge discovered from files)
func parseSimpleFileResponse(response string, userCtx types.UserIdentity) []*types.MemoryItem {
	response = strings.TrimSpace(response)
	response = cleanCodeBlock(response)

	var result struct {
		Summary  string   `json:"summary"`
		Keywords []string `json:"keywords"`
		Category string   `json:"category"`
	}

	if err := json.Unmarshal([]byte(response), &result); err != nil {
		// Fall back to simple parsing (use hall_discoveries for files)
		return []*types.MemoryItem{{
			ID:         generateID(),
			HallType:   types.HallDiscoveries, // Files use discoveries for L2
			Layer:      types.LayerL2,
			SourceType: types.SourceTypeFile,
			Content:    "",
			Summary:    response,
			Source:     "file",
			UserID:     userCtx.GetUserID(),
			Timestamp:  time.Now(),
			Tokens:     estimateTokens(response),
		}}
	}

	if result.Summary == "" {
		return nil
	}

	metadata := map[string]any{}
	if len(result.Keywords) > 0 {
		metadata["keywords"] = result.Keywords
	}
	if result.Category != "" {
		metadata["category"] = result.Category
	}

	return []*types.MemoryItem{{
		ID:         generateID(),
		HallType:   types.HallDiscoveries, // Files use discoveries for L2
		Layer:      types.LayerL2,
		SourceType: types.SourceTypeFile,
		Content:    "",
		Summary:    result.Summary,
		Source:     "file",
		UserID:     userCtx.GetUserID(),
		Timestamp:  time.Now(),
		Tokens:     estimateTokens(result.Summary),
		Metadata:   metadata,
	}}
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
