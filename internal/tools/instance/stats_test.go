package instance

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/giantswarm/klausctl/internal/server"
	"github.com/giantswarm/klausctl/pkg/archive"
)

func saveTestEntries(t *testing.T, sc *server.ServerContext) {
	t.Helper()
	now := time.Now()
	cost1 := 1.50
	cost2 := 3.00

	entries := []*archive.Entry{
		{
			UUID:         "s1",
			Name:         "run-1",
			StartedAt:    now.Add(-30 * time.Minute),
			StoppedAt:    now.Add(-20 * time.Minute),
			MessageCount: 100,
			TotalCostUSD: &cost1,
			Tags:         map[string]string{"outcome": "success", "repo": "frontend"},
		},
		{
			UUID:         "s2",
			Name:         "run-2",
			StartedAt:    now.Add(-60 * time.Minute),
			StoppedAt:    now.Add(-50 * time.Minute),
			MessageCount: 200,
			TotalCostUSD: &cost2,
			Tags:         map[string]string{"outcome": "failed", "repo": "backend"},
		},
	}
	for _, e := range entries {
		if err := archive.Save(sc.Paths.ArchivesDir, e); err != nil {
			t.Fatalf("saving entry: %v", err)
		}
	}
}

func TestHandleStatsSummary(t *testing.T) {
	sc := testServerContext(t)
	saveTestEntries(t, sc)

	req := callToolRequest(nil)
	result, err := handleStatsSummary(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractResultText(t, result)
	var stats archive.SummaryStats
	if err := json.Unmarshal([]byte(text), &stats); err != nil {
		t.Fatalf("expected JSON object, got: %s", text)
	}
	if stats.TotalRuns != 2 {
		t.Errorf("TotalRuns = %d, want 2", stats.TotalRuns)
	}
	if stats.Success != 1 {
		t.Errorf("Success = %d, want 1", stats.Success)
	}
	if stats.Failure != 1 {
		t.Errorf("Failure = %d, want 1", stats.Failure)
	}
}

func TestHandleStatsSummaryWithFilters(t *testing.T) {
	sc := testServerContext(t)
	saveTestEntries(t, sc)

	req := callToolRequest(map[string]any{"repo": "frontend"})
	result, err := handleStatsSummary(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractResultText(t, result)
	var stats archive.SummaryStats
	if err := json.Unmarshal([]byte(text), &stats); err != nil {
		t.Fatalf("expected JSON object, got: %s", text)
	}
	if stats.TotalRuns != 1 {
		t.Errorf("TotalRuns = %d, want 1", stats.TotalRuns)
	}
}

func TestHandleStatsSummaryInvalidSince(t *testing.T) {
	sc := testServerContext(t)

	req := callToolRequest(map[string]any{"since": "not-a-date"})
	result, err := handleStatsSummary(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertIsError(t, result)
}

func TestHandleStatsSummaryEmpty(t *testing.T) {
	sc := testServerContext(t)

	req := callToolRequest(nil)
	result, err := handleStatsSummary(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractResultText(t, result)
	var stats archive.SummaryStats
	if err := json.Unmarshal([]byte(text), &stats); err != nil {
		t.Fatalf("expected JSON object, got: %s", text)
	}
	if stats.TotalRuns != 0 {
		t.Errorf("TotalRuns = %d, want 0", stats.TotalRuns)
	}
}

func TestHandleStatsSpend(t *testing.T) {
	sc := testServerContext(t)
	saveTestEntries(t, sc)

	req := callToolRequest(map[string]any{"group_by": "repo"})
	result, err := handleStatsSpend(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractResultText(t, result)
	var groups []archive.SpendGroup
	if err := json.Unmarshal([]byte(text), &groups); err != nil {
		t.Fatalf("expected JSON array, got: %s", text)
	}
	if len(groups) != 2 {
		t.Errorf("expected 2 groups, got %d", len(groups))
	}
}

func TestHandleStatsSpendInvalidGroupBy(t *testing.T) {
	sc := testServerContext(t)

	req := callToolRequest(map[string]any{"group_by": "invalid"})
	result, err := handleStatsSpend(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertIsError(t, result)
}

func TestHandleStatsSpendDefaultGroupBy(t *testing.T) {
	sc := testServerContext(t)
	saveTestEntries(t, sc)

	// No group_by specified should default to "week".
	req := callToolRequest(nil)
	result, err := handleStatsSpend(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractResultText(t, result)
	var groups []archive.SpendGroup
	if err := json.Unmarshal([]byte(text), &groups); err != nil {
		t.Fatalf("expected JSON array, got: %s", text)
	}
	// Both entries are from today, so 1 week group.
	if len(groups) != 1 {
		t.Errorf("expected 1 week group, got %d", len(groups))
	}
}

func TestHandleStatsSummaryInvalidOutcome(t *testing.T) {
	sc := testServerContext(t)

	req := callToolRequest(map[string]any{"outcome": "invalid"})
	result, err := handleStatsSummary(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertIsError(t, result)
}

func TestHandleStatsSpendInvalidWeeks(t *testing.T) {
	sc := testServerContext(t)

	req := callToolRequest(map[string]any{"weeks": float64(0)})
	result, err := handleStatsSpend(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertIsError(t, result)
}

func TestHandleStatsTrends(t *testing.T) {
	sc := testServerContext(t)
	saveTestEntries(t, sc)

	req := callToolRequest(nil)
	result, err := handleStatsTrends(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractResultText(t, result)
	var trends []archive.TrendWeek
	if err := json.Unmarshal([]byte(text), &trends); err != nil {
		t.Fatalf("expected JSON array, got: %s", text)
	}
	// Both entries are from the same week.
	if len(trends) != 1 {
		t.Errorf("expected 1 trend week, got %d", len(trends))
	}
	if trends[0].Runs != 2 {
		t.Errorf("runs = %d, want 2", trends[0].Runs)
	}
}

func TestHandleStatsTrendsEmpty(t *testing.T) {
	sc := testServerContext(t)

	req := callToolRequest(nil)
	result, err := handleStatsTrends(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractResultText(t, result)
	var trends []archive.TrendWeek
	if err := json.Unmarshal([]byte(text), &trends); err != nil {
		t.Fatalf("expected JSON array, got: %s", text)
	}
	if len(trends) != 0 {
		t.Errorf("expected 0 trends, got %d", len(trends))
	}
}
