package rag

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/kinwyb/kanflux/knowledgebase"
)

// RAGManagerInterface RAG 管理器接口
type RAGManagerInterface interface {
	Close() error
	GetKnowledgeTool() KnowledgeToolInterface
	GetStats() *Stats
}

// Manager RAG 管理器接口
type Manager interface {
	Initialize(ctx context.Context) error
	AddPath(ctx context.Context, kp *KnowledgePath) error
	RemovePath(ctx context.Context, path string) error
	IndexFile(ctx context.Context, filePath string) error
	RemoveFile(ctx context.Context, filePath string) error
	ReindexAll(ctx context.Context) error
	Retrieve(ctx context.Context, query string, opts ...RetrieveOption) ([]*Result, error)
	GetStats() *Stats
	StartWatcher(ctx context.Context) error
	StopWatcher() error
	Close() error
}

// Result 检索结果
type Result struct {
	Content    string         `json:"content"`
	SourcePath string         `json:"source_path"`
	Score      float64        `json:"score"`
	ChunkID    string         `json:"chunk_id"`
	DocumentID string         `json:"document_id"`
	Metadata   map[string]any `json:"metadata"`
}

// Stats 统计信息
type Stats struct {
	TotalDocuments  int    `json:"total_documents"`
	TotalChunks     int    `json:"total_chunks"`
	TotalVectors    int    `json:"total_vectors"`
	TotalPaths      int    `json:"total_paths"`
	LastUpdateTime  int64  `json:"last_update_time"`
	IndexPath       string `json:"index_path"`
}

// RAGManager RAG 管理器实现
type RAGManager struct {
	config   *Config
	kb       *knowledgebase.KnowledgeBase
	parser   *Parser
	watcher  *FileWatcher

	paths map[string]*KnowledgePath // 已添加的知识库路径
	mu    sync.RWMutex

	initialized bool
}

// NewManager 创建 RAG 管理器
func NewManager(ctx context.Context, cfg *Config) (*RAGManager, error) {
	if cfg.Embedder == nil {
		return nil, fmt.Errorf("embedder is required")
	}
	if cfg.Workspace == "" {
		return nil, fmt.Errorf("workspace is required")
	}

	// 创建 KnowledgeBase
	kbCfg := knowledgebase.DefaultConfig()
	kbCfg.Workspace = filepath.Join(cfg.Workspace, ".kanflux", "knowledge")
	kbCfg.Embedder = knowledgebase.NewEinoEmbedder(cfg.Embedder, cfg.EmbeddingModel)
	kbCfg.ChunkSize = cfg.ChunkSize
	kbCfg.ChunkOverlap = cfg.ChunkOverlap
	kbCfg.DefaultLimit = cfg.TopK
	kbCfg.DefaultThreshold = cfg.ScoreThreshold
	kbCfg.StoreType = cfg.StoreType // 支持配置选择 sqlite 或 json

	knowledgeBase, err := knowledgebase.New(kbCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create knowledge base: %w", err)
	}

	mgr := &RAGManager{
		config:  cfg,
		kb:      knowledgeBase,
		parser:  NewParser(),
		paths:   make(map[string]*KnowledgePath),
	}

	// 创建文件监控器
	if cfg.EnableWatcher {
		watcher, err := NewFileWatcher(mgr)
		if err != nil {
			slog.Warn("[RAG] Failed to create watcher", "error", err)
		} else {
			mgr.watcher = watcher
		}
	}

	return mgr, nil
}

// LogFunc 日志回调函数类型
type LogFunc func(level, source, message string)

// InitializeAsync 异步初始化
func (m *RAGManager) InitializeAsync(log LogFunc) {
	go func() {
		initCtx := context.Background()

		if log != nil {
			log("info", "rag", "[RAG] Starting async initialization...")
		}

		if err := m.Initialize(initCtx); err != nil {
			if log != nil {
				log("error", "rag", fmt.Sprintf("[RAG] Failed to initialize: %v", err))
			}
			return
		}

		stats := m.GetStats()
		if log != nil {
			log("info", "rag", fmt.Sprintf("[RAG] Initialized: %d documents",
				stats.TotalDocuments))
		}

		// 启动文件监控
		if m.watcher != nil {
			if err := m.StartWatcher(initCtx); err != nil {
				if log != nil {
					log("warn", "rag", fmt.Sprintf("[RAG] Failed to start watcher: %v", err))
				}
			} else if log != nil {
				log("info", "rag", "[RAG] File watcher started")
			}
		}
	}()
}

