package memoria

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/kinwyb/kanflux/memoria/types"
)

// Scheduler handles periodic triggering of processors
type Scheduler struct {
	config        *ScheduleConfig
	chatProcessor types.Processor
	fileProcessor types.Processor
	storage       types.Storage

	lastChatScan   time.Time
	lastFileScan   time.Time
	lastCleanup    time.Time
	processedFiles map[string]time.Time

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	mu     sync.RWMutex
}

// NewScheduler creates a new scheduler
func NewScheduler(config *ScheduleConfig, chatProc, fileProc types.Processor, storage types.Storage) *Scheduler {
	return &Scheduler{
		config:         config,
		chatProcessor:  chatProc,
		fileProcessor:  fileProc,
		storage:        storage,
		processedFiles: make(map[string]time.Time),
	}
}

// Start starts the scheduler
func (s *Scheduler) Start(ctx context.Context) error {
	s.ctx, s.cancel = context.WithCancel(ctx)

	s.lastChatScan = time.Now()
	s.lastFileScan = time.Now()
	s.lastCleanup = time.Now()

	s.wg.Add(3)
	go s.chatScanLoop()
	go s.fileScanLoop()
	go s.cleanupLoop()

	slog.Info("Memoria scheduler started")
	return nil
}

// Stop stops the scheduler
func (s *Scheduler) Stop() error {
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()
	slog.Info("Memoria scheduler stopped")
	return nil
}

func (s *Scheduler) chatScanLoop() {
	defer s.wg.Done()

	if s.config.ChatInterval <= 0 {
		s.config.ChatInterval = 5 * time.Minute
	}

	ticker := time.NewTicker(s.config.ChatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.processChats()
		}
	}
}

func (s *Scheduler) fileScanLoop() {
	defer s.wg.Done()

	if s.config.FileInterval <= 0 {
		s.config.FileInterval = 10 * time.Minute
	}

	ticker := time.NewTicker(s.config.FileInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.processFiles()
		}
	}
}

func (s *Scheduler) cleanupLoop() {
	defer s.wg.Done()

	if s.config.CleanupInterval <= 0 {
		s.config.CleanupInterval = 1 * time.Hour
	}

	ticker := time.NewTicker(s.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.cleanup()
		}
	}
}

func (s *Scheduler) processChats() {
	s.mu.Lock()
	lastScan := s.lastChatScan
	s.lastChatScan = time.Now()
	s.mu.Unlock()

	if s.chatProcessor == nil {
		return
	}

	type SessionScanner interface {
		ScanSessions(ctx context.Context, since time.Time) ([]types.ProcessItem, error)
	}

	if scanner, ok := s.chatProcessor.(SessionScanner); ok {
		items, err := scanner.ScanSessions(s.ctx, lastScan)
		if err != nil {
			slog.Error("Failed to scan sessions", "error", err)
			return
		}

		if len(items) == 0 {
			return
		}

		slog.Info("Processing sessions", "count", len(items))

		result, err := s.chatProcessor.ProcessBatch(s.ctx, items)
		if err != nil {
			slog.Error("Failed to process sessions", "error", err)
			return
		}

		if s.storage != nil && len(result.Items) > 0 {
			if err := s.storage.StoreBatch(s.ctx, result.Items); err != nil {
				slog.Error("Failed to store memory items", "error", err)
			}
		}

		slog.Info("Session processing complete",
			"items", len(result.Items),
			"errors", len(result.Errors))
	}
}

func (s *Scheduler) processFiles() {
	s.mu.Lock()
	lastScan := s.lastFileScan
	s.lastFileScan = time.Now()
	s.mu.Unlock()

	if s.fileProcessor == nil {
		return
	}

	type FileScanner interface {
		ScanModifiedFiles(ctx context.Context, since time.Time) ([]types.ProcessItem, error)
	}

	if scanner, ok := s.fileProcessor.(FileScanner); ok {
		items, err := scanner.ScanModifiedFiles(s.ctx, lastScan)
		if err != nil {
			slog.Error("Failed to scan files", "error", err)
			return
		}

		s.mu.Lock()
		filtered := make([]types.ProcessItem, 0)
		for _, item := range items {
			if lastProcessed, exists := s.processedFiles[item.Source]; !exists || item.Timestamp.After(lastProcessed) {
				filtered = append(filtered, item)
				s.processedFiles[item.Source] = item.Timestamp
			}
		}
		s.mu.Unlock()

		if len(filtered) == 0 {
			return
		}

		slog.Info("Processing files", "count", len(filtered))

		result, err := s.fileProcessor.ProcessBatch(s.ctx, filtered)
		if err != nil {
			slog.Error("Failed to process files", "error", err)
			return
		}

		if s.storage != nil && len(result.Items) > 0 {
			if err := s.storage.StoreBatch(s.ctx, result.Items); err != nil {
				slog.Error("Failed to store memory items", "error", err)
			}
		}

		slog.Info("File processing complete",
			"items", len(result.Items),
			"errors", len(result.Errors))
	}
}

func (s *Scheduler) cleanup() {
	s.mu.Lock()
	s.lastCleanup = time.Now()
	s.mu.Unlock()

	s.mu.Lock()
	cutoff := time.Now().Add(-24 * time.Hour)
	for file, t := range s.processedFiles {
		if t.Before(cutoff) {
			delete(s.processedFiles, file)
		}
	}
	s.mu.Unlock()

	slog.Debug("Memory cleanup complete")
}

// TriggerChat forces an immediate chat processing
func (s *Scheduler) TriggerChat() {
	go s.processChats()
}

// TriggerFile forces an immediate file processing
func (s *Scheduler) TriggerFile() {
	go s.processFiles()
}

// TriggerCleanup forces an immediate cleanup
func (s *Scheduler) TriggerCleanup() {
	go s.cleanup()
}

// GetStats returns scheduler statistics
func (s *Scheduler) GetStats() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return map[string]interface{}{
		"last_chat_scan":   s.lastChatScan,
		"last_file_scan":   s.lastFileScan,
		"last_cleanup":     s.lastCleanup,
		"processed_files":  len(s.processedFiles),
		"chat_interval":    s.config.ChatInterval.String(),
		"file_interval":    s.config.FileInterval.String(),
		"cleanup_interval": s.config.CleanupInterval.String(),
	}
}
