package archive

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/giantswarm/klausctl/pkg/instance"
)

// Archive entry fixtures shared across tests.
const (
	testNameDev         = "dev"
	testNameTest        = "test"
	testNameOld         = "old"
	testNameNew         = "new"
	testStatusCompleted = "completed"
	testTagEnv          = "env"
	testTeamPlatform    = "platform"
	testEnvProd         = "prod"
)

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()

	entry := &Entry{
		UUID:         "test-uuid-1234",
		Name:         testNameDev,
		Image:        "test:latest",
		Workspace:    "/tmp/ws",
		Port:         8080,
		StartedAt:    time.Now().Add(-10 * time.Minute),
		StoppedAt:    time.Now(),
		ResultText:   "All tests passed.",
		MessageCount: 42,
		Status:       testStatusCompleted,
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
		Name:      testNameOld,
		StoppedAt: time.Now().Add(-1 * time.Hour),
		Status:    testStatusCompleted,
	}
	newer := &Entry{
		UUID:      "uuid-newer",
		Name:      testNameNew,
		StoppedAt: time.Now(),
		Status:    testStatusCompleted,
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

	entry := &Entry{UUID: "exists-test", Name: testNameTest, Status: testStatusCompleted}
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
		Name:      testNameDev,
		Image:     "test:v1",
		Workspace: "/ws",
		Port:      8080,
		StartedAt: time.Now().Add(-5 * time.Minute),
	}

	cost := 0.42
	resultJSON := mustMarshal(t, map[string]any{
		"status":         testStatusCompleted,
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
	if entry.Status != testStatusCompleted {
		t.Errorf("Status = %q, want %q", entry.Status, testStatusCompleted)
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
		Name: testNameTest,
	}

	entry, err := EntryFromResult(inst, "")
	if err != nil {
		t.Fatalf("EntryFromResult() error: %v", err)
	}
	if entry.Status != statusUnknown {
		t.Errorf("Status = %q, want %q", entry.Status, statusUnknown)
	}
}

func TestEntryFromResult_InvalidJSON(t *testing.T) {
	inst := &instance.Instance{
		UUID: "bad-uuid",
		Name: testNameTest,
	}

	entry, err := EntryFromResult(inst, "not json at all")
	if err != nil {
		t.Fatalf("EntryFromResult() error: %v", err)
	}
	if entry.Status != statusUnknown {
		t.Errorf("Status = %q, want %q", entry.Status, statusUnknown)
	}
	if entry.ResultText != "not json at all" {
		t.Errorf("ResultText = %q, want raw text", entry.ResultText)
	}
}

func TestToListSummary(t *testing.T) {
	cost := 1.23
	entry := &Entry{
		UUID:         "summary-uuid",
		Name:         testNameDev,
		Status:       testStatusCompleted,
		StoppedAt:    time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC),
		MessageCount: 42,
		TotalCostUSD: &cost,
	}

	s := entry.ToListSummary()
	if s.UUID != "summary-uuid" {
		t.Errorf("UUID = %q, want %q", s.UUID, "summary-uuid")
	}
	if s.Name != testNameDev {
		t.Errorf("Name = %q, want %q", s.Name, testNameDev)
	}
	if s.Status != testStatusCompleted {
		t.Errorf("Status = %q, want %q", s.Status, testStatusCompleted)
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

func TestTag_NewTags(t *testing.T) {
	dir := t.TempDir()

	entry := &Entry{UUID: "tag-uuid", Name: testNameDev, Status: testStatusCompleted}
	if err := Save(dir, entry); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	updated, err := Tag(dir, "tag-uuid", map[string]string{testTagEnv: testEnvProd, "team": testTeamPlatform})
	if err != nil {
		t.Fatalf("Tag() error: %v", err)
	}
	if updated.Tags[testTagEnv] != testEnvProd {
		t.Errorf("Tags[env] = %q, want %q", updated.Tags[testTagEnv], testEnvProd)
	}
	if updated.Tags["team"] != testTeamPlatform { //nolint:goconst
		t.Errorf("Tags[team] = %q, want %q", updated.Tags["team"], testTeamPlatform)
	}

	// Verify persistence by reloading.
	reloaded, err := Load(dir, "tag-uuid")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if reloaded.Tags[testTagEnv] != testEnvProd {
		t.Errorf("reloaded Tags[env] = %q, want %q", reloaded.Tags[testTagEnv], testEnvProd)
	}
	if reloaded.Tags["team"] != testTeamPlatform {
		t.Errorf("reloaded Tags[team] = %q, want %q", reloaded.Tags["team"], testTeamPlatform)
	}
}

func TestTag_MergeOverwrite(t *testing.T) {
	dir := t.TempDir()

	entry := &Entry{
		UUID:   "merge-uuid",
		Name:   testNameDev,
		Status: testStatusCompleted,
		Tags:   map[string]string{testTagEnv: "staging", "team": testTeamPlatform},
	}
	if err := Save(dir, entry); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	updated, err := Tag(dir, "merge-uuid", map[string]string{testTagEnv: testEnvProd, "region": "eu"})
	if err != nil {
		t.Fatalf("Tag() error: %v", err)
	}
	// "env" should be overwritten.
	if updated.Tags[testTagEnv] != testEnvProd {
		t.Errorf("Tags[env] = %q, want %q", updated.Tags[testTagEnv], testEnvProd)
	}
	// "team" should be preserved.
	if updated.Tags["team"] != testTeamPlatform {
		t.Errorf("Tags[team] = %q, want %q", updated.Tags["team"], testTeamPlatform)
	}
	// "region" should be added.
	if updated.Tags["region"] != "eu" {
		t.Errorf("Tags[region] = %q, want %q", updated.Tags["region"], "eu")
	}

	// Verify all three tags persisted.
	reloaded, err := Load(dir, "merge-uuid")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if len(reloaded.Tags) != 3 {
		t.Errorf("len(Tags) = %d, want 3", len(reloaded.Tags))
	}
}

func TestTag_NotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := Tag(dir, "nonexistent", map[string]string{"k": "v"})
	if err == nil {
		t.Fatal("Tag() should return error for missing archive")
	}
}

func TestLoadEntryWithoutTags(t *testing.T) {
	dir := t.TempDir()

	// Write an entry JSON without a tags field (backward compatibility).
	raw := `{"uuid":"no-tags","name":"old","status":"completed"}`
	if err := os.WriteFile(filepath.Join(dir, "no-tags.json"), []byte(raw), 0o600); err != nil {
		t.Fatalf("writing file: %v", err)
	}

	entry, err := Load(dir, "no-tags")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if entry.Tags != nil {
		t.Errorf("Tags = %v, want nil for legacy entry", entry.Tags)
	}
}

func TestFilterEntries_Since(t *testing.T) {
	now := time.Now()
	entries := []*Entry{
		{UUID: testNameOld, StoppedAt: now.Add(-2 * time.Hour)},
		{UUID: testNameNew, StoppedAt: now},
	}
	result := FilterEntries(entries, Filter{Since: now.Add(-1 * time.Hour)})
	if len(result) != 1 || result[0].UUID != testNameNew {
		t.Errorf("expected only 'new', got %v", result)
	}
}

func TestFilterEntries_Name(t *testing.T) {
	entries := []*Entry{
		{UUID: "1", Name: "dev-alpha"},
		{UUID: "2", Name: "prod-beta"},
		{UUID: "3", Name: "dev-gamma"},
	}
	result := FilterEntries(entries, Filter{Name: testNameDev})
	if len(result) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(result))
	}
}

