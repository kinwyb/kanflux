package rag

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"

	"github.com/cloudwego/eino/components/embedding"
	"github.com/kinwyb/kanflux/agent/tools"
)

// RAGManagerInterface RAG 管理器接口
type RAGManagerInterface interface {
	Close() error
	GetKnowledgeTool() tools.Tool
}

// Manager RAG 管理器接口
type Manager interface {
	// Initialize 初始化 RAG 系统，加载已有索引
	Initialize(ctx context.Context) error

	// AddPath 添加知识库路径
	AddPath(ctx context.Context, kp *KnowledgePath) error

	// RemovePath 移除知识库路径
	RemovePath(ctx context.Context, path string) error

	// IndexFile 索引单个文件
	IndexFile(ctx context.Context, filePath string) error

	// RemoveFile 从索引中移除文件
	RemoveFile(ctx context.Context, filePath string) error

	// ReindexAll 重新索引所有文件
	ReindexAll(ctx context.Context) error

	// Retrieve 检索相关文档
	Retrieve(ctx context.Context, query string, opts ...RetrieveOption) ([]*Result, error)

	// GetStats 获取统计信息
	GetStats() *Stats

	// StartWatcher 启动文件监控
	StartWatcher(ctx context.Context) error

	// StopWatcher 停止文件监控
	StopWatcher() error

	// Close 关闭 RAG 系统
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

// RAGManager RAG 管理器实现
type RAGManager struct {
	config    *Config
	store     VectorStore
	parser    *Parser
	chunker   *Chunker
	indexer   *Indexer
	retriever *Retriever
	watcher   *FileWatcher

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

	// 创建存储
	storeType := cfg.StoreType
	if storeType == "" {
		storeType = StoreTypeJSON // 默认使用 JSON 存储
	}
	store, err := NewVectorStore(&StoreConfig{
		Type:      storeType,
		Workspace: cfg.Workspace,
		Options:   cfg.StoreOptions,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create store: %w", err)
	}

	// 创建组件
	parser := NewParser()
	chunker := NewChunker(cfg.ChunkSize, cfg.ChunkOverlap)
	indexer := NewIndexer(store, chunker, cfg.Embedder)
	retriever := NewRetriever(store, cfg.Embedder, cfg.TopK, cfg.ScoreThreshold)

	mgr := &RAGManager{
		config:    cfg,
		store:     store,
		parser:    parser,
		chunker:   chunker,
		indexer:   indexer,
		retriever: retriever,
		paths:     make(map[string]*KnowledgePath),
	}

	// 创建文件监控器
	if cfg.EnableWatcher {
		watcher, err := NewFileWatcher(mgr)
		if err != nil {
			return nil, fmt.Errorf("failed to create watcher: %w", err)
		}
		mgr.watcher = watcher
	}

	return mgr, nil
}

// Initialize 初始化
func (m *RAGManager) Initialize(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.initialized {
		return nil
	}

	// 加载已有索引
	if err := m.store.Load(); err != nil && !isNotExist(err) {
		return fmt.Errorf("failed to load store: %w", err)
	}

	// 添加配置的知识库路径
	for _, kp := range m.config.KnowledgePaths {
		if err := m.addPathInternal(ctx, &kp); err != nil {
			slog.Warn("Failed to add knowledge path", "path", kp.Path, "error", err)
		}
	}

	// 保存元数据
	metadata := m.store.GetMetadata()
	metadata.KnowledgePaths = m.config.KnowledgePaths
	metadata.CreatedAt = 0
	m.store.SetMetadata(metadata)

	m.initialized = true
	slog.Info("RAG manager initialized", "documents", m.store.GetStats().TotalDocuments)
	return nil
}

// AddPath 添加知识库路径
func (m *RAGManager) AddPath(ctx context.Context, kp *KnowledgePath) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.addPathInternal(ctx, kp)
}

// addPathInternal 内部添加路径方法（不加锁）
func (m *RAGManager) addPathInternal(ctx context.Context, kp *KnowledgePath) error {
	absPath, err := filepath.Abs(kp.Path)
	if err != nil {
		return fmt.Errorf("failed to resolve path: %w", err)
	}
	kp.Path = absPath

	// 检查是否已添加
	if _, exists := m.paths[absPath]; exists {
		return nil // 已存在，忽略
	}

	// 扫描文件
	scanner := NewFileScanner(m.parser, kp.Extensions, kp.Exclude)
	files, err := scanner.ScanDir(absPath, kp.Recursive)
	if err != nil {
		return fmt.Errorf("failed to scan directory: %w", err)
	}

	// 解析并索引文件
	docs, err := m.parser.ParseFiles(files)
	if err != nil {
		slog.Warn("Some files failed to parse", "error", err)
	}

	if len(docs) > 0 {
		if err := m.indexer.IndexDocuments(ctx, docs); err != nil {
			return fmt.Errorf("failed to index documents: %w", err)
		}
	}

	// 记录路径
	m.paths[absPath] = kp

	// 添加到监控
	if m.watcher != nil {
		if err := m.watcher.AddPath(kp); err != nil {
			slog.Warn("Failed to add path to watcher", "path", absPath, "error", err)
		}
	}

	slog.Info("Knowledge path added", "path", absPath, "files", len(files))
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

	kp, exists := m.paths[absPath]
	if !exists {
		return nil
	}

	// 扫描并移除所有文件
	scanner := NewFileScanner(m.parser, kp.Extensions, kp.Exclude)
	files, err := scanner.ScanDir(absPath, kp.Recursive)
	if err != nil {
		return err
	}

	for _, file := range files {
		m.removeFileInternal(file)
	}

	// 从监控移除
	if m.watcher != nil {
		m.watcher.RemovePath(absPath)
	}

	// 删除路径记录
	delete(m.paths, absPath)

	// 保存
	return m.store.Save()
}

// IndexFile 索引单个文件
func (m *RAGManager) IndexFile(ctx context.Context, filePath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 检查文件是否在监控路径范围内
	if !m.isUnderKnowledgePath(filePath) {
		return fmt.Errorf("file not in knowledge paths: %s", filePath)
	}

	doc, err := m.parser.ParseFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to parse file: %w", err)
	}

	if err := m.indexer.IndexDocument(ctx, doc); err != nil {
		return fmt.Errorf("failed to index document: %w", err)
	}

	return m.store.Save()
}