// Initialize 初始化
func (m *RAGManager) Initialize(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.initialized {
		return nil
	}

	// 添加配置的知识库路径
	for i := range m.config.KnowledgePaths {
		kp := &m.config.KnowledgePaths[i]
		if err := m.addPathInternal(ctx, kp); err != nil {
			slog.Warn("[RAG] Failed to add knowledge path", "path", kp.Path, "error", err)
		}
	}

	m.initialized = true
	slog.Info("[RAG] Initialization completed", "paths", len(m.paths))
	return nil
}

// AddPath 添加知识库路径
func (m *RAGManager) AddPath(ctx context.Context, kp *KnowledgePath) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.addPathInternal(ctx, kp)
}

// addPathInternal 内部添加路径方法
func (m *RAGManager) addPathInternal(ctx context.Context, kp *KnowledgePath) error {
	absPath, err := filepath.Abs(kp.Path)
	if err != nil {
		return fmt.Errorf("failed to resolve path: %w", err)
	}
	kp.Path = absPath

	// 检查是否已添加
	if _, exists := m.paths[absPath]; exists {
		return nil
	}

	// 扫描文件
	files, err := m.scanPath(kp)
	if err != nil {
		return fmt.Errorf("failed to scan directory: %w", err)
	}

	if len(files) == 0 {
		m.paths[absPath] = kp
		return nil
	}

	// 索引文件
	for _, file := range files {
		if err := m.indexFileInternal(ctx, file, kp); err != nil {
			slog.Warn("[RAG] Failed to index file", "file", file, "error", err)
		}
	}

	// 记录路径
	m.paths[absPath] = kp

	// 添加到监控
	if m.watcher != nil {
		if err := m.watcher.AddPath(kp); err != nil {
			slog.Warn("[RAG] Failed to add path to watcher", "path", absPath, "error", err)
		}
	}

	slog.Info("[RAG] Knowledge path added", "path", absPath, "files", len(files))
	return nil
}

// RemovePath 移除知识库路径
func (m *RAGManager) RemovePath(ctx context.Context, path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}

	_, exists := m.paths[absPath]
	if !exists {
		return nil
	}

	// 从 KB 中删除该路径下的所有文档
	// 使用 room（目录名）来标识
	room := filepath.Base(absPath)
	if err := m.kb.DeleteByRoom(ctx, "rag", room); err != nil {
		slog.Warn("[RAG] Failed to delete from knowledge base", "error", err)
	}

	// 从监控移除
	if m.watcher != nil {
		m.watcher.RemovePath(absPath)
	}

	// 删除路径记录
	delete(m.paths, absPath)

	slog.Info("[RAG] Knowledge path removed", "path", absPath)
	return nil
}

// IndexFile 索引单个文件
func (m *RAGManager) IndexFile(ctx context.Context, filePath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 检查文件是否在监控路径范围内
	kp := m.findKnowledgePath(filePath)
	if kp == nil {
		return fmt.Errorf("file not in knowledge paths: %s", filePath)
	}

	return m.indexFileInternal(ctx, filePath, kp)
}

// indexFileInternal 内部索引文件方法
func (m *RAGManager) indexFileInternal(ctx context.Context, filePath string, kp *KnowledgePath) error {
	// 解析文件
	doc, err := m.parser.ParseFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to parse file: %w", err)
	}

	// 确定房间（目录名）
	room := filepath.Base(filepath.Dir(filePath))
	if room == "" || room == "." {
		room = "general"
	}

	// 存储到 KnowledgeBase
	_, err = m.kb.Add(ctx, doc.Content,
		knowledgebase.WithWing("rag"),
		knowledgebase.WithRoom(room),
		knowledgebase.WithSource(filePath),
		knowledgebase.WithMetadata(map[string]any{
			"source_path": filePath,
			"extension":   filepath.Ext(filePath),
			"filename":    filepath.Base(filePath),
			"mod_time":    doc.ModTime,
		}),
	)

	return err
}

