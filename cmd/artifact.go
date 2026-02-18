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

// remoteTag describes a tag available in a remote registry.
type remoteTag struct {
	Repository string `json:"repository"`
	Tag        string `json:"tag"`
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

// listRemoteTags queries the registry for available tags. It first collects
// repositories from the local cache, then discovers additional repositories
// from the registry catalog using registryBase (if non-empty). This allows
// the command to work on a clean machine with no local cache.
func listRemoteTags(ctx context.Context, cacheDir, registryBase string) ([]remoteTag, error) {
	artifacts, err := listLocalArtifacts(cacheDir)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	var repos []string
	for _, a := range artifacts {
		repo := repositoryFromRef(a.Ref)
		if repo != "" && !seen[repo] {
			seen[repo] = true
			repos = append(repos, repo)
		}
	}

	client := oci.NewDefaultClient()

	if registryBase != "" {
		discovered, err := client.ListRepositories(ctx, registryBase)
		if err != nil {
			return nil, fmt.Errorf("discovering remote repositories: %w", err)
		}
		for _, repo := range discovered {
			if !seen[repo] {
				seen[repo] = true
				repos = append(repos, repo)
			}
		}
	}

	if len(repos) == 0 {
		return nil, nil
	}

	var tags []remoteTag

	for _, repo := range repos {
		repoTags, err := client.List(ctx, repo)
		if err != nil {
			return nil, fmt.Errorf("listing tags for %s: %w", repo, err)
		}
		for _, tag := range repoTags {
			tags = append(tags, remoteTag{Repository: repo, Tag: tag})
		}
	}

	return tags, nil
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

// listOCIArtifacts implements the common list subcommand for OCI-cached artifact
// types (plugins, personalities). It handles both local and remote listing,
// empty-state messaging, and output formatting. When registryBase is non-empty
// and remote is true, the command discovers repositories directly from the
// registry catalog, allowing it to work without any local cache.
func listOCIArtifacts(ctx context.Context, out io.Writer, cacheDir, outputFmt, typeName, typePlural, registryBase string, remote bool) error {
	if remote {
		tags, err := listRemoteTags(ctx, cacheDir, registryBase)
		if err != nil {
			return err
		}
		if len(tags) == 0 {
			return printEmpty(out, outputFmt,
				fmt.Sprintf("No %s found in the remote registry.", typePlural),
			)
		}
		return printRemoteTags(out, tags, outputFmt)
	}

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

// printRemoteTags prints remote tags in table or JSON format.
func printRemoteTags(out io.Writer, tags []remoteTag, outputFmt string) error {
	if outputFmt == "json" {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(tags)
	}

	w := tabwriter.NewWriter(out, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "REPOSITORY\tTAG")
	for _, t := range tags {
		fmt.Fprintf(w, "%s\t%s\n", t.Repository, t.Tag)
	}
	return w.Flush()
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
