package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/giantswarm/klausctl/pkg/archive"
	"github.com/giantswarm/klausctl/pkg/config"
)

var statsOutput string

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Aggregate metrics from archived runs",
}

// --- summary ---

var statsSummarySince string
var statsSummaryRepo string
var statsSummaryOutcome string

var statsSummaryCmd = &cobra.Command{
	Use:   "summary",
	Short: "Aggregate overview of archived runs",
	Args:  cobra.NoArgs,
	RunE:  runStatsSummary,
}

// --- spend ---

var statsSpendBy string
var statsSpendWeeks int
var statsSpendRepo string
var statsSpendOutcome string
var statsSpendComplexity string

var statsSpendCmd = &cobra.Command{
	Use:   "spend",
	Short: "Cost breakdown grouped by a dimension",
	Args:  cobra.NoArgs,
	RunE:  runStatsSpend,
}

// --- trends ---

var statsTrendsWeeks int
var statsTrendsRepo string
var statsTrendsOutcome string
var statsTrendsComplexity string

var statsTrendsCmd = &cobra.Command{
	Use:   "trends",
	Short: "Week-over-week trends",
	Args:  cobra.NoArgs,
	RunE:  runStatsTrends,
}

// --- list ---

var statsListRepo string
var statsListOutcome string
var statsListComplexity string
var statsListSort string
var statsListLimit int

var statsListCmd = &cobra.Command{
	Use:   "list",
	Short: "Tabular view of individual archive entries",
	Args:  cobra.NoArgs,
	RunE:  runStatsList,
}

// --- top ---

var statsTopBy string
var statsTopLimit int

var statsTopCmd = &cobra.Command{
	Use:   "top",
	Short: "Show outlier runs (most expensive, longest, chattiest)",
	Args:  cobra.NoArgs,
	RunE:  runStatsTop,
}

func init() {
	statsCmd.PersistentFlags().StringVarP(&statsOutput, "output", "o", "text", "output format: text, json")

	statsSummaryCmd.Flags().StringVar(&statsSummarySince, "since", "", "include entries stopped after this date (YYYY-MM-DD)")
	statsSummaryCmd.Flags().StringVar(&statsSummaryRepo, "repo", "", "filter by repo tag")
	statsSummaryCmd.Flags().StringVar(&statsSummaryOutcome, "outcome", "", "filter by outcome tag (success, partial, failed)")

	statsSpendCmd.Flags().StringVar(&statsSpendBy, "by", "week", "group by: week, repo, complexity")
	statsSpendCmd.Flags().IntVar(&statsSpendWeeks, "weeks", 8, "limit to last N weeks")
	statsSpendCmd.Flags().StringVar(&statsSpendRepo, "repo", "", "filter by repo tag")
	statsSpendCmd.Flags().StringVar(&statsSpendOutcome, "outcome", "", "filter by outcome tag (success, partial, failed)")
	statsSpendCmd.Flags().StringVar(&statsSpendComplexity, "complexity", "", "filter by complexity tag")

	statsTrendsCmd.Flags().IntVar(&statsTrendsWeeks, "weeks", 8, "limit to last N weeks")
	statsTrendsCmd.Flags().StringVar(&statsTrendsRepo, "repo", "", "filter by repo tag")
	statsTrendsCmd.Flags().StringVar(&statsTrendsOutcome, "outcome", "", "filter by outcome tag (success, partial, failed)")
	statsTrendsCmd.Flags().StringVar(&statsTrendsComplexity, "complexity", "", "filter by complexity tag")

	statsListCmd.Flags().StringVar(&statsListRepo, "repo", "", "filter by repo tag")
	statsListCmd.Flags().StringVar(&statsListOutcome, "outcome", "", "filter by outcome tag (success, partial, failed)")
	statsListCmd.Flags().StringVar(&statsListComplexity, "complexity", "", "filter by complexity tag")
	statsListCmd.Flags().StringVar(&statsListSort, "sort", "date", "sort by: date, cost, messages")
	statsListCmd.Flags().IntVar(&statsListLimit, "limit", 0, "limit number of rows (0 = all)")

	statsTopCmd.Flags().StringVar(&statsTopBy, "by", "cost", "sort by: cost, messages, duration")
	statsTopCmd.Flags().IntVar(&statsTopLimit, "limit", 10, "number of top entries to show")

	statsCmd.AddCommand(statsSummaryCmd)
	statsCmd.AddCommand(statsSpendCmd)
	statsCmd.AddCommand(statsTrendsCmd)
	statsCmd.AddCommand(statsListCmd)
	statsCmd.AddCommand(statsTopCmd)
	rootCmd.AddCommand(statsCmd)
}

