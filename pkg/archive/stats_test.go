package archive

import (
	"testing"
	"time"
)

func f64(v float64) *float64 { return &v }

func makeEntries() []*Entry {
	now := time.Now()
	return []*Entry{
		{
			UUID:         "1",
			Name:         "run-1",
			StartedAt:    now.Add(-30 * time.Minute),
			StoppedAt:    now.Add(-20 * time.Minute),
			MessageCount: 100,
			TotalCostUSD: f64(1.50),
			Tags:         map[string]string{"outcome": "success", "repo": "frontend", "complexity": "simple"},
		},
		{
			UUID:         "2",
			Name:         "run-2",
			StartedAt:    now.Add(-60 * time.Minute),
			StoppedAt:    now.Add(-50 * time.Minute),
			MessageCount: 200,
			TotalCostUSD: f64(3.00),
			Tags:         map[string]string{"outcome": "partial", "repo": "backend", "complexity": "complex"},
		},
		{
			UUID:         "3",
			Name:         "run-3",
			StartedAt:    now.Add(-90 * time.Minute),
			StoppedAt:    now.Add(-80 * time.Minute),
			MessageCount: 150,
			TotalCostUSD: f64(2.00),
			Tags:         map[string]string{"outcome": "failed", "repo": "frontend", "complexity": "simple"},
		},
		{
			UUID:         "4",
			Name:         "run-4",
			StartedAt:    now.Add(-120 * time.Minute),
			StoppedAt:    now.Add(-110 * time.Minute),
			MessageCount: 50,
			TotalCostUSD: nil,
			Tags:         map[string]string{"outcome": "success", "repo": "backend"},
		},
	}
}

func TestComputeSummary_All(t *testing.T) {
	entries := makeEntries()
	s := ComputeSummary(entries, SummaryFilters{})

	if s.TotalRuns != 4 {
		t.Errorf("TotalRuns = %d, want 4", s.TotalRuns)
	}
	if s.Success != 2 {
		t.Errorf("Success = %d, want 2", s.Success)
	}
	if s.Partial != 1 {
		t.Errorf("Partial = %d, want 1", s.Partial)
	}
	if s.Failure != 1 {
		t.Errorf("Failure = %d, want 1", s.Failure)
	}
	// Total cost: 1.50 + 3.00 + 2.00 + 0 = 6.50
	if s.TotalCostUSD != 6.50 {
		t.Errorf("TotalCostUSD = %f, want 6.50", s.TotalCostUSD)
	}
	// Avg cost: 6.50 / 4 = 1.625
	if s.AvgCostUSD != 1.625 {
		t.Errorf("AvgCostUSD = %f, want 1.625", s.AvgCostUSD)
	}
	// Avg messages: (100+200+150+50)/4 = 125
	if s.AvgMessages != 125 {
		t.Errorf("AvgMessages = %d, want 125", s.AvgMessages)
	}
	if s.MedianDuration != "10m" {
		t.Errorf("MedianDuration = %q, want %q", s.MedianDuration, "10m")
	}
}

func TestComputeSummary_FilterByRepo(t *testing.T) {
	entries := makeEntries()
	s := ComputeSummary(entries, SummaryFilters{Repo: "frontend"})

	if s.TotalRuns != 2 {
		t.Errorf("TotalRuns = %d, want 2", s.TotalRuns)
	}
	if s.Success != 1 {
		t.Errorf("Success = %d, want 1", s.Success)
	}
	if s.Failure != 1 {
		t.Errorf("Failure = %d, want 1", s.Failure)
	}
}

func TestComputeSummary_FilterByOutcome(t *testing.T) {
	entries := makeEntries()
	s := ComputeSummary(entries, SummaryFilters{Outcome: "success"})

	if s.TotalRuns != 2 {
		t.Errorf("TotalRuns = %d, want 2", s.TotalRuns)
	}
}

func TestComputeSummary_FilterBySince(t *testing.T) {
	entries := makeEntries()
	// Only include the most recent entry (stopped 20 min ago).
	since := time.Now().Add(-25 * time.Minute)
	s := ComputeSummary(entries, SummaryFilters{Since: since})

	if s.TotalRuns != 1 {
		t.Errorf("TotalRuns = %d, want 1", s.TotalRuns)
	}
}

