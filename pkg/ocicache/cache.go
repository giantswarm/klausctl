// Package ocicache manages the on-disk klaus-oci registry response cache
// for klausctl.
//
// The cache accelerates repeated klausctl invocations by remembering the
// results of catalog lookups, tag-list queries, reference resolutions and
// manifest/blob fetches. It is a wrapper around the klaus-oci CacheStore
// rooted at a persistent directory derived from the XDG base directory
// spec.
//
// Global configuration (directory override and bypass) lives at package
// scope so the cobra root command can apply --cache-dir / --no-cache once
// and every oci.Client construction site picks them up transparently via
// Options().
package ocicache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	klausoci "github.com/giantswarm/klaus-oci"
)

// maxEntryReadBytes caps how much of an index JSON file readEntryKey will
// slurp into memory. The klaus-oci store writes small entries (a few kB) —
// anything larger is either corruption or an attacker-planted file, and we
// must not OOM the process to discover that.
const maxEntryReadBytes int64 = 1 << 20 // 1 MiB

// EnvBypass is the env var that, when truthy, disables the cache for a
// single invocation without persistent effect.
const EnvBypass = "KLAUSCTL_NO_CACHE"

// EnvCacheDir is an optional env var that overrides the cache directory.
// It is the env counterpart of the --cache-dir flag.
const EnvCacheDir = "KLAUSCTL_CACHE_DIR"

// Layers is the set of index sub-directories the klaus-oci disk cache
// uses. Kept here as a constant so our management commands (info, prune)
// can reason about the layout without re-implementing it.
var Layers = []string{"catalog", "tags", "refs", "blobs"}

var (
	mu       sync.RWMutex
	cfgDir   string // overridden directory, empty means "use default"
	cfgOff   bool   // explicit --no-cache bypass for the process
	envKnown bool   // env vars already parsed once
)

// Configure sets process-wide cache settings. Passing an empty dir keeps
// the default XDG-derived location. disabled=true bypasses the cache.
//
// It is safe to call multiple times; later calls replace the previous
// settings. cmd/root.go calls this from cobra.OnInitialize after global
// flags are parsed. OnInitialize is used rather than PersistentPreRun
// because any subcommand that defines its own PreRun would otherwise
// shadow the root's hook and skip cache configuration.
func Configure(dir string, disabled bool) {
	mu.Lock()
	defer mu.Unlock()
	cfgDir = strings.TrimSpace(dir)
	cfgOff = disabled
	envKnown = true
}

// readEnvLocked fills cfgDir/cfgOff from env vars on first use when
// Configure has not been called. Must be called with mu held.
func readEnvLocked() {
	if envKnown {
		return
	}
	if v := strings.TrimSpace(os.Getenv(EnvCacheDir)); v != "" {
		cfgDir = v
	}
	if isTruthy(os.Getenv(EnvBypass)) {
		cfgOff = true
	}
	envKnown = true
}

// Reset clears process-wide cache configuration, including the memoised
// env-var read. Intended for tests.
func Reset() {
	mu.Lock()
	defer mu.Unlock()
	cfgDir = ""
	cfgOff = false
	envKnown = false
}

// Disabled reports whether the cache is bypassed for this process.
func Disabled() bool {
	mu.Lock()
	defer mu.Unlock()
	readEnvLocked()
	return cfgOff
}

// Dir returns the resolved cache directory, consulting overrides and env
// vars in order. Returns an empty string (and no error) when no suitable
// directory can be determined, which callers should treat as "caching
// disabled".
func Dir() (string, error) {
	mu.Lock()
	defer mu.Unlock()
	readEnvLocked()
	if cfgOff {
		return "", nil
	}
	if cfgDir != "" {
		return expandPath(cfgDir), nil
	}
	return defaultDir()
}

