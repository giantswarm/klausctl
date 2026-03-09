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
			Tags: map[string]string{
				"outcome":       "success",
				"repo":          "frontend",
				"complexity":    "simple",
				"first_attempt": "true",
				"scope":         "adhered",
				"rework":        "none",
			},
		},
		{
			UUID:         "2",
			Name:         "run-2",
			StartedAt:    now.Add(-60 * time.Minute),
			StoppedAt:    now.Add(-50 * time.Minute),
			MessageCount: 200,
			TotalCostUSD: f64(3.00),
			Tags: map[string]string{
				"outcome":       "partial",
				"repo":          "backend",
				"complexity":    "complex",
				"first_attempt": "false",
				"scope":         "adhered",
				"rework":        "minor",
				"issue":         "backend#42",
			},
		},
		{
			UUID:         "3",
			Name:         "run-3",
			StartedAt:    now.Add(-90 * time.Minute),
			StoppedAt:    now.Add(-80 * time.Minute),
			MessageCount: 150,
			TotalCostUSD: f64(2.00),
			Tags: map[string]string{
				"outcome":       "failed",
				"repo":          "frontend",
				"complexity":    "simple",
				"first_attempt": "true",
				"rework":        "major",
			},
		},
		{
			UUID:         "4",
			Name:         "run-4",
			StartedAt:    now.Add(-120 * time.Minute),
			StoppedAt:    now.Add(-110 * time.Minute),
			MessageCount: 50,
			TotalCostUSD: nil,
			Tags: map[string]string{
				"outcome":       "success",
				"repo":          "backend",
				"first_attempt": "true",
				"scope":         "deviated",
				"rework":        "none",
			},
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

func TestComputeSummary_EnrichedFields(t *testing.T) {
	entries := makeEntries()
	s := ComputeSummary(entries, SummaryFilters{})

	// First attempt: entries 1, 3, 4 have first_attempt=true
	if s.FirstAttempt != 3 {
		t.Errorf("FirstAttempt = %d, want 3", s.FirstAttempt)
	}
	if s.FirstAttemptPct != 75 {
		t.Errorf("FirstAttemptPct = %g, want 75", s.FirstAttemptPct)
	}

	// Scope: entries 1 (adhered), 2 (adhered), 4 (deviated) have scope tag
	if s.ScopeTotal != 3 {
		t.Errorf("ScopeTotal = %d, want 3", s.ScopeTotal)
	}
	if s.ScopeAdherence != 2 {
		t.Errorf("ScopeAdherence = %d, want 2", s.ScopeAdherence)
	}
	if s.ScopeAdherencePct != 66.7 {
		t.Errorf("ScopeAdherencePct = %g, want 66.7", s.ScopeAdherencePct)
	}

	// Rework: none=2, minor=1, major=1
	if s.ReworkNone != 2 {
		t.Errorf("ReworkNone = %d, want 2", s.ReworkNone)
	}
	if s.ReworkMinor != 1 {
		t.Errorf("ReworkMinor = %d, want 1", s.ReworkMinor)
	}
	if s.ReworkMajor != 1 {
		t.Errorf("ReworkMajor = %d, want 1", s.ReworkMajor)
	}

	// Cost range: 1.50, 3.00, 2.00 (entry 4 is nil)
	if s.MinCost != 1.50 {
		t.Errorf("MinCost = %f, want 1.50", s.MinCost)
	}
	if s.MaxCost != 3.00 {
		t.Errorf("MaxCost = %f, want 3.00", s.MaxCost)
	}

	// Message range
	if s.MinMessages != 50 {
		t.Errorf("MinMessages = %d, want 50", s.MinMessages)
	}
	if s.MaxMessages != 200 {
		t.Errorf("MaxMessages = %d, want 200", s.MaxMessages)
	}

	// Duration range: all entries are 10 min
	if s.MinDuration != "10m" {
		t.Errorf("MinDuration = %q, want %q", s.MinDuration, "10m")
	}
	if s.MaxDuration != "10m" {
		t.Errorf("MaxDuration = %q, want %q", s.MaxDuration, "10m")
	}
}

func TestComputeSummary_ComplexityBreakdown(t *testing.T) {
	entries := makeEntries()
	s := ComputeSummary(entries, SummaryFilters{})

	// Entries 1,3 are simple; entry 2 is complex; entry 4 has no complexity tag
	if len(s.ComplexityBreakdown) != 2 {
		t.Fatalf("ComplexityBreakdown length = %d, want 2", len(s.ComplexityBreakdown))
	}

	// Order should be: simple, complex (ordered by level)
	if s.ComplexityBreakdown[0].Level != "simple" {
		t.Errorf("first breakdown level = %q, want %q", s.ComplexityBreakdown[0].Level, "simple")
	}
	if s.ComplexityBreakdown[0].Runs != 2 {
		t.Errorf("simple runs = %d, want 2", s.ComplexityBreakdown[0].Runs)
	}
	// simple avg cost: (1.50+2.00)/2 = 1.75
	if s.ComplexityBreakdown[0].AvgCost != 1.75 {
		t.Errorf("simple avg cost = %f, want 1.75", s.ComplexityBreakdown[0].AvgCost)
	}
	// simple success: 1/2 = 50%
	if s.ComplexityBreakdown[0].SuccessPct != 50 {
		t.Errorf("simple success pct = %g, want 50", s.ComplexityBreakdown[0].SuccessPct)
	}

	if s.ComplexityBreakdown[1].Level != "complex" {
		t.Errorf("second breakdown level = %q, want %q", s.ComplexityBreakdown[1].Level, "complex")
	}
	if s.ComplexityBreakdown[1].Runs != 1 {
		t.Errorf("complex runs = %d, want 1", s.ComplexityBreakdown[1].Runs)
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

func TestComputeSummary_FilterByComplexity(t *testing.T) {
	entries := makeEntries()
	s := ComputeSummary(entries, SummaryFilters{Complexity: "simple"})

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

func TestComputeSpend_PctOfTotal(t *testing.T) {
	entries := makeEntries()
	groups := ComputeSpend(entries, "repo", 0)

	var totalPct float64
	for _, g := range groups {
		if g.PctOfTotal <= 0 {
			t.Errorf("group %q PctOfTotal = %g, want > 0", g.Group, g.PctOfTotal)
		}
		totalPct += g.PctOfTotal
	}
	// Total should be ~100% (allow floating point)
	if totalPct < 99 || totalPct > 101 {
		t.Errorf("sum of PctOfTotal = %g, want ~100", totalPct)
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

func TestComputeSpend_WithFilters(t *testing.T) {
	entries := makeEntries()
	groups := ComputeSpend(entries, "repo", 0, SummaryFilters{Outcome: "success"})

	// Only success entries: run-1 (frontend) and run-4 (backend)
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
	for _, g := range groups {
		if g.Runs != 1 {
			t.Errorf("group %q runs = %d, want 1", g.Group, g.Runs)
		}
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

func TestComputeTrends_EnrichedFields(t *testing.T) {
	entries := makeEntries()
	trends := ComputeTrends(entries, 0)

	if len(trends) != 1 {
		t.Fatalf("expected 1 trend week, got %d", len(trends))
	}
	tw := trends[0]

	// First attempt: 3/4 = 75%
	if tw.FirstAttemptPct != 75 {
		t.Errorf("FirstAttemptPct = %g, want 75", tw.FirstAttemptPct)
	}

	// Total cost: 6.50
	if tw.TotalCostUSD != 6.50 {
		t.Errorf("TotalCostUSD = %f, want 6.50", tw.TotalCostUSD)
	}

	// Avg duration: all 10m
	if tw.AvgDuration != "10m" {
		t.Errorf("AvgDuration = %q, want %q", tw.AvgDuration, "10m")
	}

	// Avg complexity: entries 1,3 are simple(2), entry 2 is complex(4), entry 4 has none
	// (2+2+4)/3 = 2.666... -> 2.7
	if tw.AvgComplexity != 2.7 {
		t.Errorf("AvgComplexity = %g, want 2.7", tw.AvgComplexity)
	}
}

func TestComputeTrends_WithFilters(t *testing.T) {
	entries := makeEntries()
	trends := ComputeTrends(entries, 0, SummaryFilters{Repo: "frontend"})

	if len(trends) != 1 {
		t.Fatalf("expected 1 trend week, got %d", len(trends))
	}
	if trends[0].Runs != 2 {
		t.Errorf("runs = %d, want 2", trends[0].Runs)
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

func TestComputeList_Basic(t *testing.T) {
	entries := makeEntries()
	list := ComputeList(entries, SummaryFilters{}, "date", 0)

	if len(list) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(list))
	}
	// Default sort by date descending: most recent first (run-1 stopped 20m ago)
	if list[0].Name != "run-1" {
		t.Errorf("first entry = %q, want run-1", list[0].Name)
	}
}

func TestComputeList_SortByCost(t *testing.T) {
	entries := makeEntries()
	list := ComputeList(entries, SummaryFilters{}, "cost", 0)

	if len(list) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(list))
	}
	// Most expensive first: run-2 ($3.00)
	if list[0].Name != "run-2" {
		t.Errorf("first entry = %q, want run-2", list[0].Name)
	}
}

func TestComputeList_SortByMessages(t *testing.T) {
	entries := makeEntries()
	list := ComputeList(entries, SummaryFilters{}, "messages", 0)

	// Most messages first: run-2 (200)
	if list[0].Name != "run-2" {
		t.Errorf("first entry = %q, want run-2", list[0].Name)
	}
}

func TestComputeList_SortByDuration(t *testing.T) {
	now := time.Now()
	entries := []*Entry{
		{
			UUID: "short", Name: "short-run",
			StartedAt: now.Add(-10 * time.Minute), StoppedAt: now.Add(-5 * time.Minute),
			Tags: map[string]string{},
		},
		{
			UUID: "long", Name: "long-run",
			StartedAt: now.Add(-60 * time.Minute), StoppedAt: now.Add(-30 * time.Minute),
			Tags: map[string]string{},
		},
	}
	list := ComputeList(entries, SummaryFilters{}, "duration", 0)

	if list[0].Name != "long-run" {
		t.Errorf("first entry = %q, want long-run", list[0].Name)
	}
}

func TestComputeList_WithFilters(t *testing.T) {
	entries := makeEntries()
	list := ComputeList(entries, SummaryFilters{Repo: "frontend"}, "date", 0)

	if len(list) != 2 {
		t.Errorf("expected 2 entries, got %d", len(list))
	}
}

func TestComputeList_WithLimit(t *testing.T) {
	entries := makeEntries()
	list := ComputeList(entries, SummaryFilters{}, "date", 2)

	if len(list) != 2 {
		t.Errorf("expected 2 entries, got %d", len(list))
	}
}

func TestComputeList_WithComplexityFilter(t *testing.T) {
	entries := makeEntries()
	list := ComputeList(entries, SummaryFilters{Complexity: "complex"}, "date", 0)

	if len(list) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(list))
	}
	if list[0].Name != "run-2" {
		t.Errorf("entry = %q, want run-2", list[0].Name)
	}
}

func TestComputeList_Fields(t *testing.T) {
	entries := makeEntries()
	list := ComputeList(entries, SummaryFilters{Outcome: "partial"}, "date", 0)

	if len(list) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(list))
	}
	le := list[0]
	if le.Name != "run-2" {
		t.Errorf("Name = %q, want run-2", le.Name)
	}
	if le.Repo != "backend" {
		t.Errorf("Repo = %q, want backend", le.Repo)
	}
	if le.Issue != "backend#42" {
		t.Errorf("Issue = %q, want backend#42", le.Issue)
	}
	if le.Outcome != "partial" {
		t.Errorf("Outcome = %q, want partial", le.Outcome)
	}
	if le.Cost != 3.00 {
		t.Errorf("Cost = %f, want 3.00", le.Cost)
	}
	if le.Messages != 200 {
		t.Errorf("Messages = %d, want 200", le.Messages)
	}
	if le.Duration != "10m" {
		t.Errorf("Duration = %q, want 10m", le.Duration)
	}
	if le.Complexity != "complex" {
		t.Errorf("Complexity = %q, want complex", le.Complexity)
	}
}

func TestComplexityToNumeric(t *testing.T) {
	tests := []struct {
		level string
		want  float64
	}{
		{"trivial", 1},
		{"simple", 2},
		{"moderate", 3},
		{"complex", 4},
		{"expert", 5},
		{"", 0},
		{"unknown", 0},
	}
	for _, tt := range tests {
		if got := complexityToNumeric(tt.level); got != tt.want {
			t.Errorf("complexityToNumeric(%q) = %g, want %g", tt.level, got, tt.want)
		}
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

func TestPct64(t *testing.T) {
	if got := pct64(3.0, 6.0); got != 50 {
		t.Errorf("pct64(3,6) = %g, want 50", got)
	}
	if got := pct64(0, 0); got != 0 {
		t.Errorf("pct64(0,0) = %g, want 0", got)
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
