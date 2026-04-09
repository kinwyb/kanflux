package processor

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/kinwyb/kanflux/knowledgebase/memoria/llm"
	"github.com/kinwyb/kanflux/knowledgebase/memoria/types"
)

// WatchPath alias for backward compatibility
type WatchPath = types.WatchPath

// FileProcessor processes file modifications for memory extraction
type FileProcessor struct {
	*BaseProcessor
	watchPaths []WatchPath
	watcher    *FileWatcher
}

// NewFileProcessor creates a file processor
func NewFileProcessor(summarizer types.Summarizer, config *ProcessorConfig, watchPaths []WatchPath) *FileProcessor {
	return &FileProcessor{
		BaseProcessor: NewBaseProcessor(summarizer, config),
		watchPaths:    watchPaths,
	}
}

// Name returns the processor name
func (p *FileProcessor) Name() string {
	return "file_processor"
}

// Process processes a single file using FileSummarizePrompt
func (p *FileProcessor) Process(ctx context.Context, source string, content string, userCtx types.UserIdentity) (*types.ProcessingResult, error) {
	result := &types.ProcessingResult{
		Items:       make([]*types.MemoryItem, 0),
		LayerCounts: make(map[types.Layer]int),
		HallCounts:  make(map[types.HallType]int),
	}

	chunks := p.chunkContent(content, p.Config.MaxBatchSize)

	for _, chunk := range chunks {
		// Use the specialized ProcessFileContent which uses FileSummarizePrompt
		summarizer := llm.NewSummarizer(p.Summarizer.(*llm.SummarizerImpl).Model, 500)
		items, err := summarizer.ProcessFileContent(ctx, chunk, source, userCtx)
		if err != nil {
			// Fallback to generic ProcessContent
			items, err = summarizer.ProcessContent(ctx, chunk, userCtx)
			if err != nil {
				result.Errors = append(result.Errors, err)
				continue
			}
		}

		for _, item := range items {
			item.Source = source
			result.Items = append(result.Items, item)
			result.LayerCounts[item.Layer]++
			result.HallCounts[item.HallType]++
		}
	}

	return result, nil
}

// ProcessBatch processes multiple files
func (p *FileProcessor) ProcessBatch(ctx context.Context, items []types.ProcessItem) (*types.ProcessingResult, error) {
	result := &types.ProcessingResult{
		Items:       make([]*types.MemoryItem, 0),
		LayerCounts: make(map[types.Layer]int),
		HallCounts:  make(map[types.HallType]int),
	}

	if p.Config.EnableParallel {
		return p.processBatchParallel(ctx, items)
	}

	for _, item := range items {
		r, err := p.Process(ctx, item.Source, item.Content, item.UserCtx)
		if err != nil {
			result.Errors = append(result.Errors, err)
			continue
		}
		p.mergeResults(result, r)
	}

	return result, nil
}

func (p *FileProcessor) processBatchParallel(ctx context.Context, items []types.ProcessItem) (*types.ProcessingResult, error) {
	result := &types.ProcessingResult{
		Items:       make([]*types.MemoryItem, 0),
		LayerCounts: make(map[types.Layer]int),
		HallCounts:  make(map[types.HallType]int),
	}

	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, item := range items {
		wg.Add(1)
		go func(item types.ProcessItem) {
			defer wg.Done()
			r, err := p.Process(ctx, item.Source, item.Content, item.UserCtx)
			if err != nil {
				mu.Lock()
				result.Errors = append(result.Errors, err)
				mu.Unlock()
				return
			}
			mu.Lock()
			p.mergeResults(result, r)
			mu.Unlock()
		}(item)
	}

	wg.Wait()
	return result, nil
}

// ScanModifiedFiles scans for files modified since the given time
func (p *FileProcessor) ScanModifiedFiles(ctx context.Context, since time.Time) ([]types.ProcessItem, error) {
	items := make([]types.ProcessItem, 0)

	for _, wp := range p.watchPaths {
		files, err := p.scanPath(wp, since)
		if err != nil {
			slog.Warn("Failed to scan path", "path", wp.Path, "error", err)
			continue
		}
		items = append(items, files...)
	}

	return items, nil
}

