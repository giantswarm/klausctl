package instance

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/giantswarm/klausctl/pkg/archive"
)

func TestHandleArchiveListEmpty(t *testing.T) {
	sc := testServerContext(t)

	req := callToolRequest(nil)
	result, err := handleArchiveList(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertJSONArray(t, result, 0)
}

func TestHandleArchiveListWithEntries(t *testing.T) {
	sc := testServerContext(t)

	// Save two archive entries.
	entry1 := &archive.Entry{
		UUID:         "uuid-1",
		Name:         "dev",
		Status:       "completed",
		MessageCount: 10,
		StoppedAt:    time.Now().Add(-1 * time.Hour),
	}
	entry2 := &archive.Entry{
		UUID:         "uuid-2",
		Name:         "prod",
		Status:       "error",
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

	text := extractResultText(t, result)
	var list []archiveListEntry
	if err := json.Unmarshal([]byte(text), &list); err != nil {
		t.Fatalf("expected JSON array, got: %s", text)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(list))
	}

	// Newest first.
	if list[0].UUID != "uuid-2" {
		t.Errorf("first entry UUID = %q, want %q", list[0].UUID, "uuid-2")
	}
}

func TestHandleArchiveShowByUUID(t *testing.T) {
	sc := testServerContext(t)

	entry := &archive.Entry{
		UUID:         "show-uuid",
		Name:         "test",
		Status:       "completed",
		ResultText:   "All done.",
		MessageCount: 42,
		StoppedAt:    time.Now(),
	}
	if err := archive.Save(sc.Paths.ArchivesDir, entry); err != nil {
		t.Fatalf("saving entry: %v", err)
	}

	req := callToolRequest(map[string]any{"uuid": "show-uuid"})
	result, err := handleArchiveShow(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractResultText(t, result)
	var loaded archive.Entry
	if err := json.Unmarshal([]byte(text), &loaded); err != nil {
		t.Fatalf("expected JSON object, got: %s", text)
	}
	if loaded.UUID != "show-uuid" {
		t.Errorf("UUID = %q, want %q", loaded.UUID, "show-uuid")
	}
	if loaded.ResultText != "All done." {
		t.Errorf("ResultText = %q, want %q", loaded.ResultText, "All done.")
	}
}

func TestHandleArchiveShowNotFound(t *testing.T) {
	sc := testServerContext(t)

	req := callToolRequest(map[string]any{"uuid": "nonexistent"})
	result, err := handleArchiveShow(context.Background(), req, sc)
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
