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

// validatePushRef checks that a reference contains an explicit tag (e.g. ":v1.0.0").
// Push operations require an explicit tag; bare names or digest-only refs are rejected.
// Uses SplitNameTag so that host:port refs (e.g. "localhost:5000/repo") are not
// mistaken for tagged references.
func validatePushRef(ref string) error {
	_, tag := klausoci.SplitNameTag(ref)
	if tag != "" {
		return nil
	}
	return fmt.Errorf("reference %q must include a tag (e.g. %s:v1.0.0)", ref, ref)
}

// pushResult describes the outcome of pushing an OCI artifact, used for
// --output json on push commands.
type pushResult struct {
	Name      string `json:"name"`
	Ref       string `json:"ref"`
	Digest    string `json:"digest"`
	DryRun    bool   `json:"dryRun,omitempty"`
	Overwrote bool   `json:"overwrote,omitempty"`
}

// pushFn is a callback that performs a typed push and returns
// (digest, error). The callback is responsible for reading metadata from
// sourceDir and calling the appropriate typed push method.
type pushFn func(ctx context.Context, client *klausoci.Client, sourceDir, ref string) (digest string, err error)

// pushOpts controls optional behaviour for pushArtifact.
type pushOpts struct {
	dryRun bool
}

// pushArtifact pushes a local directory as an OCI artifact to a registry.
// The caller provides a typed push function that reads metadata and calls the
// appropriate client method. Output is formatted as text or JSON.
//
// When opts.dryRun is true the push is skipped but validation and
// overwrite detection still run.
func pushArtifact(ctx context.Context, sourceDir, ref string, push pushFn, out io.Writer, outputFmt string, opts pushOpts) error {
	shortName := klausoci.ShortName(klausoci.RepositoryFromRef(ref))
	client := orchestrator.NewDefaultClient()

	overwrote := false
	if existing, err := client.Resolve(ctx, ref); err == nil && existing != "" {
		overwrote = true
		fmt.Fprintf(os.Stderr, "Warning: tag already exists (%s); pushing will overwrite it\n", klausoci.TruncateDigest(existing))
	}

	if opts.dryRun {
		if outputFmt == "json" {
			enc := json.NewEncoder(out)
			enc.SetIndent("", "  ")
			return enc.Encode(pushResult{
				Name:   shortName,
				Ref:    ref,
				DryRun: true,
			})
		}
		fmt.Fprintf(out, "%s: validated (dry run, push skipped)\n", shortName)
		return nil
	}

	digest, err := push(ctx, client, sourceDir, ref)
	if err != nil {
		return err
	}

	if outputFmt == "json" {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(pushResult{
			Name:      shortName,
			Ref:       ref,
			Digest:    digest,
			Overwrote: overwrote,
		})
	}

	fmt.Fprintf(out, "%s: pushed (%s)\n", shortName, klausoci.TruncateDigest(digest))
	return nil
}

// printArtifactMeta prints common metadata fields shared by all describe
// commands in a key: value layout.
func printArtifactMeta(out io.Writer, meta artifactMeta) {
	fmt.Fprintf(out, "%-14s %s\n", "Name:", meta.Name)
	if meta.Version != "" {
		fmt.Fprintf(out, "%-14s %s\n", "Version:", meta.Version)
	}
	if meta.Description != "" {
		fmt.Fprintf(out, "%-14s %s\n", "Description:", meta.Description)
	}
	if meta.Author != "" {
		fmt.Fprintf(out, "%-14s %s\n", "Author:", meta.Author)
	}
	if meta.Homepage != "" {
		fmt.Fprintf(out, "%-14s %s\n", "Homepage:", meta.Homepage)
	}
	if meta.Repository != "" {
		fmt.Fprintf(out, "%-14s %s\n", "Repository:", meta.Repository)
	}
	if meta.License != "" {
		fmt.Fprintf(out, "%-14s %s\n", "License:", meta.License)
	}
	if len(meta.Keywords) > 0 {
		fmt.Fprintf(out, "%-14s %s\n", "Keywords:", strings.Join(meta.Keywords, ", "))
	}
	if meta.Digest != "" {
		fmt.Fprintf(out, "%-14s %s\n", "Digest:", meta.Digest)
	}
}

