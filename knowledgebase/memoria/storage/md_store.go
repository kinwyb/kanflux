package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/kinwyb/kanflux/knowledgebase/memoria/types"
)

// MDStore implements Storage interface using markdown files
type MDStore struct {
	config    *types.StorageConfig
	baseDir   string
	mu        sync.RWMutex
	fileCache map[string][]*types.MemoryItem
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
		}
	}

	l2Dirs := []string{s.baseDir + "/l2/events", s.baseDir + "/l2/discovery"}
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
		}
	}

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

	for key, groupItems := range groups {
		file := s.getFilePath(key)
		if err := s.writeToFile(file, groupItems, key.layer); err != nil {
			return err
		}
		s.fileCache[file] = append(s.fileCache[file], groupItems...)
	}

	return nil
}

type itemKey struct {
	layer    types.Layer
	hallType types.HallType
	userID   string
	date     string
}

func (s *MDStore) groupItems(items []*types.MemoryItem) map[itemKey][]*types.MemoryItem {
	groups := make(map[itemKey][]*types.MemoryItem)
	for _, item := range items {
		key := itemKey{
			layer:    item.Layer,
			hallType: item.HallType,
			userID:   item.UserID,
			date:     item.Timestamp.Format(s.config.DateFormat),
		}
		groups[key] = append(groups[key], item)
	}
	return groups
}

func (s *MDStore) getFilePath(key itemKey) string {
	userPart := sanitizeUserID(key.userID)
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
			return s.baseDir + "/l2/discovery/" + datePart + "_" + userPart + ".md"
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
		sb.WriteString(fmt.Sprintf("**Timestamp**: %s\n", item.Timestamp.Format(time.RFC3339)))
		sb.WriteString(fmt.Sprintf("**Source**: %s\n\n", item.Source))

		if item.Summary != "" {
			sb.WriteString("### Summary\n")
			sb.WriteString(item.Summary + "\n\n")
		}

		sb.WriteString("### Content\n")
		sb.WriteString(item.Content + "\n\n")

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

	sections := strings.Split(content, "---")
	for _, section := range sections {
		if strings.TrimSpace(section) == "" {
			continue
		}
		item := s.parseSection(section)
		if item != nil && item.ID != "" {
			items = append(items, item)
		}
	}

	return items, nil
}

func (s *MDStore) parseSection(section string) *types.MemoryItem {
	item := &types.MemoryItem{
		Metadata: make(map[string]any),
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
			item.HallType = types.HallType(strings.TrimPrefix(line, "**Hall**:"))
		} else if strings.HasPrefix(line, "**Timestamp**:") {
			ts := strings.TrimPrefix(line, "**Timestamp**:")
			if t, err := time.Parse(time.RFC3339, ts); err == nil {
				item.Timestamp = t
			}
		} else if strings.HasPrefix(line, "**Source**:") {
			item.Source = strings.TrimPrefix(line, "**Source**:")
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
		s.baseDir + "/l2/discovery/*_" + userPart + ".md",
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
	s.fileCache = nil
	return nil
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