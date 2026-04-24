package ocicache

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	klausoci "github.com/giantswarm/klaus-oci"
)

// writeIndex serializes v to JSON at path with the mtime backdated by age.
// Tests use it to stage index entries with known freshness.
func writeIndex(t *testing.T, path string, v any, age time.Duration) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	mt := time.Now().Add(-age)
	if err := os.Chtimes(path, mt, mt); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
}

// withCacheDir points the package-level config at a temp dir for the
// duration of a test and restores global state on cleanup.
func withCacheDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	Configure(dir, false)
	t.Cleanup(Reset)
	return dir
}

func TestDefaultDir_UsesXDGCacheHome(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "/tmp/some-xdg")
	Reset()
	dir, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join("/tmp/some-xdg", "klausctl", "oci")
	if dir != want {
		t.Errorf("Dir() = %q, want %q", dir, want)
	}
}

func TestDefaultDir_FallsBackToHomeCache(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("HOME", "/home/example")
	Reset()
	dir, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join("/home/example", ".cache", "klausctl", "oci")
	if dir != want {
		t.Errorf("Dir() = %q, want %q", dir, want)
	}
}

func TestEnvBypass_DisablesOptions(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "/tmp/xdg")
	t.Setenv(EnvBypass, "1")
	Reset()
	if !Disabled() {
		t.Error("Disabled() = false, want true with KLAUSCTL_NO_CACHE=1")
	}
	if opts := Options(); opts != nil {
		t.Errorf("Options() = %v, want nil when disabled", opts)
	}
}

func TestConfigureFlagTakesPrecedenceOverEnv(t *testing.T) {
	t.Setenv(EnvCacheDir, "/env/path")
	Reset()
	Configure("/flag/path", false)
	t.Cleanup(Reset)
	dir, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	if dir != "/flag/path" {
		t.Errorf("Dir() = %q, want /flag/path", dir)
	}
}

func TestConfigureNoCache(t *testing.T) {
	Configure("", true)
	t.Cleanup(Reset)
	dir, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	if dir != "" {
		t.Errorf("Dir() = %q, want empty when disabled", dir)
	}
	if opts := Options(); opts != nil {
		t.Errorf("Options() = %v, want nil when disabled", opts)
	}
}

func TestOptions_ContainsWithCache(t *testing.T) {
	dir := withCacheDir(t)
	opts := Options()
	if len(opts) == 0 {
		t.Fatalf("Options() = nil, want WithCache option for %s", dir)
	}
	// Constructing a client with the option must not panic and must
	// honour the directory.
	client := klausoci.NewClient(opts...)
	if client == nil {
		t.Fatal("NewClient returned nil")
	}
}

func TestStat_EmptyCache(t *testing.T) {
	// Point at a directory that does not exist so Stat.Exists is false.
	dir := filepath.Join(t.TempDir(), "cache")
	Configure(dir, false)
	t.Cleanup(Reset)

	info, err := Stat()
	if err != nil {
		t.Fatal(err)
	}
	if info.Dir != dir {
		t.Errorf("Stat.Dir = %q, want %q", info.Dir, dir)
	}
	if info.Exists {
		t.Error("Stat.Exists = true, want false for a non-existent dir")
	}
	if info.TotalBytes != 0 {
		t.Errorf("Stat.TotalBytes = %d, want 0", info.TotalBytes)
	}
}

func TestStat_CountsEntries(t *testing.T) {
	dir := withCacheDir(t)
	writeIndex(t, filepath.Join(dir, "refs", "a.json"),
		map[string]any{"key": "host/repo:tag"}, 0)
	writeIndex(t, filepath.Join(dir, "tags", "b.json"),
		map[string]any{"key": "host/repo"}, 0)

	info, err := Stat()
	if err != nil {
		t.Fatal(err)
	}
	if !info.Exists {
		t.Fatal("Stat.Exists = false after writing entries")
	}
	counts := map[string]int{}
	for _, l := range info.Layers {
		counts[l.Name] = l.Entries
	}
	if counts["refs"] != 1 || counts["tags"] != 1 {
		t.Errorf("layer entries = %v, want refs=1 tags=1", counts)
	}
	if info.TotalBytes == 0 {
		t.Error("Stat.TotalBytes = 0 after writing entries")
	}
}

