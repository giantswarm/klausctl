package archive

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/giantswarm/klausctl/pkg/instance"
)

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()

	entry := &Entry{
		UUID:         "test-uuid-1234",
		Name:         "dev",
		Image:        "test:latest",
		Workspace:    "/tmp/ws",
		Port:         8080,
		StartedAt:    time.Now().Add(-10 * time.Minute),
		StoppedAt:    time.Now(),
		ResultText:   "All tests passed.",
		MessageCount: 42,
		Status:       "completed",
	}

	if err := Save(dir, entry); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	// Verify file exists.
	if _, err := os.Stat(filepath.Join(dir, "test-uuid-1234.json")); err != nil {
		t.Fatalf("archive file not created: %v", err)
	}

	loaded, err := Load(dir, "test-uuid-1234")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if loaded.UUID != entry.UUID {
		t.Errorf("UUID = %q, want %q", loaded.UUID, entry.UUID)
	}
	if loaded.Name != entry.Name {
		t.Errorf("Name = %q, want %q", loaded.Name, entry.Name)
	}
	if loaded.Status != entry.Status {
		t.Errorf("Status = %q, want %q", loaded.Status, entry.Status)
	}
	if loaded.MessageCount != entry.MessageCount {
		t.Errorf("MessageCount = %d, want %d", loaded.MessageCount, entry.MessageCount)
	}
	if loaded.ResultText != entry.ResultText {
		t.Errorf("ResultText = %q, want %q", loaded.ResultText, entry.ResultText)
	}
}

func TestLoadMissing(t *testing.T) {
	dir := t.TempDir()
	_, err := Load(dir, "nonexistent")
	if err == nil {
		t.Fatal("Load() should return error for missing file")
	}
}

