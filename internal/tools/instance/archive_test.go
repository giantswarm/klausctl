package instance

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/giantswarm/klausctl/pkg/archive"
)

// archiveListResp is a test helper to decode the paginated response.
type archiveListResp struct {
	Total int                   `json:"total"`
	Items []archive.ListSummary `json:"items"`
}

func decodeArchiveListResp(t *testing.T, result *mcp.CallToolResult) archiveListResp {
	t.Helper()
	text := extractResultText(t, result)
	var resp archiveListResp
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("expected archiveListResponse JSON, got: %s", text)
	}
	return resp
}

func TestHandleArchiveListEmpty(t *testing.T) {
	sc := testServerContext(t)

	req := callToolRequest(nil)
	result, err := handleArchiveList(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resp := decodeArchiveListResp(t, result)
	if resp.Total != 0 {
		t.Errorf("expected total=0, got %d", resp.Total)
	}
	if len(resp.Items) != 0 {
		t.Errorf("expected 0 items, got %d", len(resp.Items))
	}
}

func TestHandleArchiveListWithEntries(t *testing.T) {
	sc := testServerContext(t)

	// Save two archive entries.
	entry1 := &archive.Entry{
		UUID:         "uuid-1",
		Name:         "dev",
		Status:       statusCompleted,
		MessageCount: 10,
		StoppedAt:    time.Now().Add(-1 * time.Hour),
	}
	entry2 := &archive.Entry{
		UUID:         "uuid-2",
		Name:         testEnvProd,
		Status:       testStatusError,
		MessageCount: 5,
		StoppedAt:    time.Now(),
	}
	if err := archive.Save(sc.Paths.ArchivesDir, entry1); err != nil {
		t.Fatalf("saving entry1: %v", err)
	}
	if err := archive.Save(sc.Paths.ArchivesDir, entry2); err != nil {
		t.Fatalf("saving entry2: %v", err)
	}

	req := callToolRequest(nil)
	result, err := handleArchiveList(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resp := decodeArchiveListResp(t, result)
	if resp.Total != 2 {
		t.Fatalf("expected total=2, got %d", resp.Total)
	}
	if len(resp.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(resp.Items))
	}

	// Newest first.
	if resp.Items[0].UUID != "uuid-2" {
		t.Errorf("first entry UUID = %q, want %q", resp.Items[0].UUID, "uuid-2")
	}
}

func TestHandleArchiveListLimit(t *testing.T) {
	sc := testServerContext(t)

	for i := 0; i < 5; i++ {
		e := &archive.Entry{
			UUID:      fmt.Sprintf("uuid-%d", i),
			Name:      fmt.Sprintf("inst-%d", i),
			Status:    statusCompleted,
			StoppedAt: time.Now().Add(time.Duration(i) * time.Minute),
		}
		if err := archive.Save(sc.Paths.ArchivesDir, e); err != nil {
			t.Fatalf("saving entry: %v", err)
		}
	}

	req := callToolRequest(map[string]any{paramLimit: float64(2)})
	result, err := handleArchiveList(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resp := decodeArchiveListResp(t, result)
	if resp.Total != 5 {
		t.Errorf("expected total=5, got %d", resp.Total)
	}
	if len(resp.Items) != 2 {
		t.Errorf("expected 2 items, got %d", len(resp.Items))
	}
}

func TestHandleArchiveListOffset(t *testing.T) {
	sc := testServerContext(t)

	for i := 0; i < 5; i++ {
		e := &archive.Entry{
			UUID:      fmt.Sprintf("uuid-%d", i),
			Name:      fmt.Sprintf("inst-%d", i),
			Status:    statusCompleted,
			StoppedAt: time.Now().Add(time.Duration(i) * time.Minute),
		}
		if err := archive.Save(sc.Paths.ArchivesDir, e); err != nil {
			t.Fatalf("saving entry: %v", err)
		}
	}

	req := callToolRequest(map[string]any{"offset": float64(3), paramLimit: float64(10)})
	result, err := handleArchiveList(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resp := decodeArchiveListResp(t, result)
	if resp.Total != 5 {
		t.Errorf("expected total=5, got %d", resp.Total)
	}
	if len(resp.Items) != 2 {
		t.Errorf("expected 2 items after offset=3, got %d", len(resp.Items))
	}
}

func TestHandleArchiveListNameFilter(t *testing.T) {
	sc := testServerContext(t)

	for _, name := range []string{"dev-alpha", "prod-beta", "dev-gamma"} {
		e := &archive.Entry{UUID: name, Name: name, Status: statusCompleted, StoppedAt: time.Now()}
		if err := archive.Save(sc.Paths.ArchivesDir, e); err != nil {
			t.Fatalf("saving entry: %v", err)
		}
	}

	req := callToolRequest(map[string]any{paramName: "dev"})
	result, err := handleArchiveList(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resp := decodeArchiveListResp(t, result)
	if resp.Total != 2 {
		t.Errorf("expected total=2, got %d", resp.Total)
	}
}

func TestHandleArchiveListTaggedFilter(t *testing.T) {
	sc := testServerContext(t)

	tagged := &archive.Entry{UUID: testUUIDTagged, Name: testUUIDTagged, Status: statusCompleted, StoppedAt: time.Now(), Tags: map[string]string{testTagEnv: testEnvProd}}
	untagged := &archive.Entry{UUID: "untagged", Name: "untagged", Status: statusCompleted, StoppedAt: time.Now()}
	if err := archive.Save(sc.Paths.ArchivesDir, tagged); err != nil {
		t.Fatalf("saving entry: %v", err)
	}
	if err := archive.Save(sc.Paths.ArchivesDir, untagged); err != nil {
		t.Fatalf("saving entry: %v", err)
	}

	req := callToolRequest(map[string]any{testUUIDTagged: true})
	result, err := handleArchiveList(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resp := decodeArchiveListResp(t, result)
	if resp.Total != 1 || resp.Items[0].UUID != testUUIDTagged {
		t.Errorf("expected only tagged entry, got %+v", resp)
	}
}

func TestHandleArchiveListOutcomeFilter(t *testing.T) {
	sc := testServerContext(t)

	e1 := &archive.Entry{UUID: "s1", Name: "s1", Status: statusCompleted, StoppedAt: time.Now(), Tags: map[string]string{paramOutcome: outcomeSuccess}}
	e2 := &archive.Entry{UUID: "f1", Name: "f1", Status: statusCompleted, StoppedAt: time.Now(), Tags: map[string]string{paramOutcome: testStatusFailed}}
	if err := archive.Save(sc.Paths.ArchivesDir, e1); err != nil {
		t.Fatalf("saving entry: %v", err)
	}
	if err := archive.Save(sc.Paths.ArchivesDir, e2); err != nil {
		t.Fatalf("saving entry: %v", err)
	}

	req := callToolRequest(map[string]any{paramOutcome: outcomeSuccess})
	result, err := handleArchiveList(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resp := decodeArchiveListResp(t, result)
	if resp.Total != 1 || resp.Items[0].UUID != "s1" {
		t.Errorf("expected only success entry, got %+v", resp)
	}
}

func TestHandleArchiveListSinceFilter(t *testing.T) {
	sc := testServerContext(t)

	old := &archive.Entry{UUID: testUUIDOld, Name: testUUIDOld, Status: statusCompleted, StoppedAt: time.Now().Add(-48 * time.Hour)}
	recent := &archive.Entry{UUID: testUUIDRecent, Name: testUUIDRecent, Status: statusCompleted, StoppedAt: time.Now()}
	if err := archive.Save(sc.Paths.ArchivesDir, old); err != nil {
		t.Fatalf("saving entry: %v", err)
	}
	if err := archive.Save(sc.Paths.ArchivesDir, recent); err != nil {
		t.Fatalf("saving entry: %v", err)
	}

	since := time.Now().Add(-24 * time.Hour).Format(time.RFC3339)
	req := callToolRequest(map[string]any{paramSince: since})
	result, err := handleArchiveList(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resp := decodeArchiveListResp(t, result)
	if resp.Total != 1 || resp.Items[0].UUID != testUUIDRecent {
		t.Errorf("expected only recent entry, got %+v", resp)
	}
}

func TestHandleArchiveListInvalidSince(t *testing.T) {
	sc := testServerContext(t)

	req := callToolRequest(map[string]any{paramSince: "not-a-date"})
	result, err := handleArchiveList(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertIsError(t, result)
}

func TestHandleArchiveShowByUUID(t *testing.T) {
	sc := testServerContext(t)

	entry := &archive.Entry{
		UUID:         testUUIDShow,
		Name:         testNameTest,
		Status:       statusCompleted,
		ResultText:   "All done.",
		MessageCount: 42,
		StoppedAt:    time.Now(),
	}
	if err := archive.Save(sc.Paths.ArchivesDir, entry); err != nil {
		t.Fatalf("saving entry: %v", err)
	}

	req := callToolRequest(map[string]any{paramUUID: testUUIDShow})
	result, err := handleArchiveShow(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractResultText(t, result)
	var loaded archive.Entry
	if err := json.Unmarshal([]byte(text), &loaded); err != nil {
		t.Fatalf("expected JSON object, got: %s", text)
	}
	if loaded.UUID != testUUIDShow {
		t.Errorf("UUID = %q, want %q", loaded.UUID, testUUIDShow)
	}
	if loaded.ResultText != "All done." {
		t.Errorf("ResultText = %q, want %q", loaded.ResultText, "All done.")
	}
}

func TestHandleArchiveShowOmitsMessagesByDefault(t *testing.T) {
	sc := testServerContext(t)

	entry := &archive.Entry{
		UUID:         "msg-uuid",
		Name:         testNameTest,
		Status:       statusCompleted,
		MessageCount: 5,
		Messages:     json.RawMessage(`[{"role":"user","content":"hello"}]`),
		StoppedAt:    time.Now(),
	}
	if err := archive.Save(sc.Paths.ArchivesDir, entry); err != nil {
		t.Fatalf("saving entry: %v", err)
	}

	// Without full=true, messages should be omitted.
	req := callToolRequest(map[string]any{paramUUID: "msg-uuid"})
	result, err := handleArchiveShow(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractResultText(t, result)
	var loaded map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text), &loaded); err != nil {
		t.Fatalf("expected JSON object, got: %s", text)
	}
	if _, ok := loaded["messages"]; ok {
		t.Error("messages should be omitted when full is not set")
	}
}

func TestHandleArchiveShowIncludesMessagesWhenFull(t *testing.T) {
	sc := testServerContext(t)

	entry := &archive.Entry{
		UUID:         "full-uuid",
		Name:         testNameTest,
		Status:       statusCompleted,
		MessageCount: 5,
		Messages:     json.RawMessage(`[{"role":"user","content":"hello"}]`),
		StoppedAt:    time.Now(),
	}
	if err := archive.Save(sc.Paths.ArchivesDir, entry); err != nil {
		t.Fatalf("saving entry: %v", err)
	}

	req := callToolRequest(map[string]any{paramUUID: "full-uuid", "full": true})
	result, err := handleArchiveShow(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractResultText(t, result)
	var loaded map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text), &loaded); err != nil {
		t.Fatalf("expected JSON object, got: %s", text)
	}
	if _, ok := loaded["messages"]; !ok {
		t.Error("messages should be included when full=true")
	}
}

func TestHandleArchiveShowNotFound(t *testing.T) {
	sc := testServerContext(t)

	req := callToolRequest(map[string]any{paramUUID: testNameMissing})
	result, err := handleArchiveShow(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertIsError(t, result)
}

func TestHandleArchiveTag(t *testing.T) {
	sc := testServerContext(t)

	entry := &archive.Entry{
		UUID:      "tag-uuid",
		Name:      testNameTest,
		Status:    statusCompleted,
		StoppedAt: time.Now(),
	}
	if err := archive.Save(sc.Paths.ArchivesDir, entry); err != nil {
		t.Fatalf("saving entry: %v", err)
	}

	req := callToolRequest(map[string]any{
		paramUUID: "tag-uuid",
		paramTags: map[string]any{testTagEnv: testEnvProd, "team": "platform"},
	})
	result, err := handleArchiveTag(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractResultText(t, result)
	var loaded archive.Entry
	if err := json.Unmarshal([]byte(text), &loaded); err != nil {
		t.Fatalf("expected JSON object, got: %s", text)
	}
	if loaded.Tags[testTagEnv] != testEnvProd {
		t.Errorf("Tags[env] = %q, want %q", loaded.Tags[testTagEnv], testEnvProd)
	}
	if loaded.Tags["team"] != "platform" {
		t.Errorf("Tags[team] = %q, want %q", loaded.Tags["team"], "platform")
	}
}

func TestHandleArchiveTagMerge(t *testing.T) {
	sc := testServerContext(t)

	entry := &archive.Entry{
		UUID:      "merge-uuid",
		Name:      testNameTest,
		Status:    statusCompleted,
		StoppedAt: time.Now(),
		Tags:      map[string]string{testTagEnv: "staging", "team": testUUIDOld},
	}
	if err := archive.Save(sc.Paths.ArchivesDir, entry); err != nil {
		t.Fatalf("saving entry: %v", err)
	}

	req := callToolRequest(map[string]any{
		paramUUID: "merge-uuid",
		paramTags: map[string]any{testTagEnv: testEnvProd},
	})
	result, err := handleArchiveTag(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractResultText(t, result)
	var loaded archive.Entry
	if err := json.Unmarshal([]byte(text), &loaded); err != nil {
		t.Fatalf("expected JSON object, got: %s", text)
	}
	if loaded.Tags[testTagEnv] != testEnvProd {
		t.Errorf("Tags[env] = %q, want %q", loaded.Tags[testTagEnv], testEnvProd)
	}
	if loaded.Tags["team"] != testUUIDOld {
		t.Errorf("Tags[team] = %q, want %q (should be preserved)", loaded.Tags["team"], testUUIDOld)
	}
}

func TestHandleArchiveTagNotFound(t *testing.T) {
	sc := testServerContext(t)

	req := callToolRequest(map[string]any{
		paramUUID: testNameMissing,
		paramTags: map[string]any{"k": "v"},
	})
	result, err := handleArchiveTag(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertIsError(t, result)
}

func TestHandleArchiveTagEmptyTags(t *testing.T) {
	sc := testServerContext(t)

	req := callToolRequest(map[string]any{
		paramUUID: "some-uuid",
		paramTags: map[string]any{},
	})
	result, err := handleArchiveTag(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertIsError(t, result)
}

func TestHandleArchiveTagMissingUUID(t *testing.T) {
	sc := testServerContext(t)

	req := callToolRequest(map[string]any{
		paramTags: map[string]any{"k": "v"},
	})
	result, err := handleArchiveTag(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertIsError(t, result)
}

func TestHandleArchiveShowMissingUUID(t *testing.T) {
	sc := testServerContext(t)

	req := callToolRequest(map[string]any{})
	result, err := handleArchiveShow(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertIsError(t, result)
}
