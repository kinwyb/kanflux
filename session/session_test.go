package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

func TestInstructionEntry(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected string
	}{
		{
			name:     "simple content",
			content:  "test instruction",
			expected: computeHash("test instruction"),
		},
		{
			name:     "empty content",
			content:  "",
			expected: computeHash(""),
		},
		{
			name:     "long content",
			content:  "This is a very long instruction content that should still produce a consistent hash",
			expected: computeHash("This is a very long instruction content that should still produce a consistent hash"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := InstructionEntry{
				AgentName: "test-agent",
				Content:   tt.content,
				Timestamp: time.Now(),
			}
			// ContentHash should be computed
			if entry.ContentHash == "" {
				entry.ContentHash = computeHash(entry.Content)
			}
			if entry.ContentHash != tt.expected {
				t.Errorf("ContentHash mismatch: got %s, want %s", entry.ContentHash, tt.expected)
			}
		})
	}
}

// newTestSession 创建测试用的 Session
func newTestSession(key string) *Session {
	meta := &SessionMeta{
		Key:          key,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		Metadata:     make(map[string]interface{}),
		MessageCount: 0,
		InstrCount:   0,
	}
	data := &SessionData{
		Instructions: []InstructionEntry{},
		Messages:     []adk.Message{},
	}
	return &Session{
		meta: meta,
		data: data,
	}
}

func TestSessionAddInstruction(t *testing.T) {
	sess := newTestSession("test-session")

	// First instruction should be added
	entry1 := InstructionEntry{
		AgentName: "agent1",
		Content:   "instruction content 1",
		Timestamp: time.Now(),
	}
	if !sess.AddInstruction(entry1) {
		t.Error("First instruction should be added")
	}
	instructions := sess.GetInstructions()
	if len(instructions) != 1 {
		t.Errorf("Expected 1 instruction, got %d", len(instructions))
	}

	// Same content (duplicate) should not be added
	entry2 := InstructionEntry{
		AgentName: "agent2",
		Content:   "instruction content 1", // Same content as entry1
		Timestamp: time.Now(),
	}
	if sess.AddInstruction(entry2) {
		t.Error("Duplicate instruction should not be added")
	}
	instructions = sess.GetInstructions()
	if len(instructions) != 1 {
		t.Errorf("Expected 1 instruction after duplicate, got %d", len(instructions))
	}

	// Different content should be added
	entry3 := InstructionEntry{
		AgentName: "agent3",
		Content:   "instruction content 2",
		Timestamp: time.Now(),
	}
	if !sess.AddInstruction(entry3) {
		t.Error("Different instruction should be added")
	}
	instructions = sess.GetInstructions()
	if len(instructions) != 2 {
		t.Errorf("Expected 2 instructions, got %d", len(instructions))
	}
}

func TestSessionGetInstructions(t *testing.T) {
	sess := newTestSession("test-session")
	sess.AddInstruction(InstructionEntry{AgentName: "agent1", Content: "content1", Timestamp: time.Now()})
	sess.AddInstruction(InstructionEntry{AgentName: "agent2", Content: "content2", Timestamp: time.Now()})

	instructions := sess.GetInstructions()
	if len(instructions) != 2 {
		t.Errorf("Expected 2 instructions, got %d", len(instructions))
	}

	// Should return a copy
	instructions[0].Content = "modified"
	original := sess.GetInstructions()
	if original[0].Content == "modified" {
		t.Error("GetInstructions should return a copy, not reference")
	}
}

func TestSessionClearWithInstructions(t *testing.T) {
	sess := newTestSession("test-session")
	sess.AddInstruction(InstructionEntry{AgentName: "agent1", Content: "content1", Timestamp: time.Now()})
	sess.AddMessage(schema.UserMessage("hello"))

	sess.Clear()
	instructions := sess.GetInstructions()
	if len(instructions) != 0 {
		t.Errorf("Clear should empty Instructions, got %d", len(instructions))
	}
	history := sess.GetHistory(0)
	if len(history) != 0 {
		t.Errorf("Clear should empty Messages, got %d", len(history))
	}
}