// defaultDir returns $XDG_CACHE_HOME/klausctl/oci, falling back to
// $HOME/.cache/klausctl/oci. The directory is not created here; the
// klaus-oci store creates it lazily on first use.
func defaultDir() (string, error) {
	if v := strings.TrimSpace(os.Getenv("XDG_CACHE_HOME")); v != "" {
		return filepath.Join(v, "klausctl", "oci"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("determining home directory: %w", err)
	}
	return filepath.Join(home, ".cache", "klausctl", "oci"), nil
}

// Options returns the klaus-oci client options required to wire up the
// on-disk cache. When caching is disabled (via --no-cache or missing
// environment) it returns nil. Errors resolving the cache directory are
// returned as nil options to preserve network-only behavior rather than
// failing unrelated commands.
func Options() []klausoci.ClientOption {
	dir, err := Dir()
	if err != nil || dir == "" {
		return nil
	}
	return []klausoci.ClientOption{klausoci.WithCache(dir)}
}

// Info describes the on-disk cache for the `cache info` command.
type Info struct {
	// Dir is the resolved cache directory. Empty when caching is
	// disabled for this process.
	Dir string `json:"dir"`
	// Disabled reports whether the cache is bypassed for this invocation.
	Disabled bool `json:"disabled"`
	// Exists reports whether Dir exists on disk.
	Exists bool `json:"exists"`
	// TotalBytes is the summed size of all files under Dir.
	TotalBytes int64 `json:"total_bytes"`
	// Layers lists per-subdirectory statistics for the four klaus-oci
	// index layers (catalog, tags, refs, blobs).
	Layers []LayerInfo `json:"layers"`
	// NewestEntry is the modification time of the most recently written
	// cache entry, or zero when the cache is empty.
	NewestEntry time.Time `json:"newest_entry"`
	// FreshTTL is the window within which entries are served with zero
	// network traffic.
	FreshTTL time.Duration `json:"fresh_ttl"`
	// StaleTTL is the outer bound for ref/tag entries.
	StaleTTL time.Duration `json:"stale_ttl"`
	// CatalogStaleTTL is the outer bound for catalog entries.
	CatalogStaleTTL time.Duration `json:"catalog_stale_ttl"`
}

// LayerInfo is a per-subdirectory summary of the cache.
type LayerInfo struct {
	Name    string `json:"name"`
	Entries int    `json:"entries"`
	Bytes   int64  `json:"bytes"`
}

// Stat returns an Info snapshot for the current cache configuration.
// The returned error is non-nil only for unexpected filesystem problems;
// a missing cache directory returns an Info with Exists=false.
func Stat() (*Info, error) {
	info := &Info{
		Disabled:        Disabled(),
		FreshTTL:        klausoci.DefaultCacheFreshTTL,
		StaleTTL:        klausoci.DefaultCacheStaleTTL,
		CatalogStaleTTL: klausoci.DefaultCacheCatalogStaleTTL,
	}

	dir, err := Dir()
	if err != nil {
		return info, err
	}
	info.Dir = dir
	if dir == "" {
		return info, nil
	}

	if _, err := os.Stat(dir); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return info, nil
		}
		return info, fmt.Errorf("stat cache dir: %w", err)
	}
	info.Exists = true

	for _, layer := range Layers {
		sub := filepath.Join(dir, layer)
		l := LayerInfo{Name: layer}
		_ = filepath.WalkDir(sub, func(path string, d fs.DirEntry, werr error) error {
			if werr != nil {
				return nil
			}
			if d.IsDir() {
				return nil
			}
			fi, err := d.Info()
			if err != nil {
				return nil
			}
			if fi.Mode()&fs.ModeSymlink != 0 || !fi.Mode().IsRegular() {
				return nil
			}
			if fi.ModTime().After(info.NewestEntry) {
				info.NewestEntry = fi.ModTime()
			}
			// Index layers (catalog/tags/refs) store one JSON file per
			// entry. The blobs layer is an oci-layout content store whose
			// "entries" count we approximate as the number of blob files.
			l.Entries++
			l.Bytes += fi.Size()
			info.TotalBytes += fi.Size()
			return nil
		})
		info.Layers = append(info.Layers, l)
	}
	sort.Slice(info.Layers, func(i, j int) bool {
		return info.Layers[i].Name < info.Layers[j].Name
	})
	return info, nil
}

// PruneOptions controls which entries `Prune` removes.
type PruneOptions struct {
	// All removes everything under the cache directory, including fresh
	// entries. When false, only entries that the klaus-oci cache would
	// treat as stale (older than the relevant stale TTL) are removed.
	All bool
	// Now is the reference time used to evaluate entry freshness.
	// Defaults to time.Now().
	Now time.Time
}

