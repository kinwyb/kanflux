package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/kinwyb/kanflux/memoria/types"
)

// MDStore implements Storage interface using markdown files
type MDStore struct {
	config    *types.StorageConfig
	baseDir   string
	mu        sync.RWMutex
	fileCache map[string][]*types.MemoryItem
	fileIndex *FileIndex // 文件处理状态索引
}

// StorageConfig alias for backward compatibility
type StorageConfig = types.StorageConfig

// NewMDStore creates a new MD file store
func NewMDStore(baseDir string, config *StorageConfig) (*MDStore, error) {
	if config == nil {
		config = &StorageConfig{
			MaxL1Tokens:  120,
			MaxL2Tokens:  500,
			DateFormat:   "2006-01-02",
			EnableBackup: true,
		}
	}

	store := &MDStore{
		config:    config,
		baseDir:   baseDir,
		fileCache: make(map[string][]*types.MemoryItem),
	}

	if err := store.ensureDirs(); err != nil {
		return nil, err
	}

	if err := store.loadCache(); err != nil {
		slog.Warn("Failed to load cache", "error", err)
	}

	// 加载文件索引
	store.fileIndex = LoadFileIndex(baseDir + "/metadata/file_index.json")

	return store, nil
}

func (s *MDStore) ensureDirs() error {
	dirs := []string{
		s.baseDir,
		s.baseDir + "/l1",
		s.baseDir + "/l1/facts",
		s.baseDir + "/l1/preferences",
		s.baseDir + "/l2",
		s.baseDir + "/l2/events",
		s.baseDir + "/l2/discoveries",
		s.baseDir + "/files",
		s.baseDir + "/files/facts",
		s.baseDir + "/files/preferences",
		s.baseDir + "/files/events",
		s.baseDir + "/files/discoveries",
		s.baseDir + "/metadata",
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}
	return nil
}

func (s *MDStore) loadCache() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	l1Dirs := []string{s.baseDir + "/l1/facts", s.baseDir + "/l1/preferences"}
	for _, dir := range l1Dirs {
		files, err := filepath.Glob(dir + "/*.md")
		if err != nil {
			continue
		}
		for _, file := range files {
			items, err := s.parseMDFile(file)
			if err != nil {
				slog.Warn("Failed to parse file", "file", file, "error", err)
				continue
			}
			s.fileCache[file] = items
			slog.Debug("Loaded L1 cache", "file", file, "items", len(items))
		}
	}

	l2Dirs := []string{s.baseDir + "/l2/events", s.baseDir + "/l2/discoveries"}
	for _, dir := range l2Dirs {
		files, err := filepath.Glob(dir + "/*.md")
		if err != nil {
			continue
		}
		for _, file := range files {
			items, err := s.parseMDFile(file)
			if err != nil {
				slog.Warn("Failed to parse file", "file", file, "error", err)
				continue
			}
			s.fileCache[file] = items
			slog.Debug("Loaded L2 cache", "file", file, "items", len(items))
		}
	}

	slog.Info("Cache loaded", "total_files", len(s.fileCache))
	return nil
}

// Store saves a memory item
func (s *MDStore) Store(ctx context.Context, item *types.MemoryItem) error {
	return s.StoreBatch(ctx, []*types.MemoryItem{item})
}

// StoreBatch saves multiple items
func (s *MDStore) StoreBatch(ctx context.Context, items []*types.MemoryItem) error {
	if len(items) == 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	groups := s.groupItems(items)

	// 先收集文件来源的源文件路径，稍后统一清理
	fileSources := make(map[string]bool)

	for key, groupItems := range groups {
		// 记录文件来源
		if key.sourceType == "file" && key.sourcePath != "" {
			fileSources[key.sourcePath] = true
		}

		file := s.getFilePath(key)

		// 文件来源：直接覆盖，不追加
		if key.sourceType == "file" {
			if err := s.writeToFile(file, groupItems, key.layer); err != nil {
				return err
			}
			s.fileCache[file] = groupItems
		} else {
			// 聊天来源：追加
			if err := s.writeToFile(file, groupItems, key.layer); err != nil {
				return err
			}
			s.fileCache[file] = append(s.fileCache[file], groupItems...)
		}
	}

	return nil
}

type itemKey struct {
	layer      types.Layer
	hallType   types.HallType
	userID     string
	date       string
	sourceType string // "chat" 或 "file"
	sourcePath string // 文件来源时为文件路径
}

