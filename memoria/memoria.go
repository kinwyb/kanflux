package memoria

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/kinwyb/kanflux/memoria/llm"
	"github.com/kinwyb/kanflux/memoria/processor"
	"github.com/kinwyb/kanflux/memoria/storage"
	"github.com/kinwyb/kanflux/memoria/types"
)

// Memoria is the main orchestrator for the memory agent service
type Memoria struct {
	config *Config

	storage       types.Storage
	summarizer    types.Summarizer
	chatProcessor types.Processor
	fileProcessor types.Processor
	scheduler     *Scheduler

	l1         *L1FactsLayer
	l2         *L2EventsLayer
	l3         *L3RawLayer
	sqliteStore *storage.SQLiteStore // Shared SQLite store for L2 and L3

	// L3 语义搜索
	embedder types.Embedder

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

	// 自动初始化 SQLite 存储（L2 + L3 共用）
	if m.config.Embedding != nil {
		if err := m.initSQLiteStore(); err != nil {
			slog.Warn("Failed to initialize SQLite store", "error", err)
		}
	}

	slog.Info("Memoria initialized", "workspace", m.config.Workspace)
	return nil
}

// initSQLiteStore initializes SQLite store for L2 and L3
func (m *Memoria) initSQLiteStore() error {
	ctx := context.Background()
	embedder, err := NewEmbedderFromConfig(ctx, m.config.Embedding)
	if err != nil {
		return fmt.Errorf("failed to create embedder: %w", err)
	}
	m.embedder = embedder

	// Create shared SQLite store
	sqliteStore, err := storage.NewSQLiteStore(m.config.GetMemoriaDir(), embedder)
	if err != nil {
		return fmt.Errorf("failed to create SQLite store: %w", err)
	}

	if err := sqliteStore.Initialize(ctx); err != nil {
		return fmt.Errorf("failed to initialize SQLite store: %w", err)
	}

	m.sqliteStore = sqliteStore

	// Share SQLite store with L2 and L3
	m.l2.SetSQLiteStore(sqliteStore)

	m.l3 = NewL3RawLayer(m.config.GetMemoriaDir(), embedder)
	m.l3.SetSQLiteStore(sqliteStore)

	slog.Info("SQLite store initialized for L2 and L3",
		"provider", m.config.Embedding.Provider,
		"model", m.config.Embedding.Model)
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
	// Set storage for file index tracking
	if fp, ok := m.fileProcessor.(*processor.FileProcessor); ok {
		fp.SetStorage(m.storage)
	}
}

// SetEmbedder sets the embedder for L2/L3 semantic search
func (m *Memoria) SetEmbedder(emb types.Embedder) {
	m.embedder = emb

	// Initialize SQLite store if not already initialized
	if m.sqliteStore == nil && emb != nil {
		sqliteStore, err := storage.NewSQLiteStore(m.config.GetMemoriaDir(), emb)
		if err != nil {
			slog.Warn("Failed to create SQLite store", "error", err)
			return
		}
		if err := sqliteStore.Initialize(context.Background()); err != nil {
			slog.Warn("Failed to initialize SQLite store", "error", err)
			return
		}
		m.sqliteStore = sqliteStore
		m.l2.SetSQLiteStore(sqliteStore)
	}

	// Initialize L3 layer
	if m.l3 == nil && emb != nil {
		m.l3 = NewL3RawLayer(m.config.GetMemoriaDir(), emb)
		m.l3.SetSQLiteStore(m.sqliteStore)
	}
}

// SetL3KnowledgeBase initializes L3 layer with embedder
func (m *Memoria) SetL3KnowledgeBase() {
	if m.embedder != nil && m.l3 == nil {
		m.l3 = NewL3RawLayer(m.config.GetMemoriaDir(), m.embedder)
		m.l3.SetSQLiteStore(m.sqliteStore)
	}
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

	// 初始化时进行异步解析
	if m.config.InitialScan {
		go m.initialScan()
	}

	slog.Info("Memoria service started")
	return nil
}

