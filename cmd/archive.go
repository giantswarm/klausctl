package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/giantswarm/klausctl/pkg/archive"
	"github.com/giantswarm/klausctl/pkg/config"
)

var archiveOutput string
var archiveShowFull bool

// archive list flags
var (
	archiveListLimit    int
	archiveListOffset   int
	archiveListSince    string
	archiveListName     string
	archiveListTagged   bool
	archiveListUntagged bool
	archiveListOutcome  string
)

var archiveCmd = &cobra.Command{
	Use:   "archive",
	Short: "Browse archived instance transcripts",
}

var archiveListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all archived transcripts",
	Args:  cobra.NoArgs,
	RunE:  runArchiveList,
}

var archiveShowCmd = &cobra.Command{
	Use:   "show <uuid>",
	Short: "Show a single archived transcript",
	Args:  cobra.ExactArgs(1),
	RunE:  runArchiveShow,
}

var archiveTagCmd = &cobra.Command{
	Use:   "tag <uuid> [--key=value ...]",
	Short: "Attach metadata tags to an archived transcript",
	Long:  "Add or update tags on an archive entry. Tags are free-form --key=value pairs.",
	Args:  cobra.ExactArgs(1),
	// Allow unknown flags so that free-form --key=value pairs are not rejected.
	FParseErrWhitelist: cobra.FParseErrWhitelist{UnknownFlags: true},
	RunE:               runArchiveTag,
}

func init() {
	archiveCmd.PersistentFlags().StringVarP(&archiveOutput, "output", "o", "text", "output format: text, json")
	archiveShowCmd.Flags().BoolVar(&archiveShowFull, "full", false, "include the full messages array in the output")

	archiveListCmd.Flags().IntVar(&archiveListLimit, "limit", 20, "max entries to return")
	archiveListCmd.Flags().IntVar(&archiveListOffset, "offset", 0, "skip first N matched entries")
	archiveListCmd.Flags().StringVar(&archiveListSince, "since", "", "only entries stopped after this RFC3339 date")
	archiveListCmd.Flags().StringVar(&archiveListName, "name", "", "substring match on instance name")
	archiveListCmd.Flags().BoolVar(&archiveListTagged, "tagged", false, "only entries with tags")
	archiveListCmd.Flags().BoolVar(&archiveListUntagged, "untagged", false, "only entries without tags")
	archiveListCmd.Flags().StringVar(&archiveListOutcome, "outcome", "", "filter by tags.outcome value")

	archiveCmd.AddCommand(archiveListCmd)
	archiveCmd.AddCommand(archiveShowCmd)
	archiveCmd.AddCommand(archiveTagCmd)
	rootCmd.AddCommand(archiveCmd)
}

func buildArchiveFilter(cmd *cobra.Command) (archive.Filter, error) {
	if archiveListTagged && archiveListUntagged {
		return archive.Filter{}, fmt.Errorf("--tagged and --untagged are mutually exclusive")
	}

	var f archive.Filter
	if archiveListSince != "" {
		t, err := time.Parse(time.RFC3339, archiveListSince)
		if err != nil {
			return archive.Filter{}, fmt.Errorf("invalid --since value: %w", err)
		}
		f.Since = t
	}
	f.Name = archiveListName
	f.Outcome = archiveListOutcome
	if cmd.Flags().Changed("tagged") {
		v := archiveListTagged
		f.Tagged = &v
	}
	if cmd.Flags().Changed("untagged") {
		v := !archiveListUntagged // --untagged => Tagged=false
		f.Tagged = &v
	}
	return f, nil
}

