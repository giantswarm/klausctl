package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"text/tabwriter"
	"time"

	klausoci "github.com/giantswarm/klaus-oci"

	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/orchestrator"
)

// validOutputFormats lists the accepted values for --output flags.
var validOutputFormats = []string{"text", "json"}

// validateOutputFormat returns an error if format is not a recognised output format.
func validateOutputFormat(format string) error {
	for _, f := range validOutputFormats {
		if format == f {
			return nil
		}
	}
	return fmt.Errorf("unsupported output format %q: must be one of %v", format, validOutputFormats)
}

// cachedArtifact describes a locally cached OCI artifact for the list command.
type cachedArtifact struct {
	Name     string    `json:"name"`
	Ref      string    `json:"ref"`
	Digest   string    `json:"digest"`
	PulledAt time.Time `json:"pulledAt"`
}

// listLocalArtifacts scans a cache directory for downloaded OCI artifacts.
// Each subdirectory with valid cache metadata is returned as a cachedArtifact.
func listLocalArtifacts(cacheDir string) ([]cachedArtifact, error) {
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading cache directory: %w", err)
	}

	var artifacts []cachedArtifact
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		dir := filepath.Join(cacheDir, entry.Name())
		cache, err := klausoci.ReadCacheEntry(dir)
		if err != nil {
			continue
		}

		artifacts = append(artifacts, cachedArtifact{
			Name:     entry.Name(),
			Ref:      cache.Ref,
			Digest:   cache.Digest,
			PulledAt: cache.PulledAt,
		})
	}

	sort.Slice(artifacts, func(i, j int) bool {
		return artifacts[i].Name < artifacts[j].Name
	})

	return artifacts, nil
}

// pullResult describes the outcome of pulling an OCI artifact, used for
// --output json on pull commands.
type pullResult struct {
	Name   string `json:"name"`
	Ref    string `json:"ref"`
	Digest string `json:"digest"`
	Cached bool   `json:"cached"`
}

// pullArtifact pulls an OCI artifact by reference to a cache directory.
// The artifact is stored at <cacheDir>/<shortName>/. The shortName is
// extracted from the repository portion of the reference (tag/digest stripped).
func pullArtifact(ctx context.Context, ref string, cacheDir string, kind klausoci.ArtifactKind, out io.Writer, outputFmt string) error {
	shortName := klausoci.ShortName(klausoci.RepositoryFromRef(ref))
	destDir := filepath.Join(cacheDir, shortName)

	client := orchestrator.NewDefaultClient()
	result, err := client.Pull(ctx, ref, destDir, kind)
	if err != nil {
		return err
	}

	if outputFmt == "json" {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(pullResult{
			Name:   shortName,
			Ref:    ref,
			Digest: result.Digest,
			Cached: result.Cached,
		})
	}

	if result.Cached {
		fmt.Fprintf(out, "%s: up-to-date (%s)\n", shortName, klausoci.TruncateDigest(result.Digest))
	} else {
		fmt.Fprintf(out, "%s: pulled (%s)\n", shortName, klausoci.TruncateDigest(result.Digest))
	}

	return nil
}

// remoteArtifactEntry describes a remote OCI artifact with its latest
// available tag and local pull timestamp.
type remoteArtifactEntry struct {
	Source   string    `json:"source,omitempty"`
	Name     string    `json:"name"`
	Ref      string    `json:"ref"`
	PulledAt time.Time `json:"pulledAt,omitempty"`
}

// remoteListOptions customises how listLatestRemoteArtifacts discovers and
// names repositories.
type remoteListOptions struct {
	// Filter, when non-nil, is called for each discovered repository.
	// Only repositories for which it returns true are included.
	Filter func(repo string) bool
	// ShortName extracts a display name from a repository path.
	// Defaults to klausoci.ShortName when nil.
	ShortName func(repo string) string
}

// listLatestRemoteArtifacts discovers repositories from the registry,
// resolves the latest semver tag for each, and checks local pull status.
// Uses the high-level ListArtifacts API for concurrent resolution.
func listLatestRemoteArtifacts(ctx context.Context, cacheDir, registryBase string, opts *remoteListOptions) ([]remoteArtifactEntry, error) {
	client := orchestrator.NewDefaultClient()

	var listOpts []klausoci.ListOption
	if opts != nil && opts.Filter != nil {
		listOpts = append(listOpts, klausoci.WithFilter(opts.Filter))
	}

	artifacts, err := client.ListArtifacts(ctx, registryBase, listOpts...)
	if err != nil {
		return nil, fmt.Errorf("discovering remote artifacts: %w", err)
	}

	shortNameFn := klausoci.ShortName
	if opts != nil && opts.ShortName != nil {
		shortNameFn = opts.ShortName
	}

	localArtifacts, _ := listLocalArtifacts(cacheDir)
	cacheByName := make(map[string]cachedArtifact, len(localArtifacts))
	for _, a := range localArtifacts {
		cacheByName[a.Name] = a
	}

	var entries []remoteArtifactEntry
	for _, a := range artifacts {
		name := shortNameFn(a.Repository)
		entry := remoteArtifactEntry{
			Name: name,
			Ref:  a.Reference,
		}

		if cached, ok := cacheByName[name]; ok {
			entry.PulledAt = cached.PulledAt
		}

		entries = append(entries, entry)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})

	return entries, nil
}