// artifactMeta holds the common metadata fields used by printArtifactMeta.
type artifactMeta struct {
	Name        string
	Version     string
	Description string
	Author      string
	Homepage    string
	Repository  string
	License     string
	Keywords    []string
	Digest      string
}

// metaFromPlugin builds an artifactMeta from a DescribedPlugin.
func metaFromPlugin(dp *klausoci.DescribedPlugin) artifactMeta {
	m := artifactMeta{
		Name:        dp.Plugin.Name,
		Version:     dp.Plugin.Version,
		Description: dp.Plugin.Description,
		Homepage:    dp.Plugin.Homepage,
		Repository:  dp.Plugin.SourceRepo,
		License:     dp.Plugin.License,
		Keywords:    dp.Plugin.Keywords,
		Digest:      dp.ArtifactInfo.Digest,
	}
	if dp.Plugin.Author != nil {
		m.Author = formatAuthor(dp.Plugin.Author)
	}
	return m
}

// metaFromPersonality builds an artifactMeta from a DescribedPersonality.
func metaFromPersonality(dp *klausoci.DescribedPersonality) artifactMeta {
	m := artifactMeta{
		Name:        dp.Personality.Name,
		Version:     dp.Personality.Version,
		Description: dp.Personality.Description,
		Homepage:    dp.Personality.Homepage,
		Repository:  dp.Personality.SourceRepo,
		License:     dp.Personality.License,
		Keywords:    dp.Personality.Keywords,
		Digest:      dp.ArtifactInfo.Digest,
	}
	if dp.Personality.Author != nil {
		m.Author = formatAuthor(dp.Personality.Author)
	}
	return m
}

// metaFromToolchain builds an artifactMeta from a DescribedToolchain.
func metaFromToolchain(dt *klausoci.DescribedToolchain) artifactMeta {
	m := artifactMeta{
		Name:        dt.Toolchain.Name,
		Version:     dt.Toolchain.Version,
		Description: dt.Toolchain.Description,
		Homepage:    dt.Toolchain.Homepage,
		Repository:  dt.Toolchain.SourceRepo,
		License:     dt.Toolchain.License,
		Keywords:    dt.Toolchain.Keywords,
		Digest:      dt.ArtifactInfo.Digest,
	}
	if dt.Toolchain.Author != nil {
		m.Author = formatAuthor(dt.Toolchain.Author)
	}
	return m
}

// formatAuthor renders an Author as a display string.
func formatAuthor(a *klausoci.Author) string {
	if a == nil {
		return ""
	}
	if a.Email != "" {
		return fmt.Sprintf("%s <%s>", a.Name, a.Email)
	}
	return a.Name
}

// describePluginJSON is the JSON envelope for plugin describe output.
type describePluginJSON struct {
	Name        string   `json:"name"`
	Version     string   `json:"version,omitempty"`
	Description string   `json:"description,omitempty"`
	Author      string   `json:"author,omitempty"`
	Homepage    string   `json:"homepage,omitempty"`
	Repository  string   `json:"repository,omitempty"`
	License     string   `json:"license,omitempty"`
	Keywords    []string `json:"keywords,omitempty"`
	Ref         string   `json:"ref"`
	Digest      string   `json:"digest"`
	Skills      []string `json:"skills,omitempty"`
	Commands    []string `json:"commands,omitempty"`
	Agents      []string `json:"agents,omitempty"`
	HasHooks    bool     `json:"hasHooks,omitempty"`
	MCPServers  []string `json:"mcpServers,omitempty"`
	LSPServers  []string `json:"lspServers,omitempty"`
}