// PruneResult reports what Prune removed.
type PruneResult struct {
	// Dir is the cache directory that was pruned.
	Dir string `json:"dir"`
	// FilesRemoved is the number of files deleted.
	FilesRemoved int `json:"files_removed"`
	// BytesRemoved is the summed size of deleted files.
	BytesRemoved int64 `json:"bytes_removed"`
	// Removed is the cache-relative path of each deleted file. Populated
	// only for --all prunes so large stale sweeps do not produce unwieldy
	// output.
	Removed []string `json:"removed,omitempty"`
}

// Prune deletes cached entries. When opts.All is true the entire cache
// directory tree is removed, leaving only the root directory. Otherwise
// only stale index entries are removed -- content-store blobs are kept
// under the klaus-oci LRU-size policy.
func Prune(opts PruneOptions) (*PruneResult, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	res := &PruneResult{Dir: dir}
	if dir == "" {
		return res, nil
	}
	if _, err := os.Stat(dir); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return res, nil
		}
		return res, fmt.Errorf("stat cache dir: %w", err)
	}

	if opts.All {
		return pruneAll(dir, res)
	}

	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	return pruneStale(dir, now, res)
}

func pruneAll(dir string, res *PruneResult) (*PruneResult, error) {
	// Only sweep directories that belong to the klaus-oci layout. This
	// prevents `klausctl --cache-dir $HOME cache prune --all` from nuking
	// unrelated files — the caller's misconfiguration is bounded to the
	// known layer names.
	for _, layer := range Layers {
		sub := filepath.Join(dir, layer)
		// Skip symlinks at the layer root to defeat a symlink-swap that
		// would redirect the walk outside the cache directory.
		fi, err := os.Lstat(sub)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return res, fmt.Errorf("stat %s: %w", sub, err)
		}
		if fi.Mode()&fs.ModeSymlink != 0 {
			continue
		}
		_ = filepath.WalkDir(sub, func(path string, d fs.DirEntry, werr error) error {
			if werr != nil || d.IsDir() {
				return nil
			}
			// filepath.WalkDir does not follow symlinks for iteration, but a
			// symlinked regular file inside the cache could still be removed;
			// skip those so we only delete cache-owned content.
			info, err := d.Info()
			if err != nil {
				return nil
			}
			if info.Mode()&fs.ModeSymlink != 0 || !info.Mode().IsRegular() {
				return nil
			}
			size := info.Size()
			if rmErr := os.Remove(path); rmErr != nil {
				return nil
			}
			res.FilesRemoved++
			res.BytesRemoved += size
			if rel, relErr := filepath.Rel(dir, path); relErr == nil {
				res.Removed = append(res.Removed, rel)
			}
			return nil
		})
		// Best-effort: remove now-empty directory tree. If the walk left
		// anything behind (e.g. a skipped symlink), RemoveAll would still
		// follow it — so we use a RemoveAll on the known-safe layer path only
		// after confirming it is not a symlink (checked above).
		_ = os.RemoveAll(sub)
	}
	return res, nil
}

func pruneStale(dir string, now time.Time, res *PruneResult) (*PruneResult, error) {
	staleForLayer := map[string]time.Duration{
		"catalog": klausoci.DefaultCacheCatalogStaleTTL,
		"tags":    klausoci.DefaultCacheStaleTTL,
		"refs":    klausoci.DefaultCacheStaleTTL,
	}
	for layer, ttl := range staleForLayer {
		sub := filepath.Join(dir, layer)
		_ = filepath.WalkDir(sub, func(path string, d fs.DirEntry, werr error) error {
			if werr != nil || d.IsDir() {
				return nil
			}
			fi, err := d.Info()
			if err != nil {
				return nil
			}
			if fi.Mode()&fs.ModeSymlink != 0 || !fi.Mode().IsRegular() {
				return nil
			}
			if now.Sub(fi.ModTime()) <= ttl {
				return nil
			}
			size := fi.Size()
			if rmErr := os.Remove(path); rmErr != nil {
				return nil
			}
			res.FilesRemoved++
			res.BytesRemoved += size
			return nil
		})
	}
	return res, nil
}

// RefreshOptions controls which entries Refresh forces a revalidation of.
type RefreshOptions struct {
	// Registry is an optional registry base ("host" or "host/prefix"). When
	// non-empty, only catalog entries for this base are invalidated.
	Registry string
	// Repo is an optional repository ("host/name"). When non-empty, only
	// tag and ref entries for this repository are invalidated.
	Repo string
}

