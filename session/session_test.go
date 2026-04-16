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

func TestSessionAddInstruction(t *testing.T) {
	sess := &Session{
		Key:          "test-session",
		Instructions: []InstructionEntry{},
		Messages:     []adk.Message{},
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		Metadata:     make(map[string]interface{}),
	}

	// First instruction should be added
	entry1 := InstructionEntry{
		AgentName: "agent1",
		Content:   "instruction content 1",
		Timestamp: time.Now(),
	}
	if !sess.AddInstruction(entry1) {
		t.Error("First instruction should be added")
	}
	if len(sess.Instructions) != 1 {
		t.Errorf("Expected 1 instruction, got %d", len(sess.Instructions))
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
	if len(sess.Instructions) != 1 {
		t.Errorf("Expected 1 instruction after duplicate, got %d", len(sess.Instructions))
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
	if len(sess.Instructions) != 2 {
		t.Errorf("Expected 2 instructions, got %d", len(sess.Instructions))
	}
}

func TestSessionGetInstructions(t *testing.T) {
	sess := &Session{
		Key:          "test-session",
		Instructions: []InstructionEntry{
			{AgentName: "agent1", Content: "content1", Timestamp: time.Now()},
			{AgentName: "agent2", Content: "content2", Timestamp: time.Now()},
		},
		Messages:  []adk.Message{},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Metadata:  make(map[string]interface{}),
	}

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
	sess := &Session{
		Key:          "test-session",
		Instructions: []InstructionEntry{
			{AgentName: "agent1", Content: "content1", Timestamp: time.Now()},
		},
		Messages: []adk.Message{
			schema.UserMessage("hello"),
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Metadata:  make(map[string]interface{}),
	}

	sess.Clear()
	if len(sess.Instructions) != 0 {
		t.Errorf("Clear should empty Instructions, got %d", len(sess.Instructions))
	}
	if len(sess.Messages) != 0 {
		t.Errorf("Clear should empty Messages, got %d", len(sess.Messages))
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

	// Clear cache and reload
	mgr.mu.Lock()
	delete(mgr.sessions, "test-key")
	mgr.mu.Unlock()

	// Load from disk
	sess2, err := mgr.GetOrCreate("test-key")
	if err != nil {
		t.Fatalf("Failed to load session: %v", err)
	}

	// Verify instructions loaded
	if len(sess2.Instructions) != 1 {
		t.Errorf("Expected 1 instruction after load, got %d", len(sess2.Instructions))
	}
	if sess2.Instructions[0].Content != "test instruction content" {
		t.Errorf("Instruction content mismatch: got %s", sess2.Instructions[0].Content)
	}
	if sess2.Instructions[0].AgentName != "test-agent" {
		t.Errorf("Instruction agent name mismatch: got %s", sess2.Instructions[0].AgentName)
	}

	// Verify messages loaded
	if len(sess2.Messages) != 1 {
		t.Errorf("Expected 1 message after load, got %d", len(sess2.Messages))
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
	if len(sess.Messages) != 2 {
		t.Errorf("Expected 2 messages from legacy format, got %d", len(sess.Messages))
	}
	if sess.Messages[0].Content != "hello" {
		t.Errorf("First message content mismatch: got %s", sess.Messages[0].Content)
	}
}