func TestPrune_All(t *testing.T) {
	dir := withCacheDir(t)
	writeIndex(t, filepath.Join(dir, "refs", "a.json"),
		map[string]any{"key": "host/repo:tag"}, time.Second)
	writeIndex(t, filepath.Join(dir, "blobs", "sha256", "deadbeef"),
		map[string]any{"payload": "x"}, time.Second)

	res, err := Prune(PruneOptions{All: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.FilesRemoved != 2 {
		t.Errorf("FilesRemoved = %d, want 2", res.FilesRemoved)
	}
	// Both sub-trees should be gone.
	if _, err := os.Stat(filepath.Join(dir, "refs")); !os.IsNotExist(err) {
		t.Errorf("refs still present: err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "blobs")); !os.IsNotExist(err) {
		t.Errorf("blobs still present: err=%v", err)
	}
}

func TestPrune_AllOnlyTouchesKnownLayers(t *testing.T) {
	// Guard: a misconfigured --cache-dir must not let pruneAll recurse
	// outside the klaus-oci layout. Unknown siblings are left alone.
	dir := withCacheDir(t)
	foreign := filepath.Join(dir, "not-a-layer", "do-not-delete.txt")
	if err := os.MkdirAll(filepath.Dir(foreign), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(foreign, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeIndex(t, filepath.Join(dir, "refs", "a.json"),
		map[string]any{"key": "host/repo:tag"}, time.Second)

	if _, err := Prune(PruneOptions{All: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Errorf("foreign file removed by --all prune: %v", err)
	}
}

func TestPrune_StaleOnly(t *testing.T) {
	dir := withCacheDir(t)
	fresh := filepath.Join(dir, "refs", "fresh.json")
	stale := filepath.Join(dir, "refs", "stale.json")
	// Use the default stale TTL so the fresh entry is kept and the
	// stale one is evicted.
	writeIndex(t, fresh, map[string]any{"key": "x"}, time.Second)
	writeIndex(t, stale, map[string]any{"key": "y"},
		klausoci.DefaultCacheStaleTTL+time.Hour)

	res, err := Prune(PruneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.FilesRemoved != 1 {
		t.Errorf("FilesRemoved = %d, want 1 (only stale)", res.FilesRemoved)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("fresh entry removed: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale entry still present: err=%v", err)
	}
}

func TestPrune_Disabled(t *testing.T) {
	Configure("", true)
	t.Cleanup(Reset)
	res, err := Prune(PruneOptions{All: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Dir != "" {
		t.Errorf("res.Dir = %q, want empty when disabled", res.Dir)
	}
	if res.FilesRemoved != 0 {
		t.Errorf("FilesRemoved = %d, want 0", res.FilesRemoved)
	}
}

func TestRefresh_All(t *testing.T) {
	dir := withCacheDir(t)
	writeIndex(t, filepath.Join(dir, "refs", "a.json"),
		map[string]any{"key": "host/repo:tag"}, time.Second)
	writeIndex(t, filepath.Join(dir, "tags", "b.json"),
		map[string]any{"key": "host/repo"}, time.Second)
	writeIndex(t, filepath.Join(dir, "catalog", "c.json"),
		map[string]any{"key": "host"}, time.Second)
	writeIndex(t, filepath.Join(dir, "blobs", "sha256", "deadbeef"),
		map[string]any{"payload": "x"}, time.Second)

	res, err := Refresh(context.Background(), RefreshOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.FilesRemoved != 3 {
		t.Errorf("FilesRemoved = %d, want 3 (refs+tags+catalog)", res.FilesRemoved)
	}
	// Blob content must survive.
	if _, err := os.Stat(filepath.Join(dir, "blobs", "sha256", "deadbeef")); err != nil {
		t.Errorf("blob removed by refresh: %v", err)
	}
}

func TestRefresh_ScopedByRepo(t *testing.T) {
	dir := withCacheDir(t)
	writeIndex(t, filepath.Join(dir, "refs", "matching.json"),
		map[string]any{"key": "host/repo:tag"}, time.Second)
	writeIndex(t, filepath.Join(dir, "refs", "other.json"),
		map[string]any{"key": "host/elsewhere:tag"}, time.Second)
	writeIndex(t, filepath.Join(dir, "tags", "match.json"),
		map[string]any{"key": "host/repo"}, time.Second)
	writeIndex(t, filepath.Join(dir, "catalog", "cat.json"),
		map[string]any{"key": "host"}, time.Second)

	res, err := Refresh(context.Background(), RefreshOptions{Repo: "host/repo"})
	if err != nil {
		t.Fatal(err)
	}
	if res.FilesRemoved != 2 {
		t.Errorf("FilesRemoved = %d, want 2 (matching ref + tag list)", res.FilesRemoved)
	}
	// The untargeted ref should still be there.
	if _, err := os.Stat(filepath.Join(dir, "refs", "other.json")); err != nil {
		t.Errorf("unrelated ref removed: %v", err)
	}
	// Catalog entries should not be touched by --repo scoping.
	if _, err := os.Stat(filepath.Join(dir, "catalog", "cat.json")); err != nil {
		t.Errorf("catalog removed by --repo refresh: %v", err)
	}
}

func TestRefresh_ScopedByRegistry(t *testing.T) {
	dir := withCacheDir(t)
	writeIndex(t, filepath.Join(dir, "catalog", "a.json"),
		map[string]any{"key": "registry.example.com"}, time.Second)
	writeIndex(t, filepath.Join(dir, "catalog", "b.json"),
		map[string]any{"key": "other.example.com"}, time.Second)

	res, err := Refresh(context.Background(), RefreshOptions{Registry: "registry.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if res.FilesRemoved != 1 {
		t.Errorf("FilesRemoved = %d, want 1", res.FilesRemoved)
	}
	if _, err := os.Stat(filepath.Join(dir, "catalog", "b.json")); err != nil {
		t.Errorf("unrelated catalog entry removed: %v", err)
	}
}

func TestMatchesPrefix(t *testing.T) {
	cases := []struct {
		layer, key, prefix string
		want               bool
	}{
		{"refs", "host/repo:tag", "host/repo", true},
		{"refs", "host/other:tag", "host/repo", false},
		{"refs", "host/repo/sub:tag", "host/repo", true},
		{"tags", "host/repo", "host/repo", true},
		{"tags", "host/repository", "host/repo", false},
		{"catalog", "host", "host", true},
		{"catalog", "host/prefix", "host", true},
		{"catalog", "hostel", "host", false},
	}
	for _, tc := range cases {
		got := matchesPrefix(tc.layer, tc.key, tc.prefix)
		if got != tc.want {
			t.Errorf("matchesPrefix(%q, %q, %q) = %v, want %v",
				tc.layer, tc.key, tc.prefix, got, tc.want)
		}
	}
}
