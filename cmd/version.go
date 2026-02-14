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

func runVersion(_ *cobra.Command, _ []string) {
	fmt.Printf("klausctl %s\n", buildVersion)
	fmt.Printf("  commit: %s\n", buildCommit)
	fmt.Printf("  built:  %s\n", buildDate)
}