// printRemoteArtifacts prints remote artifacts in table or JSON format.
// When any entry has a Source field set, a SOURCE column is shown.
func printRemoteArtifacts(out io.Writer, entries []remoteArtifactEntry, outputFmt string) error {
	if outputFmt == "json" {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(entries)
	}

	multiSource := false
	for _, e := range entries {
		if e.Source != "" {
			multiSource = true
			break
		}
	}

	w := tabwriter.NewWriter(out, 0, 0, 3, ' ', 0)
	if multiSource {
		fmt.Fprintln(w, "SOURCE\tNAME\tREF\tPULLED")
	} else {
		fmt.Fprintln(w, "NAME\tREF\tPULLED")
	}
	for _, e := range entries {
		pulled := "-"
		if !e.PulledAt.IsZero() {
			pulled = formatAge(e.PulledAt)
		}
		if multiSource {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", e.Source, e.Name, e.Ref, pulled)
		} else {
			fmt.Fprintf(w, "%s\t%s\t%s\n", e.Name, e.Ref, pulled)
		}
	}
	return w.Flush()
}

// listOCIArtifacts implements the common list subcommand for OCI-cached artifact
// types (plugins, personalities). By default it queries the remote registry for
// the latest available version of each artifact and indicates local cache status.
// With --local, it shows only locally cached artifacts.
func listOCIArtifacts(ctx context.Context, out io.Writer, cacheDir, outputFmt, typeName, typePlural string, registries []config.SourceRegistry, local bool) error {
	if local {
		artifacts, err := listLocalArtifacts(cacheDir)
		if err != nil {
			return err
		}
		if len(artifacts) == 0 {
			return printEmpty(out, outputFmt,
				fmt.Sprintf("No %s cached locally.", typePlural),
				fmt.Sprintf("Use 'klausctl %s pull <ref>' to pull a %s.", typeName, typeName),
			)
		}
		return printLocalArtifacts(out, artifacts, outputFmt)
	}

	return listMultiSourceRemoteArtifacts(ctx, out, cacheDir, registries, outputFmt,
		fmt.Sprintf("No %s found in the remote registry.", typePlural))
}

// listMultiSourceRemoteArtifacts aggregates remote artifacts from multiple source registries.
// When querying multiple sources, failures on individual sources are reported
// as warnings rather than aborting the entire operation.
func listMultiSourceRemoteArtifacts(ctx context.Context, out io.Writer, cacheDir string, registries []config.SourceRegistry, outputFmt, emptyMsg string) error {
	multiSource := len(registries) > 1
	var allEntries []remoteArtifactEntry
	var warnings []string

	for _, sr := range registries {
		entries, err := listLatestRemoteArtifacts(ctx, cacheDir, sr.Registry, nil)
		if err != nil {
			if multiSource {
				warnings = append(warnings, fmt.Sprintf("Warning: source %q: %v", sr.Source, err))
				continue
			}
			return err
		}
		if multiSource {
			for i := range entries {
				entries[i].Source = sr.Source
			}
		}
		allEntries = append(allEntries, entries...)
	}

	if len(allEntries) == 0 && len(warnings) == 0 {
		return printEmpty(out, outputFmt, emptyMsg)
	}

	sort.Slice(allEntries, func(i, j int) bool {
		if allEntries[i].Source != allEntries[j].Source {
			return allEntries[i].Source < allEntries[j].Source
		}
		return allEntries[i].Name < allEntries[j].Name
	})

	if len(allEntries) > 0 {
		if err := printRemoteArtifacts(out, allEntries, outputFmt); err != nil {
			return err
		}
	}

	for _, w := range warnings {
		fmt.Fprintln(out, w)
	}

	return nil
}

// printEmpty writes an empty result. For JSON, it emits []; for text, it
// prints the provided hint lines.
func printEmpty(out io.Writer, outputFmt string, hints ...string) error {
	if outputFmt == "json" {
		fmt.Fprintln(out, "[]")
		return nil
	}
	for _, h := range hints {
		fmt.Fprintln(out, h)
	}
	return nil
}

// printLocalArtifacts prints locally cached artifacts in table or JSON format.
func printLocalArtifacts(out io.Writer, artifacts []cachedArtifact, outputFmt string) error {
	if outputFmt == "json" {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(artifacts)
	}

	w := tabwriter.NewWriter(out, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "NAME\tREF\tDIGEST\tPULLED")
	for _, a := range artifacts {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			a.Name,
			a.Ref,
			klausoci.TruncateDigest(a.Digest),
			formatAge(a.PulledAt),
		)
	}
	return w.Flush()
}

// formatAge returns a human-readable age string from a timestamp.
func formatAge(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		days := int(d.Hours()) / 24
		return fmt.Sprintf("%dd ago", days)
	}
}