func TestLoadAll(t *testing.T) {
	dir := t.TempDir()

	// Save two entries with different stop times.
	older := &Entry{
		UUID:      "uuid-older",
		Name:      "old",
		StoppedAt: time.Now().Add(-1 * time.Hour),
		Status:    "completed",
	}
	newer := &Entry{
		UUID:      "uuid-newer",
		Name:      "new",
		StoppedAt: time.Now(),
		Status:    "completed",
	}

	if err := Save(dir, older); err != nil {
		t.Fatalf("Save older: %v", err)
	}
	if err := Save(dir, newer); err != nil {
		t.Fatalf("Save newer: %v", err)
	}

	entries, err := LoadAll(dir)
	if err != nil {
		t.Fatalf("LoadAll() error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("LoadAll() returned %d entries, want 2", len(entries))
	}

	// Should be sorted newest first.
	if entries[0].UUID != "uuid-newer" {
		t.Errorf("first entry UUID = %q, want %q", entries[0].UUID, "uuid-newer")
	}
	if entries[1].UUID != "uuid-older" {
		t.Errorf("second entry UUID = %q, want %q", entries[1].UUID, "uuid-older")
	}
}

func TestLoadAllEmpty(t *testing.T) {
	dir := t.TempDir()
	entries, err := LoadAll(dir)
	if err != nil {
		t.Fatalf("LoadAll() error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("LoadAll() returned %d entries, want 0", len(entries))
	}
}

func TestLoadAllMissingDir(t *testing.T) {
	entries, err := LoadAll("/nonexistent/path")
	if err != nil {
		t.Fatalf("LoadAll() error: %v", err)
	}
	if entries != nil {
		t.Errorf("LoadAll() = %v, want nil", entries)
	}
}

func TestExists(t *testing.T) {
	dir := t.TempDir()

	if Exists(dir, "nope") {
		t.Error("Exists() should return false for missing archive")
	}
	if Exists(dir, "") {
		t.Error("Exists() should return false for empty UUID")
	}

	entry := &Entry{UUID: "exists-test", Name: "test", Status: "completed"}
	if err := Save(dir, entry); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	if !Exists(dir, "exists-test") {
		t.Error("Exists() should return true after Save()")
	}
}

func TestEntryFromResult_FullJSON(t *testing.T) {
	inst := &instance.Instance{
		UUID:      "inst-uuid",
		Name:      "dev",
		Image:     "test:v1",
		Workspace: "/ws",
		Port:      8080,
		StartedAt: time.Now().Add(-5 * time.Minute),
	}

	cost := 0.42
	resultJSON := mustMarshal(t, map[string]any{
		"status":         "completed",
		"result_text":    "All done.",
		"message_count":  50,
		"session_id":     "sess-123",
		"total_cost_usd": cost,
		"tool_calls":     map[string]int{"Read": 10, "Write": 5},
		"model_usage":    map[string]int{"opus": 30, "haiku": 20},
		"pr_urls":        []string{"https://github.com/org/repo/pull/1"},
		"error_count":    0,
	})

	entry, err := EntryFromResult(inst, resultJSON)
	if err != nil {
		t.Fatalf("EntryFromResult() error: %v", err)
	}

	if entry.UUID != "inst-uuid" {
		t.Errorf("UUID = %q, want %q", entry.UUID, "inst-uuid")
	}
	if entry.Status != "completed" {
		t.Errorf("Status = %q, want %q", entry.Status, "completed")
	}
	if entry.ResultText != "All done." {
		t.Errorf("ResultText = %q, want %q", entry.ResultText, "All done.")
	}
	if entry.MessageCount != 50 {
		t.Errorf("MessageCount = %d, want %d", entry.MessageCount, 50)
	}
	if entry.TotalCostUSD == nil || *entry.TotalCostUSD != 0.42 {
		t.Errorf("TotalCostUSD = %v, want 0.42", entry.TotalCostUSD)
	}
	if len(entry.ToolCalls) != 2 {
		t.Errorf("ToolCalls len = %d, want 2", len(entry.ToolCalls))
	}
	if len(entry.PRURLs) != 1 {
		t.Errorf("PRURLs len = %d, want 1", len(entry.PRURLs))
	}
}

func TestEntryFromResult_EmptyJSON(t *testing.T) {
	inst := &instance.Instance{
		UUID: "empty-uuid",
		Name: "test",
	}

	entry, err := EntryFromResult(inst, "")
	if err != nil {
		t.Fatalf("EntryFromResult() error: %v", err)
	}
	if entry.Status != "unknown" {
		t.Errorf("Status = %q, want %q", entry.Status, "unknown")
	}
}

func TestEntryFromResult_InvalidJSON(t *testing.T) {
	inst := &instance.Instance{
		UUID: "bad-uuid",
		Name: "test",
	}

	entry, err := EntryFromResult(inst, "not json at all")
	if err != nil {
		t.Fatalf("EntryFromResult() error: %v", err)
	}
	if entry.Status != "unknown" {
		t.Errorf("Status = %q, want %q", entry.Status, "unknown")
	}
	if entry.ResultText != "not json at all" {
		t.Errorf("ResultText = %q, want raw text", entry.ResultText)
	}
}

func TestToListSummary(t *testing.T) {
	cost := 1.23
	entry := &Entry{
		UUID:         "summary-uuid",
		Name:         "dev",
		Status:       "completed",
		StoppedAt:    time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC),
		MessageCount: 42,
		TotalCostUSD: &cost,
	}

	s := entry.ToListSummary()
	if s.UUID != "summary-uuid" {
		t.Errorf("UUID = %q, want %q", s.UUID, "summary-uuid")
	}
	if s.Name != "dev" {
		t.Errorf("Name = %q, want %q", s.Name, "dev")
	}
	if s.Status != "completed" {
		t.Errorf("Status = %q, want %q", s.Status, "completed")
	}
	if s.MessageCount != 42 {
		t.Errorf("MessageCount = %d, want %d", s.MessageCount, 42)
	}
	if s.TotalCostUSD == nil || *s.TotalCostUSD != 1.23 {
		t.Errorf("TotalCostUSD = %v, want 1.23", s.TotalCostUSD)
	}
	if s.StoppedAt != "2025-06-15T10:30:00Z" {
		t.Errorf("StoppedAt = %q, want %q", s.StoppedAt, "2025-06-15T10:30:00Z")
	}
}

func mustMarshal(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return string(data)
}
