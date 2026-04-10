// Package storage provides SQLite-based vector storage for memoria
package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/kinwyb/kanflux/memoria/types"

	_ "modernc.org/sqlite"
)

// SQLiteVectorStore implements vector storage using SQLite with FTS5
type SQLiteVectorStore struct {
	db        *sql.DB
	dbPath    string
	embedder  types.Embedder // only used for L2/L3 semantic search
	baseDir   string         // base directory for MD file output
	mu        sync.RWMutex
	muMD      sync.Mutex     // mutex for MD file operations
}

// NewSQLiteVectorStore creates a new SQLite vector store
func NewSQLiteVectorStore(dbPath string, embedder types.Embedder, baseDir string) (*SQLiteVectorStore, error) {
	// ensure directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// enable WAL mode for better performance
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		slog.Warn("failed to enable WAL mode", "error", err)
	}

	store := &SQLiteVectorStore{
		db:       db,
		dbPath:   dbPath,
		embedder: embedder,
		baseDir:  baseDir,
	}

	return store, nil
}

// Initialize creates the necessary tables
func (s *SQLiteVectorStore) Initialize(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// create documents table (shared by L1, L2, L3)
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS documents (
			id TEXT PRIMARY KEY,
			layer INTEGER NOT NULL,
			content TEXT NOT NULL,
			hall_type TEXT NOT NULL,
			user_id TEXT NOT NULL,
			source TEXT,
			source_type TEXT,
			tokens INTEGER,
			timestamp TEXT,
			metadata TEXT,
			created_at TEXT,
			updated_at TEXT
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create documents table: %w", err)
	}

	// create embeddings table (L2 + L3)
	_, err = s.db.Exec(`
		CREATE TABLE IF NOT EXISTS embeddings (
			document_id TEXT PRIMARY KEY,
			embedding BLOB NOT NULL,
			layer INTEGER NOT NULL,
			created_at TEXT,
			FOREIGN KEY (document_id) REFERENCES documents(id)
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create embeddings table: %w", err)
	}

	// create FTS5 virtual table for full-text search
	_, err = s.db.Exec(`
		CREATE VIRTUAL TABLE IF NOT EXISTS documents_fts USING fts5(
			id,
			content,
			hall_type,
			user_id,
			layer,
			content='documents',
			content_rowid='rowid'
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create FTS5 table: %w", err)
	}

	// create indexes
	_, err = s.db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_documents_layer ON documents(layer);
		CREATE INDEX IF NOT EXISTS idx_documents_layer_user ON documents(layer, user_id);
		CREATE INDEX IF NOT EXISTS idx_documents_layer_hall ON documents(layer, hall_type);
		CREATE INDEX IF NOT EXISTS idx_documents_layer_user_hall ON documents(layer, user_id, hall_type);
		CREATE INDEX IF NOT EXISTS idx_embeddings_layer ON embeddings(layer);
	`)
	if err != nil {
		return fmt.Errorf("failed to create indexes: %w", err)
	}

	slog.Info("Initialized SQLite vector store", "path", s.dbPath)
	return nil
}

// Add adds documents to the store
// L1: only store document, no embedding
// L2: store document + embedding + output MD file for validation
// L3: store document + embedding
func (s *SQLiteVectorStore) Add(ctx context.Context, docs []Document) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, doc := range docs {
		metadataJSON := metadataToJSON(doc.Metadata)
		now := time.Now().Format(time.RFC3339)
		timestamp := doc.Timestamp.Format(time.RFC3339)
		if timestamp == "" || doc.Timestamp.IsZero() {
			timestamp = now
		}

		// insert document
		_, err := s.db.Exec(`
			INSERT OR REPLACE INTO documents
			(id, layer, content, hall_type, user_id, source, source_type, tokens, timestamp, metadata, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, doc.ID, doc.Layer, doc.Content, doc.HallType, doc.UserID, doc.Source, doc.SourceType, doc.Tokens, timestamp, metadataJSON, now, now)
		if err != nil {
			slog.Warn("failed to insert document", "id", doc.ID, "error", err)
			continue
		}

		// update FTS5 index
		_, err = s.db.Exec(`
			INSERT INTO documents_fts (id, content, hall_type, user_id, layer)
			VALUES (?, ?, ?, ?, ?)
		`, doc.ID, doc.Content, doc.HallType, doc.UserID, doc.Layer)
		if err != nil {
			slog.Warn("failed to update FTS5 index", "id", doc.ID, "error", err)
		}

		// generate embedding for L2/L3
		if doc.Layer == 2 || doc.Layer == 3 {
			if s.embedder != nil {
				embedding, err := s.embedder.Embed(ctx, doc.Content)
				if err != nil {
					slog.Warn("failed to generate embedding", "id", doc.ID, "layer", doc.Layer, "error", err)
					continue
				}

				embeddingBlob := floatsToBlob(embedding)
				_, err = s.db.Exec(`
					INSERT OR REPLACE INTO embeddings (document_id, embedding, layer, created_at)
					VALUES (?, ?, ?, ?)
				`, doc.ID, embeddingBlob, doc.Layer, now)
				if err != nil {
					slog.Warn("failed to store embedding", "id", doc.ID, "error", err)
				}
			}
		}

		// L2: output MD file for validation
		if doc.Layer == 2 {
			s.writeL2MDFile(&doc)
		}
	}

	return nil
}

