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
	cost3 := 5.00

	entries := []*archive.Entry{
		{
			UUID:         "s1",
			Name:         "run-1",
			StartedAt:    now.Add(-30 * time.Minute),
			StoppedAt:    now.Add(-20 * time.Minute),
			MessageCount: 100,
			TotalCostUSD: &cost1,
			Tags: map[string]string{
				"outcome":       "success",
				"repo":          "frontend",
				"complexity":    "simple",
				"first_attempt": "yes",
				"scope":         "yes",
				"rework":        "none",
			},
		},
		{
			UUID:         "s2",
			Name:         "run-2",
			StartedAt:    now.Add(-60 * time.Minute),
			StoppedAt:    now.Add(-50 * time.Minute),
			MessageCount: 200,
			TotalCostUSD: &cost2,
			Tags: map[string]string{
				"outcome":       "failed",
				"repo":          "backend",
				"complexity":    "complex",
				"first_attempt": "false",
				"rework":        "minor",
				"issue":         "backend#42",
			},
		},
		{
			UUID:         "s3",
			Name:         "run-3",
			StartedAt:    now.Add(-90 * time.Minute),
			StoppedAt:    now.Add(-80 * time.Minute),
			MessageCount: 300,
			TotalCostUSD: &cost3,
			Tags: map[string]string{
				"outcome":       "success",
				"repo":          "frontend",
				"complexity":    "moderate",
				"first_attempt": "yes",
				"scope":         "yes",
				"rework":        "none",
			},
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
	if stats.TotalRuns != 3 {
		t.Errorf("TotalRuns = %d, want 3", stats.TotalRuns)
	}
	if stats.Success != 2 {
		t.Errorf("Success = %d, want 2", stats.Success)
	}
	if stats.Failure != 1 {
		t.Errorf("Failure = %d, want 1", stats.Failure)
	}
	// Enriched fields
	if stats.FirstAttempt != 2 {
		t.Errorf("FirstAttempt = %d, want 2", stats.FirstAttempt)
	}
	if stats.ReworkNone != 2 {
		t.Errorf("ReworkNone = %d, want 2", stats.ReworkNone)
	}
	if stats.ReworkMinor != 1 {
		t.Errorf("ReworkMinor = %d, want 1", stats.ReworkMinor)
	}
	if len(stats.ComplexityBreakdown) != 3 {
		t.Errorf("ComplexityBreakdown length = %d, want 3", len(stats.ComplexityBreakdown))
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
	if stats.TotalRuns != 2 {
		t.Errorf("TotalRuns = %d, want 2", stats.TotalRuns)
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
	// Verify PctOfTotal is present
	for _, g := range groups {
		if g.PctOfTotal <= 0 {
			t.Errorf("group %q PctOfTotal = %g, want > 0", g.Group, g.PctOfTotal)
		}
	}
}

func TestHandleStatsSpendByComplexity(t *testing.T) {
	sc := testServerContext(t)
	saveTestEntries(t, sc)

	req := callToolRequest(map[string]any{"group_by": "complexity"})
	result, err := handleStatsSpend(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractResultText(t, result)
	var groups []archive.SpendGroup
	if err := json.Unmarshal([]byte(text), &groups); err != nil {
		t.Fatalf("expected JSON array, got: %s", text)
	}
	// 3 entries with simple, complex, moderate
	if len(groups) != 3 {
		t.Errorf("expected 3 groups, got %d", len(groups))
	}
}

func TestHandleStatsSpendWithFilters(t *testing.T) {
	sc := testServerContext(t)
	saveTestEntries(t, sc)

	req := callToolRequest(map[string]any{"group_by": "repo", "outcome": "success"})
	result, err := handleStatsSpend(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractResultText(t, result)
	var groups []archive.SpendGroup
	if err := json.Unmarshal([]byte(text), &groups); err != nil {
		t.Fatalf("expected JSON array, got: %s", text)
	}
	// Only success entries: both are frontend
	if len(groups) != 1 {
		t.Errorf("expected 1 group, got %d", len(groups))
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
	// All entries are from today, so 1 week group.
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
	// All entries are from the same week.
	if len(trends) != 1 {
		t.Errorf("expected 1 trend week, got %d", len(trends))
	}
	if trends[0].Runs != 3 {
		t.Errorf("runs = %d, want 3", trends[0].Runs)
	}
	// Enriched fields
	if trends[0].TotalCostUSD == 0 {
		t.Error("TotalCostUSD should be > 0")
	}
	if trends[0].AvgDuration == "" {
		t.Error("AvgDuration should not be empty")
	}
	if trends[0].AvgComplexity == 0 {
		t.Error("AvgComplexity should be > 0")
	}
}

func TestHandleStatsTrendsWithFilters(t *testing.T) {
	sc := testServerContext(t)
	saveTestEntries(t, sc)

	req := callToolRequest(map[string]any{"repo": "frontend"})
	result, err := handleStatsTrends(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractResultText(t, result)
	var trends []archive.TrendWeek
	if err := json.Unmarshal([]byte(text), &trends); err != nil {
		t.Fatalf("expected JSON array, got: %s", text)
	}
	if len(trends) != 1 {
		t.Fatalf("expected 1 trend week, got %d", len(trends))
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

func TestHandleStatsList(t *testing.T) {
	sc := testServerContext(t)
	saveTestEntries(t, sc)

	req := callToolRequest(nil)
	result, err := handleStatsList(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractResultText(t, result)
	var list []archive.ListEntry
	if err := json.Unmarshal([]byte(text), &list); err != nil {
		t.Fatalf("expected JSON array, got: %s", text)
	}
	if len(list) != 3 {
		t.Errorf("expected 3 entries, got %d", len(list))
	}
}

func TestHandleStatsListWithFilters(t *testing.T) {
	sc := testServerContext(t)
	saveTestEntries(t, sc)

	req := callToolRequest(map[string]any{"repo": "frontend", "sort_by": "cost"})
	result, err := handleStatsList(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractResultText(t, result)
	var list []archive.ListEntry
	if err := json.Unmarshal([]byte(text), &list); err != nil {
		t.Fatalf("expected JSON array, got: %s", text)
	}
	if len(list) != 2 {
		t.Errorf("expected 2 entries, got %d", len(list))
	}
	// Sorted by cost descending
	if len(list) >= 2 && list[0].Cost < list[1].Cost {
		t.Error("expected list sorted by cost descending")
	}
}

func TestHandleStatsListWithLimit(t *testing.T) {
	sc := testServerContext(t)
	saveTestEntries(t, sc)

	req := callToolRequest(map[string]any{"limit": float64(1)})
	result, err := handleStatsList(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractResultText(t, result)
	var list []archive.ListEntry
	if err := json.Unmarshal([]byte(text), &list); err != nil {
		t.Fatalf("expected JSON array, got: %s", text)
	}
	if len(list) != 1 {
		t.Errorf("expected 1 entry, got %d", len(list))
	}
}

func TestHandleStatsListInvalidSortBy(t *testing.T) {
	sc := testServerContext(t)

	req := callToolRequest(map[string]any{"sort_by": "invalid"})
	result, err := handleStatsList(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertIsError(t, result)
}

func TestHandleStatsTop(t *testing.T) {
	sc := testServerContext(t)
	saveTestEntries(t, sc)

	req := callToolRequest(nil)
	result, err := handleStatsTop(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractResultText(t, result)
	var list []archive.ListEntry
	if err := json.Unmarshal([]byte(text), &list); err != nil {
		t.Fatalf("expected JSON array, got: %s", text)
	}
	if len(list) != 3 {
		t.Errorf("expected 3 entries, got %d", len(list))
	}
	// Default sort by cost descending
	if len(list) >= 2 && list[0].Cost < list[1].Cost {
		t.Error("expected list sorted by cost descending")
	}
}

func TestHandleStatsTopByMessages(t *testing.T) {
	sc := testServerContext(t)
	saveTestEntries(t, sc)

	req := callToolRequest(map[string]any{"sort_by": "messages", "limit": float64(2)})
	result, err := handleStatsTop(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractResultText(t, result)
	var list []archive.ListEntry
	if err := json.Unmarshal([]byte(text), &list); err != nil {
		t.Fatalf("expected JSON array, got: %s", text)
	}
	if len(list) != 2 {
		t.Errorf("expected 2 entries, got %d", len(list))
	}
	// Sorted by messages descending: run-3 (300) first
	if list[0].Messages != 300 {
		t.Errorf("first entry messages = %d, want 300", list[0].Messages)
	}
}

func TestHandleStatsTopInvalidSortBy(t *testing.T) {
	sc := testServerContext(t)

	req := callToolRequest(map[string]any{"sort_by": "invalid"})
	result, err := handleStatsTop(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertIsError(t, result)
}