func TestManagerSaveLoadWithInstructions(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()
	kanfluxDir := filepath.Join(tmpDir, ".kanflux", "sessions")
	if err := os.MkdirAll(kanfluxDir, 0755); err != nil {
		t.Fatalf("Failed to create dir: %v", err)
	}

	mgr, err := NewManager(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Create session with instructions
	sess, err := mgr.GetOrCreate("test-key")
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	// Add instruction
	entry := InstructionEntry{
		AgentName: "test-agent",
		Content:   "test instruction content",
		Timestamp: time.Now(),
	}
	sess.AddInstruction(entry)

	// Add a message
	sess.AddMessage(schema.UserMessage("hello"))

	// Save
	if err := mgr.Save(sess); err != nil {
		t.Fatalf("Failed to save session: %v", err)
	}

	// Clear caches and reload
	mgr.mu.Lock()
	delete(mgr.metaCache, "test-key")
	delete(mgr.dataCache, "test-key")
	mgr.mu.Unlock()

	// Load from disk
	sess2, err := mgr.GetOrCreate("test-key")
	if err != nil {
		t.Fatalf("Failed to load session: %v", err)
	}

	// Verify instructions loaded
	instructions := sess2.GetInstructions()
	if len(instructions) != 1 {
		t.Errorf("Expected 1 instruction after load, got %d", len(instructions))
	}
	if instructions[0].Content != "test instruction content" {
		t.Errorf("Instruction content mismatch: got %s", instructions[0].Content)
	}
	if instructions[0].AgentName != "test-agent" {
		t.Errorf("Instruction agent name mismatch: got %s", instructions[0].AgentName)
	}

	// Verify messages loaded
	history := sess2.GetHistory(0)
	if len(history) != 1 {
		t.Errorf("Expected 1 message after load, got %d", len(history))
	}
}

func TestManagerLoadLegacyFormat(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()
	kanfluxDir := filepath.Join(tmpDir, ".kanflux", "sessions")
	if err := os.MkdirAll(kanfluxDir, 0755); err != nil {
		t.Fatalf("Failed to create dir: %v", err)
	}

	// Write legacy format file (no _type field, just messages)
	legacyFile := filepath.Join(kanfluxDir, "legacy-session.jsonl")
	content := `{"role":"user","content":"hello"}
{"role":"assistant","content":"hi there"}
`
	if err := os.WriteFile(legacyFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write legacy file: %v", err)
	}

	mgr, err := NewManager(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Load legacy session
	sess, err := mgr.GetOrCreate("legacy-session")
	if err != nil {
		t.Fatalf("Failed to load legacy session: %v", err)
	}

	// Verify messages loaded
	history := sess.GetHistory(0)
	if len(history) != 2 {
		t.Errorf("Expected 2 messages from legacy format, got %d", len(history))
	}
	if history[0].Content != "hello" {
		t.Errorf("First message content mismatch: got %s", history[0].Content)
	}
}

// TestLazyLoading 测试懒加载功能
func TestLazyLoading(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()
	kanfluxDir := filepath.Join(tmpDir, ".kanflux", "sessions")
	if err := os.MkdirAll(kanfluxDir, 0755); err != nil {
		t.Fatalf("Failed to create dir: %v", err)
	}

	// Write a session file with metadata
	sessionFile := filepath.Join(kanfluxDir, "lazy-test.jsonl")
	content := `{"_type":"metadata","key":"lazy-test","created_at":"2024-01-15T10:00:00Z","updated_at":"2024-01-15T11:00:00Z","message_count":5,"instruction_count":2}
{"_type":"instruction","agent_name":"agent1","content":"test instruction","timestamp":"2024-01-15T10:30:00Z","content_hash":"hash1"}
{"role":"user","content":"hello"}
{"role":"assistant","content":"hi"}
{"role":"user","content":"how are you"}
{"role":"assistant","content":"fine"}
{"role":"user","content":"bye"}
`
	if err := os.WriteFile(sessionFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write session file: %v", err)
	}

	mgr, err := NewManager(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Get session - should only load metadata
	sess, err := mgr.GetOrCreate("lazy-test")
	if err != nil {
		t.Fatalf("Failed to get session: %v", err)
	}

	// Verify metadata loaded (fast, no data loading)
	meta := sess.GetMeta()
	if meta.Key != "lazy-test" {
		t.Errorf("Expected key 'lazy-test', got %s", meta.Key)
	}
	if meta.MessageCount != 5 {
		t.Errorf("Expected MessageCount 5, got %d", meta.MessageCount)
	}
	if meta.InstrCount != 2 {
		t.Errorf("Expected InstrCount 2, got %d", meta.InstrCount)
	}

	// Data should not be loaded yet
	if sess.IsDataLoaded() {
		t.Error("Data should not be loaded yet after GetOrCreate")
	}

	// GetHistoryLen() should use meta count, not load data
	msgLen := sess.GetHistoryLen()
	if msgLen != 5 {
		t.Errorf("Expected GetHistoryLen 5 (from meta), got %d", msgLen)
	}

	// Data should still not be loaded
	if sess.IsDataLoaded() {
		t.Error("Data should not be loaded after GetHistoryLen")
	}

	// GetHistory() triggers lazy loading
	history := sess.GetHistory(0)
	if len(history) != 5 {
		t.Errorf("Expected 5 messages after GetHistory, got %d", len(history))
	}

	// Now data should be loaded
	if !sess.IsDataLoaded() {
		t.Error("Data should be loaded after GetHistory")
	}
}

// TestGetMetaFast 测试 GetMeta 不触发数据加载
func TestGetMetaFast(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()
	kanfluxDir := filepath.Join(tmpDir, ".kanflux", "sessions")
	if err := os.MkdirAll(kanfluxDir, 0755); err != nil {
		t.Fatalf("Failed to create dir: %v", err)
	}

	// Write a large session file (simulate many messages)
	sessionFile := filepath.Join(kanfluxDir, "large-test.jsonl")
	content := `{"_type":"metadata","key":"large-test","created_at":"2024-01-15T10:00:00Z","updated_at":"2024-01-15T11:00:00Z","message_count":100,"instruction_count":10}
`
	// Add many messages (but we won't load them)
	for i := 0; i < 100; i++ {
		content += `{"role":"user","content":"message ` + string(rune(i)) + `"}
`
	}
	if err := os.WriteFile(sessionFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write session file: %v", err)
	}

	mgr, err := NewManager(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// GetMeta should only read first line
	meta, err := mgr.GetMeta("large-test")
	if err != nil {
		t.Fatalf("Failed to get meta: %v", err)
	}

	if meta.MessageCount != 100 {
		t.Errorf("Expected MessageCount 100, got %d", meta.MessageCount)
	}

	// GetMeta should not load data into dataCache
	mgr.mu.RLock()
	_, hasData := mgr.dataCache["large-test"]
	mgr.mu.RUnlock()
	if hasData {
		t.Error("GetMeta should not load data into dataCache")
	}
}

// TestListMeta 测试 ListMeta 快速列出元数据
func TestListMeta(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()
	kanfluxDir := filepath.Join(tmpDir, ".kanflux", "sessions")
	if err := os.MkdirAll(kanfluxDir, 0755); err != nil {
		t.Fatalf("Failed to create dir: %v", err)
	}

	// Create multiple session files
	for i := 1; i <= 3; i++ {
		sessionFile := filepath.Join(kanfluxDir, "session-"+string(rune('0'+i))+".jsonl")
		content := `{"_type":"metadata","key":"session-` + string(rune('0'+i)) + `","created_at":"2024-01-15T10:00:00Z","updated_at":"2024-01-15T11:00:00Z","message_count":` + string(rune('0'+i)) + `,"instruction_count":0}
{"role":"user","content":"hello"}
`
		if err := os.WriteFile(sessionFile, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to write session file: %v", err)
		}
	}

	mgr, err := NewManager(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// ListMeta should return all metadata
	metas, err := mgr.ListMeta()
	if err != nil {
		t.Fatalf("Failed to list meta: %v", err)
	}

	if len(metas) != 3 {
		t.Errorf("Expected 3 metas, got %d", len(metas))
	}

	// Verify message counts
	for _, meta := range metas {
		expected := 0
		if meta.Key == "session-1" {
			expected = 1
		} else if meta.Key == "session-2" {
			expected = 2
		} else if meta.Key == "session-3" {
			expected = 3
		}
		if meta.MessageCount != expected {
			t.Errorf("Session %s: expected MessageCount %d, got %d", meta.Key, expected, meta.MessageCount)
		}
	}
}

// TestBackwardCompatibility 测试向后兼容（旧格式文件）
func TestBackwardCompatibility(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()
	kanfluxDir := filepath.Join(tmpDir, ".kanflux", "sessions")
	if err := os.MkdirAll(kanfluxDir, 0755); err != nil {
		t.Fatalf("Failed to create dir: %v", err)
	}

	// Write old format file (metadata without message_count/instruction_count)
	sessionFile := filepath.Join(kanfluxDir, "old-format.jsonl")
	content := `{"_type":"metadata","created_at":"2024-01-15T10:00:00Z","updated_at":"2024-01-15T11:00:00Z"}
{"_type":"instruction","agent_name":"agent1","content":"test","timestamp":"2024-01-15T10:30:00Z","content_hash":"hash1"}
{"role":"user","content":"hello"}
{"role":"assistant","content":"hi"}
`
	if err := os.WriteFile(sessionFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write session file: %v", err)
	}

	mgr, err := NewManager(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Load old format session
	sess, err := mgr.GetOrCreate("old-format")
	if err != nil {
		t.Fatalf("Failed to load session: %v", err)
	}

	// Meta should have default 0 counts before data loaded
	meta := sess.GetMeta()
	if meta.MessageCount != 0 {
		t.Errorf("Old format should have 0 MessageCount before data loaded, got %d", meta.MessageCount)
	}

	// Trigger data loading
	history := sess.GetHistory(0)
	if len(history) != 2 {
		t.Errorf("Expected 2 messages, got %d", len(history))
	}

	// After loading, counts should be updated
	meta = sess.GetMeta()
	if meta.MessageCount != 2 {
		t.Errorf("After loading, MessageCount should be 2, got %d", meta.MessageCount)
	}
	if meta.InstrCount != 1 {
		t.Errorf("After loading, InstrCount should be 1, got %d", meta.InstrCount)
	}
}