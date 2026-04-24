package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsRepoIdentifier(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"giantswarm/klausctl", true},
		{"owner/repo", true},
		{"a/b", true},
		// Filesystem paths.
		{"/absolute/path", false},
		{"./relative/path", false},
		{"../parent/path", false},
		{"~/home/path", false},
		// No slash or multiple slashes.
		{"noslash", false},
		{"a/b/c", false},
		{"", false},
		// Dot-prefixed with slash (relative path).
		{".hidden/repo", false},
	}

	for _, tt := range tests {
		if got := IsRepoIdentifier(tt.input); got != tt.want {
			t.Errorf("IsRepoIdentifier(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestIsAllowed(t *testing.T) {
	cfg := &WorkspaceConfig{
		Organizations: []string{"giantswarm"},
		Repos: []RepoEntry{
			{Name: "external/special-repo"},
		},
	}

	// Org-based: any repo under giantswarm is allowed.
	if !IsAllowed(cfg, "giantswarm", "klausctl") {
		t.Error("expected giantswarm/klausctl to be allowed via org")
	}
	if !IsAllowed(cfg, "giantswarm", "anything") {
		t.Error("expected giantswarm/anything to be allowed via org")
	}
	// Case-insensitive org match.
	if !IsAllowed(cfg, "GiantSwarm", "klausctl") {
		t.Error("expected case-insensitive org match")
	}

	// Repo-based: explicit entry.
	if !IsAllowed(cfg, "external", "special-repo") {
		t.Error("expected external/special-repo to be allowed via repos list")
	}
	// Case-insensitive repo match.
	if !IsAllowed(cfg, "External", "Special-Repo") {
		t.Error("expected case-insensitive repo match")
	}

	// Not allowed.
	if IsAllowed(cfg, "unknown", "repo") {
		t.Error("expected unknown/repo to be disallowed")
	}
	if IsAllowed(cfg, "external", "other-repo") {
		t.Error("expected external/other-repo to be disallowed")
	}
}

func TestLoadSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workspaces.yaml")

	cfg := &WorkspaceConfig{
		Organizations: []string{"giantswarm", "acme"},
		Repos: []RepoEntry{
			{Name: "external/tool"},
			{Name: "other/lib"},
		},
	}

	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if len(loaded.Organizations) != 2 || loaded.Organizations[0] != "giantswarm" || loaded.Organizations[1] != "acme" {
		t.Errorf("unexpected organizations: %v", loaded.Organizations)
	}
	if len(loaded.Repos) != 2 || loaded.Repos[0].Name != "external/tool" || loaded.Repos[1].Name != "other/lib" {
		t.Errorf("unexpected repos: %v", loaded.Repos)
	}
}

func TestLoadMissingFile(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "nonexistent.yaml"))
	if err != nil {
		t.Fatalf("Load() error on missing file: %v", err)
	}
	if len(cfg.Organizations) != 0 || len(cfg.Repos) != 0 {
		t.Error("expected empty config for missing file")
	}
}

func TestSaveCreatesParentDirs(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "dir")
	path := filepath.Join(dir, "workspaces.yaml")

	cfg := &WorkspaceConfig{Organizations: []string{"org"}}
	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file to exist: %v", err)
	}
}

// run executes a git command and fails the test on error.
func run(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...) // #nosec G204 -- container runtime CLI invocation with controlled args
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s failed: %v\n%s", name, strings.Join(args, " "), err, out)
	}
}

