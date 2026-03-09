package archive

import (
	"fmt"
	"math"
	"sort"
	"time"
)

// SummaryFilters controls which entries are included in the summary.
type SummaryFilters struct {
	Since   time.Time // zero means no lower bound
	Repo    string    // empty means all repos
	Outcome string    // empty means all outcomes
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
}

// SpendGroup is a single row in a cost breakdown.
type SpendGroup struct {
	Group      string  `json:"group"`
	Runs       int     `json:"runs"`
	TotalCost  float64 `json:"total_cost"`
	AvgCost    float64 `json:"avg_cost"`
}

// TrendWeek is a single row in the week-over-week trends table.
type TrendWeek struct {
	Week        string  `json:"week"`
	Runs        int     `json:"runs"`
	SuccessPct  float64 `json:"success_pct"`
	AvgCostUSD  float64 `json:"avg_cost_usd"`
	AvgMessages int     `json:"avg_messages"`
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

		if e.TotalCostUSD != nil {
			totalCost += *e.TotalCostUSD
		}
		totalMessages += e.MessageCount

		if !e.StartedAt.IsZero() && !e.StoppedAt.IsZero() {
			durations = append(durations, e.StoppedAt.Sub(e.StartedAt))
		}
	}

	n := float64(s.TotalRuns)
	s.TotalCostUSD = totalCost
	s.AvgCostUSD = totalCost / n
	s.AvgMessages = int(math.Round(float64(totalMessages) / n))

	s.SuccessPct = pct(s.Success, s.TotalRuns)
	s.PartialPct = pct(s.Partial, s.TotalRuns)
	s.FailurePct = pct(s.Failure, s.TotalRuns)

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
	}

	return s
}

// ComputeSpend groups entries by the given dimension and returns cost breakdowns.
// groupBy is one of "week", "repo", or "complexity". weeks limits to the last N weeks.
func ComputeSpend(entries []*Entry, groupBy string, weeks int) []SpendGroup {
	cutoff := weekCutoff(weeks)
	filtered := make([]*Entry, 0, len(entries))
	for _, e := range entries {
		if !e.StoppedAt.Before(cutoff) {
			filtered = append(filtered, e)
		}
	}

	type accumulator struct {
		runs int
		cost float64
	}
	groups := make(map[string]*accumulator)

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
		}
	}

	result := make([]SpendGroup, 0, len(groups))
	for k, acc := range groups {
		result = append(result, SpendGroup{
			Group:     k,
			Runs:      acc.runs,
			TotalCost: acc.cost,
			AvgCost:   acc.cost / float64(acc.runs),
		})
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Group < result[j].Group
	})

	return result
}

// ComputeTrends returns week-over-week trend data for the last N weeks.
func ComputeTrends(entries []*Entry, weeks int) []TrendWeek {
	cutoff := weekCutoff(weeks)

	type accumulator struct {
		runs     int
		success  int
		cost     float64
		messages int
	}
	weekMap := make(map[string]*accumulator)

	for _, e := range entries {
		if e.StoppedAt.Before(cutoff) {
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
		if e.TotalCostUSD != nil {
			acc.cost += *e.TotalCostUSD
		}
		acc.messages += e.MessageCount
	}

	result := make([]TrendWeek, 0, len(weekMap))
	for k, acc := range weekMap {
		result = append(result, TrendWeek{
			Week:        k,
			Runs:        acc.runs,
			SuccessPct:  pct(acc.success, acc.runs),
			AvgCostUSD:  acc.cost / float64(acc.runs),
			AvgMessages: int(math.Round(float64(acc.messages) / float64(acc.runs))),
		})
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Week < result[j].Week
	})

	return result
}

// --- helpers ---

func filterEntries(entries []*Entry, f SummaryFilters) []*Entry {
	result := make([]*Entry, 0, len(entries))
	for _, e := range entries {
		if !f.Since.IsZero() && e.StoppedAt.Before(f.Since) {
			continue
		}
		if f.Repo != "" && e.Tags["repo"] != f.Repo {
			continue
		}
		if f.Outcome != "" && e.Tags["outcome"] != f.Outcome {
			continue
		}
		result = append(result, e)
	}
	return result
}

func pct(count, total int) float64 {
	if total == 0 {
		return 0
	}
	return math.Round(float64(count)/float64(total)*1000) / 10
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
