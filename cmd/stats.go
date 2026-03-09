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

var statsSpendCmd = &cobra.Command{
	Use:   "spend",
	Short: "Cost breakdown grouped by a dimension",
	Args:  cobra.NoArgs,
	RunE:  runStatsSpend,
}

// --- trends ---

var statsTrendsWeeks int

var statsTrendsCmd = &cobra.Command{
	Use:   "trends",
	Short: "Week-over-week trends",
	Args:  cobra.NoArgs,
	RunE:  runStatsTrends,
}

func init() {
	statsCmd.PersistentFlags().StringVarP(&statsOutput, "output", "o", "text", "output format: text, json")

	statsSummaryCmd.Flags().StringVar(&statsSummarySince, "since", "", "include entries stopped after this date (YYYY-MM-DD)")
	statsSummaryCmd.Flags().StringVar(&statsSummaryRepo, "repo", "", "filter by repo tag")
	statsSummaryCmd.Flags().StringVar(&statsSummaryOutcome, "outcome", "", "filter by outcome tag (success, partial, failed)")

	statsSpendCmd.Flags().StringVar(&statsSpendBy, "by", "week", "group by: week, repo, complexity")
	statsSpendCmd.Flags().IntVar(&statsSpendWeeks, "weeks", 8, "limit to last N weeks")

	statsTrendsCmd.Flags().IntVar(&statsTrendsWeeks, "weeks", 8, "limit to last N weeks")

	statsCmd.AddCommand(statsSummaryCmd)
	statsCmd.AddCommand(statsSpendCmd)
	statsCmd.AddCommand(statsTrendsCmd)
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
	fmt.Fprintf(out, "Total runs:      %d\n", s.TotalRuns)
	fmt.Fprintf(out, "Success:         %d (%g%%)\n", s.Success, s.SuccessPct)
	fmt.Fprintf(out, "Partial:         %d (%g%%)\n", s.Partial, s.PartialPct)
	fmt.Fprintf(out, "Failure:         %d (%g%%)\n", s.Failure, s.FailurePct)
	fmt.Fprintf(out, "Total cost:      $%.2f\n", s.TotalCostUSD)
	fmt.Fprintf(out, "Avg cost/run:    $%.2f\n", s.AvgCostUSD)
	fmt.Fprintf(out, "Avg messages:    %d\n", s.AvgMessages)
	fmt.Fprintf(out, "Median duration: %s\n", s.MedianDuration)
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

	groups := archive.ComputeSpend(entries, statsSpendBy, statsSpendWeeks)

	out := cmd.OutOrStdout()
	if statsOutput == "json" {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(groups)
	}

	return renderSpendText(out, groups)
}

func renderSpendText(out io.Writer, groups []archive.SpendGroup) error {
	fmt.Fprintf(out, "%-20s  %5s  %10s  %10s\n", "GROUP", "RUNS", "TOTAL", "AVG")
	for _, g := range groups {
		fmt.Fprintf(out, "%-20s  %5d  %10s  %10s\n",
			truncate(g.Group, 20), g.Runs,
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

	trends := archive.ComputeTrends(entries, statsTrendsWeeks)

	out := cmd.OutOrStdout()
	if statsOutput == "json" {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(trends)
	}

	return renderTrendsText(out, trends)
}

func renderTrendsText(out io.Writer, trends []archive.TrendWeek) error {
	fmt.Fprintf(out, "%-12s  %5s  %8s  %10s  %8s\n", "WEEK", "RUNS", "SUCCESS%", "AVG COST", "AVG MSGS")
	for _, tw := range trends {
		fmt.Fprintf(out, "%-12s  %5d  %7g%%  %10s  %8d\n",
			tw.Week, tw.Runs, tw.SuccessPct,
			fmt.Sprintf("$%.2f", tw.AvgCostUSD), tw.AvgMessages)
	}
	return nil
}
