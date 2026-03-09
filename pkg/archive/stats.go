package archive

import (
	"fmt"
	"math"
	"sort"
	"time"
)

// SummaryFilters controls which entries are included in the summary.
type SummaryFilters struct {
	Since      time.Time // zero means no lower bound
	Repo       string    // empty means all repos
	Outcome    string    // empty means all outcomes
	Complexity string    // empty means all complexities
}

// ComplexityGroup holds per-complexity-level stats for the summary breakdown.
type ComplexityGroup struct {
	Level      string  `json:"level"`
	Runs       int     `json:"runs"`
	AvgCost    float64 `json:"avg_cost"`
	SuccessPct float64 `json:"success_pct"`
}

// SummaryStats is the aggregated overview of archived runs.
type SummaryStats struct {
	TotalRuns      int     `json:"total_runs"`
	Success        int     `json:"success"`
	SuccessPct     float64 `json:"success_pct"`
	Partial        int     `json:"partial"`
	PartialPct     float64 `json:"partial_pct"`
	Failure        int     `json:"failure"`
	FailurePct     float64 `json:"failure_pct"`
	TotalCostUSD   float64 `json:"total_cost_usd"`
	AvgCostUSD     float64 `json:"avg_cost_usd"`
	AvgMessages    int     `json:"avg_messages"`
	MedianDuration string  `json:"median_duration"`

	// Enriched fields
	FirstAttempt        int               `json:"first_attempt"`
	FirstAttemptPct     float64           `json:"first_attempt_pct"`
	ScopeAdherence      int               `json:"scope_adherence"`
	ScopeAdherencePct   float64           `json:"scope_adherence_pct"`
	ScopeTotal          int               `json:"scope_total"`
	ReworkNone          int               `json:"rework_none"`
	ReworkMinor         int               `json:"rework_minor"`
	ReworkMajor         int               `json:"rework_major"`
	MinCost             float64           `json:"min_cost"`
	MaxCost             float64           `json:"max_cost"`
	MinMessages         int               `json:"min_messages"`
	MaxMessages         int               `json:"max_messages"`
	MinDuration         string            `json:"min_duration"`
	MaxDuration         string            `json:"max_duration"`
	ComplexityBreakdown []ComplexityGroup `json:"complexity_breakdown,omitempty"`
}

// SpendGroup is a single row in a cost breakdown.
type SpendGroup struct {
	Group      string  `json:"group"`
	Runs       int     `json:"runs"`
	PctOfTotal float64 `json:"pct_of_total"`
	TotalCost  float64 `json:"total_cost"`
	AvgCost    float64 `json:"avg_cost"`
}

// TrendWeek is a single row in the week-over-week trends table.
type TrendWeek struct {
	Week            string  `json:"week"`
	Runs            int     `json:"runs"`
	SuccessPct      float64 `json:"success_pct"`
	FirstAttemptPct float64 `json:"first_attempt_pct"`
	AvgCostUSD      float64 `json:"avg_cost_usd"`
	TotalCostUSD    float64 `json:"total_cost_usd"`
	AvgMessages     int     `json:"avg_messages"`
	AvgDuration     string  `json:"avg_duration"`
	AvgComplexity   float64 `json:"avg_complexity"`
}

// ListEntry holds the display fields for a single archive entry.
type ListEntry struct {
	Date       string        `json:"date"`
	Name       string        `json:"name"`
	Repo       string        `json:"repo"`
	Issue      string        `json:"issue"`
	Outcome    string        `json:"outcome"`
	Cost       float64       `json:"cost"`
	Messages   int           `json:"messages"`
	Duration   string        `json:"duration"`
	Complexity string        `json:"complexity"`
	duration   time.Duration // unexported, used for sorting
}

