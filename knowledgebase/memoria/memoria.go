package memoria

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"github.com/kinwyb/kanflux/knowledgebase/memoria/llm"
	"github.com/kinwyb/kanflux/knowledgebase/memoria/processor"
	"github.com/kinwyb/kanflux/knowledgebase/memoria/storage"
	"github.com/kinwyb/kanflux/knowledgebase/memoria/types"
)

// Memoria is the main orchestrator for the memory agent service
type Memoria struct {
	config *Config

	storage       types.Storage
	summarizer    types.Summarizer
	chatProcessor types.Processor
	fileProcessor types.Processor
	scheduler     *Scheduler

	l1 *L1FactsLayer
	l2 *L2EventsLayer
	l3 *L3RawLayer

	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.RWMutex
}

// New creates a new Memoria service
func New(cfg *Config) (*Memoria, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	m := &Memoria{
		config: cfg,
	}

	if err := m.initialize(); err != nil {
		m.Close()
		return nil, err
	}

	return m, nil
}

func (m *Memoria) initialize() error {
	if err := os.MkdirAll(m.config.GetMemoriaDir(), 0755); err != nil {
		return fmt.Errorf("failed to create memoria directory: %w", err)
	}

	mdStore, err := storage.NewMDStore(m.config.GetMemoriaDir(), m.config.StorageConfig)
	if err != nil {
		return fmt.Errorf("failed to create MD storage: %w", err)
	}
	m.storage = mdStore

	m.l1 = NewL1FactsLayer(mdStore, m.config.StorageConfig.MaxL1Tokens)
	m.l2 = NewL2EventsLayer(mdStore, m.config.StorageConfig.MaxL2Tokens)

	slog.Info("Memoria initialized", "workspace", m.config.Workspace)
	return nil
}

// SetChatModel sets the LLM model for summarization
func (m *Memoria) SetChatModel(model types.ChatModel) {
	m.summarizer = llm.NewSummarizer(model, 500)

	m.chatProcessor = processor.NewChatProcessor(
		m.summarizer,
		m.config.ProcessorConfig,
		m.config.GetSessionDir(),
	)

	m.fileProcessor = processor.NewFileProcessor(
		m.summarizer,
		m.config.ProcessorConfig,
		m.config.WatchPaths,
	)
}

// SetL3KnowledgeBase sets the knowledge base for L3 deep search
func (m *Memoria) SetL3KnowledgeBase() {
	m.l3 = NewL3RawLayer(nil)
}

// Start starts the Memoria service
func (m *Memoria) Start(ctx context.Context) error {
	m.ctx, m.cancel = context.WithCancel(ctx)

	if err := m.l1.Initialize(ctx); err != nil {
		slog.Warn("Failed to initialize L1 cache", "error", err)
	}

	if m.config.ScheduleConfig.Enabled {
		m.scheduler = NewScheduler(
			m.config.ScheduleConfig,
			m.chatProcessor,
			m.fileProcessor,
			m.storage,
		)
		if err := m.scheduler.Start(ctx); err != nil {
			return fmt.Errorf("failed to start scheduler: %w", err)
		}
	}

	slog.Info("Memoria service started")
	return nil
}

// Stop stops the Memoria service
func (m *Memoria) Stop() error {
	if m.cancel != nil {
		m.cancel()
	}

	if m.scheduler != nil {
		m.scheduler.Stop()
	}

	slog.Info("Memoria service stopped")
	return nil
}

// Close closes all resources
func (m *Memoria) Close() error {
	m.Stop()

	if m.l1 != nil {
		m.l1.Close()
	}
	if m.l2 != nil {
		m.l2.Close()
	}
	if m.l3 != nil {
		m.l3.Close()
	}
	if m.storage != nil {
		m.storage.Close()
	}

	return nil
}

// AddMemory adds a memory item to the appropriate layer
func (m *Memoria) AddMemory(ctx context.Context, item *types.MemoryItem) error {
	switch item.Layer {
	case types.LayerL1:
		return m.l1.Add(ctx, item)
	case types.LayerL2:
		return m.l2.Add(ctx, item)
	case types.LayerL3:
		if m.l3 != nil {
			return m.l3.Add(ctx, item)
		}
		return fmt.Errorf("L3 not initialized")
	default:
		return fmt.Errorf("invalid layer: %d", item.Layer)
	}
}

// GetL1Facts returns all L1 facts for a user
func (m *Memoria) GetL1Facts(userID string) []*types.MemoryItem {
	return m.l1.GetForUser(userID)
}

// GetL1All returns all L1 memories
func (m *Memoria) GetL1All() []*types.MemoryItem {
	return m.l1.GetAll()
}

// GetL2Recent returns recent L2 memories for a user
func (m *Memoria) GetL2Recent(ctx context.Context, userID string, days int) ([]*types.MemoryItem, error) {
	return m.l2.LoadRecent(ctx, userID, days)
}

// SearchL3 performs semantic search in L3
func (m *Memoria) SearchL3(ctx context.Context, query string, opts *types.RetrieveOptions) ([]*types.MemoryItem, error) {
	if m.l3 == nil {
		return nil, fmt.Errorf("L3 not initialized")
	}
	return m.l3.Search(ctx, query, opts)
}

// ProcessChat processes chat content for memory extraction
func (m *Memoria) ProcessChat(ctx context.Context, source, content string, userCtx types.UserIdentity) (*types.ProcessingResult, error) {
	if m.chatProcessor == nil {
		return nil, fmt.Errorf("chat processor not initialized - set a chat model first")
	}

	result, err := m.chatProcessor.Process(ctx, source, content, userCtx)
	if err != nil {
		return nil, err
	}

	for _, item := range result.Items {
		if err := m.AddMemory(ctx, item); err != nil {
			result.Errors = append(result.Errors, err)
		}
	}

	return result, nil
}

// ProcessFile processes file content for memory extraction
func (m *Memoria) ProcessFile(ctx context.Context, source, content string, userCtx types.UserIdentity) (*types.ProcessingResult, error) {
	if m.fileProcessor == nil {
		return nil, fmt.Errorf("file processor not initialized - set a chat model first")
	}

	result, err := m.fileProcessor.Process(ctx, source, content, userCtx)
	if err != nil {
		return nil, err
	}

	for _, item := range result.Items {
		if err := m.AddMemory(ctx, item); err != nil {
			result.Errors = append(result.Errors, err)
		}
	}

	return result, nil
}

// TriggerChatProcessing triggers immediate chat processing
func (m *Memoria) TriggerChatProcessing() {
	if m.scheduler != nil {
		m.scheduler.TriggerChat()
	}
}

// TriggerFileProcessing triggers immediate file processing
func (m *Memoria) TriggerFileProcessing() {
	if m.scheduler != nil {
		m.scheduler.TriggerFile()
	}
}

// GetStats returns service statistics
func (m *Memoria) GetStats() map[string]interface{} {
	stats := map[string]interface{}{
		"workspace": m.config.Workspace,
		"l1_items":  len(m.l1.GetAll()),
	}

	if m.scheduler != nil {
		stats["scheduler"] = m.scheduler.GetStats()
	}

	return stats
}

// GetConfig returns the current configuration
func (m *Memoria) GetConfig() *Config {
	return m.config
}

// GetMemoriaDir returns the memoria storage directory
func (m *Memoria) GetMemoriaDir() string {
	return m.config.GetMemoriaDir()
}