func TestFilterEntries_Tagged(t *testing.T) {
	tagged := true
	untagged := false
	entries := []*Entry{
		{UUID: "1", Tags: map[string]string{testTagEnv: testEnvProd}},
		{UUID: "2"},
		{UUID: "3", Tags: map[string]string{tagOutcome: outcomeSuccess}},
	}

	onlyTagged := FilterEntries(entries, Filter{Tagged: &tagged})
	if len(onlyTagged) != 2 {
		t.Errorf("expected 2 tagged entries, got %d", len(onlyTagged))
	}

	onlyUntagged := FilterEntries(entries, Filter{Tagged: &untagged})
	if len(onlyUntagged) != 1 || onlyUntagged[0].UUID != "2" {
		t.Errorf("expected 1 untagged entry, got %v", onlyUntagged)
	}
}

func TestFilterEntries_Outcome(t *testing.T) {
	entries := []*Entry{
		{UUID: "1", Tags: map[string]string{tagOutcome: outcomeSuccess}},
		{UUID: "2", Tags: map[string]string{tagOutcome: outcomeFailed}},
		{UUID: "3"},
	}
	result := FilterEntries(entries, Filter{Outcome: outcomeSuccess})
	if len(result) != 1 || result[0].UUID != "1" {
		t.Errorf("expected only entry 1, got %v", result)
	}
}

func TestFilterEntries_Combined(t *testing.T) {
	now := time.Now()
	tagged := true
	entries := []*Entry{
		{UUID: "1", Name: "dev-one", StoppedAt: now, Tags: map[string]string{tagOutcome: outcomeSuccess}},
		{UUID: "2", Name: "dev-two", StoppedAt: now.Add(-2 * time.Hour), Tags: map[string]string{tagOutcome: outcomeSuccess}},
		{UUID: "3", Name: testEnvProd, StoppedAt: now, Tags: map[string]string{tagOutcome: outcomeSuccess}},
		{UUID: "4", Name: "dev-three", StoppedAt: now},
	}
	result := FilterEntries(entries, Filter{
		Since:   now.Add(-1 * time.Hour),
		Name:    testNameDev,
		Tagged:  &tagged,
		Outcome: outcomeSuccess,
	})
	if len(result) != 1 || result[0].UUID != "1" {
		t.Errorf("expected only entry 1, got %v", result)
	}
}

func TestFilterEntries_NoFilter(t *testing.T) {
	entries := []*Entry{{UUID: "1"}, {UUID: "2"}}
	result := FilterEntries(entries, Filter{})
	if len(result) != 2 {
		t.Errorf("expected 2 entries with no filter, got %d", len(result))
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