func newDescribePluginJSON(dp *klausoci.DescribedPlugin) describePluginJSON {
	m := metaFromPlugin(dp)
	return describePluginJSON{
		Name:        m.Name,
		Version:     m.Version,
		Description: m.Description,
		Author:      m.Author,
		Homepage:    m.Homepage,
		Repository:  m.Repository,
		License:     m.License,
		Keywords:    m.Keywords,
		Ref:         dp.ArtifactInfo.Ref,
		Digest:      m.Digest,
		Skills:      dp.Plugin.Skills,
		Commands:    dp.Plugin.Commands,
		Agents:      dp.Plugin.Agents,
		HasHooks:    dp.Plugin.HasHooks,
		MCPServers:  dp.Plugin.MCPServers,
		LSPServers:  dp.Plugin.LSPServers,
	}
}

// describePersonalityJSON is the JSON envelope for personality describe output.
type describePersonalityJSON struct {
	Name        string   `json:"name"`
	Version     string   `json:"version,omitempty"`
	Description string   `json:"description,omitempty"`
	Author      string   `json:"author,omitempty"`
	Homepage    string   `json:"homepage,omitempty"`
	Repository  string   `json:"repository,omitempty"`
	License     string   `json:"license,omitempty"`
	Keywords    []string `json:"keywords,omitempty"`
	Ref         string   `json:"ref"`
	Digest      string   `json:"digest"`
	Toolchain   string   `json:"toolchain,omitempty"`
	Plugins     []string `json:"plugins,omitempty"`

	ResolvedDeps *resolvedDepsJSON `json:"resolvedDependencies,omitempty"`
}

type resolvedDepsJSON struct {
	Toolchain *describeToolchainJSON `json:"toolchain,omitempty"`
	Plugins   []describePluginJSON   `json:"plugins,omitempty"`
	Warnings  []string               `json:"warnings,omitempty"`
}

func newDescribePersonalityJSON(dp *klausoci.DescribedPersonality, deps *klausoci.ResolvedDependencies) describePersonalityJSON {
	m := metaFromPersonality(dp)
	result := describePersonalityJSON{
		Name:        m.Name,
		Version:     m.Version,
		Description: m.Description,
		Author:      m.Author,
		Homepage:    m.Homepage,
		Repository:  m.Repository,
		License:     m.License,
		Keywords:    m.Keywords,
		Ref:         dp.ArtifactInfo.Ref,
		Digest:      m.Digest,
	}
	if dp.Personality.Toolchain.Repository != "" {
		result.Toolchain = dp.Personality.Toolchain.Ref()
	}
	for _, p := range dp.Personality.Plugins {
		result.Plugins = append(result.Plugins, p.Ref())
	}
	if deps != nil {
		rd := &resolvedDepsJSON{
			Warnings: deps.Warnings,
		}
		if deps.Toolchain != nil {
			tc := newDescribeToolchainJSON(deps.Toolchain)
			rd.Toolchain = &tc
		}
		for i := range deps.Plugins {
			rd.Plugins = append(rd.Plugins, newDescribePluginJSON(&deps.Plugins[i]))
		}
		result.ResolvedDeps = rd
	}
	return result
}

// describeToolchainJSON is the JSON envelope for toolchain describe output.
type describeToolchainJSON struct {
	Name        string   `json:"name"`
	Version     string   `json:"version,omitempty"`
	Description string   `json:"description,omitempty"`
	Author      string   `json:"author,omitempty"`
	Homepage    string   `json:"homepage,omitempty"`
	Repository  string   `json:"repository,omitempty"`
	License     string   `json:"license,omitempty"`
	Keywords    []string `json:"keywords,omitempty"`
	Ref         string   `json:"ref"`
	Digest      string   `json:"digest"`
}

func newDescribeToolchainJSON(dt *klausoci.DescribedToolchain) describeToolchainJSON {
	m := metaFromToolchain(dt)
	return describeToolchainJSON{
		Name:        m.Name,
		Version:     m.Version,
		Description: m.Description,
		Author:      m.Author,
		Homepage:    m.Homepage,
		Repository:  m.Repository,
		License:     m.License,
		Keywords:    m.Keywords,
		Ref:         dt.ArtifactInfo.Ref,
		Digest:      m.Digest,
	}
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