func runStatsSummary(cmd *cobra.Command, _ []string) error {
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}

	entries, err := archive.LoadAll(paths.ArchivesDir)
	if err != nil {
		return err
	}

	var filters archive.SummaryFilters
	if statsSummarySince != "" {
		t, err := time.Parse("2006-01-02", statsSummarySince)
		if err != nil {
			return fmt.Errorf("invalid --since date %q: expected YYYY-MM-DD", statsSummarySince)
		}
		filters.Since = t
	}
	filters.Repo = statsSummaryRepo
	if statsSummaryOutcome != "" {
		switch statsSummaryOutcome {
		case "success", "partial", "failed":
		default:
			return fmt.Errorf("invalid --outcome %q: use success, partial, or failed", statsSummaryOutcome)
		}
	}
	filters.Outcome = statsSummaryOutcome

	stats := archive.ComputeSummary(entries, filters)

	out := cmd.OutOrStdout()
	if statsOutput == "json" {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(stats)
	}

	return renderSummaryText(out, stats)
}

func renderSummaryText(out io.Writer, s *archive.SummaryStats) error {
	fmt.Fprintf(out, "Total runs:      %-8d  First attempt:   %d/%d (%g%%)\n",
		s.TotalRuns, s.FirstAttempt, s.TotalRuns, s.FirstAttemptPct)
	fmt.Fprintf(out, "Success:         %d (%g%%)", s.Success, s.SuccessPct)
	if s.ScopeTotal > 0 {
		fmt.Fprintf(out, "  Scope adherence: %d/%d (%g%%)", s.ScopeAdherence, s.ScopeTotal, s.ScopeAdherencePct)
	}
	fmt.Fprintln(out)
	fmt.Fprintf(out, "Partial:         %d (%g%%)", s.Partial, s.PartialPct)
	if s.ReworkNone > 0 || s.ReworkMinor > 0 || s.ReworkMajor > 0 {
		fmt.Fprintf(out, "  Rework: none %d, minor %d, major %d", s.ReworkNone, s.ReworkMinor, s.ReworkMajor)
	}
	fmt.Fprintln(out)
	fmt.Fprintf(out, "Failure:         %d (%g%%)\n", s.Failure, s.FailurePct)

	fmt.Fprintln(out)
	if s.TotalRuns > 0 {
		fmt.Fprintf(out, "Cost:  $%.2f total, $%.2f avg", s.TotalCostUSD, s.AvgCostUSD)
		if s.MinCost != s.MaxCost {
			fmt.Fprintf(out, ", $%.2f-$%.2f range", s.MinCost, s.MaxCost)
		}
		fmt.Fprintln(out)
		fmt.Fprintf(out, "Msgs:  %d avg", s.AvgMessages)
		if s.MinMessages != s.MaxMessages {
			fmt.Fprintf(out, ", %d-%d range", s.MinMessages, s.MaxMessages)
		}
		fmt.Fprintln(out)
		if s.MedianDuration != "" {
			fmt.Fprintf(out, "Time:  %s median", s.MedianDuration)
			if s.MinDuration != s.MaxDuration {
				fmt.Fprintf(out, ", %s-%s range", s.MinDuration, s.MaxDuration)
			}
			fmt.Fprintln(out)
		}
	}

	if len(s.ComplexityBreakdown) > 0 {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Complexity:")
		for _, cg := range s.ComplexityBreakdown {
			runWord := "runs"
			if cg.Runs == 1 {
				runWord = "run "
			}
			fmt.Fprintf(out, "  %-10s %d %s  $%.2f avg  %g%% success\n",
				cg.Level, cg.Runs, runWord, cg.AvgCost, cg.SuccessPct)
		}
	}
	return nil
}

func runStatsSpend(cmd *cobra.Command, _ []string) error {
	switch statsSpendBy {
	case "week", "repo", "complexity":
	default:
		return fmt.Errorf("invalid --by value %q: use week, repo, or complexity", statsSpendBy)
	}

	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}

	entries, err := archive.LoadAll(paths.ArchivesDir)
	if err != nil {
		return err
	}

	filters := archive.SummaryFilters{
		Repo:       statsSpendRepo,
		Outcome:    statsSpendOutcome,
		Complexity: statsSpendComplexity,
	}

	groups := archive.ComputeSpend(entries, statsSpendBy, statsSpendWeeks, filters)

	out := cmd.OutOrStdout()
	if statsOutput == "json" {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(groups)
	}

	return renderSpendText(out, groups)
}

