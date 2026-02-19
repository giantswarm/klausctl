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
	"strings"
	"text/tabwriter"
	"time"

	"github.com/giantswarm/klausctl/pkg/oci"
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
		cache, err := oci.ReadCacheEntry(dir)
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
func pullArtifact(ctx context.Context, ref string, cacheDir string, kind oci.ArtifactKind, out io.Writer, outputFmt string) error {
	shortName := oci.ShortName(repositoryFromRef(ref))
	destDir := filepath.Join(cacheDir, shortName)

	client := oci.NewDefaultClient()
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
		fmt.Fprintf(out, "%s: up-to-date (%s)\n", shortName, oci.TruncateDigest(result.Digest))
	} else {
		fmt.Fprintf(out, "%s: pulled (%s)\n", shortName, oci.TruncateDigest(result.Digest))
	}

	return nil
}

// remoteArtifactEntry describes a remote OCI artifact with its latest
// available tag, resolved digest, and local pull timestamp.
type remoteArtifactEntry struct {
	Name     string    `json:"name"`
	Ref      string    `json:"ref"`
	Digest   string    `json:"digest"`
	PulledAt time.Time `json:"pulledAt,omitempty"`
}

// remoteListOptions customises how listLatestRemoteArtifacts discovers and
// names repositories.
type remoteListOptions struct {
	// Filter, when non-nil, is called for each discovered repository.
	// Only repositories for which it returns true are included.
	Filter func(repo string) bool
	// ShortName extracts a display name from a repository path.
	// Defaults to oci.ShortName when nil.
	ShortName func(repo string) string
}

// listLatestRemoteArtifacts discovers repositories from the registry,
// resolves the latest semver tag and digest for each, and checks local
// pull status. Pass nil opts for default behaviour (no filtering,
// oci.ShortName for display names).
func listLatestRemoteArtifacts(ctx context.Context, cacheDir, registryBase string, opts *remoteListOptions) ([]remoteArtifactEntry, error) {
	client := oci.NewDefaultClient()

	repos, err := client.ListRepositories(ctx, registryBase)
	if err != nil {
		return nil, fmt.Errorf("discovering remote repositories: %w", err)
	}

	if opts != nil && opts.Filter != nil {
		filtered := repos[:0]
		for _, r := range repos {
			if opts.Filter(r) {
				filtered = append(filtered, r)
			}
		}
		repos = filtered
	}

	shortNameFn := oci.ShortName
	if opts != nil && opts.ShortName != nil {
		shortNameFn = opts.ShortName
	}

	// Errors from listLocalArtifacts are intentionally ignored so the
	// command works on a clean machine with no local cache directory.
	localArtifacts, _ := listLocalArtifacts(cacheDir)
	cacheByName := make(map[string]cachedArtifact, len(localArtifacts))
	for _, a := range localArtifacts {
		cacheByName[a.Name] = a
	}

	var entries []remoteArtifactEntry
	for _, repo := range repos {
		tags, err := client.List(ctx, repo)
		if err != nil {
			return nil, fmt.Errorf("listing tags for %s: %w", repo, err)
		}

		latest := oci.LatestSemverTag(tags)
		if latest == "" {
			continue
		}

		ref := repo + ":" + latest
		digest, err := client.Resolve(ctx, ref)
		if err != nil {
			return nil, fmt.Errorf("resolving digest for %s: %w", ref, err)
		}

		name := shortNameFn(repo)
		entry := remoteArtifactEntry{
			Name:   name,
			Ref:    ref,
			Digest: digest,
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
func printRemoteArtifacts(out io.Writer, entries []remoteArtifactEntry, outputFmt string) error {
	if outputFmt == "json" {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(entries)
	}

	w := tabwriter.NewWriter(out, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "NAME\tREF\tDIGEST\tPULLED")
	for _, e := range entries {
		pulled := "-"
		if !e.PulledAt.IsZero() {
			pulled = formatAge(e.PulledAt)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", e.Name, e.Ref, oci.TruncateDigest(e.Digest), pulled)
	}
	return w.Flush()
}

// listOCIArtifacts implements the common list subcommand for OCI-cached artifact
// types (plugins, personalities). By default it queries the remote registry for
// the latest available version of each artifact and indicates local cache status.
// With --local, it shows only locally cached artifacts.
func listOCIArtifacts(ctx context.Context, out io.Writer, cacheDir, outputFmt, typeName, typePlural, registryBase string, local bool) error {
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

	entries, err := listLatestRemoteArtifacts(ctx, cacheDir, registryBase, nil)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return printEmpty(out, outputFmt,
			fmt.Sprintf("No %s found in the remote registry.", typePlural),
		)
	}
	return printRemoteArtifacts(out, entries, outputFmt)
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
			oci.TruncateDigest(a.Digest),
			formatAge(a.PulledAt),
		)
	}
	return w.Flush()
}

// resolveArtifactRef resolves a short artifact name to a full OCI reference.
// Short names like "gs-ae" or "gs-ae:v0.0.7" are expanded using the
// registryBase (e.g., "gsoci.azurecr.io/giantswarm/klaus-plugins").
// If no tag is specified, the latest semver tag is resolved from the registry.
// Full OCI references (containing "/") are returned as-is.
func resolveArtifactRef(ctx context.Context, ref, registryBase string) (string, error) {
	return oci.ResolveArtifactRef(ctx, ref, registryBase, "")
}

// splitNameTag splits "name:tag" into name and tag. If no colon is present,
// tag is empty.
func splitNameTag(ref string) (name, tag string) {
	if idx := strings.LastIndex(ref, ":"); idx >= 0 {
		return ref[:idx], ref[idx+1:]
	}
	return ref, ""
}

// repositoryFromRef extracts the repository part from an OCI reference,
// stripping the tag or digest suffix. Handles both repo:tag and
// repo@sha256:digest formats. Port-only colons (e.g. localhost:5000/repo)
// are preserved.
func repositoryFromRef(ref string) string {
	if idx := strings.Index(ref, "@"); idx > 0 {
		return ref[:idx]
	}
	if idx := strings.LastIndex(ref, ":"); idx > 0 {
		if !strings.Contains(ref[idx+1:], "/") {
			return ref[:idx]
		}
	}
	return ref
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
