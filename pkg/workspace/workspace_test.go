package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
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
		{"", false},
		{"/absolute/path", false},
		{"./relative/path", false},
		{"~/home/path", false},
		{"just-a-name", false},
		{"a/b/c", false},
		{"/owner/repo", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := IsRepoIdentifier(tt.input); got != tt.want {
				t.Errorf("IsRepoIdentifier(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsAllowed(t *testing.T) {
	reg := &Registry{
		Orgs: []string{"giantswarm"},
		Repos: []RepoSpec{
			{Owner: "other", Repo: "special"},
		},
	}

	tests := []struct {
		owner, repo string
		want        bool
	}{
		{"giantswarm", "klausctl", true},
		{"giantswarm", "anything", true},
		{"other", "special", true},
		{"other", "nope", false},
		{"unknown", "repo", false},
	}
	for _, tt := range tests {
		t.Run(tt.owner+"/"+tt.repo, func(t *testing.T) {
			if got := IsAllowed(reg, tt.owner, tt.repo); got != tt.want {
				t.Errorf("IsAllowed(%q, %q) = %v, want %v", tt.owner, tt.repo, got, tt.want)
			}
		})
	}
}

func TestLoad_MissingFile(t *testing.T) {
	reg, err := Load(filepath.Join(t.TempDir(), "nonexistent.yaml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reg.Orgs) != 0 || len(reg.Repos) != 0 {
		t.Fatalf("expected empty registry, got %+v", reg)
	}
}

func TestLoad_ValidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workspaces.yaml")
	content := `orgs:
  - giantswarm
repos:
  - owner: other
    repo: special
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	reg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reg.Orgs) != 1 || reg.Orgs[0] != "giantswarm" {
		t.Fatalf("unexpected orgs: %v", reg.Orgs)
	}
	if len(reg.Repos) != 1 || reg.Repos[0].Owner != "other" {
		t.Fatalf("unexpected repos: %v", reg.Repos)
	}
}

func TestEnsureCached_ClonesLocalRepo(t *testing.T) {
	// Create a bare repo to act as "remote".
	bare := filepath.Join(t.TempDir(), "origin.git")
	run(t, "", "git", "init", "--bare", "--initial-branch=main", bare)

	// Create a clone, add a commit, and push.
	src := filepath.Join(t.TempDir(), "src")
	run(t, "", "git", "clone", bare, src)
	run(t, src, "git", "config", "user.email", "test@test.com")
	run(t, src, "git", "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(src, "README.md"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, src, "git", "add", ".")
	run(t, src, "git", "commit", "-m", "init")
	run(t, src, "git", "push", "-u", "origin", "main")

	// EnsureCached expects to clone from https://github.com/owner/repo.git,
	// but we can't do that in tests. Instead, test the "already cached" path.
	reposDir := filepath.Join(t.TempDir(), "repos")
	cacheDir := filepath.Join(reposDir, "testowner", "testrepo")

	// Pre-populate cache with a clone of the bare repo.
	run(t, "", "git", "clone", bare, cacheDir)

	got, err := EnsureCached(reposDir, "testowner", "testrepo", true)
	if err != nil {
		t.Fatalf("EnsureCached() error: %v", err)
	}
	if got != cacheDir {
		t.Fatalf("EnsureCached() = %q, want %q", got, cacheDir)
	}
}

func run(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v failed: %v\n%s", name, args, err, out)
	}
}