// initialScan performs initial scan of knowledge files and chat sessions
func (m *Memoria) initialScan() {
	ctx := m.ctx
	slog.Info("Starting initial memory scan")

	// 1. 扫描并解析知识文件
	m.scanKnowledgeFiles(ctx)

	// 2. 扫描并解析聊天记录
	m.scanChatSessions(ctx)

	slog.Info("Initial memory scan completed")
}

// ScanAndProcess performs a synchronous scan of knowledge files and chat sessions.
// This is useful for initialization or manual triggering of memory processing.
// Returns the number of items processed and any errors encountered.
func (m *Memoria) ScanAndProcess(ctx context.Context) (knowledgeItems, chatItems int, err error) {
	slog.Info("Starting synchronous memory scan")

	// 1. 扫描知识库文件
	knowledgeItems = m.scanKnowledgeFilesSync(ctx)

	// 2. 扫描聊天记录
	chatItems = m.scanChatSessionsSync(ctx)

	slog.Info("Synchronous memory scan completed",
		"knowledge_items", knowledgeItems,
		"chat_items", chatItems)

	return knowledgeItems, chatItems, nil
}

// scanKnowledgeFiles 扫描并解析知识库文件
func (m *Memoria) scanKnowledgeFiles(ctx context.Context) {
	if m.fileProcessor == nil {
		slog.Warn("File processor not initialized, skipping knowledge scan")
		return
	}

	// 获取所有需要扫描的路径
	watchPaths := m.config.GetAllWatchPaths()
	if len(watchPaths) == 0 {
		slog.Debug("No knowledge paths configured")
		return
	}

	slog.Info("Scanning knowledge files", "paths", len(watchPaths))

	// Cast to FileProcessor to access ScanModifiedFiles
	fileProc, ok := m.fileProcessor.(*processor.FileProcessor)
	if !ok {
		slog.Warn("File processor type mismatch")
		return
	}

	// 使用时间零点扫描所有文件（不限修改时间）
	items, err := fileProc.ScanModifiedFiles(ctx, time.Time{})
	if err != nil {
		slog.Error("Failed to scan knowledge files", "error", err)
		return
	}

	if len(items) == 0 {
		slog.Debug("No knowledge files found")
		return
	}

	slog.Info("Processing knowledge files", "count", len(items))

	// 批量处理
	result, err := m.fileProcessor.ProcessBatch(ctx, items)
	if err != nil {
		slog.Error("Failed to process knowledge files", "error", err)
		return
	}

	// 存储到各层
	for _, item := range result.Items {
		if err := m.AddMemory(ctx, item); err != nil {
			slog.Warn("Failed to store memory item", "id", item.ID, "error", err)
		}
	}

	slog.Info("Knowledge files processed",
		"items", len(result.Items),
		"l1", result.LayerCounts[types.LayerL1],
		"l2", result.LayerCounts[types.LayerL2],
		"l3", result.LayerCounts[types.LayerL3],
		"errors", len(result.Errors))
}

// scanChatSessions 扫描并解析聊天记录
func (m *Memoria) scanChatSessions(ctx context.Context) {
	if m.chatProcessor == nil {
		slog.Warn("Chat processor not initialized, skipping session scan")
		return
	}

	sessionDir := m.config.GetSessionDir()
	slog.Info("Scanning chat sessions", "dir", sessionDir)

	// Cast to ChatProcessor to access ScanSessions
	chatProc, ok := m.chatProcessor.(*processor.ChatProcessor)
	if !ok {
		slog.Warn("Chat processor type mismatch")
		return
	}

	// 扫描所有 session 文件（使用时间零点扫描全部）
	items, err := chatProc.ScanSessions(ctx, time.Time{})
	if err != nil {
		slog.Error("Failed to scan chat sessions", "error", err)
		return
	}

	if len(items) == 0 {
		slog.Debug("No chat sessions found")
		return
	}

	slog.Info("Processing chat sessions", "count", len(items))

	// 批量处理
	result, err := m.chatProcessor.ProcessBatch(ctx, items)
	if err != nil {
		slog.Error("Failed to process chat sessions", "error", err)
		return
	}

	// 存储到各层
	for _, item := range result.Items {
		if err := m.AddMemory(ctx, item); err != nil {
			slog.Warn("Failed to store memory item", "id", item.ID, "error", err)
		}
	}

	slog.Info("Chat sessions processed",
		"items", len(result.Items),
		"l1", result.LayerCounts[types.LayerL1],
		"l2", result.LayerCounts[types.LayerL2],
		"l3", result.LayerCounts[types.LayerL3],
		"errors", len(result.Errors))
}

