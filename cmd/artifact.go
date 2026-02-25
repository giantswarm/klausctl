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

// pullFn is a callback that performs a typed pull and returns
// (digest, cached, error).
type pullFn func(ctx context.Context, client *klausoci.Client, ref, destDir string) (digest string, cached bool, err error)

// pullArtifact pulls an OCI artifact by reference to a cache directory.
// The artifact is stored at <cacheDir>/<shortName>/. The shortName is
// extracted from the repository portion of the reference (tag/digest stripped).
func pullArtifact(ctx context.Context, ref string, cacheDir string, pull pullFn, out io.Writer, outputFmt string) error {
	shortName := klausoci.ShortName(klausoci.RepositoryFromRef(ref))
	destDir := filepath.Join(cacheDir, shortName)

	client := orchestrator.NewDefaultClient()
	digest, cached, err := pull(ctx, client, ref, destDir)
	if err != nil {
		return err
	}

	if outputFmt == "json" {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(pullResult{
			Name:   shortName,
			Ref:    ref,
			Digest: digest,
			Cached: cached,
		})
	}

	if cached {
		fmt.Fprintf(out, "%s: up-to-date (%s)\n", shortName, klausoci.TruncateDigest(digest))
	} else {
		fmt.Fprintf(out, "%s: pulled (%s)\n", shortName, klausoci.TruncateDigest(digest))
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

// listFn is a callback that performs a typed list operation and returns
// a slice of ListEntry results.
type listFn func(ctx context.Context, client *klausoci.Client, opts ...klausoci.ListOption) ([]klausoci.ListEntry, error)

// listLatestRemoteArtifacts discovers repositories from the registry,
// resolves the latest semver tag for each, and checks local pull status.
// The caller provides a typed list function (e.g. client.ListPlugins).
func listLatestRemoteArtifacts(ctx context.Context, cacheDir, registryBase string, list listFn) ([]remoteArtifactEntry, error) {
	client := orchestrator.NewDefaultClient()

	artifacts, err := list(ctx, client, klausoci.WithRegistry(registryBase))
	if err != nil {
		return nil, fmt.Errorf("discovering remote artifacts: %w", err)
	}

	localArtifacts, _ := listLocalArtifacts(cacheDir)
	cacheByName := make(map[string]cachedArtifact, len(localArtifacts))
	for _, a := range localArtifacts {
		cacheByName[a.Name] = a
	}

	var entries []remoteArtifactEntry
	for _, a := range artifacts {
		entry := remoteArtifactEntry{
			Name: a.Name,
			Ref:  a.Reference,
		}

		if cached, ok := cacheByName[a.Name]; ok {
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
func listOCIArtifacts(ctx context.Context, out io.Writer, cacheDir, outputFmt, typeName, typePlural string, registries []config.SourceRegistry, local bool, list listFn) error {
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
		fmt.Sprintf("No %s found in the remote registry.", typePlural), list)
}

// listMultiSourceRemoteArtifacts aggregates remote artifacts from multiple source registries.
// When querying multiple sources, failures on individual sources are reported
// as warnings rather than aborting the entire operation.
func listMultiSourceRemoteArtifacts(ctx context.Context, out io.Writer, cacheDir string, registries []config.SourceRegistry, outputFmt, emptyMsg string, list listFn) error {
	multiSource := len(registries) > 1

	allEntries, warnings, err := config.AggregateFromSources(registries, "artifacts", func(sr config.SourceRegistry) ([]remoteArtifactEntry, error) {
		entries, err := listLatestRemoteArtifacts(ctx, cacheDir, sr.Registry, list)
		if err != nil {
			return nil, err
		}
		if multiSource {
			for i := range entries {
				entries[i].Source = sr.Source
			}
		}
		return entries, nil
	})
	if err != nil {
		return err
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
		fmt.Fprintf(out, "Warning: %s\n", w)
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
