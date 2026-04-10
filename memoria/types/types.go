// Package types defines shared types for the memoria module
package types

import (
	"context"
	"time"
)

// Layer represents a memory layer in MemPalace design
type Layer int

const (
	// LayerL1 Key facts (~120 tokens) - always loaded
	LayerL1 Layer = 1
	// LayerL2 Room memories (~200-500 tokens) - loaded on demand
	LayerL2 Layer = 2
	// LayerL3 Deep search - full semantic search over raw data
	LayerL3 Layer = 3
)

// SourceType represents the source of memory content
type SourceType string

const (
	// SourceTypeChat indicates content from chat/conversation
	SourceTypeChat SourceType = "chat"
	// SourceTypeFile indicates content from file processing
	SourceTypeFile SourceType = "file"
)

// SearchMode represents the search strategy
type SearchMode string

const (
	// SearchModeKeyword uses keyword matching
	SearchModeKeyword SearchMode = "keyword"
	// SearchModeSemantic uses vector similarity search
	SearchModeSemantic SearchMode = "semantic"
)

// HallType represents the five hall types for organizing memories
type HallType string

const (
	// HallFacts Decisions, locked choices
	HallFacts HallType = "hall_facts"
	// HallEvents Sessions, milestones, debugging process
	HallEvents HallType = "hall_events"
	// HallDiscoveries Breakthroughs, new insights
	HallDiscoveries HallType = "hall_discoveries"
	// HallPreferences Habits, preferences, opinions
	HallPreferences HallType = "hall_preferences"
	// HallAdvice Recommendations and solutions
	HallAdvice HallType = "hall_advice"
)