// RemoveFile 从索引中移除文件
func (m *RAGManager) RemoveFile(ctx context.Context, filePath string) error {
	// 生成文档ID并删除
	docID := GenerateDocumentID(filePath)
	return m.kb.Delete(ctx, docID)
}

// ReindexAll 重新索引所有文件
func (m *RAGManager) ReindexAll(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 清空 rag wing
	if err := m.kb.DeleteByWing(ctx, "rag"); err != nil {
		return fmt.Errorf("failed to clear knowledge base: %w", err)
	}

	// 重新索引所有路径
	for _, kp := range m.paths {
		files, err := m.scanPath(kp)
		if err != nil {
			slog.Warn("[RAG] Failed to scan path", "path", kp.Path, "error", err)
			continue
		}

		for _, file := range files {
			if err := m.indexFileInternal(ctx, file, kp); err != nil {
				slog.Warn("[RAG] Failed to index file", "file", file, "error", err)
			}
		}
	}

	slog.Info("[RAG] Reindex completed")
	return nil
}

// Retrieve 检索相关文档
func (m *RAGManager) Retrieve(ctx context.Context, query string, opts ...RetrieveOption) ([]*Result, error) {
	cfg := ApplyOptions(opts...)

	searchResults, err := m.kb.Search(ctx, query,
		knowledgebase.WithWingFilter("rag"),
		knowledgebase.WithLimit(cfg.TopK),
	)
	if err != nil {
		return nil, err
	}

	results := make([]*Result, len(searchResults))
	for i, r := range searchResults {
		results[i] = &Result{
			Content:    r.Content,
			SourcePath: r.Source,
			Score:      r.Score,
			DocumentID: r.ID,
			Metadata:   r.Metadata,
		}
	}

	return results, nil
}

// GetStats 获取统计信息
func (m *RAGManager) GetStats() *Stats {
	stats, err := m.kb.Stats(context.Background())
	if err != nil {
		return &Stats{}
	}

	return &Stats{
		TotalDocuments: stats.TotalDocuments,
		TotalPaths:     len(m.paths),
	}
}

// StartWatcher 启动文件监控
func (m *RAGManager) StartWatcher(ctx context.Context) error {
	if m.watcher == nil {
		return fmt.Errorf("watcher not configured")
	}
	return m.watcher.Start(ctx)
}

// StopWatcher 停止文件监控
func (m *RAGManager) StopWatcher() error {
	if m.watcher == nil {
		return nil
	}
	return m.watcher.Stop()
}

// Close 关闭
func (m *RAGManager) Close() error {
	if m.watcher != nil {
		m.watcher.Stop()
	}
	return m.kb.Close()
}

// GetKnowledgeTool 获取知识检索工具
func (m *RAGManager) GetKnowledgeTool() KnowledgeToolInterface {
	return NewKnowledgeTool(m)
}

// ============ 辅助方法 ============

// scanPath 扫描知识库路径
func (m *RAGManager) scanPath(kp *KnowledgePath) ([]string, error) {
	var files []string

	err := filepath.Walk(kp.Path, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			if !kp.Recursive && path != kp.Path {
				return filepath.SkipDir
			}
			return nil
		}

		// 检查扩展名
		if len(kp.Extensions) > 0 {
			ext := strings.ToLower(filepath.Ext(path))
			found := false
			for _, allowed := range kp.Extensions {
				if ext == "."+strings.ToLower(allowed) || ext == strings.ToLower(allowed) {
					found = true
					break
				}
			}
			if !found {
				return nil
			}
		}

		// 检查排除模式
		for _, pattern := range kp.Exclude {
			matched, _ := filepath.Match(pattern, filepath.Base(path))
			if matched {
				return nil
			}
		}

		// 检查是否支持
		if m.parser.IsSupported(path) {
			files = append(files, path)
		}

		return nil
	})

	return files, err
}

// findKnowledgePath 查找文件所属的知识库路径
func (m *RAGManager) findKnowledgePath(filePath string) *KnowledgePath {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return nil
	}

	for _, kp := range m.paths {
		if strings.HasPrefix(absPath, kp.Path) {
			return kp
		}
	}

	return nil
}

// GenerateDocumentID 生成文档ID
func GenerateDocumentID(path string) string {
	return knowledgebase.GenerateDocumentID(path)
}