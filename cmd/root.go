// Package cmd implements the klausctl CLI commands using Cobra.
package cmd

import (
	"github.com/spf13/cobra"

	"github.com/giantswarm/klausctl/pkg/ocicache"
)

// applyCacheFlags propagates the global --cache-dir / --no-cache flag state
// into the ocicache package. Registered with cobra.OnInitialize so it fires
// on every Execute() regardless of whether a subcommand defines its own
// PreRun hook (a child's PreRun otherwise shadows the root's).
func applyCacheFlags() {
	ocicache.Configure(cacheDirFlag, noCacheFlag)
}

var (
	buildVersion = "dev"
	buildCommit  = "none"
	buildDate    = "unknown"

	// cfgFile is the optional path to the config file (overrides default).
	cfgFile string

	// cacheDirFlag overrides the on-disk OCI cache directory. Empty means
	// "use the XDG-derived default".
	cacheDirFlag string

	// noCacheFlag bypasses the OCI cache for this invocation.
	noCacheFlag bool
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
	rootCmd.PersistentFlags().StringVar(&cacheDirFlag, "cache-dir", "", "override OCI cache directory (default: $XDG_CACHE_HOME/klausctl/oci)")
	rootCmd.PersistentFlags().BoolVar(&noCacheFlag, "no-cache", false, "bypass the OCI cache for this invocation (also set via KLAUSCTL_NO_CACHE=1)")

	cobra.OnInitialize(applyCacheFlags)
}