func (s *MDStore) groupItems(items []*types.MemoryItem) map[itemKey][]*types.MemoryItem {
	groups := make(map[itemKey][]*types.MemoryItem)
	for _, item := range items {
		// 根据 SourceType 字段判断来源类型
		sourceType := "chat"
		sourcePath := ""
		if item.SourceType == types.SourceTypeFile {
			sourceType = "file"
			sourcePath = item.Source
		}

		key := itemKey{
			layer:      item.Layer,
			hallType:   item.HallType,
			userID:     item.UserID,
			date:       item.Timestamp.Format(s.config.DateFormat),
			sourceType: sourceType,
			sourcePath: sourcePath,
		}
		groups[key] = append(groups[key], item)
	}
	return groups
}

func (s *MDStore) getFilePath(key itemKey) string {
	userPart := sanitizeUserID(key.userID)

	// 判断来源：聊天记录按时间，文件按源路径
	if key.sourceType == "file" && key.sourcePath != "" {
		// 文件来源：使用文件路径 hash 作为存储路径
		fileHash := HashFilePath(key.sourcePath)
		switch key.layer {
		case types.LayerL1:
			switch key.hallType {
			case types.HallFacts:
				return s.baseDir + "/files/facts/" + fileHash + ".md"
			case types.HallPreferences:
				return s.baseDir + "/files/preferences/" + fileHash + ".md"
			default:
				return s.baseDir + "/files/facts/" + fileHash + ".md"
			}
		case types.LayerL2:
			switch key.hallType {
			case types.HallEvents:
				return s.baseDir + "/files/events/" + fileHash + ".md"
			case types.HallDiscoveries:
				return s.baseDir + "/files/discoveries/" + fileHash + ".md"
			default:
				return s.baseDir + "/files/events/" + fileHash + ".md"
			}
		default:
			return s.baseDir + "/files/" + fileHash + ".md"
		}
	}

	// 聊天记录：按用户和时间存储
	switch key.layer {
	case types.LayerL1:
		switch key.hallType {
		case types.HallFacts:
			return s.baseDir + "/l1/facts/" + userPart + ".md"
		case types.HallPreferences:
			return s.baseDir + "/l1/preferences/" + userPart + ".md"
		default:
			return s.baseDir + "/l1/facts/" + userPart + ".md"
		}
	case types.LayerL2:
		datePart := key.date
		switch key.hallType {
		case types.HallEvents:
			return s.baseDir + "/l2/events/" + datePart + "_" + userPart + ".md"
		case types.HallDiscoveries:
			return s.baseDir + "/l2/discoveries/" + datePart + "_" + userPart + ".md"
		default:
			return s.baseDir + "/l2/events/" + datePart + "_" + userPart + ".md"
		}
	default:
		return s.baseDir + "/l3/" + userPart + "_" + key.date + ".md"
	}
}

func (s *MDStore) writeToFile(file string, items []*types.MemoryItem, layer types.Layer) error {
	if s.config.EnableBackup && fileExists(file) {
		if err := s.backupFile(file); err != nil {
			slog.Warn("Failed to backup file", "file", file, "error", err)
		}
	}

	existing := s.fileCache[file]
	allItems := mergeItems(existing, items)
	content := s.generateMarkdown(allItems, layer)

	if err := os.WriteFile(file, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write file %s: %w", file, err)
	}

	return nil
}

func (s *MDStore) generateMarkdown(items []*types.MemoryItem, layer types.Layer) string {
	var sb strings.Builder

	sb.WriteString("# Memory Store\n\n")
	sb.WriteString(fmt.Sprintf("Layer: L%d\n", layer))
	sb.WriteString(fmt.Sprintf("Generated: %s\n\n", time.Now().Format(time.RFC3339)))

	for _, item := range items {
		sb.WriteString("---\n\n")
		sb.WriteString(fmt.Sprintf("**ID**: `%s`\n", item.ID))
		sb.WriteString(fmt.Sprintf("**Hall**: %s\n", item.HallType))
		sb.WriteString(fmt.Sprintf("**Layer**: L%d\n", item.Layer))
		sb.WriteString(fmt.Sprintf("**SourceType**: %s\n", item.SourceType))
		sb.WriteString(fmt.Sprintf("**Timestamp**: %s\n", item.Timestamp.Format(time.RFC3339)))
		sb.WriteString(fmt.Sprintf("**Source**: %s\n\n", item.Source))

		if item.Summary != "" {
			sb.WriteString("### Summary\n")
			sb.WriteString(item.Summary + "\n\n")
		}

		// L3 stores full content for semantic search, L1/L2 only need summary
		if layer == types.LayerL3 && item.Content != "" {
			sb.WriteString("### Content\n")
			sb.WriteString(item.Content + "\n\n")
		}

		if len(item.Metadata) > 0 {
			sb.WriteString("### Metadata\n")
			sb.WriteString("```json\n")
			metaJSON, _ := json.MarshalIndent(item.Metadata, "", "  ")
			sb.WriteString(string(metaJSON) + "\n")
			sb.WriteString("```\n\n")
		}
	}

	return sb.String()
}