func runArchiveList(cmd *cobra.Command, _ []string) error {
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}

	entries, err := archive.LoadAll(paths.ArchivesDir)
	if err != nil {
		return err
	}

	f, err := buildArchiveFilter(cmd)
	if err != nil {
		return err
	}
	entries = archive.FilterEntries(entries, f)

	// Pagination.
	if archiveListOffset < 0 {
		archiveListOffset = 0
	}
	if archiveListOffset > len(entries) {
		archiveListOffset = len(entries)
	}
	entries = entries[archiveListOffset:]
	if archiveListLimit > 0 && archiveListLimit < len(entries) {
		entries = entries[:archiveListLimit]
	}

	out := cmd.OutOrStdout()

	if archiveOutput == "json" { //nolint:goconst
		summaries := make([]archive.ListSummary, 0, len(entries))
		for _, e := range entries {
			summaries = append(summaries, e.ToListSummary())
		}
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(summaries)
	}

	if len(entries) == 0 {
		_, _ = fmt.Fprintln(out, "No archived transcripts.")
		return nil
	}

	return renderArchiveListText(out, entries)
}

func renderArchiveListText(out io.Writer, entries []*archive.Entry) error {
	_, _ = fmt.Fprintf(out, "%-36s  %-20s  %-12s  %5s  %10s  %s\n", "UUID", "NAME", "STATUS", "MSGS", "COST", "STOPPED")
	for _, e := range entries {
		stopped := e.StoppedAt.Format("2006-01-02 15:04")
		cost := "-"
		if e.TotalCostUSD != nil {
			cost = fmt.Sprintf("$%.4f", *e.TotalCostUSD)
		}
		_, _ = fmt.Fprintf(out, "%-36s  %-20s  %-12s  %5d  %10s  %s\n",
			e.UUID, truncate(e.Name, 20), e.Status, e.MessageCount, cost, stopped)
	}
	return nil
}

func runArchiveShow(cmd *cobra.Command, args []string) error {
	uuid := args[0]

	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}

	entry, err := archive.Load(paths.ArchivesDir, uuid)
	if err != nil {
		return err
	}

	if !archiveShowFull {
		entry.Messages = nil
	}

	out := cmd.OutOrStdout()

	if archiveOutput == "json" {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(entry)
	}

	return renderArchiveShowText(out, entry)
}