// scanKnowledgeFilesSync 同步扫描知识库文件，返回处理的条目数
func (m *Memoria) scanKnowledgeFilesSync(ctx context.Context) int {
	if m.fileProcessor == nil {
		slog.Warn("File processor not initialized, skipping knowledge scan")
		return 0
	}

	watchPaths := m.config.GetAllWatchPaths()
	if len(watchPaths) == 0 {
		slog.Debug("No knowledge paths configured")
		return 0
	}

	slog.Info("Scanning knowledge files", "paths", len(watchPaths))

	fileProc, ok := m.fileProcessor.(*processor.FileProcessor)
	if !ok {
		slog.Warn("File processor type mismatch")
		return 0
	}

	items, err := fileProc.ScanModifiedFiles(ctx, time.Time{})
	if err != nil {
		slog.Error("Failed to scan knowledge files", "error", err)
		return 0
	}

	if len(items) == 0 {
		slog.Debug("No knowledge files found")
		return 0
	}

	slog.Info("Processing knowledge files", "count", len(items))

	result, err := m.fileProcessor.ProcessBatch(ctx, items)
	if err != nil {
		slog.Error("Failed to process knowledge files", "error", err)
		return 0
	}

	storedCount := 0
	for _, item := range result.Items {
		if err := m.AddMemory(ctx, item); err != nil {
			slog.Warn("Failed to store memory item", "id", item.ID, "error", err)
		} else {
			storedCount++
		}
	}

	slog.Info("Knowledge files processed",
		"items", len(result.Items),
		"stored", storedCount,
		"l1", result.LayerCounts[types.LayerL1],
		"l2", result.LayerCounts[types.LayerL2],
		"l3", result.LayerCounts[types.LayerL3],
		"errors", len(result.Errors))

	return storedCount
}

// scanChatSessionsSync 同步扫描聊天记录，返回处理的条目数
func (m *Memoria) scanChatSessionsSync(ctx context.Context) int {
	if m.chatProcessor == nil {
		slog.Warn("Chat processor not initialized, skipping session scan")
		return 0
	}

	sessionDir := m.config.GetSessionDir()
	slog.Info("Scanning chat sessions", "dir", sessionDir)

	chatProc, ok := m.chatProcessor.(*processor.ChatProcessor)
	if !ok {
		slog.Warn("Chat processor type mismatch")
		return 0
	}

	items, err := chatProc.ScanSessions(ctx, time.Time{})
	if err != nil {
		slog.Error("Failed to scan chat sessions", "error", err)
		return 0
	}

	if len(items) == 0 {
		slog.Debug("No chat sessions found")
		return 0
	}

	slog.Info("Processing chat sessions", "count", len(items))

	result, err := m.chatProcessor.ProcessBatch(ctx, items)
	if err != nil {
		slog.Error("Failed to process chat sessions", "error", err)
		return 0
	}

	storedCount := 0
	for _, item := range result.Items {
		if err := m.AddMemory(ctx, item); err != nil {
			slog.Warn("Failed to store memory item", "id", item.ID, "error", err)
		} else {
			storedCount++
		}
	}

	slog.Info("Chat sessions processed",
		"items", len(result.Items),
		"stored", storedCount,
		"l1", result.LayerCounts[types.LayerL1],
		"l2", result.LayerCounts[types.LayerL2],
		"l3", result.LayerCounts[types.LayerL3],
		"errors", len(result.Errors))

	return storedCount
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
	// Close SQLite store (shared by L2 and L3)
	if m.sqliteStore != nil {
		m.sqliteStore.Close()
	}
	if m.storage != nil {
		m.storage.Close()
	}

	return nil
}