func (s *MDStore) parseMDFile(file string) ([]*types.MemoryItem, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}

	content := string(data)
	items := make([]*types.MemoryItem, 0)

	// Parse layer from file header or path
	layer := s.detectLayerFromPath(file)

	sections := strings.Split(content, "---")
	for _, section := range sections {
		if strings.TrimSpace(section) == "" {
			continue
		}
		item := s.parseSection(section)
		if item != nil && item.ID != "" {
			// Set layer from file path if not set (Layer default is 0, valid values are 1,2,3)
			if item.Layer == 0 {
				item.Layer = layer
			}
			items = append(items, item)
		}
	}

	return items, nil
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// detectLayerFromPath determines layer from file path
func (s *MDStore) detectLayerFromPath(filePath string) types.Layer {
	if strings.Contains(filePath, "/l1/") || strings.Contains(filePath, "\\l1\\") {
		return types.LayerL1
	}
	if strings.Contains(filePath, "/l2/") || strings.Contains(filePath, "\\l2\\") {
		return types.LayerL2
	}
	if strings.Contains(filePath, "/l3/") || strings.Contains(filePath, "\\l3\\") {
		return types.LayerL3
	}
	if strings.Contains(filePath, "/files/") || strings.Contains(filePath, "\\files\\") {
		// Files directory, need to check sub-path
		// Files are stored in L2 for summary, L3 for raw content
		return types.LayerL2
	}
	return types.LayerL1
}

func (s *MDStore) parseSection(section string) *types.MemoryItem {
	item := &types.MemoryItem{
		Metadata:   make(map[string]any),
		SourceType: types.SourceTypeChat, // Default to chat
	}

	lines := strings.Split(section, "\n")
	inContent := false
	inSummary := false
	inMetadata := false
	var contentLines, summaryLines, metadataLines []string

	for _, line := range lines {
		line = strings.TrimSpace(line)

		if strings.HasPrefix(line, "**ID**:") {
			item.ID = strings.Trim(strings.TrimPrefix(line, "**ID**:"), " `")
		} else if strings.HasPrefix(line, "**Hall**:") {
			item.HallType = types.HallType(strings.TrimSpace(strings.TrimPrefix(line, "**Hall**:")))
		} else if strings.HasPrefix(line, "**Layer**:") {
			layerStr := strings.TrimSpace(strings.TrimPrefix(line, "**Layer**:"))
			switch layerStr {
			case "L1", "1":
				item.Layer = types.LayerL1
			case "L2", "2":
				item.Layer = types.LayerL2
			case "L3", "3":
				item.Layer = types.LayerL3
			}
		} else if strings.HasPrefix(line, "**SourceType**:") {
			st := strings.TrimSpace(strings.TrimPrefix(line, "**SourceType**:"))
			item.SourceType = types.SourceType(st)
		} else if strings.HasPrefix(line, "**Timestamp**:") {
			ts := strings.TrimSpace(strings.TrimPrefix(line, "**Timestamp**:"))
			if t, err := time.Parse(time.RFC3339, ts); err == nil {
				item.Timestamp = t
			}
		} else if strings.HasPrefix(line, "**Source**:") {
			item.Source = strings.TrimSpace(strings.TrimPrefix(line, "**Source**:"))
		} else if line == "### Summary" {
			inSummary = true
			inContent = false
			inMetadata = false
		} else if line == "### Content" {
			inContent = true
			inSummary = false
			inMetadata = false
		} else if line == "### Metadata" {
			inMetadata = true
			inContent = false
			inSummary = false
		} else if line == "```json" {
		} else if line == "```" {
			if inMetadata && len(metadataLines) > 0 {
				metaStr := strings.Join(metadataLines, "\n")
				if err := json.Unmarshal([]byte(metaStr), &item.Metadata); err != nil {
					slog.Warn("Failed to parse metadata", "error", err)
				}
			}
			inMetadata = false
		} else if inSummary {
			summaryLines = append(summaryLines, line)
		} else if inContent {
			contentLines = append(contentLines, line)
		} else if inMetadata {
			metadataLines = append(metadataLines, line)
		}
	}

	item.Summary = strings.Join(summaryLines, "\n")
	item.Content = strings.Join(contentLines, "\n")

	return item
}

