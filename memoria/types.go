// Package memoria provides a background memory agent module for processing
// chat history and files, organizing memories following MemPalace layered design.
package memoria

import (
	"github.com/kinwyb/kanflux/memoria/types"
)

// Re-export types from types package
type (
	MemoryItem          = types.MemoryItem
	ProcessingResult    = types.ProcessingResult
	ProcessItem         = types.ProcessItem
	UserIdentity        = types.UserIdentity
	DefaultUserIdentity = types.DefaultUserIdentity
	RetrieveOptions     = types.RetrieveOptions
	SearchResult        = types.SearchResult
	TimeRange           = types.TimeRange
	Processor           = types.Processor
	Summarizer          = types.Summarizer
	ChatModel           = types.ChatModel
	Storage             = types.Storage
	Embedder            = types.Embedder
	VectorStore         = types.VectorStore
	Layer               = types.Layer
	HallType            = types.HallType
)

// Re-export constants
const (
	LayerL1 = types.LayerL1
	LayerL2 = types.LayerL2
	LayerL3 = types.LayerL3

	HallFacts       = types.HallFacts
	HallEvents      = types.HallEvents
	HallDiscoveries = types.HallDiscoveries
	HallPreferences = types.HallPreferences
	HallAdvice      = types.HallAdvice
)

// ParseSessionKey parses a session key into DefaultUserIdentity
func ParseSessionKey(sessionKey string) *DefaultUserIdentity {
	parts := splitSessionKey(sessionKey)
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

func splitSessionKey(key string) []string {
	result := make([]string, 0)
	start := 0
	for i := 0; i < len(key); i++ {
		if key[i] == ':' {
			result = append(result, key[start:i])
			start = i + 1
		}
	}
	if start < len(key) {
		result = append(result, key[start:])
	}
	return result
}