// Search performs search (prefer L2, then L3)
func (s *SQLiteVectorStore) Search(ctx context.Context, query string, opts *SearchOptions) ([]SearchResult, error) {
	if opts == nil {
		opts = DefaultSearchOptions()
	}

	// generate query embedding if embedder available
	if s.embedder == nil {
		// fallback to FTS5 only
		return s.ftsSearch(ctx, query, opts)
	}

	embedding, err := s.embedder.Embed(ctx, query)
	if err != nil {
		slog.Warn("failed to embed query, fallback to FTS", "error", err)
		return s.ftsSearch(ctx, query, opts)
	}

	return s.SearchByVector(ctx, embedding, opts)
}

// KeywordSearch performs FTS5 full-text keyword search
func (s *SQLiteVectorStore) KeywordSearch(ctx context.Context, query string, opts *SearchOptions) ([]SearchResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ftsSearch(ctx, query, opts)
}

// SearchByVector performs semantic search using embedding vector
// Priority: L2 (precise summary) first, then L3 (full content) as supplement
func (s *SQLiteVectorStore) SearchByVector(ctx context.Context, vector []float32, opts *SearchOptions) ([]SearchResult, error) {
	if opts == nil {
		opts = DefaultSearchOptions()
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []SearchResult

	// determine search order: prefer L2 first
	searchLayers := opts.Layers
	if len(searchLayers) == 0 {
		searchLayers = []int{2, 3}
	}

	// search L2 first (precise)
	l2Count := 0
	for _, layer := range searchLayers {
		if layer != 2 {
			continue
		}
		layerResults, err := s.searchByVectorInLayer(ctx, vector, layer, opts)
		if err != nil {
			slog.Warn("failed to search layer", "layer", layer, "error", err)
			continue
		}
		results = append(results, layerResults...)
		l2Count = len(layerResults)
	}

	// if L2 results not enough, supplement from L3
	if l2Count < opts.Limit {
		for _, layer := range searchLayers {
			if layer != 3 {
				continue
			}
			remaining := opts.Limit - len(results)
			if remaining <= 0 {
				break
			}
			// adjust limit for L3
			l3Opts := *opts
			l3Opts.Limit = remaining + l2Count // request more to merge and filter
			layerResults, err := s.searchByVectorInLayer(ctx, vector, layer, &l3Opts)
			if err != nil {
				slog.Warn("failed to search L3", "error", err)
				continue
			}
			results = s.mergeResults(results, layerResults)
		}
	}

	// filter by min score
	if opts.MinScore > 0 {
		filtered := make([]SearchResult, 0)
		for _, r := range results {
			if r.Score >= opts.MinScore {
				filtered = append(filtered, r)
			}
		}
		results = filtered
	}

	// limit results
	if opts.Limit > 0 && len(results) > opts.Limit {
		results = results[:opts.Limit]
	}

	return results, nil
}

// searchByVectorInLayer searches within a specific layer
func (s *SQLiteVectorStore) searchByVectorInLayer(ctx context.Context, vector []float32, layer int, opts *SearchOptions) ([]SearchResult, error) {
	query := `
		SELECT d.id, d.content, d.hall_type, d.user_id, d.source, d.source_type, d.metadata, d.layer
		FROM documents d
		JOIN embeddings e ON d.id = e.document_id
		WHERE d.layer = ?
	`
	args := []any{layer}

	if opts.UserID != "" {
		query += " AND d.user_id = ?"
		args = append(args, opts.UserID)
	}
	if opts.HallType != "" {
		query += " AND d.hall_type = ?"
		args = append(args, opts.HallType)
	}
	if opts.SourceType != "" {
		query += " AND d.source_type = ?"
		args = append(args, opts.SourceType)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var candidates []SearchResult
	for rows.Next() {
		var result SearchResult
		var metadataJSON string
		var layer int

		err := rows.Scan(&result.ID, &result.Content, &result.HallType, &result.UserID, &result.Source, &result.SourceType, &metadataJSON, &layer)
		if err != nil {
			continue
		}

		result.Layer = layer
		result.Metadata = jsonToMetadata(metadataJSON)

		// get embedding and compute similarity
		embeddingBlob, err := s.getEmbedding(result.ID)
		if err != nil {
			continue
		}
		docEmbedding := blobToFloats(embeddingBlob)
		result.Score = cosineSimilarity(vector, docEmbedding)

		candidates = append(candidates, result)
	}

	// sort by score descending
	sortByScore(candidates)

	if opts.Limit > 0 && len(candidates) > opts.Limit {
		candidates = candidates[:opts.Limit]
	}

	return candidates, nil
}

// getEmbedding retrieves embedding blob by document ID
func (s *SQLiteVectorStore) getEmbedding(docID string) ([]byte, error) {
	var blob []byte
	err := s.db.QueryRow("SELECT embedding FROM embeddings WHERE document_id = ?", docID).Scan(&blob)
	return blob, err
}

// ftsSearch performs FTS5 full-text search (fallback)
func (s *SQLiteVectorStore) ftsSearch(ctx context.Context, query string, opts *SearchOptions) ([]SearchResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ftsQuery := s.buildFTSQuery(query, opts)

	searchQuery := query
	if searchQuery == "" {
		searchQuery = "*" // match all
	}

	rows, err := s.db.Query(ftsQuery, searchQuery, opts.Limit)
	if err != nil {
		return nil, fmt.Errorf("failed to FTS search: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var result SearchResult
		var metadataJSON string
		var layer int

		err := rows.Scan(&result.ID, &result.Content, &result.Score, &metadataJSON, &layer)
		if err != nil {
			continue
		}

		// BM25 returns negative scores (lower is better), convert to positive
		if result.Score < 0 {
			result.Score = -result.Score
		}
		// normalize to 0-1 range (rough approximation)
		result.Score = 1.0 / (1.0 + result.Score)

		result.Metadata = jsonToMetadata(metadataJSON)
		result.Layer = layer
		results = append(results, result)
	}

	return results, nil
}

// buildFTSQuery builds FTS5 search query
func (s *SQLiteVectorStore) buildFTSQuery(query string, opts *SearchOptions) string {
	baseQuery := `
		SELECT d.id, d.content, bm25(documents_fts) as score, d.metadata, d.layer
		FROM documents d
		JOIN documents_fts fts ON d.id = fts.id
		WHERE documents_fts MATCH ?
	`

	if opts.UserID != "" {
		baseQuery += " AND d.user_id = '" + opts.UserID + "'"
	}
	if opts.HallType != "" {
		baseQuery += " AND d.hall_type = '" + opts.HallType + "'"
	}
	if opts.SourceType != "" {
		baseQuery += " AND d.source_type = '" + opts.SourceType + "'"
	}
	if len(opts.Layers) > 0 {
		layerConditions := make([]string, len(opts.Layers))
		for i, l := range opts.Layers {
			layerConditions[i] = fmt.Sprintf("d.layer = %d", l)
		}
		baseQuery += " AND (" + strings.Join(layerConditions, " OR ") + ")"
	}

	baseQuery += " ORDER BY score LIMIT ?"
	return baseQuery
}

// Get retrieves a document by ID
func (s *SQLiteVectorStore) Get(ctx context.Context, id string) (*Document, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var doc Document
	var metadataJSON string
	var createdAt, updatedAt sql.NullString

	err := s.db.QueryRow(`
		SELECT id, layer, content, hall_type, user_id, source, source_type, tokens, metadata, created_at, updated_at
		FROM documents WHERE id = ?
	`, id).Scan(&doc.ID, &doc.Layer, &doc.Content, &doc.HallType, &doc.UserID, &doc.Source, &doc.SourceType, &doc.Tokens, &metadataJSON, &createdAt, &updatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	doc.Metadata = jsonToMetadata(metadataJSON)
	return &doc, nil
}

// GetByLayer retrieves documents by layer and optional user filter
func (s *SQLiteVectorStore) GetByLayer(ctx context.Context, layer int, userID string, limit int) ([]Document, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := "SELECT id, layer, content, hall_type, user_id, source, source_type, tokens, metadata FROM documents WHERE layer = ?"
	args := []any{layer}

	if userID != "" {
		query += " AND user_id = ?"
		args = append(args, userID)
	}

	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var docs []Document
	for rows.Next() {
		var doc Document
		var metadataJSON string
		err := rows.Scan(&doc.ID, &doc.Layer, &doc.Content, &doc.HallType, &doc.UserID, &doc.Source, &doc.SourceType, &doc.Tokens, &metadataJSON)
		if err != nil {
			continue
		}
		doc.Metadata = jsonToMetadata(metadataJSON)
		docs = append(docs, doc)
	}

	return docs, nil
}

// Delete removes a document by ID
func (s *SQLiteVectorStore) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// delete from embeddings first
	_, err := s.db.Exec("DELETE FROM embeddings WHERE document_id = ?", id)
	if err != nil {
		return err
	}

	// delete from documents
	_, err = s.db.Exec("DELETE FROM documents WHERE id = ?", id)
	if err != nil {
		return err
	}

	// delete from FTS5
	_, err = s.db.Exec("DELETE FROM documents_fts WHERE id = ?", id)
	return err
}

// DeleteByUserID removes all documents for a user
func (s *SQLiteVectorStore) DeleteByUserID(ctx context.Context, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// get IDs for FTS5 deletion
	rows, err := s.db.Query("SELECT id FROM documents WHERE user_id = ?", userID)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()

	// delete from embeddings
	_, err = s.db.Exec("DELETE FROM embeddings WHERE document_id IN (SELECT id FROM documents WHERE user_id = ?)", userID)
	if err != nil {
		return err
	}

	// delete from documents
	_, err = s.db.Exec("DELETE FROM documents WHERE user_id = ?", userID)
	if err != nil {
		return err
	}

	// delete from FTS5
	for _, id := range ids {
		s.db.Exec("DELETE FROM documents_fts WHERE id = ?", id)
	}

	return nil
}

// DeleteByLayer removes documents by layer for a user
func (s *SQLiteVectorStore) DeleteByLayer(ctx context.Context, layer int, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	query := "SELECT id FROM documents WHERE layer = ?"
	args := []any{layer}
	if userID != "" {
		query += " AND user_id = ?"
		args = append(args, userID)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()

	if len(ids) == 0 {
		return nil
	}

	// delete from embeddings
	_, err = s.db.Exec("DELETE FROM embeddings WHERE document_id IN (?)", ids[0])
	if len(ids) > 1 {
		for _, id := range ids[1:] {
			s.db.Exec("DELETE FROM embeddings WHERE document_id = ?", id)
		}
	}

	// delete from documents
	deleteQuery := "DELETE FROM documents WHERE layer = ?"
	deleteArgs := []any{layer}
	if userID != "" {
		deleteQuery += " AND user_id = ?"
		deleteArgs = append(deleteArgs, userID)
	}
	_, err = s.db.Exec(deleteQuery, deleteArgs...)
	if err != nil {
		return err
	}

	// delete from FTS5
	for _, id := range ids {
		s.db.Exec("DELETE FROM documents_fts WHERE id = ?", id)
	}

	return nil
}

// Count returns total document count
func (s *SQLiteVectorStore) Count(ctx context.Context) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM documents").Scan(&count)
	return count, err
}

// CountByLayer returns document count by layer
func (s *SQLiteVectorStore) CountByLayer(ctx context.Context, layer int) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM documents WHERE layer = ?", layer).Scan(&count)
	return count, err
}

// GetStats returns statistics about the store
func (s *SQLiteVectorStore) GetStats(ctx context.Context) (*StoreStats, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := &StoreStats{
		LayerCounts: make(map[int]int),
	}

	// total documents
	err := s.db.QueryRow("SELECT COUNT(*) FROM documents").Scan(&stats.TotalDocuments)
	if err != nil {
		return nil, err
	}

	// total vectors (embeddings)
	err = s.db.QueryRow("SELECT COUNT(*) FROM embeddings").Scan(&stats.TotalVectors)
	if err != nil {
		stats.TotalVectors = 0
	}

	// layer counts
	rows, err := s.db.Query("SELECT layer, COUNT(*) FROM documents GROUP BY layer")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var layer, count int
		if err := rows.Scan(&layer, &count); err != nil {
			continue
		}
		stats.LayerCounts[layer] = count
	}

	// storage size
	info, err := os.Stat(s.dbPath)
	if err == nil {
		stats.StorageSize = info.Size()
	}

	return stats, nil
}

// Close closes the database connection
func (s *SQLiteVectorStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db.Close()
}

// ============ Helper Functions ============

// mergeResults merges results from different layers, deduplicating by ID
func (s *SQLiteVectorStore) mergeResults(primary, secondary []SearchResult) []SearchResult {
	seen := make(map[string]bool)
	var merged []SearchResult

	// add primary results first (L2 preferred)
	for _, r := range primary {
		if !seen[r.ID] {
			merged = append(merged, r)
			seen[r.ID] = true
		}
	}

	// add secondary results
	for _, r := range secondary {
		if !seen[r.ID] {
			merged = append(merged, r)
			seen[r.ID] = true
		}
	}

	return merged
}

// writeL2MDFile writes L2 content to MD file for validation
// Uses the same directory structure as md_store.go for easy comparison:
// - Chat source: l2/events or l2/discoveries, filename: {date}_{user}.md
// - File source: files/events or files/discoveries, filename: {fileHash}.md
func (s *SQLiteVectorStore) writeL2MDFile(doc *Document) {
	if s.baseDir == "" {
		return
	}

	s.muMD.Lock()
	defer s.muMD.Unlock()

	// determine hall subdirectory
	hallDir := ""
	switch doc.HallType {
	case "hall_events":
		hallDir = "events"
	case "hall_discoveries":
		hallDir = "discoveries"
	default:
		hallDir = "events" // default
	}

	// determine file path based on source type
	var filePath string
	if doc.SourceType == "file" && doc.Source != "" {
		// file source: use file path hash as filename
		fileHash := HashFilePath(doc.Source)
		dirPath := filepath.Join(s.baseDir, "files", hallDir)
		if err := os.MkdirAll(dirPath, 0755); err != nil {
			slog.Warn("failed to create files directory", "error", err)
			return
		}
		filePath = filepath.Join(dirPath, fileHash+".md")
	} else {
		// chat source: use date + userID as filename
		datePart := doc.Timestamp.Format("2006-01-02")
		if datePart == "" || doc.Timestamp.IsZero() {
			datePart = time.Now().Format("2006-01-02")
		}
		userPart := sanitizeUserID(doc.UserID)
		dirPath := filepath.Join(s.baseDir, "l2", hallDir)
		if err := os.MkdirAll(dirPath, 0755); err != nil {
			slog.Warn("failed to create L2 directory", "error", err)
			return
		}
		filePath = filepath.Join(dirPath, datePart+"_"+userPart+".md")
	}

	// generate markdown content
	content := s.generateL2Markdown(doc)

	// append to existing file or create new
	var existingContent string
	if data, err := os.ReadFile(filePath); err == nil {
		existingContent = string(data)
	}

	// check if ID already exists
	if strings.Contains(existingContent, fmt.Sprintf("**ID**: `%s`", doc.ID)) {
		return // already exists, skip
	}

	// append new entry
	finalContent := existingContent + content
	if err := os.WriteFile(filePath, []byte(finalContent), 0644); err != nil {
		slog.Warn("failed to write L2 MD file", "path", filePath, "error", err)
	}
}

// generateL2Markdown generates markdown for a single L2 document
func (s *SQLiteVectorStore) generateL2Markdown(doc *Document) string {
	var sb strings.Builder

	timestamp := doc.Timestamp.Format(time.RFC3339)
	if timestamp == "" || doc.Timestamp.IsZero() {
		timestamp = time.Now().Format(time.RFC3339)
	}

	sb.WriteString("---\n\n")
	sb.WriteString(fmt.Sprintf("**ID**: `%s`\n", doc.ID))
	sb.WriteString(fmt.Sprintf("**Hall**: %s\n", doc.HallType))
	sb.WriteString(fmt.Sprintf("**Layer**: L%d\n", doc.Layer))
	sb.WriteString(fmt.Sprintf("**SourceType**: %s\n", doc.SourceType))
	sb.WriteString(fmt.Sprintf("**Timestamp**: %s\n", timestamp))
	sb.WriteString(fmt.Sprintf("**Source**: %s\n\n", doc.Source))

	if doc.Content != "" {
		sb.WriteString("### Summary\n")
		sb.WriteString(doc.Content + "\n\n")
	}

	if len(doc.Metadata) > 0 {
		sb.WriteString("### Metadata\n")
		sb.WriteString("```json\n")
		metaJSON, _ := json.MarshalIndent(doc.Metadata, "", "  ")
		sb.WriteString(string(metaJSON) + "\n")
		sb.WriteString("```\n\n")
	}

	return sb.String()
}

func metadataToJSON(m map[string]any) string {
	if m == nil {
		return "{}"
	}
	data, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func jsonToMetadata(s string) map[string]any {
	if s == "" || s == "{}" {
		return make(map[string]any)
	}

	result := make(map[string]any)
	if err := json.Unmarshal([]byte(s), &result); err != nil {
		slog.Warn("failed to parse metadata JSON", "error", err, "json", s)
		return make(map[string]any)
	}
	return result
}

func floatsToBlob(f []float32) []byte {
	blob := make([]byte, len(f)*4)
	for i, v := range f {
		bits := math.Float32bits(v)
		blob[i*4] = byte(bits >> 24)
		blob[i*4+1] = byte(bits >> 16)
		blob[i*4+2] = byte(bits >> 8)
		blob[i*4+3] = byte(bits)
	}
	return blob
}

func blobToFloats(b []byte) []float32 {
	count := len(b) / 4
	f := make([]float32, count)
	for i := 0; i < count; i++ {
		bits := uint32(b[i*4])<<24 | uint32(b[i*4+1])<<16 | uint32(b[i*4+2])<<8 | uint32(b[i*4+3])
		f[i] = math.Float32frombits(bits)
	}
	return f
}

func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}

	var dotProduct, normA, normB float64
	for i := range a {
		dotProduct += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dotProduct / (sqrt(normA) * sqrt(normB))
}

func sqrt(x float64) float64 {
	if x < 0 {
		return 0
	}
	z := x
	for i := 0; i < 10; i++ {
		z = (z + x/z) / 2
	}
	return z
}

func sortByScore(results []SearchResult) {
	for i := 0; i < len(results)-1; i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].Score > results[i].Score {
				results[i], results[j] = results[j], results[i]
			}
		}
	}
}