// AddMemory adds a memory item to the appropriate layer
func (m *Memoria) AddMemory(ctx context.Context, item *types.MemoryItem) error {
	slog.Debug("Adding memory item",
		"id", item.ID,
		"layer", item.Layer,
		"hall_type", item.HallType,
		"source_type", item.SourceType,
		"source", item.Source,
		"tokens", item.Tokens)

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

// ============ 层级搜索 ============

// Search performs hierarchical search across all layers
// Search behavior is determined by SearchMode:
// - keyword (default): L1/L2/L3 keyword matching
// - semantic: L2/L3 vector similarity search
// Results are filtered by SourceType if specified
func (m *Memoria) Search(ctx context.Context, query string, opts *types.RetrieveOptions) ([]*types.SearchResult, error) {
	if opts == nil {
		opts = &types.RetrieveOptions{Limit: 10}
	}
	if opts.Limit <= 0 {
		opts.Limit = 10
	}

	// Default to keyword search if not specified
	searchMode := opts.SearchMode
	if searchMode == "" {
		searchMode = types.SearchModeKeyword
	}

	slog.Debug("Memory search started",
		"query", query,
		"mode", searchMode,
		"limit", opts.Limit,
		"layers", opts.Layers,
		"source_type", opts.SourceType)

	startTime := time.Now()
	var results []*types.SearchResult
	var err error

	switch searchMode {
	case types.SearchModeSemantic:
		results, err = m.semanticSearch(ctx, query, opts)
	default:
		results, err = m.keywordSearch(ctx, query, opts)
	}

	if err != nil {
		slog.Error("Memory search failed",
			"query", query,
			"mode", searchMode,
			"error", err)
		return nil, err
	}

	// Filter by SourceType if specified
	if opts.SourceType != "" {
		filtered := make([]*types.SearchResult, 0)
		for _, r := range results {
			if r.Item.SourceType == opts.SourceType {
				filtered = append(filtered, r)
			}
		}
		results = filtered
	}

	// Limit results
	if len(results) > opts.Limit {
		results = results[:opts.Limit]
	}

	slog.Info("Memory search completed",
		"query", query,
		"mode", searchMode,
		"results", len(results),
		"duration", time.Since(startTime).Milliseconds())

	return results, nil
}

// keywordSearch performs keyword-based search across L1/L2/L3
func (m *Memoria) keywordSearch(ctx context.Context, query string, opts *types.RetrieveOptions) ([]*types.SearchResult, error) {
	results := make([]*types.SearchResult, 0)
	seen := make(map[string]bool)

	// Check which layers to search
	searchL1 := true
	searchL2 := true
	searchL3 := true
	if len(opts.Layers) > 0 {
		searchL1 = false
		searchL2 = false
		searchL3 = false
		for _, l := range opts.Layers {
			if l == types.LayerL1 {
				searchL1 = true
			} else if l == types.LayerL2 {
				searchL2 = true
			} else if l == types.LayerL3 {
				searchL3 = true
			}
		}
	}

	// 1. L1 搜索：精确/关键词匹配（内存缓存，最快）
	if searchL1 {
		l1Results := m.searchL1(query, opts)
		for _, r := range l1Results {
			if !seen[r.Item.ID] {
				seen[r.Item.ID] = true
				results = append(results, r)
			}
		}
	}

	// 2. L2 搜索：FTS5 关键词搜索（SQLite）
	if searchL2 && len(results) < opts.Limit {
		l2Results := m.searchL2Keyword(ctx, query, opts)
		for _, r := range l2Results {
			if !seen[r.Item.ID] {
				seen[r.Item.ID] = true
				results = append(results, r)
			}
		}
	}

	// 3. L3 搜索：关键词匹配（如果有 L3）
	if searchL3 && len(results) < opts.Limit && m.l3 != nil {
		l3Results, err := m.searchL3Keyword(ctx, query, opts)
		if err != nil {
			slog.Warn("L3 keyword search failed", "error", err)
		} else {
			for _, r := range l3Results {
				if !seen[r.Item.ID] {
					seen[r.Item.ID] = true
					results = append(results, r)
				}
			}
		}
	}

	return results, nil
}

// semanticSearch performs vector similarity search across L2/L3
func (m *Memoria) semanticSearch(ctx context.Context, query string, opts *types.RetrieveOptions) ([]*types.SearchResult, error) {
	results := make([]*types.SearchResult, 0)
	seen := make(map[string]bool)

	// Check which layers to search
	searchL2 := true
	searchL3 := true
	if len(opts.Layers) > 0 {
		searchL2 = false
		searchL3 = false
		for _, l := range opts.Layers {
			if l == types.LayerL2 {
				searchL2 = true
			} else if l == types.LayerL3 {
				searchL3 = true
			}
		}
	}

	// Use unified SQLite search for L2/L3 (prefer L2)
	if (searchL2 || searchL3) && m.sqliteStore != nil {
		layers := make([]int, 0)
		if searchL2 {
			layers = append(layers, 2)
		}
		if searchL3 {
			layers = append(layers, 3)
		}

		sqliteResults, err := m.sqliteStore.Search(ctx, query, opts)
		if err != nil {
			slog.Warn("SQLite semantic search failed", "error", err)
		} else {
			for _, r := range sqliteResults {
				if !seen[r.Item.ID] {
					seen[r.Item.ID] = true
					results = append(results, r)
				}
			}
		}
	}

	// Sort by score (semantic similarity)
	sortResultsByScore(results)
	return results, nil
}

// searchL1 searches L1 layer (exact/keyword match)
func (m *Memoria) searchL1(query string, opts *types.RetrieveOptions) []*types.SearchResult {
	results := make([]*types.SearchResult, 0)
	queryLower := strings.ToLower(query)
	queryTerms := strings.Fields(queryLower)

	var items []*types.MemoryItem
	if opts.UserID != "" {
		items = m.l1.GetForUser(opts.UserID)
	} else {
		items = m.l1.GetAll()
	}

	for _, item := range items {
		// 过滤 HallType
		if len(opts.HallTypes) > 0 {
			found := false
			for _, ht := range opts.HallTypes {
				if item.HallType == ht {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		// 过滤 SourceType (L1 only has chat content, but check anyway)
		if opts.SourceType != "" && item.SourceType != opts.SourceType {
			continue
		}

		score := m.calculateKeywordScore(item, queryLower, queryTerms)
		if score > 0 {
			matchType := "keyword"
			if score >= 0.8 {
				matchType = "exact"
			}
			results = append(results, &types.SearchResult{
				Item:      item,
				Score:     score,
				Layer:     types.LayerL1,
				MatchType: matchType,
			})
		}
	}

	// 按分数排序
	sortResultsByScore(results)
	return results
}

// searchL2Keyword searches L2 layer using SQLite FTS5 keyword search
func (m *Memoria) searchL2Keyword(ctx context.Context, query string, opts *types.RetrieveOptions) []*types.SearchResult {
	results := make([]*types.SearchResult, 0)

	// Use SQLite FTS5 search if available
	if m.sqliteStore != nil {
		l2Opts := &types.RetrieveOptions{
			Layers:     []types.Layer{types.LayerL2},
			UserID:     opts.UserID,
			SourceType: opts.SourceType,
			Limit:      opts.Limit * 2, // Get more for filtering
			HallTypes:  opts.HallTypes,
		}

		sqliteResults, err := m.sqliteStore.KeywordSearch(ctx, query, l2Opts)
		if err != nil {
			slog.Warn("L2 FTS search failed", "error", err)
		} else {
			for _, r := range sqliteResults {
				r.MatchType = "keyword"
				results = append(results, r)
			}
			sortResultsByScore(results)
			return results
		}
	}

	// Fallback to MD storage
	return m.searchL2Fallback(ctx, query, opts)
}

// searchL2Fallback searches L2 using MD storage (fallback)
func (m *Memoria) searchL2Fallback(ctx context.Context, query string, opts *types.RetrieveOptions) []*types.SearchResult {
	results := make([]*types.SearchResult, 0)
	queryLower := strings.ToLower(query)
	queryTerms := strings.Fields(queryLower)

	l2Opts := &types.RetrieveOptions{
		Layers:     []types.Layer{types.LayerL2},
		UserID:     opts.UserID,
		SourceType: opts.SourceType,
		Limit:      50,
		HallTypes:  opts.HallTypes,
		TimeRange:  opts.TimeRange,
	}

	items, err := m.storage.Retrieve(ctx, l2Opts)
	if err != nil {
		slog.Warn("L2 retrieval failed", "error", err)
		return results
	}

	for _, item := range items {
		score := m.calculateKeywordScore(item, queryLower, queryTerms)
		if score > 0 {
			results = append(results, &types.SearchResult{
				Item:      item,
				Score:     score,
				Layer:     types.LayerL2,
				MatchType: "keyword",
			})
		}
	}

	sortResultsByScore(results)
	return results
}

// searchL3 searches L3 layer (semantic search)
func (m *Memoria) searchL3(ctx context.Context, query string, opts *types.RetrieveOptions) ([]*types.SearchResult, error) {
	if m.l3 == nil {
		return nil, fmt.Errorf("L3 not initialized: set embedder first")
	}

	// 使用 L3 层的语义搜索
	return m.l3.SearchWithScores(ctx, query, opts)
}

// searchL3Keyword performs keyword-based search on L3 content
// This is used when SearchMode is "keyword" but we need to search L3
func (m *Memoria) searchL3Keyword(ctx context.Context, query string, opts *types.RetrieveOptions) ([]*types.SearchResult, error) {
	if m.l3 == nil {
		return nil, nil // L3 not initialized, skip
	}

	// L3 doesn't support native keyword search, so we do semantic search
	// and filter results by keyword presence
	results, err := m.l3.SearchWithScores(ctx, query, opts)
	if err != nil {
		return nil, err
	}

	// Filter results by keyword presence
	queryLower := strings.ToLower(query)
	queryTerms := strings.Fields(queryLower)
	filtered := make([]*types.SearchResult, 0)

	for _, r := range results {
		// Check if query or any query term appears in content
		contentLower := strings.ToLower(r.Item.Content)
		summaryLower := strings.ToLower(r.Item.Summary)

		hasMatch := false
		if strings.Contains(contentLower, queryLower) || strings.Contains(summaryLower, queryLower) {
			hasMatch = true
		} else {
			for _, term := range queryTerms {
				if strings.Contains(contentLower, term) || strings.Contains(summaryLower, term) {
					hasMatch = true
					break
				}
			}
		}

		if hasMatch {
			r.MatchType = "keyword" // Override match type
			filtered = append(filtered, r)
		}
	}

	return filtered, nil
}

// calculateKeywordScore calculates keyword match score
func (m *Memoria) calculateKeywordScore(item *types.MemoryItem, queryLower string, queryTerms []string) float64 {
	summaryLower := strings.ToLower(item.Summary)

	// 完全匹配 Summary
	if strings.Contains(summaryLower, queryLower) {
		return 1.0
	}

	// L1/L2 only have Summary, L3 has Content for semantic search
	contentLower := strings.ToLower(item.Content)
	if contentLower != "" && strings.Contains(contentLower, queryLower) {
		return 0.9
	}

	// 关键词匹配
	summaryTerms := strings.Fields(summaryLower)

	summaryMatch := 0
	for _, qt := range queryTerms {
		for _, st := range summaryTerms {
			if st == qt {
				summaryMatch++
			}
		}
	}

	if len(queryTerms) == 0 {
		return 0
	}

	summaryScore := float64(summaryMatch) / float64(len(queryTerms))

	// If Content exists (L3), include it in scoring with lower weight
	if contentLower != "" {
		contentTerms := strings.Fields(contentLower)
		contentMatch := 0
		for _, qt := range queryTerms {
			for _, ct := range contentTerms {
				if ct == qt {
					contentMatch++
				}
			}
		}
		contentScore := float64(contentMatch) / float64(len(queryTerms)*2)
		return summaryScore*0.7 + contentScore*0.3
	}

	return summaryScore
}

// sortResultsByScore sorts results by score descending
func sortResultsByScore(results []*types.SearchResult) {
	for i := 0; i < len(results)-1; i++ {
		for j := i + 1; j < len(results); j++ {
			if results[i].Score < results[j].Score {
				results[i], results[j] = results[j], results[i]
			}
		}
	}
}

// ProcessChat processes chat content for memory extraction
func (m *Memoria) ProcessChat(ctx context.Context, source, content string, userCtx types.UserIdentity) (*types.ProcessingResult, error) {
	if m.chatProcessor == nil {
		return nil, fmt.Errorf("chat processor not initialized - set a chat model first")
	}

	slog.Info("Processing chat content",
		"source", source,
		"content_len", len(content),
		"user_id", userCtx.GetUserID())

	startTime := time.Now()
	result, err := m.chatProcessor.Process(ctx, source, content, userCtx)
	if err != nil {
		slog.Error("Chat processing failed", "source", source, "error", err)
		return nil, err
	}

	// Store extracted memories
	storedCount := 0
	for _, item := range result.Items {
		if err := m.AddMemory(ctx, item); err != nil {
			result.Errors = append(result.Errors, err)
			slog.Warn("Failed to store memory item", "id", item.ID, "error", err)
		} else {
			storedCount++
		}
	}

	slog.Info("Chat processing completed",
		"source", source,
		"items", len(result.Items),
		"stored", storedCount,
		"errors", len(result.Errors),
		"duration", time.Since(startTime).Milliseconds())

	return result, nil
}

// ProcessFile processes file content for memory extraction
func (m *Memoria) ProcessFile(ctx context.Context, source, content string, userCtx types.UserIdentity) (*types.ProcessingResult, error) {
	if m.fileProcessor == nil {
		return nil, fmt.Errorf("file processor not initialized - set a chat model first")
	}

	slog.Info("Processing file content",
		"source", source,
		"content_len", len(content),
		"user_id", userCtx.GetUserID())

	startTime := time.Now()
	result, err := m.fileProcessor.Process(ctx, source, content, userCtx)
	if err != nil {
		slog.Error("File processing failed", "source", source, "error", err)
		return nil, err
	}

	// Store extracted memories
	storedCount := 0
	for _, item := range result.Items {
		if err := m.AddMemory(ctx, item); err != nil {
			result.Errors = append(result.Errors, err)
			slog.Warn("Failed to store memory item", "id", item.ID, "error", err)
		} else {
			storedCount++
		}
	}

	slog.Info("File processing completed",
		"source", source,
		"items", len(result.Items),
		"stored", storedCount,
		"errors", len(result.Errors),
		"duration", time.Since(startTime).Milliseconds())

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

// GetStorage returns the storage interface
func (m *Memoria) GetStorage() types.Storage {
	return m.storage
}
