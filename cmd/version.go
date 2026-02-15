package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version information",
	Long:  `Display the klausctl version, commit, and build date.`,
	Run:   runVersion,
}

func init() {
	rootCmd.AddCommand(versionCmd)
}

func runVersion(cmd *cobra.Command, _ []string) {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "klausctl %s\n", buildVersion)
	fmt.Fprintf(out, "  commit: %s\n", buildCommit)
	fmt.Fprintf(out, "  built:  %s\n", buildDate)
}