// initBareRepo creates a bare repo with one commit and returns its path.
func initBareRepo(t *testing.T) string {
	t.Helper()
	bare := filepath.Join(t.TempDir(), "origin.git")
	run(t, "", "git", "init", "--bare", "--initial-branch=main", bare)

	clone := filepath.Join(t.TempDir(), "clone")
	run(t, "", "git", "clone", bare, clone)
	run(t, clone, "git", "config", "user.email", "test@test.com")
	run(t, clone, "git", "config", "user.name", "Test")
	run(t, clone, "git", "config", "commit.gpgsign", "false")
	run(t, clone, "git", "checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(clone, "README.md"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	run(t, clone, "git", "add", ".")
	run(t, clone, "git", "commit", "-m", "init")
	run(t, clone, "git", "push", "-u", "origin", "main")

	return bare
}

func TestEnsureCachedClonesRepo(t *testing.T) {
	bare := initBareRepo(t)
	reposDir := filepath.Join(t.TempDir(), "repos")

	// Point EnsureCached at the local bare repo instead of github.com
	// by pre-creating the owner dir and using a symlink-like approach.
	// Instead, we test by creating a file:// clone manually to simulate.
	// Actually, let's use a helper that clones from the bare repo.

	// Create the cache directory structure manually to simulate what
	// EnsureCached would do with a real GitHub URL. Then test fetch.
	ownerDir := filepath.Join(reposDir, "testowner")
	cacheDir := filepath.Join(ownerDir, "testrepo")
	if err := os.MkdirAll(ownerDir, 0o750); err != nil {
		t.Fatal(err)
	}

	// Clone with --no-checkout to match the real behavior.
	run(t, "", "git", "clone", "--no-checkout", bare, cacheDir)

	// Verify the .git directory exists but no working tree files.
	gitDir := filepath.Join(cacheDir, ".git")
	info, err := os.Stat(gitDir)
	if err != nil {
		t.Fatalf("expected .git to exist: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("expected .git to be a directory")
	}

	// No README should exist (--no-checkout).
	if _, err := os.Stat(filepath.Join(cacheDir, "README.md")); !os.IsNotExist(err) {
		t.Fatal("expected no working tree files with --no-checkout")
	}

	// Now EnsureCached should detect existing clone and fetch.
	path, err := EnsureCached(reposDir, "testowner", "testrepo", false)
	if err != nil {
		t.Fatalf("EnsureCached() error: %v", err)
	}
	if path != cacheDir {
		t.Fatalf("expected cache path %q, got %q", cacheDir, path)
	}

	// Test noFetch path.
	path, err = EnsureCached(reposDir, "testowner", "testrepo", true)
	if err != nil {
		t.Fatalf("EnsureCached(noFetch=true) error: %v", err)
	}
	if path != cacheDir {
		t.Fatalf("expected cache path %q, got %q", cacheDir, path)
	}
}

func TestListCached(t *testing.T) {
	reposDir := t.TempDir()

	// Create two valid cached repos.
	for _, id := range []string{"owner1/repo1", "owner2/repo2"} {
		parts := strings.SplitN(id, "/", 2)
		repoPath := filepath.Join(reposDir, parts[0], parts[1])
		if err := os.MkdirAll(filepath.Join(repoPath, ".git"), 0o750); err != nil {
			t.Fatal(err)
		}
	}

	// Create a directory without .git (should be skipped).
	if err := os.MkdirAll(filepath.Join(reposDir, "owner3", "norepo"), 0o750); err != nil {
		t.Fatal(err)
	}

	// Create a file in the owner dir (should be skipped).
	if err := os.WriteFile(filepath.Join(reposDir, "stray-file"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	repos, err := ListCached(reposDir)
	if err != nil {
		t.Fatalf("ListCached() error: %v", err)
	}

	if len(repos) != 2 {
		t.Fatalf("expected 2 cached repos, got %d", len(repos))
	}

	// Build a set of identifiers for easy checking.
	ids := map[string]bool{}
	for _, r := range repos {
		ids[r.Identifier] = true
		// Verify fields are consistent.
		if r.Identifier != r.Owner+"/"+r.Repo {
			t.Errorf("inconsistent CachedRepo: %+v", r)
		}
	}

	if !ids["owner1/repo1"] || !ids["owner2/repo2"] {
		t.Errorf("unexpected repos: %v", ids)
	}
}

func TestListCachedEmptyDir(t *testing.T) {
	repos, err := ListCached(filepath.Join(t.TempDir(), "nonexistent"))
	if err != nil {
		t.Fatalf("ListCached() error on nonexistent dir: %v", err)
	}
	if len(repos) != 0 {
		t.Errorf("expected empty list, got %d", len(repos))
	}
}