func renderArchiveShowText(out io.Writer, e *archive.Entry) error {
	_, _ = fmt.Fprintf(out, "UUID:         %s\n", e.UUID)
	_, _ = fmt.Fprintf(out, "Name:         %s\n", e.Name)
	_, _ = fmt.Fprintf(out, "Status:       %s\n", colorStatus(e.Status))
	_, _ = fmt.Fprintf(out, "Image:        %s\n", e.Image)
	if e.Personality != "" {
		_, _ = fmt.Fprintf(out, "Personality:  %s\n", e.Personality)
	}
	_, _ = fmt.Fprintf(out, "Workspace:    %s\n", e.Workspace)
	_, _ = fmt.Fprintf(out, "Port:         %d\n", e.Port)
	_, _ = fmt.Fprintf(out, "Started:      %s\n", e.StartedAt.Format("2006-01-02 15:04:05"))
	_, _ = fmt.Fprintf(out, "Stopped:      %s\n", e.StoppedAt.Format("2006-01-02 15:04:05"))
	_, _ = fmt.Fprintf(out, "Messages:     %d\n", e.MessageCount)
	if e.TotalCostUSD != nil {
		_, _ = fmt.Fprintf(out, "Cost:         $%.4f\n", *e.TotalCostUSD)
	}
	if e.SessionID != "" {
		_, _ = fmt.Fprintf(out, "Session ID:   %s\n", e.SessionID)
	}
	if len(e.PRURLs) > 0 {
		_, _ = fmt.Fprintf(out, "PR URLs:\n")
		for _, url := range e.PRURLs {
			_, _ = fmt.Fprintf(out, "  - %s\n", url)
		}
	}
	if e.ErrorCount > 0 {
		_, _ = fmt.Fprintf(out, "Errors:       %d\n", e.ErrorCount)
	}
	if e.ErrorMessage != "" {
		_, _ = fmt.Fprintf(out, "Error:        %s\n", e.ErrorMessage)
	}
	renderTags(out, e.Tags)
	if len(e.ToolCalls) > 0 {
		_, _ = fmt.Fprintf(out, "\nTool Calls:\n")
		type toolCount struct {
			Name  string
			Count int
		}
		sorted := make([]toolCount, 0, len(e.ToolCalls))
		for name, count := range e.ToolCalls {
			sorted = append(sorted, toolCount{name, count})
		}
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].Count > sorted[j].Count
		})
		_, _ = fmt.Fprintf(out, "  %-30s  %s\n", "TOOL", "COUNT")
		for _, tc := range sorted {
			_, _ = fmt.Fprintf(out, "  %-30s  %d\n", tc.Name, tc.Count)
		}
	}
	if len(e.ModelUsage) > 0 {
		_, _ = fmt.Fprintf(out, "\nModel Usage:\n")
		type modelCount struct {
			Model string
			Count int
		}
		sortedModels := make([]modelCount, 0, len(e.ModelUsage))
		for model, count := range e.ModelUsage {
			sortedModels = append(sortedModels, modelCount{model, count})
		}
		sort.Slice(sortedModels, func(i, j int) bool {
			return sortedModels[i].Count > sortedModels[j].Count
		})
		_, _ = fmt.Fprintf(out, "  %-30s  %s\n", "MODEL", "COUNT")
		for _, mc := range sortedModels {
			_, _ = fmt.Fprintf(out, "  %-30s  %d\n", mc.Model, mc.Count)
		}
	}
	if len(e.TokenUsage) > 0 {
		var tokens struct {
			Input       int `json:"input"`
			Output      int `json:"output"`
			CacheCreate int `json:"cache_create"`
			CacheRead   int `json:"cache_read"`
		}
		if json.Unmarshal(e.TokenUsage, &tokens) == nil && (tokens.Input > 0 || tokens.Output > 0) {
			_, _ = fmt.Fprintf(out, "\nToken Usage:\n")
			_, _ = fmt.Fprintf(out, "  Input:         %d\n", tokens.Input)
			_, _ = fmt.Fprintf(out, "  Output:        %d\n", tokens.Output)
			_, _ = fmt.Fprintf(out, "  Cache Create:  %d\n", tokens.CacheCreate)
			_, _ = fmt.Fprintf(out, "  Cache Read:    %d\n", tokens.CacheRead)
		}
	}
	if e.ResultText != "" {
		_, _ = fmt.Fprintf(out, "\n%s\n", e.ResultText)
	}
	return nil
}

func runArchiveTag(cmd *cobra.Command, args []string) error {
	uuid := args[0]

	tags := parseTagFlags()
	if len(tags) == 0 {
		return fmt.Errorf("no tags provided; use --key=value to set tags")
	}

	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}

	entry, err := archive.Tag(paths.ArchivesDir, uuid, tags)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()

	if archiveOutput == "json" {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(entry)
	}

	_, _ = fmt.Fprintf(out, "Tagged archive %s\n", entry.UUID)
	renderTags(out, entry.Tags)
	return nil
}

// parseTagFlags extracts free-form --key=value pairs from os.Args. It scans
// arguments after the "tag" subcommand and skips known flags (--output/-o).
func parseTagFlags() map[string]string {
	tags := make(map[string]string)
	inTag := false
	for _, arg := range os.Args {
		if arg == "tag" {
			inTag = true
			continue
		}
		if !inTag {
			continue
		}
		if !strings.HasPrefix(arg, "--") {
			continue
		}
		// Skip known flags.
		if strings.HasPrefix(arg, "--output") || strings.HasPrefix(arg, "-o") {
			continue
		}
		kv := strings.TrimPrefix(arg, "--")
		if k, v, ok := strings.Cut(kv, "="); ok && k != "" {
			tags[k] = v
		}
	}
	return tags
}

func renderTags(out io.Writer, tags map[string]string) {
	if len(tags) == 0 {
		return
	}
	_, _ = fmt.Fprintf(out, "\nTags:\n")
	sorted := make([]string, 0, len(tags))
	for k := range tags {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)
	for _, k := range sorted {
		_, _ = fmt.Fprintf(out, "  %s = %s\n", k, tags[k])
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
