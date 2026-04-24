package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/giantswarm/klausctl/pkg/ocicache"
)

var (
	cachePruneAll    bool
	cacheInfoFormat  string
	cachePruneFormat string

	cacheRefreshRegistry string
	cacheRefreshRepo     string
	cacheRefreshFormat   string
)

var cacheCmd = &cobra.Command{
	Use:   "cache",
	Short: "Manage the persistent OCI registry response cache",
	Long: `Manage the on-disk cache that klausctl uses to accelerate repeated
registry lookups (catalog, tag lists, reference resolution, blob fetches).

The cache lives under $XDG_CACHE_HOME/klausctl/oci (falling back to
~/.cache/klausctl/oci). It is safe to wipe at any time -- the next
invocation will repopulate what it needs.`,
}

var cacheInfoCmd = &cobra.Command{
	Use:   "info",
	Short: "Show cache location, size, and per-layer entry counts",
	Args:  cobra.NoArgs,
	RunE:  runCacheInfo,
}

var cachePruneCmd = &cobra.Command{
	Use:   "prune",
	Short: "Remove stale cache entries",
	Long: `Remove cache entries that the klaus-oci store would treat as stale.

Use --all to wipe the entire cache (including fresh entries and the
content-addressed blob store).`,
	Args: cobra.NoArgs,
	RunE: runCachePrune,
}

var cacheRefreshCmd = &cobra.Command{
	Use:   "refresh",
	Short: "Force foreground revalidation of cached entries",
	Long: `Invalidate cache index entries so the next client call refetches the
data from the registry.

Without flags, all catalog, tag-list and reference entries are
invalidated (blob content is kept because blobs are content-addressed
and never become stale). --registry scopes the refresh to a specific
registry catalog. --repo scopes it to a single repository.`,
	Args: cobra.NoArgs,
	RunE: runCacheRefresh,
}

func init() {
	cacheInfoCmd.Flags().StringVar(&cacheInfoFormat, "output", "text", "output format: text|json")

	cachePruneCmd.Flags().BoolVar(&cachePruneAll, "all", false, "remove all cache entries, not only stale ones")
	cachePruneCmd.Flags().StringVar(&cachePruneFormat, "output", "text", "output format: text|json")

	cacheRefreshCmd.Flags().StringVar(&cacheRefreshRegistry, "registry", "", "limit refresh to the given registry base (host or host/prefix)")
	cacheRefreshCmd.Flags().StringVar(&cacheRefreshRepo, "repo", "", "limit refresh to the given repository (host/name)")
	cacheRefreshCmd.Flags().StringVar(&cacheRefreshFormat, "output", "text", "output format: text|json")

	cacheCmd.AddCommand(cacheInfoCmd)
	cacheCmd.AddCommand(cachePruneCmd)
	cacheCmd.AddCommand(cacheRefreshCmd)
	rootCmd.AddCommand(cacheCmd)
}

func runCacheInfo(cmd *cobra.Command, _ []string) error {
	if err := validateOutputFormat(cacheInfoFormat); err != nil {
		return err
	}
	info, err := ocicache.Stat()
	if err != nil {
		return err
	}
	return writeCacheInfo(cmd.OutOrStdout(), info, cacheInfoFormat)
}

func writeCacheInfo(w io.Writer, info *ocicache.Info, format string) error {
	if format == "json" {
		return writeJSON(w, info)
	}
	_, _ = fmt.Fprintf(w, "Cache directory: %s\n", displayDir(info.Dir))
	if info.Disabled {
		_, _ = fmt.Fprintln(w, "Status:          disabled (KLAUSCTL_NO_CACHE or --no-cache)")
		return nil
	}
	if !info.Exists {
		_, _ = fmt.Fprintln(w, "Status:          empty (not yet populated)")
		return nil
	}
	_, _ = fmt.Fprintf(w, "Total size:      %s\n", humanBytes(info.TotalBytes))
	if !info.NewestEntry.IsZero() {
		_, _ = fmt.Fprintf(w, "Last write:      %s\n", info.NewestEntry.Format(time.RFC3339))
	}
	_, _ = fmt.Fprintf(w, "Fresh TTL:       %s\n", info.FreshTTL)
	_, _ = fmt.Fprintf(w, "Stale TTL:       %s (catalog %s)\n", info.StaleTTL, info.CatalogStaleTTL)
	_, _ = fmt.Fprintln(w)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "LAYER\tENTRIES\tSIZE")
	for _, l := range info.Layers {
		_, _ = fmt.Fprintf(tw, "%s\t%d\t%s\n", l.Name, l.Entries, humanBytes(l.Bytes))
	}
	return tw.Flush()
}

func runCachePrune(cmd *cobra.Command, _ []string) error {
	if err := validateOutputFormat(cachePruneFormat); err != nil {
		return err
	}
	res, err := ocicache.Prune(ocicache.PruneOptions{All: cachePruneAll})
	if err != nil {
		return err
	}
	return writePruneResult(cmd.OutOrStdout(), res, cachePruneAll, cachePruneFormat)
}

func writePruneResult(w io.Writer, res *ocicache.PruneResult, all bool, format string) error {
	if format == "json" {
		return writeJSON(w, res)
	}
	if res.Dir == "" {
		_, _ = fmt.Fprintln(w, "Cache is disabled; nothing to prune.")
		return nil
	}
	scope := "stale"
	if all {
		scope = "all"
	}
	_, _ = fmt.Fprintf(w, "Pruned %d %s entries (%s) from %s.\n",
		res.FilesRemoved, scope, humanBytes(res.BytesRemoved), displayDir(res.Dir))
	return nil
}

func runCacheRefresh(cmd *cobra.Command, _ []string) error {
	if err := validateOutputFormat(cacheRefreshFormat); err != nil {
		return err
	}
	if cacheRefreshRegistry != "" && cacheRefreshRepo != "" {
		return fmt.Errorf("--registry and --repo are mutually exclusive")
	}
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	res, err := ocicache.Refresh(ctx, ocicache.RefreshOptions{
		Registry: cacheRefreshRegistry,
		Repo:     cacheRefreshRepo,
	})
	if err != nil {
		return err
	}
	return writeRefreshResult(cmd.OutOrStdout(), res, cacheRefreshFormat)
}

func writeRefreshResult(w io.Writer, res *ocicache.RefreshResult, format string) error {
	if format == "json" {
		return writeJSON(w, res)
	}
	if res.Dir == "" {
		_, _ = fmt.Fprintln(w, "Cache is disabled; nothing to refresh.")
		return nil
	}
	_, _ = fmt.Fprintf(w, "Invalidated %d index entries (scope=%s) under %s.\n",
		res.FilesRemoved, res.Scope, displayDir(res.Dir))
	_, _ = fmt.Fprintln(w, "Entries will be refetched on the next klausctl call.")
	return nil
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func displayDir(dir string) string {
	if dir == "" {
		return "(disabled)"
	}
	return dir
}

// humanBytes formats a byte count for cache info output.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	units := []string{"KiB", "MiB", "GiB", "TiB", "PiB"}
	return fmt.Sprintf("%.1f %s", float64(n)/float64(div), units[exp])
}
