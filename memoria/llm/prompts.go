package llm

import (
	"fmt"
	"strings"
)

// Prompt templates for memory extraction

const (
	// SystemPrompt is the base system prompt for memory extraction
	SystemPrompt = `You are a memory extraction assistant. Your job is to analyze content and extract useful information that should be stored for long-term memory.

You categorize content into one of 5 hall types:
- hall_facts: Important decisions, locked choices, key facts that should be remembered
- hall_events: Sessions, milestones, debugging process, notable activities
- hall_discoveries: Breakthroughs, new insights, learned information
- hall_preferences: Habits, preferences, opinions, user tendencies (ALWAYS assign to L1)
- hall_advice: Recommendations, solutions, helpful advice given

Layer assignment rules:
- L1: ONLY for user preferences (hall_preferences). These are concise, always loaded, ~120 tokens total.
- L2: For facts (hall_facts), events (hall_events), and discoveries (hall_discoveries). ~500 tokens per entry.
- L3: Raw content for deep semantic search.

IMPORTANT:
- Preferences (hall_preferences) must ALWAYS go to L1
- Facts (hall_facts) must ALWAYS go to L2, never L1

Respond concisely and in the requested format.`
)

// Hall type keywords for automatic detection
var HallKeywords = map[string][]string{
	"hall_facts": {
		"decided", "decision", "chose", "choice", "locked", "final",
		"confirmed", "approved", "agreed", "settled", "because",
		"trade-off", "conclusion", "determined", "established",
	},
	"hall_events": {
		"session", "meeting", "milestone", "completed", "finished",
		"debugged", "fixed", "deployed", "released", "started",
		"ended", "reviewed", "discussed", "worked on", "debugging",
	},
	"hall_discoveries": {
		"discovered", "found", "learned", "realized", "insight",
		"breakthrough", "noticed", "uncovered", "observed", "understood",
		"figured out", "solved", "worked out",
	},
	"hall_preferences": {
		"prefer", "preference", "always use", "never", "like",
		"dislike", "hate", "love", "my rule", "habit", "tend to",
		"usually", "typically", "standard", "default",
	},
	"hall_advice": {
		"recommend", "suggest", "advice", "tip", "should", "best practice",
		"consider", "try", "use", "avoid", "helpful", "solution",
		"workaround", "fix", "approach",
	},
}

// LayerKeywords for layer detection
var LayerKeywords = map[string][]string{
	"L1": {
		"critical", "important", "essential", "key", "core",
		"must", "always", "never", "fundamental", "permanent",
	},
	"L3": {
		"detailed", "full", "complete", "raw", "verbatim",
		"entire", "whole", "transcript",
	},
}

// GetHallPrompt returns a prompt for a specific hall type
func GetHallPrompt(hallType string) string {
	switch hallType {
	case "hall_facts":
		return "Extract key decisions and locked choices that must be remembered."
	case "hall_events":
		return "Summarize the session/milestone/event details with timestamp and outcome."
	case "hall_discoveries":
		return "Capture the insight or discovery and its significance."
	case "hall_preferences":
		return "Record the preference/habit and its context."
	case "hall_advice":
		return "Preserve the recommendation/solution and when to apply it."
	default:
		return "Summarize the content for memory storage."
	}
}

// ChatSummarizePrompt generates a prompt for summarizing a single Q&A
func ChatSummarizePrompt(question, answer string) string {
	return fmt.Sprintf(`Analyze this Q&A interaction and extract useful memory.

Question:
%s

Answer:
%s

Extract:
1. Any decisions or facts (hall_facts) → assign to L2
2. Any preferences expressed (hall_preferences) → assign to L1
3. Any notable events or activities (hall_events) → assign to L2
4. Any insights or discoveries (hall_discoveries) → assign to L2
5. Any advice or solutions (hall_advice) → assign to L2

Layer rules:
- L1: ONLY for hall_preferences (user preferences, habits)
- L2: For hall_facts, hall_events, hall_discoveries, hall_advice
- L3: Raw content (not used for summaries)

Return a JSON object with the extracted information in this format:
{
  "hall_type": "...",
  "layer": "L1/L2",
  "summary": "...",
  "keywords": ["..."]
}`, question, answer)
}

// ChatBatchSummarizePrompt generates a prompt for summarizing multiple Q&A pairs
func ChatBatchSummarizePrompt(qaPairs []QAPair) string {
	var pairsBuilder strings.Builder
	for i, qa := range qaPairs {
		pairsBuilder.WriteString(fmt.Sprintf("## Q&A %d\n**Question:**\n%s\n\n**Answer:**\n%s\n\n",
			i+1, qa.Question, qa.Answer))
	}

	return fmt.Sprintf(`Analyze these Q&A interactions and extract useful memories.

%s
For each Q&A that contains useful information, extract:
1. Any decisions or facts (hall_facts) → assign to L2
2. Any preferences expressed (hall_preferences) → assign to L1
3. Any notable events or activities (hall_events) → assign to L2
4. Any insights or discoveries (hall_discoveries) → assign to L2
5. Any advice or solutions (hall_advice) → assign to L2

Layer rules:
- L1: ONLY for hall_preferences (user preferences, habits)
- L2: For hall_facts, hall_events, hall_discoveries, hall_advice

Return a JSON array of extracted memories. Skip Q&A pairs that don't contain useful information.
[
  {
    "qa_index": 0,
    "hall_type": "...",
    "layer": "L1/L2",
    "summary": "...",
    "keywords": ["..."]
  },
  ...
]

If no useful information found, return empty array: []`, pairsBuilder.String())
}

// QAPair represents a question-answer pair for batch processing
type QAPair struct {
	Question string
	Answer   string
}

// FileSummarizePrompt generates a prompt for summarizing file content
// DEPRECATED: Use FileSimplePrompt for simplified file processing
func FileSummarizePrompt(content, filePath string) string {
	return fmt.Sprintf(`Analyze this file content and extract useful information for memory storage.

File: %s

Content:
%s

Extract:
1. Key decisions or facts (hall_facts)
2. Notable events or changes (hall_events)
3. New insights or discoveries (hall_discoveries)
4. Any preferences or habits mentioned (hall_preferences)
5. Useful advice or recommendations (hall_advice)

Return a JSON object:
{
  "hall_type": "...",
  "layer": "L1/L2/L3",
  "summary": "...",
  "keywords": ["..."]
}`, filePath, content)
}

// FileSimplePrompt generates a simplified prompt for file content summarization
// This version only extracts a summary without HallType classification
func FileSimplePrompt(content, filePath string) string {
	return fmt.Sprintf(`Summarize this file content concisely for knowledge storage.

File: %s

Content:
%s

Provide a brief summary that captures the main points and key information.
The summary should be useful for later retrieval.

Return a JSON object:
{
  "summary": "...",
  "keywords": ["..."],
  "category": "..." // Optional: e.g., "documentation", "code", "config", "notes"
}`, filePath, content)
}

// CompactPrompt generates a prompt for compacting multiple memories
func CompactPrompt(items []string, maxTokens int) string {
	itemsStr := strings.Join(items, "\n---\n")
	return fmt.Sprintf(`Compact these memories into fewer, more concise entries while preserving key information.
Target total tokens: ~%d

Memories:
%s

Return a JSON array of compacted memories:
[
  {"summary": "...", "hall_type": "..."},
  ...
]`, maxTokens, itemsStr)
}