// MemoryItem represents a single memory entry
type MemoryItem struct {
	ID         string         `json:"id"`
	HallType   HallType       `json:"hall_type"`
	Layer      Layer          `json:"layer"`
	SourceType SourceType     `json:"source_type"`
	Content    string         `json:"content"`
	Summary    string         `json:"summary"`
	Source     string         `json:"source"`
	UserID     string         `json:"user_id"`     // Full session key (channel:accountID:chatID)
	AccountID  string         `json:"account_id"`  // The actual user identifier
	Timestamp  time.Time      `json:"timestamp"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	Tokens     int            `json:"tokens"`
}

// ProcessingResult represents the result of memory extraction
type ProcessingResult struct {
	Items       []*MemoryItem    `json:"items"`
	LayerCounts map[Layer]int    `json:"layer_counts"`
	HallCounts  map[HallType]int `json:"hall_counts"`
	Errors      []error          `json:"errors,omitempty"`
}

// ProcessItem represents an item to be processed
type ProcessItem struct {
	Source    string       `json:"source"`
	Content   string       `json:"content"`
	UserCtx   UserIdentity `json:"user_context"`
	Timestamp time.Time    `json:"timestamp"`
}

// UserIdentity is the interface for user identity
type UserIdentity interface {
	GetUserID() string
	GetDisplayName() string
	GetChannel() string
	GetAccountID() string
	GetMetadata() map[string]any
}

// DefaultUserIdentity is the default implementation
type DefaultUserIdentity struct {
	UserID    string         `json:"user_id"`
	Channel   string         `json:"channel"`
	AccountID string         `json:"account_id"`
	ChatID    string         `json:"chat_id"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// GetUserID returns the unique user identifier
func (u *DefaultUserIdentity) GetUserID() string {
	return u.UserID
}

// GetDisplayName returns the user's display name
func (u *DefaultUserIdentity) GetDisplayName() string {
	if u.Metadata != nil {
		if name, ok := u.Metadata["display_name"].(string); ok {
			return name
		}
	}
	return u.UserID
}

// GetChannel returns the user's source channel
func (u *DefaultUserIdentity) GetChannel() string {
	return u.Channel
}

// GetAccountID returns the account identifier
func (u *DefaultUserIdentity) GetAccountID() string {
	return u.AccountID
}

// GetMetadata returns user metadata
func (u *DefaultUserIdentity) GetMetadata() map[string]any {
	return u.Metadata
}

// RetrieveOptions for memory retrieval
type RetrieveOptions struct {
	HallTypes  []HallType  `json:"hall_types,omitempty"`
	Layers     []Layer     `json:"layers,omitempty"`
	SourceType SourceType  `json:"source_type,omitempty"`
	SearchMode SearchMode  `json:"search_mode,omitempty"`
	UserID     string      `json:"user_id,omitempty"`
	Limit      int         `json:"limit"`
	TimeRange  *TimeRange  `json:"time_range,omitempty"`
	Query      string      `json:"query,omitempty"` // 搜索查询
}

// SearchResult represents a search result with score
type SearchResult struct {
	Item      *MemoryItem `json:"item"`
	Score     float64     `json:"score"`     // 相关性分数 0-1
	Layer     Layer       `json:"layer"`     // 来源层级
	MatchType string      `json:"match_type"` // "exact", "keyword", "semantic"
}

// TimeRange for time-based filtering
type TimeRange struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// Processor is the interface for memory extraction processors
type Processor interface {
	Process(ctx context.Context, source string, content string, userCtx UserIdentity) (*ProcessingResult, error)
	ProcessBatch(ctx context.Context, items []ProcessItem) (*ProcessingResult, error)
	Name() string
}

// Summarizer is the interface for LLM-based summarization
type Summarizer interface {
	Summarize(ctx context.Context, content string, hallType HallType, layer Layer) (string, error)
	ExtractFacts(ctx context.Context, content string, userCtx UserIdentity) ([]*MemoryItem, error)
	Categorize(ctx context.Context, content string) (HallType, Layer, error)
}

// ChatModel is the interface for LLM-based summarization
type ChatModel interface {
	Generate(ctx context.Context, prompt string) (string, error)
	GenerateWithSystem(ctx context.Context, system, prompt string) (string, error)
}

// Storage is the interface for persisting memories
type Storage interface {
	Store(ctx context.Context, item *MemoryItem) error
	StoreBatch(ctx context.Context, items []*MemoryItem) error
	Retrieve(ctx context.Context, opts *RetrieveOptions) ([]*MemoryItem, error)
	Delete(ctx context.Context, id string) error
	DeleteByUser(ctx context.Context, userID string) error
	Close() error
}

// ============ Configuration Types ============

// StorageConfig for MD file storage
type StorageConfig struct {
	MaxL1Tokens  int    `json:"max_l1_tokens"`
	MaxL2Tokens  int    `json:"max_l2_tokens"`
	DateFormat   string `json:"date_format"`
	EnableBackup bool   `json:"enable_backup"`
	BackupDir    string `json:"backup_dir"`
}

// ProcessorConfig for memory processors
type ProcessorConfig struct {
	MaxBatchSize   int           `json:"max_batch_size"`
	Timeout        time.Duration `json:"timeout"`
	EnableParallel bool          `json:"enable_parallel"`
	SessionDir     string        `json:"session_dir"`
	MaxRetries     int           `json:"max_retries"`
}

// WatchPath for file watching configuration
type WatchPath struct {
	Path       string   `json:"path"`
	Extensions []string `json:"extensions"`
	Recursive  bool     `json:"recursive"`
	Exclude    []string `json:"exclude"`
}

// Embedder is the interface for generating text embeddings (for L3 semantic search)
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
	Dimension() int
}

// VectorStore is the interface for vector storage (for L3)
type VectorStore interface {
	StoreWithEmbedding(ctx context.Context, item *MemoryItem, embedding []float32) error
	SearchByEmbedding(ctx context.Context, embedding []float32, opts *RetrieveOptions) ([]*SearchResult, error)
	DeleteBySource(ctx context.Context, source string) error
}