func (p *FileProcessor) scanPath(wp WatchPath, since time.Time) ([]types.ProcessItem, error) {
	items := make([]types.ProcessItem, 0)

	err := filepath.Walk(wp.Path, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		if info.IsDir() {
			for _, excl := range wp.Exclude {
				if strings.Contains(path, excl) {
					if !wp.Recursive {
						return filepath.SkipDir
					}
				}
			}
			return nil
		}

		ext := filepath.Ext(path)
		if !p.hasExtension(ext, wp.Extensions) {
			return nil
		}

		if info.ModTime().Before(since) {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			slog.Warn("Failed to read file", "file", path, "error", err)
			return nil
		}

		items = append(items, types.ProcessItem{
			Source:    path,
			Content:   string(content),
			UserCtx:   &types.DefaultUserIdentity{UserID: "default"},
			Timestamp: info.ModTime(),
		})

		return nil
	})

	return items, err
}

func (p *FileProcessor) hasExtension(ext string, extensions []string) bool {
	if len(extensions) == 0 {
		return true
	}
	for _, e := range extensions {
		if strings.EqualFold(ext, e) {
			return true
		}
	}
	return false
}

func (p *FileProcessor) chunkContent(content string, maxChunkSize int) []string {
	if len(content) <= maxChunkSize {
		return []string{content}
	}

	chunks := make([]string, 0)
	lines := strings.Split(content, "\n")

	var currentChunk strings.Builder
	for _, line := range lines {
		if currentChunk.Len()+len(line) > maxChunkSize {
			if currentChunk.Len() > 0 {
				chunks = append(chunks, currentChunk.String())
				currentChunk.Reset()
			}
		}
		currentChunk.WriteString(line + "\n")
	}

	if currentChunk.Len() > 0 {
		chunks = append(chunks, currentChunk.String())
	}

	return chunks
}

// StartWatcher starts watching the configured paths
func (p *FileProcessor) StartWatcher(ctx context.Context) error {
	if p.watcher != nil {
		return nil
	}

	p.watcher = NewFileWatcher(p.watchPaths)
	return p.watcher.Start(ctx)
}

// StopWatcher stops the file watcher
func (p *FileProcessor) StopWatcher() error {
	if p.watcher == nil {
		return nil
	}
	return p.watcher.Stop()
}

// GetModifiedFiles returns files modified since watcher started
func (p *FileProcessor) GetModifiedFiles() []string {
	if p.watcher == nil {
		return nil
	}
	return p.watcher.GetModifiedFiles()
}

func (p *FileProcessor) mergeResults(dst, src *types.ProcessingResult) {
	dst.Items = append(dst.Items, src.Items...)
	for layer, count := range src.LayerCounts {
		dst.LayerCounts[layer] += count
	}
	for hall, count := range src.HallCounts {
		dst.HallCounts[hall] += count
	}
	dst.Errors = append(dst.Errors, src.Errors...)
}

// FileWatcher watches for file modifications
type FileWatcher struct {
	watchPaths    []WatchPath
	modifiedFiles map[string]time.Time
	modifiedMu    sync.Mutex
	debounce      time.Duration
	ctx           context.Context
	cancel        context.CancelFunc
	running       bool
}

// NewFileWatcher creates a new file watcher
func NewFileWatcher(watchPaths []WatchPath) *FileWatcher {
	return &FileWatcher{
		watchPaths:    watchPaths,
		modifiedFiles: make(map[string]time.Time),
		debounce:      2 * time.Second,
	}
}

// Start starts watching the configured paths
func (w *FileWatcher) Start(ctx context.Context) error {
	w.ctx, w.cancel = context.WithCancel(ctx)
	w.running = true
	go w.pollLoop()
	return nil
}

// Stop stops the watcher
func (w *FileWatcher) Stop() error {
	if w.cancel != nil {
		w.cancel()
	}
	w.running = false
	return nil
}

func (w *FileWatcher) pollLoop() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-w.ctx.Done():
			return
		case <-ticker.C:
			w.pollModifications()
		}
	}
}

func (w *FileWatcher) pollModifications() {
	for _, wp := range w.watchPaths {
		filepath.Walk(wp.Path, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}

			ext := filepath.Ext(path)
			for _, e := range wp.Extensions {
				if strings.EqualFold(ext, e) {
					w.modifiedMu.Lock()
					if time.Since(info.ModTime()) < 30*time.Second {
						w.modifiedFiles[path] = info.ModTime()
					}
					w.modifiedMu.Unlock()
					break
				}
			}
			return nil
		})
	}
}

// GetModifiedFiles returns the list of modified files
func (w *FileWatcher) GetModifiedFiles() []string {
	w.modifiedMu.Lock()
	defer w.modifiedMu.Unlock()

	files := make([]string, 0, len(w.modifiedFiles))
	for file := range w.modifiedFiles {
		files = append(files, file)
	}
	w.modifiedFiles = make(map[string]time.Time)
	return files
}