// Retrieve retrieves items matching criteria
func (s *MDStore) Retrieve(ctx context.Context, opts *types.RetrieveOptions) ([]*types.MemoryItem, error) {
	if opts == nil {
		opts = &types.RetrieveOptions{Limit: 10}
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []*types.MemoryItem

	for _, items := range s.fileCache {
		for _, item := range items {
			if s.matchesFilter(item, opts) {
				results = append(results, item)
			}
		}
	}

	sortByTimestamp(results)

	if opts.Limit > 0 && len(results) > opts.Limit {
		results = results[:opts.Limit]
	}

	return results, nil
}

func (s *MDStore) matchesFilter(item *types.MemoryItem, opts *types.RetrieveOptions) bool {
	if len(opts.Layers) > 0 {
		found := false
		for _, l := range opts.Layers {
			if item.Layer == l {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	if len(opts.HallTypes) > 0 {
		found := false
		for _, h := range opts.HallTypes {
			if item.HallType == h {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// SourceType filter
	if opts.SourceType != "" && item.SourceType != opts.SourceType {
		return false
	}

	if opts.UserID != "" && item.UserID != opts.UserID {
		return false
	}

	if opts.TimeRange != nil {
		if item.Timestamp.Before(opts.TimeRange.Start) || item.Timestamp.After(opts.TimeRange.End) {
			return false
		}
	}

	return true
}

// Delete removes a memory item by ID
func (s *MDStore) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for file, items := range s.fileCache {
		for i, item := range items {
			if item.ID == id {
				s.fileCache[file] = append(items[:i], items[i+1:]...)
				if len(s.fileCache[file]) > 0 {
					if err := s.writeToFile(file, s.fileCache[file], s.fileCache[file][0].Layer); err != nil {
						return err
					}
				}
				return nil
			}
		}
	}

	return nil
}

// DeleteByUser removes all memories for a user
func (s *MDStore) DeleteByUser(ctx context.Context, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	userPart := sanitizeUserID(userID)

	patterns := []string{
		s.baseDir + "/l1/facts/" + userPart + ".md",
		s.baseDir + "/l1/preferences/" + userPart + ".md",
		s.baseDir + "/l2/events/*_" + userPart + ".md",
		s.baseDir + "/l2/discoveries/*_" + userPart + ".md",
	}

	for _, pattern := range patterns {
		files, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		for _, file := range files {
			if s.config.EnableBackup {
				s.backupFile(file)
			}
			os.Remove(file)
			delete(s.fileCache, file)
		}
	}

	return nil
}

// Close closes the storage
func (s *MDStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 保存文件索引
	if s.fileIndex != nil {
		if err := s.fileIndex.Save(); err != nil {
			slog.Warn("Failed to save file index", "error", err)
		}
	}

	s.fileCache = nil
	return nil
}

// ShouldProcessFile 检查文件是否需要处理（内容是否变化）
func (s *MDStore) ShouldProcessFile(filePath string, content []byte) bool {
	if s.fileIndex == nil {
		return true
	}

	contentHash := HashContent(content)
	return s.fileIndex.NeedsProcessing(filePath, contentHash)
}

// MarkFileProcessed 标记文件已处理
func (s *MDStore) MarkFileProcessed(filePath string, content []byte, itemCount int) {
	if s.fileIndex == nil {
		return
	}

	contentHash := HashContent(content)
	s.fileIndex.MarkProcessed(filePath, contentHash, itemCount)
}

// DeleteFileMemories 删除指定文件的记忆（文件更新或删除时调用）
func (s *MDStore) DeleteFileMemories(filePath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	fileHash := HashFilePath(filePath)

	// 删除文件来源的记忆文件
	patterns := []string{
		s.baseDir + "/files/facts/" + fileHash + ".md",
		s.baseDir + "/files/preferences/" + fileHash + ".md",
		s.baseDir + "/files/events/" + fileHash + ".md",
		s.baseDir + "/files/discoveries/" + fileHash + ".md",
		s.baseDir + "/files/" + fileHash + ".md",
	}

	for _, pattern := range patterns {
		if fileExists(pattern) {
			if s.config.EnableBackup {
				s.backupFile(pattern)
			}
			os.Remove(pattern)
			delete(s.fileCache, pattern)
		}
	}

	// 从索引中移除
	if s.fileIndex != nil {
		s.fileIndex.Remove(filePath)
	}

	return nil
}

// GetFileIndex 获取文件索引
func (s *MDStore) GetFileIndex() *FileIndex {
	return s.fileIndex
}

func (s *MDStore) backupFile(file string) error {
	backupDir := s.baseDir + "/backup"
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return err
	}

	backupFile := backupDir + "/" + filepath.Base(file) + "." + time.Now().Format("20060102150405") + ".bak"
	return copyFile(file, backupFile)
}

func sanitizeUserID(userID string) string {
	result := strings.ReplaceAll(userID, "/", "_")
	result = strings.ReplaceAll(result, "\\", "_")
	result = strings.ReplaceAll(result, ":", "_")
	result = strings.ReplaceAll(result, " ", "_")
	return result
}

func fileExists(file string) bool {
	_, err := os.Stat(file)
	return err == nil
}

func copyFile(src, dst string) error {
	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer source.Close()

	dest, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dest.Close()

	_, err = io.Copy(dest, source)
	return err
}

func mergeItems(existing, new []*types.MemoryItem) []*types.MemoryItem {
	seen := make(map[string]bool)
	result := make([]*types.MemoryItem, 0)

	for _, item := range existing {
		if !seen[item.ID] {
			seen[item.ID] = true
			result = append(result, item)
		}
	}

	for _, item := range new {
		if !seen[item.ID] {
			seen[item.ID] = true
			result = append(result, item)
		}
	}

	return result
}

func sortByTimestamp(items []*types.MemoryItem) {
	for i := 0; i < len(items)-1; i++ {
		for j := i + 1; j < len(items); j++ {
			if items[i].Timestamp.Before(items[j].Timestamp) {
				items[i], items[j] = items[j], items[i]
			}
		}
	}
}

// ============ 文件索引相关 ============

// FileIndex 记录文件处理状态
type FileIndex struct {
	filePath string
	mu       sync.RWMutex
	Entries  map[string]*FileEntry `json:"entries"` // 文件路径 -> 处理记录
}

// FileEntry 文件处理记录
type FileEntry struct {
	ContentHash string    `json:"content_hash"` // 内容 hash
	ProcessedAt time.Time `json:"processed_at"` // 处理时间
	ItemCount   int       `json:"item_count"`   // 提取的记忆数量
}

// LoadFileIndex 加载文件索引
func LoadFileIndex(path string) *FileIndex {
	idx := &FileIndex{
		filePath: path,
		Entries:  make(map[string]*FileEntry),
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return idx
	}

	if err := json.Unmarshal(data, idx); err != nil {
		slog.Warn("Failed to parse file index", "error", err)
	}

	return idx
}

// Save 保存文件索引
func (idx *FileIndex) Save() error {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(idx.filePath, data, 0644)
}

// NeedsProcessing 检查文件是否需要处理
func (idx *FileIndex) NeedsProcessing(filePath, contentHash string) bool {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	entry, exists := idx.Entries[filePath]
	if !exists {
		return true
	}

	return entry.ContentHash != contentHash
}

// MarkProcessed 标记文件已处理
func (idx *FileIndex) MarkProcessed(filePath, contentHash string, itemCount int) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	idx.Entries[filePath] = &FileEntry{
		ContentHash: contentHash,
		ProcessedAt: time.Now(),
		ItemCount:   itemCount,
	}
}

// Remove 移除文件记录
func (idx *FileIndex) Remove(filePath string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	delete(idx.Entries, filePath)
}

// GetEntry 获取文件处理记录
func (idx *FileIndex) GetEntry(filePath string) *FileEntry {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	return idx.Entries[filePath]
}

// HashFilePath 计算文件路径的 hash（用于存储文件名）
func HashFilePath(filePath string) string {
	h := sha256.New()
	h.Write([]byte(filePath))
	return hex.EncodeToString(h.Sum(nil))[:16] // 取前 16 字符
}

// HashContent 计算内容的 hash
func HashContent(content []byte) string {
	h := sha256.New()
	h.Write(content)
	return hex.EncodeToString(h.Sum(nil))
}