func renderSpendText(out io.Writer, groups []archive.SpendGroup) error {
	fmt.Fprintf(out, "%-20s  %5s  %7s  %10s  %10s\n", "GROUP", "RUNS", "%TOTAL", "TOTAL", "AVG")
	for _, g := range groups {
		fmt.Fprintf(out, "%-20s  %5d  %6g%%  %10s  %10s\n",
			truncate(g.Group, 20), g.Runs, g.PctOfTotal,
			fmt.Sprintf("$%.2f", g.TotalCost),
			fmt.Sprintf("$%.2f", g.AvgCost))
	}
	return nil
}

func runStatsTrends(cmd *cobra.Command, _ []string) error {
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}

	entries, err := archive.LoadAll(paths.ArchivesDir)
	if err != nil {
		return err
	}

	filters := archive.SummaryFilters{
		Repo:       statsTrendsRepo,
		Outcome:    statsTrendsOutcome,
		Complexity: statsTrendsComplexity,
	}

	trends := archive.ComputeTrends(entries, statsTrendsWeeks, filters)

	out := cmd.OutOrStdout()
	if statsOutput == "json" {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(trends)
	}

	return renderTrendsText(out, trends)
}

func renderTrendsText(out io.Writer, trends []archive.TrendWeek) error {
	fmt.Fprintf(out, "%-12s  %5s  %8s  %5s  %10s  %10s  %8s  %7s  %9s\n",
		"WEEK", "RUNS", "SUCCESS%", "1ST%", "AVG COST", "TOTAL COST", "AVG MSGS", "AVG DUR", "AVG CMPLX")
	for _, tw := range trends {
		cmplx := ""
		if tw.AvgComplexity > 0 {
			cmplx = fmt.Sprintf("%.1f", tw.AvgComplexity)
		}
		dur := tw.AvgDuration
		if dur == "" {
			dur = "-"
		}
		fmt.Fprintf(out, "%-12s  %5d  %7g%%  %4g%%  %10s  %10s  %8d  %7s  %9s\n",
			tw.Week, tw.Runs, tw.SuccessPct, tw.FirstAttemptPct,
			fmt.Sprintf("$%.2f", tw.AvgCostUSD),
			fmt.Sprintf("$%.2f", tw.TotalCostUSD),
			tw.AvgMessages, dur, cmplx)
	}
	return nil
}

func runStatsList(cmd *cobra.Command, _ []string) error {
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}

	entries, err := archive.LoadAll(paths.ArchivesDir)
	if err != nil {
		return err
	}

	if statsListLimit < 0 {
		return fmt.Errorf("--limit must be non-negative, got %d", statsListLimit)
	}

	switch statsListSort {
	case "date", "cost", "messages":
	default:
		return fmt.Errorf("invalid --sort value %q: use date, cost, or messages", statsListSort)
	}

	filters := archive.SummaryFilters{
		Repo:       statsListRepo,
		Outcome:    statsListOutcome,
		Complexity: statsListComplexity,
	}

	list := archive.ComputeList(entries, filters, statsListSort, statsListLimit)

	out := cmd.OutOrStdout()
	if statsOutput == "json" {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(list)
	}

	return renderListText(out, list)
}

func renderListText(out io.Writer, list []archive.ListEntry) error {
	fmt.Fprintf(out, "%-12s  %-26s  %-22s  %-14s  %-8s  %7s  %5s  %8s  %-10s\n",
		"DATE", "NAME", "REPO", "ISSUE", "OUTCOME", "COST", "MSGS", "DURATION", "COMPLEXITY")
	for _, le := range list {
		dur := le.Duration
		if dur == "" {
			dur = "-"
		}
		fmt.Fprintf(out, "%-12s  %-26s  %-22s  %-14s  %-8s  %7s  %5d  %8s  %-10s\n",
			le.Date, truncate(le.Name, 26), truncate(le.Repo, 22), truncate(le.Issue, 14),
			le.Outcome, fmt.Sprintf("$%.2f", le.Cost), le.Messages, dur, le.Complexity)
	}
	return nil
}

func runStatsTop(cmd *cobra.Command, _ []string) error {
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}

	entries, err := archive.LoadAll(paths.ArchivesDir)
	if err != nil {
		return err
	}

	sortBy := statsTopBy
	switch sortBy {
	case "cost", "messages", "duration":
	default:
		return fmt.Errorf("invalid --by value %q: use cost, messages, or duration", sortBy)
	}

	list := archive.ComputeList(entries, archive.SummaryFilters{}, sortBy, statsTopLimit)

	out := cmd.OutOrStdout()
	if statsOutput == "json" {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(list)
	}

	return renderListText(out, list)
}