// ComputeSummary aggregates stats from the given entries after applying filters.
func ComputeSummary(entries []*Entry, filters SummaryFilters) *SummaryStats {
	filtered := filterEntries(entries, filters)

	s := &SummaryStats{}
	s.TotalRuns = len(filtered)
	if s.TotalRuns == 0 {
		return s
	}

	var totalCost float64
	var totalMessages int
	var durations []time.Duration
	hasCost := false
	hasMessages := false
	var minMsgs, maxMsgs int

	// Complexity accumulators
	type complexityAcc struct {
		runs    int
		cost    float64
		success int
	}
	complexityMap := make(map[string]*complexityAcc)

	for _, e := range filtered {
		outcome := e.Tags["outcome"]
		switch outcome {
		case "success":
			s.Success++
		case "partial":
			s.Partial++
		case "failed":
			s.Failure++
		}

		// First attempt
		if e.Tags["first_attempt"] == "true" {
			s.FirstAttempt++
		}

		// Scope adherence (only count entries that have the scope tag)
		if scope := e.Tags["scope"]; scope != "" {
			s.ScopeTotal++
			if scope == "adhered" {
				s.ScopeAdherence++
			}
		}

		// Rework distribution
		switch e.Tags["rework"] {
		case "none":
			s.ReworkNone++
		case "minor":
			s.ReworkMinor++
		case "major":
			s.ReworkMajor++
		}

		if e.TotalCostUSD != nil {
			c := *e.TotalCostUSD
			totalCost += c
			if !hasCost {
				s.MinCost = c
				s.MaxCost = c
				hasCost = true
			} else {
				if c < s.MinCost {
					s.MinCost = c
				}
				if c > s.MaxCost {
					s.MaxCost = c
				}
			}
		}
		totalMessages += e.MessageCount
		if !hasMessages {
			minMsgs = e.MessageCount
			maxMsgs = e.MessageCount
			hasMessages = true
		} else {
			if e.MessageCount < minMsgs {
				minMsgs = e.MessageCount
			}
			if e.MessageCount > maxMsgs {
				maxMsgs = e.MessageCount
			}
		}

		if !e.StartedAt.IsZero() && !e.StoppedAt.IsZero() {
			durations = append(durations, e.StoppedAt.Sub(e.StartedAt))
		}

		// Complexity breakdown
		if cplx := e.Tags["complexity"]; cplx != "" {
			acc, ok := complexityMap[cplx]
			if !ok {
				acc = &complexityAcc{}
				complexityMap[cplx] = acc
			}
			acc.runs++
			if e.TotalCostUSD != nil {
				acc.cost += *e.TotalCostUSD
			}
			if outcome == "success" {
				acc.success++
			}
		}
	}

	n := float64(s.TotalRuns)
	s.TotalCostUSD = totalCost
	s.AvgCostUSD = totalCost / n
	s.AvgMessages = int(math.Round(float64(totalMessages) / n))
	s.MinMessages = minMsgs
	s.MaxMessages = maxMsgs

	s.SuccessPct = pct(s.Success, s.TotalRuns)
	s.PartialPct = pct(s.Partial, s.TotalRuns)
	s.FailurePct = pct(s.Failure, s.TotalRuns)
	s.FirstAttemptPct = pct(s.FirstAttempt, s.TotalRuns)
	if s.ScopeTotal > 0 {
		s.ScopeAdherencePct = pct(s.ScopeAdherence, s.ScopeTotal)
	}

	if len(durations) > 0 {
		sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
		mid := len(durations) / 2
		var median time.Duration
		if len(durations)%2 == 0 {
			median = (durations[mid-1] + durations[mid]) / 2
		} else {
			median = durations[mid]
		}
		s.MedianDuration = formatStatsDuration(median)
		s.MinDuration = formatStatsDuration(durations[0])
		s.MaxDuration = formatStatsDuration(durations[len(durations)-1])
	}

	// Build complexity breakdown sorted by numeric level
	if len(complexityMap) > 0 {
		order := []string{"trivial", "simple", "moderate", "complex", "expert"}
		for _, level := range order {
			acc, ok := complexityMap[level]
			if !ok {
				continue
			}
			cg := ComplexityGroup{
				Level:      level,
				Runs:       acc.runs,
				SuccessPct: pct(acc.success, acc.runs),
			}
			if acc.runs > 0 {
				cg.AvgCost = acc.cost / float64(acc.runs)
			}
			s.ComplexityBreakdown = append(s.ComplexityBreakdown, cg)
		}
	}

	return s
}