// RemoveFile 移除文件索引
func (m *RAGManager) RemoveFile(ctx context.Context, filePath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.removeFileInternal(filePath)
	return m.store.Save()
}

// removeFileInternal 内部移除文件方法（不加锁）
func (m *RAGManager) removeFileInternal(filePath string) {
	doc, exists := m.store.GetDocumentByPath(filePath)
	if !exists {
		return
	}

	m.indexer.RemoveDocument(doc.ID)
	slog.Info("File removed from index", "path", filePath)
}

// ReindexAll 重新索引所有文件
func (m *RAGManager) ReindexAll(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 收集所有文件
	var allFiles []string
	for _, kp := range m.paths {
		scanner := NewFileScanner(m.parser, kp.Extensions, kp.Exclude)
		files, err := scanner.ScanDir(kp.Path, kp.Recursive)
		if err != nil {
			slog.Warn("Failed to scan path", "path", kp.Path, "error", err)
			continue
		}
		allFiles = append(allFiles, files...)
	}

	// 解析所有文件
	docs, err := m.parser.ParseFiles(allFiles)
	if err != nil {
		slog.Warn("Some files failed to parse", "error", err)
	}

	// 重新索引
	if err := m.indexer.ReindexAll(ctx, docs); err != nil {
		return fmt.Errorf("failed to reindex: %w", err)
	}

	slog.Info("Reindexed all files", "total", len(docs))
	return nil
}

// Retrieve 检索相关文档
func (m *RAGManager) Retrieve(ctx context.Context, query string, opts ...RetrieveOption) ([]*Result, error) {
	docs, err := m.retriever.Retrieve(ctx, query, opts...)
	if err != nil {
		return nil, err
	}

	results := make([]*Result, len(docs))
	for i, doc := range docs {
		results[i] = &Result{
			Content:    doc.Content,
			SourcePath: doc.MetaData["source_path"].(string),
			Score:      doc.MetaData["score"].(float64),
			ChunkID:    doc.MetaData["chunk_id"].(string),
			DocumentID: doc.MetaData["document_id"].(string),
			Metadata:   doc.MetaData,
		}
	}

	return results, nil
}

// GetStats 获取统计信息
func (m *RAGManager) GetStats() *Stats {
	return m.store.GetStats()
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
	return m.store.Close()
}

// isUnderKnowledgePath 检查文件是否在知识库路径范围内
func (m *RAGManager) isUnderKnowledgePath(filePath string) bool {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return false
	}

	for _, kp := range m.paths {
		if m.isUnderPath(absPath, kp.Path, kp.Recursive) {
			// 检查扩展名和排除
			ext := filepath.Ext(absPath)
			if len(kp.Extensions) > 0 {
				found := false
				for _, allowed := range kp.Extensions {
					if ext == "."+allowed || ext == allowed {
						found = true
						break
					}
				}
				if !found {
					return false
				}
			}

			for _, pattern := range kp.Exclude {
				matched, _ := filepath.Match(pattern, filepath.Base(absPath))
				if matched {
					return false
				}
			}

			return true
		}
	}

	return false
}

// isUnderPath 检查路径是否在指定目录下
func (m *RAGManager) isUnderPath(path, dir string, recursive bool) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}

	// 如果相对路径以 .. 开头，说明不在目录下
	if strings.HasPrefix(rel, "..") {
		return false
	}

	// 如果非递归，只允许直接子文件
	if !recursive {
		// 只允许当前目录下的文件，不允许子目录
		return !strings.Contains(rel, string(filepath.Separator))
	}

	return true
}

// isNotExist 检查是否是文件不存在错误
func isNotExist(err error) bool {
	return err != nil && (err.Error() == "file does not exist" || err.Error() == "no such file or directory")
}

// GetEmbedder 获取 Embedder
func (m *RAGManager) GetEmbedder() embedding.Embedder {
	return m.config.Embedder
}

// GetConfig 获取配置
func (m *RAGManager) GetConfig() *Config {
	return m.config
}

// GetKnowledgeTool 获取知识检索工具
func (m *RAGManager) GetKnowledgeTool() tools.Tool {
	return NewKnowledgeTool(m)
}
