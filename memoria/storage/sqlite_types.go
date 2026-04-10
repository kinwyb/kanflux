// Package storage provides memory storage implementations
package storage

import (
	"time"

	"github.com/kinwyb/kanflux/memoria/types"
)

// Document represents a document to be stored in SQLite vector store
type Document struct {
	ID         string         `json:"id"`
	Layer      int            `json:"layer"`       // 1=L1, 2=L2, 3=L3
	Content    string         `json:"content"`     // L2: summary; L3: original content
	HallType   string         `json:"hall_type"`
	UserID     string         `json:"user_id"`
	Source     string         `json:"source"`
	SourceType string         `json:"source_type"`
	Tokens     int            `json:"tokens"`
	Timestamp  time.Time      `json:"timestamp"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

// SearchResult represents a search result from SQLite vector store
type SearchResult struct {
	ID         string         `json:"id"`
	Content    string         `json:"content"`
	HallType   string         `json:"hall_type"`
	UserID     string         `json:"user_id"`
	Source     string         `json:"source"`
	SourceType string         `json:"source_type"`
	Score      float64        `json:"score"`       // relevance score 0-1
	Layer      int            `json:"layer"`
	Timestamp  time.Time      `json:"timestamp"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

// SearchOptions for SQLite search operations
type SearchOptions struct {
	HallType    string   // filter by HallType
	UserID      string   // filter by user
	Limit       int      // max results
	Layers      []int    // search layers, default [2, 3]
	PreferLayer int      // preferred layer, default 2
	MinScore    float64  // minimum relevance score
	SourceType  string   // filter by source type
}

// StoreStats holds statistics about the SQLite store
type StoreStats struct {
	TotalDocuments int
	TotalVectors   int
	StorageSize    int64
	LayerCounts    map[int]int // layer -> count
}

// NewDocumentFromMemoryItem converts MemoryItem to Document
func NewDocumentFromMemoryItem(item *types.MemoryItem) *Document {
	return &Document{
		ID:         item.ID,
		Layer:      int(item.Layer),
		Content:    item.Content,
		HallType:   string(item.HallType),
		UserID:     item.UserID,
		Source:     item.Source,
		SourceType: string(item.SourceType),
		Tokens:     item.Tokens,
		Timestamp:  item.Timestamp,
		Metadata:   item.Metadata,
	}
}

// ToMemoryItem converts Document to MemoryItem
func (d *Document) ToMemoryItem() *types.MemoryItem {
	return &types.MemoryItem{
		ID:         d.ID,
		Layer:      types.Layer(d.Layer),
		Content:    d.Content,
		HallType:   types.HallType(d.HallType),
		UserID:     d.UserID,
		Source:     d.Source,
		SourceType: types.SourceType(d.SourceType),
		Tokens:     d.Tokens,
		Timestamp:  d.Timestamp,
		Metadata:   d.Metadata,
	}
}

// ToSearchResult converts SearchResult to types.SearchResult
func (r *SearchResult) ToSearchResult() *types.SearchResult {
	return &types.SearchResult{
		Item: &types.MemoryItem{
			ID:         r.ID,
			Content:    r.Content,
			HallType:   types.HallType(r.HallType),
			UserID:     r.UserID,
			Source:     r.Source,
			SourceType: types.SourceType(r.SourceType),
			Layer:      types.Layer(r.Layer),
			Timestamp:  r.Timestamp,
			Metadata:   r.Metadata,
		},
		Score:     r.Score,
		Layer:     types.Layer(r.Layer),
		MatchType: "semantic",
	}
}

// DefaultSearchOptions returns default search options
func DefaultSearchOptions() *SearchOptions {
	return &SearchOptions{
		Layers:      []int{2, 3}, // search L2 first, then L3
		PreferLayer: 2,           // prefer L2 results
		Limit:       10,
		MinScore:    0.0,
	}
}