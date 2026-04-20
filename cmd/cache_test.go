package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giantswarm/klausctl/pkg/ocicache"
)

func TestCacheInfo_JSON(t *testing.T) {
	dir := t.TempDir()
	ocicache.Configure(dir, false)
	t.Cleanup(ocicache.Reset)

	// Populate a single index entry so Exists=true and Layers non-zero.
	entry := filepath.Join(dir, "refs", "x.json")
	if err := os.MkdirAll(filepath.Dir(entry), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entry, []byte(`{"key":"host/repo:tag"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	cacheInfoCmd.SetOut(&buf)
	cacheInfoCmd.SetErr(&buf)
	cacheInfoFormat = "json"
	t.Cleanup(func() { cacheInfoFormat = "text" })

	if err := runCacheInfo(cacheInfoCmd, nil); err != nil {
		t.Fatalf("runCacheInfo: %v", err)
	}

	var info ocicache.Info
	if err := json.Unmarshal(buf.Bytes(), &info); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, buf.String())
	}
	if info.Dir != dir {
		t.Errorf("info.Dir = %q, want %q", info.Dir, dir)
	}
	if !info.Exists {
		t.Error("info.Exists = false after writing an entry")
	}
}

func TestCacheInfo_TextOutput(t *testing.T) {
	dir := t.TempDir()
	ocicache.Configure(dir, false)
	t.Cleanup(ocicache.Reset)

	var buf bytes.Buffer
	cacheInfoCmd.SetOut(&buf)
	cacheInfoFormat = "text"
	if err := runCacheInfo(cacheInfoCmd, nil); err != nil {
		t.Fatalf("runCacheInfo: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, dir) {
		t.Errorf("text output missing cache dir: %s", out)
	}
}

func TestCachePrune_All(t *testing.T) {
	dir := t.TempDir()
	ocicache.Configure(dir, false)
	t.Cleanup(ocicache.Reset)

	entry := filepath.Join(dir, "refs", "x.json")
	if err := os.MkdirAll(filepath.Dir(entry), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entry, []byte(`{"key":"host/repo:tag"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	cachePruneCmd.SetOut(&buf)
	cachePruneAll = true
	cachePruneFormat = "text"
	t.Cleanup(func() { cachePruneAll = false })

	if err := runCachePrune(cachePruneCmd, nil); err != nil {
		t.Fatalf("runCachePrune: %v", err)
	}

	if _, err := os.Stat(entry); !os.IsNotExist(err) {
		t.Errorf("entry not removed after --all prune: err=%v", err)
	}
	if !strings.Contains(buf.String(), "Pruned") {
		t.Errorf("unexpected output: %s", buf.String())
	}
}

func TestCacheRefresh_MutuallyExclusiveFlags(t *testing.T) {
	ocicache.Configure(t.TempDir(), false)
	t.Cleanup(ocicache.Reset)

	var buf bytes.Buffer
	cacheRefreshCmd.SetOut(&buf)
	cacheRefreshRegistry = "registry.example.com"
	cacheRefreshRepo = "registry.example.com/repo"
	cacheRefreshFormat = "text"
	t.Cleanup(func() {
		cacheRefreshRegistry = ""
		cacheRefreshRepo = ""
	})

	err := runCacheRefresh(cacheRefreshCmd, nil)
	if err == nil {
		t.Fatal("expected error for mutually exclusive flags")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("error = %v, want 'mutually exclusive'", err)
	}
}

func TestCacheRefresh_ScopedByRepo(t *testing.T) {
	dir := t.TempDir()
	ocicache.Configure(dir, false)
	t.Cleanup(ocicache.Reset)

	target := filepath.Join(dir, "refs", "match.json")
	keep := filepath.Join(dir, "refs", "keep.json")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(`{"key":"host/repo:tag"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keep, []byte(`{"key":"host/other:tag"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	cacheRefreshCmd.SetOut(&buf)
	cacheRefreshRepo = "host/repo"
	cacheRefreshRegistry = ""
	cacheRefreshFormat = "text"
	t.Cleanup(func() { cacheRefreshRepo = "" })

	if err := runCacheRefresh(cacheRefreshCmd, nil); err != nil {
		t.Fatalf("runCacheRefresh: %v", err)
	}

	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Errorf("matching entry not removed: err=%v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("unrelated entry removed: %v", err)
	}
}

func TestGlobalFlags_NoCache_BypassesOptions(t *testing.T) {
	// Simulate cobra.OnInitialize firing with --no-cache set.
	cacheDirFlag = "/tmp/ignored"
	noCacheFlag = true
	applyCacheFlags()
	t.Cleanup(func() {
		cacheDirFlag = ""
		noCacheFlag = false
		ocicache.Reset()
	})

	if !ocicache.Disabled() {
		t.Error("Disabled() = false after --no-cache applyCacheFlags")
	}
	if opts := ocicache.Options(); opts != nil {
		t.Errorf("Options() = %v, want nil when --no-cache set", opts)
	}
}

func TestGlobalFlags_CacheDir_Applied(t *testing.T) {
	dir := t.TempDir()
	cacheDirFlag = dir
	noCacheFlag = false
	applyCacheFlags()
	t.Cleanup(func() {
		cacheDirFlag = ""
		ocicache.Reset()
	})

	got, err := ocicache.Dir()
	if err != nil {
		t.Fatal(err)
	}
	if got != dir {
		t.Errorf("Dir() = %q, want %q", got, dir)
	}
}
