package memoria

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kinwyb/kanflux/knowledgebase/memoria/storage"
	"github.com/kinwyb/kanflux/knowledgebase/memoria/types"
)

func TestConfig(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Workspace = t.TempDir()

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate failed: %v", err)
	}

	if cfg.GetMemoriaDir() == "" {
		t.Error("GetMemoriaDir should not be empty")
	}

	if cfg.GetL1Dir() == "" {
		t.Error("GetL1Dir should not be empty")
	}

	if cfg.GetL2Dir() == "" {
		t.Error("GetL2Dir should not be empty")
	}
}

func TestParseSessionKey(t *testing.T) {
	tests := []struct {
		key      string
		expected *types.DefaultUserIdentity
	}{
		{
			key: "tui:default:ABC123",
			expected: &types.DefaultUserIdentity{
				UserID:    "tui:default:ABC123",
				Channel:   "tui",
				AccountID: "default",
				ChatID:    "ABC123",
			},
		},
		{
			key: "simple",
			expected: &types.DefaultUserIdentity{
				UserID: "simple",
			},
		},
	}

	for _, tt := range tests {
		result := ParseSessionKey(tt.key)
		if result.UserID != tt.expected.UserID {
			t.Errorf("UserID mismatch: got %s, want %s", result.UserID, tt.expected.UserID)
		}
		if result.Channel != tt.expected.Channel {
			t.Errorf("Channel mismatch: got %s, want %s", result.Channel, tt.expected.Channel)
		}
	}
}

func TestMemoryItem(t *testing.T) {
	item := &types.MemoryItem{
		ID:        "test_1",
		HallType:  types.HallFacts,
		Layer:     types.LayerL1,
		Content:   "Test content",
		Summary:   "Test summary",
		Source:    "test",
		UserID:    "user1",
		Timestamp: time.Now(),
		Tokens:    10,
	}

	if item.ID != "test_1" {
		t.Errorf("ID mismatch: got %s", item.ID)
	}
	if item.HallType != types.HallFacts {
		t.Errorf("HallType mismatch: got %s", item.HallType)
	}
	if item.Layer != types.LayerL1 {
		t.Errorf("Layer mismatch: got %d", item.Layer)
	}
}

func TestL1Layer(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := storage.NewMDStore(tmpDir, &types.StorageConfig{
		MaxL1Tokens:  120,
		MaxL2Tokens:  500,
		DateFormat:   "2006-01-02",
		EnableBackup: false,
	})
	if err != nil {
		t.Fatalf("NewMDStore failed: %v", err)
	}
	defer store.Close()

	l1 := NewL1FactsLayer(store, 120)

	ctx := context.Background()
	if err := l1.Initialize(ctx); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	item := &types.MemoryItem{
		ID:        "l1_test_1",
		HallType:  types.HallFacts,
		Layer:     types.LayerL1,
		Content:   "L1 test content",
		Summary:   "L1 test summary",
		Source:    "test",
		UserID:    "user1",
		Timestamp: time.Now(),
		Tokens:    10,
	}

	if err := l1.Add(ctx, item); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	all := l1.GetAll()
	if len(all) == 0 {
		t.Error("Expected at least one item")
	}

	userItems := l1.GetForUser("user1")
	if len(userItems) == 0 {
		t.Error("Expected at least one item for user1")
	}
}

func TestL2Layer(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := storage.NewMDStore(tmpDir, &types.StorageConfig{
		MaxL1Tokens:  120,
		MaxL2Tokens:  500,
		DateFormat:   "2006-01-02",
		EnableBackup: false,
	})
	if err != nil {
		t.Fatalf("NewMDStore failed: %v", err)
	}
	defer store.Close()

	l2 := NewL2EventsLayer(store, 500)

	ctx := context.Background()
	if err := l2.Initialize(ctx); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	item := &types.MemoryItem{
		ID:        "l2_test_1",
		HallType:  types.HallEvents,
		Layer:     types.LayerL2,
		Content:   "L2 test content",
		Summary:   "L2 test summary",
		Source:    "test",
		UserID:    "user1",
		Timestamp: time.Now(),
		Tokens:    50,
	}

	if err := l2.Add(ctx, item); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	items, err := l2.LoadRecent(ctx, "user1", 7)
	if err != nil {
		t.Fatalf("LoadRecent failed: %v", err)
	}

	if len(items) == 0 {
		t.Error("Expected at least one item")
	}
}

func TestHallTypes(t *testing.T) {
	tests := []struct {
		content  string
		hallType types.HallType
		layer    types.Layer
	}{
		{
			content:  "We decided to use PostgreSQL for the database",
			hallType: types.HallFacts,
			layer:    types.LayerL1,
		},
		{
			content:  "I prefer dark mode in all my editors",
			hallType: types.HallPreferences,
			layer:    types.LayerL1,
		},
		{
			content:  "Just finished the debugging session for the auth module",
			hallType: types.HallEvents,
			layer:    types.LayerL2,
		},
		{
			content:  "Discovered that the issue was caused by race condition",
			hallType: types.HallDiscoveries,
			layer:    types.LayerL2,
		},
		{
			content:  "I recommend using connection pooling for better performance",
			hallType: types.HallAdvice,
			layer:    types.LayerL2,
		},
	}

	for _, tt := range tests {
		if tt.hallType != types.HallFacts && tt.hallType != types.HallEvents &&
			tt.hallType != types.HallDiscoveries && tt.hallType != types.HallPreferences &&
			tt.hallType != types.HallAdvice {
			t.Errorf("Invalid hall type: %s", tt.hallType)
		}
		if tt.layer != types.LayerL1 && tt.layer != types.LayerL2 && tt.layer != types.LayerL3 {
			t.Errorf("Invalid layer: %d", tt.layer)
		}
	}
}

func TestMemoriaService(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := DefaultConfig()
	cfg.Workspace = tmpDir
	cfg.ScheduleConfig.Enabled = false

	m, err := New(cfg)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	defer m.Close()

	ctx := context.Background()
	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	stats := m.GetStats()
	if stats == nil {
		t.Error("GetStats should return non-nil")
	}

	facts := m.GetL1All()
	if facts == nil {
		t.Error("GetL1All should return non-nil (empty slice)")
	}
}

func TestFileStructure(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := DefaultConfig()
	cfg.Workspace = tmpDir

	m, err := New(cfg)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	defer m.Close()

	dirs := []string{
		m.GetMemoriaDir(),
		cfg.GetL1Dir(),
		cfg.GetL2Dir(),
	}

	for _, dir := range dirs {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			t.Errorf("Directory should exist: %s", dir)
		}
	}
}

func TestWatchPath(t *testing.T) {
	workspace := "/test/workspace"
	paths := GetDefaultWatchPaths(workspace)

	if len(paths) == 0 {
		t.Error("Expected at least one watch path")
	}

	expectedPaths := []string{".kanflux/sessions/", "docs/", "memory/"}
	for _, expected := range expectedPaths {
		found := false
		for _, p := range paths {
			if filepath.Base(p.Path) == filepath.Base(expected) ||
				filepath.Dir(p.Path) == filepath.Dir(expected) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected watch path containing %s", expected)
		}
	}
}