// ComputeSpend groups entries by the given dimension and returns cost breakdowns.
// groupBy is one of "week", "repo", or "complexity". weeks limits to the last N weeks.
func ComputeSpend(entries []*Entry, groupBy string, weeks int, filters ...SummaryFilters) []SpendGroup {
	cutoff := weekCutoff(weeks)

	var f SummaryFilters
	if len(filters) > 0 {
		f = filters[0]
	}

	filtered := make([]*Entry, 0, len(entries))
	for _, e := range entries {
		if !e.StoppedAt.Before(cutoff) {
			filtered = append(filtered, e)
		}
	}

	// Apply additional filters
	filtered = filterEntries(filtered, f)

	type accumulator struct {
		runs int
		cost float64
	}
	groups := make(map[string]*accumulator)

	var grandTotal float64
	for _, e := range filtered {
		var key string
		switch groupBy {
		case "repo":
			key = e.Tags["repo"]
			if key == "" {
				key = "untagged"
			}
		case "complexity":
			key = e.Tags["complexity"]
			if key == "" {
				key = "untagged"
			}
		default: // "week"
			key = isoWeek(e.StoppedAt)
		}

		acc, ok := groups[key]
		if !ok {
			acc = &accumulator{}
			groups[key] = acc
		}
		acc.runs++
		if e.TotalCostUSD != nil {
			acc.cost += *e.TotalCostUSD
			grandTotal += *e.TotalCostUSD
		}
	}

	result := make([]SpendGroup, 0, len(groups))
	for k, acc := range groups {
		result = append(result, SpendGroup{
			Group:      k,
			Runs:       acc.runs,
			PctOfTotal: pct64(acc.cost, grandTotal),
			TotalCost:  acc.cost,
			AvgCost:    acc.cost / float64(acc.runs),
		})
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Group < result[j].Group
	})

	return result
}

// ComputeTrends returns week-over-week trend data for the last N weeks.
func ComputeTrends(entries []*Entry, weeks int, filters ...SummaryFilters) []TrendWeek {
	cutoff := weekCutoff(weeks)

	var f SummaryFilters
	if len(filters) > 0 {
		f = filters[0]
	}

	type accumulator struct {
		runs         int
		success      int
		firstAttempt int
		cost         float64
		messages     int
		totalDur     time.Duration
		durCount     int
		complexSum   float64
		complexCount int
	}
	weekMap := make(map[string]*accumulator)

	for _, e := range entries {
		if e.StoppedAt.Before(cutoff) {
			continue
		}
		if !matchesFilters(e, f) {
			continue
		}
		key := isoWeek(e.StoppedAt)
		acc, ok := weekMap[key]
		if !ok {
			acc = &accumulator{}
			weekMap[key] = acc
		}
		acc.runs++
		if e.Tags["outcome"] == "success" {
			acc.success++
		}
		if e.Tags["first_attempt"] == "true" {
			acc.firstAttempt++
		}
		if e.TotalCostUSD != nil {
			acc.cost += *e.TotalCostUSD
		}
		acc.messages += e.MessageCount

		if !e.StartedAt.IsZero() && !e.StoppedAt.IsZero() {
			acc.totalDur += e.StoppedAt.Sub(e.StartedAt)
			acc.durCount++
		}

		if v := complexityToNumeric(e.Tags["complexity"]); v > 0 {
			acc.complexSum += v
			acc.complexCount++
		}
	}

	result := make([]TrendWeek, 0, len(weekMap))
	for k, acc := range weekMap {
		tw := TrendWeek{
			Week:            k,
			Runs:            acc.runs,
			SuccessPct:      pct(acc.success, acc.runs),
			FirstAttemptPct: pct(acc.firstAttempt, acc.runs),
			AvgCostUSD:      acc.cost / float64(acc.runs),
			TotalCostUSD:    acc.cost,
			AvgMessages:     int(math.Round(float64(acc.messages) / float64(acc.runs))),
		}
		if acc.durCount > 0 {
			tw.AvgDuration = formatStatsDuration(acc.totalDur / time.Duration(acc.durCount))
		}
		if acc.complexCount > 0 {
			tw.AvgComplexity = math.Round(acc.complexSum/float64(acc.complexCount)*10) / 10
		}
		result = append(result, tw)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Week < result[j].Week
	})

	return result
}

