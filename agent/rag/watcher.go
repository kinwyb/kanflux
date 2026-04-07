package rag

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// FileWatcher 文件监控器
type FileWatcher struct {
	watcher         *fsnotify.Watcher
	manager         *RAGManager
	paths           map[string]*KnowledgePath // 监控的路径配置
	mu              sync.RWMutex
	ctx             context.Context
	cancel          context.CancelFunc
	debounce        map[string]time.Time // 事件去抖
	debounceMu      sync.Mutex
	debounceInterval time.Duration
}

// NewFileWatcher 创建文件监控器
func NewFileWatcher(manager *RAGManager) (*FileWatcher, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	return &FileWatcher{
		watcher:          fw,
		manager:          manager,
		paths:            make(map[string]*KnowledgePath),
		debounce:         make(map[string]time.Time),
		debounceInterval: 2 * time.Second, // 2秒去抖
	}, nil
}

// AddPath 添加监控路径
func (w *FileWatcher) AddPath(kp *KnowledgePath) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	absPath := kp.Path
	w.paths[absPath] = kp

	// 添加监控
	if kp.Recursive {
		return w.addRecursiveWatch(absPath)
	}
	return w.watcher.Add(absPath)
}

// RemovePath 移除监控路径
func (w *FileWatcher) RemovePath(path string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	delete(w.paths, path)
	w.watcher.Remove(path)
}

// addRecursiveWatch 递归添加目录监控
func (w *FileWatcher) addRecursiveWatch(root string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// 只监控目录
		if info.IsDir() {
			if err := w.watcher.Add(path); err != nil {
				slog.Debug("Failed to add watch", "path", path, "error", err)
			}
		}
		return nil
	})
}

// Start 启动监控
func (w *FileWatcher) Start(ctx context.Context) error {
	w.ctx, w.cancel = context.WithCancel(ctx)

	go w.processEvents()
	slog.Info("File watcher started")
	return nil
}

// processEvents 处理文件系统事件
func (w *FileWatcher) processEvents() {
	for {
		select {
		case <-w.ctx.Done():
			return
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			w.handleEvent(event)
		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			slog.Error("Watcher error", "error", err)
		}
	}
}

// handleEvent 处理单个事件（带去抖）
func (w *FileWatcher) handleEvent(event fsnotify.Event) {
	// 检查是否需要处理该文件类型
	if !w.shouldProcess(event.Name) {
		return
	}

	// 去抖处理
	w.debounceMu.Lock()
	lastTime, exists := w.debounce[event.Name]
	now := time.Now()
	if exists && now.Sub(lastTime) < w.debounceInterval {
		w.debounceMu.Unlock()
		return
	}
	w.debounce[event.Name] = now
	w.debounceMu.Unlock()

	ctx := context.Background()

	switch {
	case event.Op&fsnotify.Create == fsnotify.Create:
		slog.Info("File created", "path", event.Name)
		if err := w.manager.IndexFile(ctx, event.Name); err != nil {
			slog.Warn("Failed to index new file", "path", event.Name, "error", err)
		}

	case event.Op&fsnotify.Write == fsnotify.Write:
		slog.Info("File modified", "path", event.Name)
		if err := w.manager.IndexFile(ctx, event.Name); err != nil {
			slog.Warn("Failed to reindex modified file", "path", event.Name, "error", err)
		}

	case event.Op&fsnotify.Remove == fsnotify.Remove:
		slog.Info("File removed", "path", event.Name)
		if err := w.manager.RemoveFile(ctx, event.Name); err != nil {
			slog.Warn("Failed to remove file from index", "path", event.Name, "error", err)
		}

	case event.Op&fsnotify.Rename == fsnotify.Rename:
		// Rename 通常会导致 Remove 事件
		slog.Info("File renamed", "path", event.Name)
		if err := w.manager.RemoveFile(ctx, event.Name); err != nil {
			slog.Warn("Failed to remove renamed file from index", "path", event.Name, "error", err)
		}
	}
}

// shouldProcess 检查是否应该处理该文件
func (w *FileWatcher) shouldProcess(filePath string) bool {
	w.mu.RLock()
	defer w.mu.RUnlock()

	// 检查每个路径配置
	for _, kp := range w.paths {
		// 检查是否在监控范围内
		if !w.isUnderPath(filePath, kp.Path, kp.Recursive) {
			continue
		}

		// 检查扩展名
		if len(kp.Extensions) > 0 {
			ext := strings.ToLower(filepath.Ext(filePath))
			found := false
			for _, allowedExt := range kp.Extensions {
				allowed := strings.ToLower(allowedExt)
				if ext == allowed || ext == "."+allowed {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}

		// 检查排除模式
		for _, pattern := range kp.Exclude {
			matched, _ := filepath.Match(pattern, filepath.Base(filePath))
			if matched {
				return false
			}
		}

		return true
	}
	return false
}

// isUnderPath 检查文件是否在指定路径下
func (w *FileWatcher) isUnderPath(filePath, dir string, recursive bool) bool {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return false
	}

	rel, err := filepath.Rel(dir, absPath)
	if err != nil {
		return false
	}

	// 如果相对路径以 .. 开头，说明不在目录下
	if strings.HasPrefix(rel, "..") {
		return false
	}

	// 如果非递归，只允许直接子文件
	if !recursive {
		return !strings.Contains(rel, string(filepath.Separator))
	}

	return true
}

// Stop 停止监控
func (w *FileWatcher) Stop() error {
	if w.cancel != nil {
		w.cancel()
	}
	if w.watcher != nil {
		return w.watcher.Close()
	}
	return nil
}