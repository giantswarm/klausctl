// Package cmd implements the klausctl CLI commands using Cobra.
package cmd

import (
	"github.com/spf13/cobra"
)

var (
	buildVersion = "dev"
	buildCommit  = "none"
	buildDate    = "unknown"

	// cfgFile is the optional path to the config file (overrides default).
	cfgFile string
)

// SetBuildInfo sets the build metadata for version display.
func SetBuildInfo(version, commit, date string) {
	buildVersion = version
	buildCommit = commit
	buildDate = date
	rootCmd.Version = version
}

var rootCmd = &cobra.Command{
	Use:   "klausctl",
	Short: "Manage local klaus instances",
	Long: `klausctl manages local klaus containers backed by Docker or Podman.

It produces the same environment variables, flags, and file mounts that the
klaus Go binary expects, but through a developer-friendly CLI. This is the
local-mode counterpart to the Helm chart and the klaus-operator.`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: ~/.config/klausctl/instances/default/config.yaml)")
}
