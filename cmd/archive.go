package cmd

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/giantswarm/klausctl/pkg/archive"
	"github.com/giantswarm/klausctl/pkg/config"
)

var archiveOutput string

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

func init() {
	archiveCmd.PersistentFlags().StringVarP(&archiveOutput, "output", "o", "text", "output format: text, json")
	archiveCmd.AddCommand(archiveListCmd)
	archiveCmd.AddCommand(archiveShowCmd)
	rootCmd.AddCommand(archiveCmd)
}

// archiveListSummary is the text/json representation for the list command.
type archiveListSummary struct {
	UUID         string   `json:"uuid"`
	Name         string   `json:"name"`
	Status       string   `json:"status"`
	StoppedAt    string   `json:"stopped_at"`
	MessageCount int      `json:"message_count"`
	TotalCostUSD *float64 `json:"total_cost_usd,omitempty"`
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

	out := cmd.OutOrStdout()

	if archiveOutput == "json" {
		summaries := make([]archiveListSummary, 0, len(entries))
		for _, e := range entries {
			summaries = append(summaries, archiveListSummary{
				UUID:         e.UUID,
				Name:         e.Name,
				Status:       e.Status,
				StoppedAt:    e.StoppedAt.Format("2006-01-02T15:04:05Z07:00"),
				MessageCount: e.MessageCount,
				TotalCostUSD: e.TotalCostUSD,
			})
		}
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(summaries)
	}

	if len(entries) == 0 {
		fmt.Fprintln(out, "No archived transcripts.")
		return nil
	}

	return renderArchiveListText(out, entries)
}

func renderArchiveListText(out io.Writer, entries []*archive.Entry) error {
	fmt.Fprintf(out, "%-36s  %-20s  %-12s  %5s  %s\n", "UUID", "NAME", "STATUS", "MSGS", "STOPPED")
	for _, e := range entries {
		stopped := e.StoppedAt.Format("2006-01-02 15:04")
		fmt.Fprintf(out, "%-36s  %-20s  %-12s  %5d  %s\n",
			e.UUID, truncate(e.Name, 20), e.Status, e.MessageCount, stopped)
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

	out := cmd.OutOrStdout()

	if archiveOutput == "json" {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(entry)
	}

	return renderArchiveShowText(out, entry)
}

func renderArchiveShowText(out io.Writer, e *archive.Entry) error {
	fmt.Fprintf(out, "UUID:         %s\n", e.UUID)
	fmt.Fprintf(out, "Name:         %s\n", e.Name)
	fmt.Fprintf(out, "Status:       %s\n", colorStatus(e.Status))
	fmt.Fprintf(out, "Image:        %s\n", e.Image)
	if e.Personality != "" {
		fmt.Fprintf(out, "Personality:  %s\n", e.Personality)
	}
	fmt.Fprintf(out, "Workspace:    %s\n", e.Workspace)
	fmt.Fprintf(out, "Port:         %d\n", e.Port)
	fmt.Fprintf(out, "Started:      %s\n", e.StartedAt.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(out, "Stopped:      %s\n", e.StoppedAt.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(out, "Messages:     %d\n", e.MessageCount)
	if e.TotalCostUSD != nil {
		fmt.Fprintf(out, "Cost:         $%.4f\n", *e.TotalCostUSD)
	}
	if e.SessionID != "" {
		fmt.Fprintf(out, "Session ID:   %s\n", e.SessionID)
	}
	if len(e.PRURLs) > 0 {
		fmt.Fprintf(out, "PR URLs:\n")
		for _, url := range e.PRURLs {
			fmt.Fprintf(out, "  - %s\n", url)
		}
	}
	if e.ErrorMessage != "" {
		fmt.Fprintf(out, "Error:        %s\n", e.ErrorMessage)
	}
	if e.ResultText != "" {
		fmt.Fprintf(out, "\n%s\n", e.ResultText)
	}
	return nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