func TestComputeSummary_Empty(t *testing.T) {
	s := ComputeSummary(nil, SummaryFilters{})
	if s.TotalRuns != 0 {
		t.Errorf("TotalRuns = %d, want 0", s.TotalRuns)
	}
	if s.MedianDuration != "" {
		t.Errorf("MedianDuration = %q, want empty", s.MedianDuration)
	}
}

func TestComputeSpend_ByRepo(t *testing.T) {
	entries := makeEntries()
	groups := ComputeSpend(entries, "repo", 0)

	byGroup := map[string]SpendGroup{}
	for _, g := range groups {
		byGroup[g.Group] = g
	}

	be, ok := byGroup["backend"]
	if !ok {
		t.Fatal("missing backend group")
	}
	if be.Runs != 2 {
		t.Errorf("backend runs = %d, want 2", be.Runs)
	}
	if be.TotalCost != 3.00 {
		t.Errorf("backend total cost = %f, want 3.00", be.TotalCost)
	}

	fe, ok := byGroup["frontend"]
	if !ok {
		t.Fatal("missing frontend group")
	}
	if fe.Runs != 2 {
		t.Errorf("frontend runs = %d, want 2", fe.Runs)
	}
	if fe.TotalCost != 3.50 {
		t.Errorf("frontend total cost = %f, want 3.50", fe.TotalCost)
	}
}

func TestComputeSpend_ByComplexity(t *testing.T) {
	entries := makeEntries()
	groups := ComputeSpend(entries, "complexity", 0)

	byGroup := map[string]SpendGroup{}
	for _, g := range groups {
		byGroup[g.Group] = g
	}

	if _, ok := byGroup["untagged"]; !ok {
		t.Error("expected untagged group for entry without complexity tag")
	}
	if simple, ok := byGroup["simple"]; !ok {
		t.Error("missing simple group")
	} else if simple.Runs != 2 {
		t.Errorf("simple runs = %d, want 2", simple.Runs)
	}
}

func TestComputeSpend_ByWeek(t *testing.T) {
	entries := makeEntries()
	groups := ComputeSpend(entries, "week", 0)

	// All entries are from today, so should be a single week group.
	if len(groups) != 1 {
		t.Errorf("expected 1 week group, got %d", len(groups))
	}
	if groups[0].Runs != 4 {
		t.Errorf("week runs = %d, want 4", groups[0].Runs)
	}
}

func TestComputeTrends(t *testing.T) {
	entries := makeEntries()
	trends := ComputeTrends(entries, 0)

	// All entries in the same week.
	if len(trends) != 1 {
		t.Fatalf("expected 1 trend week, got %d", len(trends))
	}
	tw := trends[0]
	if tw.Runs != 4 {
		t.Errorf("runs = %d, want 4", tw.Runs)
	}
	if tw.SuccessPct != 50 {
		t.Errorf("success pct = %g, want 50", tw.SuccessPct)
	}
}

func TestComputeTrends_WeeksFilter(t *testing.T) {
	now := time.Now()
	entries := []*Entry{
		{
			UUID:         "recent",
			StoppedAt:    now.Add(-24 * time.Hour),
			MessageCount: 10,
			TotalCostUSD: f64(1.0),
			Tags:         map[string]string{"outcome": "success"},
		},
		{
			UUID:         "old",
			StoppedAt:    now.Add(-100 * 24 * time.Hour),
			MessageCount: 10,
			TotalCostUSD: f64(1.0),
			Tags:         map[string]string{"outcome": "success"},
		},
	}

	trends := ComputeTrends(entries, 2)
	totalRuns := 0
	for _, tw := range trends {
		totalRuns += tw.Runs
	}
	if totalRuns != 1 {
		t.Errorf("expected 1 run within 2 weeks, got %d", totalRuns)
	}
}

func TestPct(t *testing.T) {
	if got := pct(1, 3); got != 33.3 {
		t.Errorf("pct(1,3) = %g, want 33.3", got)
	}
	if got := pct(0, 0); got != 0 {
		t.Errorf("pct(0,0) = %g, want 0", got)
	}
	if got := pct(3, 3); got != 100 {
		t.Errorf("pct(3,3) = %g, want 100", got)
	}
}

func TestFormatStatsDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "0m"},
		{12 * time.Minute, "12m"},
		{90 * time.Minute, "1h30m"},
		{2 * time.Hour, "2h"},
	}
	for _, tt := range tests {
		if got := formatStatsDuration(tt.d); got != tt.want {
			t.Errorf("formatStatsDuration(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}