// RefreshResult reports the outcome of a foreground refresh.
type RefreshResult struct {
	// Dir is the cache directory that was refreshed.
	Dir string `json:"dir"`
	// FilesRemoved is the count of index files invalidated.
	FilesRemoved int `json:"files_removed"`
	// Scope describes what was refreshed (all, registry, repo, ...).
	Scope string `json:"scope"`
}

// Refresh forces revalidation by invalidating the matching index
// entries. On the next client call the klaus-oci store will synchronously
// refetch the data from the registry. Blob content is kept because blobs
// are content-addressed and never become stale.
//
// Refresh does not itself issue any network requests. It prepares the
// cache so that subsequent reads hit the registry.
func Refresh(_ context.Context, opts RefreshOptions) (*RefreshResult, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	res := &RefreshResult{Dir: dir}
	if dir == "" {
		return res, nil
	}
	if _, err := os.Stat(dir); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return res, nil
		}
		return res, fmt.Errorf("stat cache dir: %w", err)
	}

	switch {
	case opts.Repo != "":
		res.Scope = "repo=" + opts.Repo
		res.FilesRemoved = removeByKeyPrefix(dir, []string{"tags", "refs"}, opts.Repo)
	case opts.Registry != "":
		res.Scope = "registry=" + opts.Registry
		res.FilesRemoved = removeByKeyPrefix(dir, []string{"catalog"}, opts.Registry)
	default:
		res.Scope = "all"
		for _, layer := range []string{"catalog", "tags", "refs"} {
			res.FilesRemoved += removeAllInLayer(filepath.Join(dir, layer))
		}
	}
	return res, nil
}

// removeByKeyPrefix deletes every index entry under the given layers
// whose persisted Key begins with prefix. The klaus-oci store stores the
// full key inside the JSON file; we parse each to decide whether to
// remove it.
func removeByKeyPrefix(dir string, layers []string, prefix string) int {
	var removed int
	for _, layer := range layers {
		sub := filepath.Join(dir, layer)
		_ = filepath.WalkDir(sub, func(path string, d fs.DirEntry, werr error) error {
			if werr != nil || d.IsDir() {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			if info.Mode()&fs.ModeSymlink != 0 || !info.Mode().IsRegular() {
				return nil
			}
			key, ok := readEntryKey(path)
			if !ok {
				return nil
			}
			if !matchesPrefix(layer, key, prefix) {
				return nil
			}
			if err := os.Remove(path); err == nil {
				removed++
			}
			return nil
		})
	}
	return removed
}

// matchesPrefix decides whether a stored index key matches the caller's
// scoping prefix. For catalog keys (layer=catalog) the stored key is the
// registry base with a trailing slash removed; for tags/refs it is the
// full repo path (optionally with ":tag"). We match by prefix on the
// canonical stored form.
func matchesPrefix(layer, key, prefix string) bool {
	key = strings.TrimSuffix(key, "/")
	prefix = strings.TrimSuffix(prefix, "/")
	if layer == "catalog" {
		return key == prefix || strings.HasPrefix(key, prefix+"/")
	}
	// For tags/refs the key looks like "host/repo" or "host/repo:tag".
	// A prefix of "host/repo" should match both.
	if key == prefix {
		return true
	}
	if strings.HasPrefix(key, prefix+":") {
		return true
	}
	return strings.HasPrefix(key, prefix+"/")
}

func removeAllInLayer(sub string) int {
	var removed int
	_ = filepath.WalkDir(sub, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil || d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if info.Mode()&fs.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil
		}
		if err := os.Remove(path); err == nil {
			removed++
		}
		return nil
	})
	return removed
}

// readEntryKey reads the "key" field from an index JSON file without
// decoding the rest. Returns false on any read/parse failure. A LimitReader
// cap prevents a malformed or malicious cache file from OOMing the process.
func readEntryKey(path string) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxEntryReadBytes))
	if err != nil {
		return "", false
	}
	var entry struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(data, &entry); err != nil {
		return "", false
	}
	return entry.Key, true
}

func expandPath(p string) string {
	if strings.HasPrefix(p, "~/") || p == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return p
		}
		if p == "~" {
			return home
		}
		return filepath.Join(home, p[2:])
	}
	return p
}

func isTruthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