// ComputeList returns a flat list of entries with display fields, filtered and sorted.
func ComputeList(entries []*Entry, filters SummaryFilters, sortBy string, limit int) []ListEntry {
	filtered := filterEntries(entries, filters)

	result := make([]ListEntry, 0, len(filtered))
	for _, e := range filtered {
		var cost float64
		if e.TotalCostUSD != nil {
			cost = *e.TotalCostUSD
		}
		var dur string
		var rawDur time.Duration
		if !e.StartedAt.IsZero() && !e.StoppedAt.IsZero() {
			rawDur = e.StoppedAt.Sub(e.StartedAt)
			dur = formatStatsDuration(rawDur)
		}
		result = append(result, ListEntry{
			Date:       e.StoppedAt.Format("2006-01-02"),
			Name:       e.Name,
			Repo:       e.Tags["repo"],
			Issue:      e.Tags["issue"],
			Outcome:    e.Tags["outcome"],
			Cost:       cost,
			Messages:   e.MessageCount,
			Duration:   dur,
			Complexity: e.Tags["complexity"],
			duration:   rawDur,
		})
	}

	switch sortBy {
	case "cost":
		sort.Slice(result, func(i, j int) bool { return result[i].Cost > result[j].Cost })
	case "messages":
		sort.Slice(result, func(i, j int) bool { return result[i].Messages > result[j].Messages })
	case "duration":
		sort.Slice(result, func(i, j int) bool { return result[i].duration > result[j].duration })
	default: // "date"
		sort.Slice(result, func(i, j int) bool { return result[i].Date > result[j].Date })
	}

	if limit > 0 && limit < len(result) {
		result = result[:limit]
	}
	return result
}

// --- helpers ---

func filterEntries(entries []*Entry, f SummaryFilters) []*Entry {
	result := make([]*Entry, 0, len(entries))
	for _, e := range entries {
		if matchesFilters(e, f) {
			result = append(result, e)
		}
	}
	return result
}

func matchesFilters(e *Entry, f SummaryFilters) bool {
	if !f.Since.IsZero() && e.StoppedAt.Before(f.Since) {
		return false
	}
	if f.Repo != "" && e.Tags["repo"] != f.Repo {
		return false
	}
	if f.Outcome != "" && e.Tags["outcome"] != f.Outcome {
		return false
	}
	if f.Complexity != "" && e.Tags["complexity"] != f.Complexity {
		return false
	}
	return true
}

func pct(count, total int) float64 {
	if total == 0 {
		return 0
	}
	return math.Round(float64(count)/float64(total)*1000) / 10
}

func pct64(value, total float64) float64 {
	if total == 0 {
		return 0
	}
	return math.Round(value/total*1000) / 10
}

func isoWeek(t time.Time) string {
	year, week := t.ISOWeek()
	return time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC).
		AddDate(0, 0, (week-1)*7-int(time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC).Weekday())+1).
		Format("2006-01-02")
}

func weekCutoff(weeks int) time.Time {
	if weeks <= 0 {
		return time.Time{}
	}
	return time.Now().AddDate(0, 0, -7*weeks)
}

func formatStatsDuration(d time.Duration) string {
	if d < time.Minute {
		return "0m"
	}
	totalMinutes := int(d.Minutes())
	if totalMinutes < 60 {
		return fmt.Sprintf("%dm", totalMinutes)
	}
	hours := totalMinutes / 60
	mins := totalMinutes % 60
	if mins == 0 {
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dh%dm", hours, mins)
}

// complexityToNumeric maps complexity tag values to numeric scores.
// Returns 0 for unknown/empty values.
func complexityToNumeric(level string) float64 {
	switch level {
	case "trivial":
		return 1
	case "simple":
		return 2
	case "moderate":
		return 3
	case "complex":
		return 4
	case "expert":
		return 5
	default:
		return 0
	}
}
