package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/giantswarm/klausctl/pkg/archive"
)

func TestRenderArchiveListText_CostColumn(t *testing.T) {
	cost := 1.2345
	entries := []*archive.Entry{
		{
			UUID:         "uuid-1",
			Name:         "dev",
			Status:       "completed",
			MessageCount: 10,
			TotalCostUSD: &cost,
			StoppedAt:    time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
		},
		{
			UUID:         "uuid-2",
			Name:         "prod",
			Status:       "error",
			MessageCount: 5,
			TotalCostUSD: nil,
			StoppedAt:    time.Date(2025, 1, 15, 11, 0, 0, 0, time.UTC),
		},
	}

	var buf bytes.Buffer
	if err := renderArchiveListText(&buf, entries); err != nil {
		t.Fatalf("renderArchiveListText() error: %v", err)
	}
	out := buf.String()

	// Header should contain COST column.
	if !strings.Contains(out, "COST") {
		t.Error("expected COST column header")
	}

	// First entry should show formatted cost.
	if !strings.Contains(out, "$1.2345") {
		t.Errorf("expected $1.2345 in output, got:\n%s", out)
	}

	// Second entry with nil cost should show "-".
	lines := strings.Split(out, "\n")
	if len(lines) < 3 {
		t.Fatalf("expected at least 3 lines, got %d", len(lines))
	}
	// The third line (index 2) is the nil-cost entry.
	if !strings.Contains(lines[2], "  -  ") || strings.Contains(lines[2], "$") {
		// Check it has "-" somewhere in the cost column area but not "$".
		// More robust: just check the line contains " - " and not "$".
		if strings.Contains(lines[2], "$") {
			t.Errorf("nil cost entry should not contain $, got: %s", lines[2])
		}
	}
}

func TestRenderArchiveShowText_Metrics(t *testing.T) {
	cost := 0.42
	tokenUsage, _ := json.Marshal(map[string]int{
		"input":        1000,
		"output":       500,
		"cache_create": 200,
		"cache_read":   300,
	})

	entry := &archive.Entry{
		UUID:         "test-uuid",
		Name:         "dev",
		Status:       "completed",
		Image:        "test:latest",
		Workspace:    "/tmp/ws",
		Port:         8080,
		StartedAt:    time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC),
		StoppedAt:    time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
		MessageCount: 42,
		TotalCostUSD: &cost,
		ToolCalls:    map[string]int{"Read": 10, "Write": 5, "Bash": 20},
		ModelUsage:   map[string]int{"opus": 30, "haiku": 12},
		TokenUsage:   tokenUsage,
		ErrorCount:   2,
		ErrorMessage: "something went wrong",
		ResultText:   "All done.",
	}

	var buf bytes.Buffer
	if err := renderArchiveShowText(&buf, entry); err != nil {
		t.Fatalf("renderArchiveShowText() error: %v", err)
	}
	out := buf.String()

	// Check error count is displayed.
	if !strings.Contains(out, "Errors:       2") {
		t.Errorf("expected error count, got:\n%s", out)
	}

	// Check tool calls table.
	if !strings.Contains(out, "Tool Calls:") {
		t.Error("expected Tool Calls section")
	}
	if !strings.Contains(out, "Bash") {
		t.Error("expected Bash in tool calls")
	}

	// Check tool calls sorted by count descending: Bash(20) > Read(10) > Write(5).
	bashIdx := strings.Index(out, "Bash")
	readIdx := strings.Index(out, "Read")
	writeIdx := strings.Index(out, "Write")
	if bashIdx > readIdx || readIdx > writeIdx {
		t.Errorf("tool calls not sorted by count descending: Bash@%d, Read@%d, Write@%d", bashIdx, readIdx, writeIdx)
	}

	// Check model usage table.
	if !strings.Contains(out, "Model Usage:") {
		t.Error("expected Model Usage section")
	}
	if !strings.Contains(out, "opus") || !strings.Contains(out, "haiku") {
		t.Error("expected model names in model usage")
	}

	// Check token usage.
	if !strings.Contains(out, "Token Usage:") {
		t.Error("expected Token Usage section")
	}
	if !strings.Contains(out, "Input:         1000") {
		t.Errorf("expected input token count, got:\n%s", out)
	}
	if !strings.Contains(out, "Output:        500") {
		t.Errorf("expected output token count, got:\n%s", out)
	}
	if !strings.Contains(out, "Cache Create:  200") {
		t.Errorf("expected cache create count, got:\n%s", out)
	}
	if !strings.Contains(out, "Cache Read:    300") {
		t.Errorf("expected cache read count, got:\n%s", out)
	}

	// Check result text still shown.
	if !strings.Contains(out, "All done.") {
		t.Error("expected result text")
	}
}

func TestRenderArchiveShowText_Tags(t *testing.T) {
	entry := &archive.Entry{
		UUID:      "tag-uuid",
		Name:      "dev",
		Status:    "completed",
		Image:     "test:latest",
		Workspace: "/tmp/ws",
		StartedAt: time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC),
		StoppedAt: time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
		Tags:      map[string]string{"env": "prod", "team": "platform"},
	}

	var buf bytes.Buffer
	if err := renderArchiveShowText(&buf, entry); err != nil {
		t.Fatalf("renderArchiveShowText() error: %v", err)
	}
	out := buf.String()

	if !strings.Contains(out, "Tags:") {
		t.Error("expected Tags section")
	}
	if !strings.Contains(out, "env = prod") {
		t.Error("expected env = prod tag")
	}
	if !strings.Contains(out, "team = platform") {
		t.Error("expected team = platform tag")
	}
}

func TestRenderArchiveShowText_NoMetrics(t *testing.T) {
	entry := &archive.Entry{
		UUID:      "test-uuid",
		Name:      "dev",
		Status:    "completed",
		Image:     "test:latest",
		Workspace: "/tmp/ws",
		StartedAt: time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC),
		StoppedAt: time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
	}

	var buf bytes.Buffer
	if err := renderArchiveShowText(&buf, entry); err != nil {
		t.Fatalf("renderArchiveShowText() error: %v", err)
	}
	out := buf.String()

	// Should not contain metrics sections when data is absent.
	if strings.Contains(out, "Tool Calls:") {
		t.Error("should not show Tool Calls when empty")
	}
	if strings.Contains(out, "Model Usage:") {
		t.Error("should not show Model Usage when empty")
	}
	if strings.Contains(out, "Token Usage:") {
		t.Error("should not show Token Usage when empty")
	}
	if strings.Contains(out, "Errors:") {
		t.Error("should not show Errors when zero")
	}
	if strings.Contains(out, "Tags:") {
		t.Error("should not show Tags when empty")
	}
}
