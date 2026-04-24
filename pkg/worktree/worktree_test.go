package worktree

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// initBareRepo creates a bare repo with one commit and returns its path.
func initBareRepo(t *testing.T) string {
	t.Helper()

	bare := filepath.Join(t.TempDir(), "origin.git")
	run(t, "", "git", "init", "--bare", "--initial-branch=main", bare)

	// Create a clone, add a commit, push.
	clone := filepath.Join(t.TempDir(), "clone")
	run(t, "", "git", "clone", bare, clone)
	run(t, clone, "git", "config", "user.email", "test@test.com")
	run(t, clone, "git", "config", "user.name", "Test")
	run(t, clone, "git", "config", "commit.gpgsign", "false")
	run(t, clone, "git", "config", "tag.gpgsign", "false")
	run(t, clone, "git", "checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(clone, "README.md"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	run(t, clone, "git", "add", ".")
	run(t, clone, "git", "commit", "-m", "init")
	run(t, clone, "git", "push", "-u", "origin", "main")

	return bare
}

// cloneRepo clones the bare repo and returns the clone path.
func cloneRepo(t *testing.T, bare string) string {
	t.Helper()
	clone := filepath.Join(t.TempDir(), "workspace")
	run(t, "", "git", "clone", bare, clone)
	run(t, clone, "git", "config", "user.email", "test@test.com")
	run(t, clone, "git", "config", "user.name", "Test")
	run(t, clone, "git", "config", "commit.gpgsign", "false")
	run(t, clone, "git", "config", "tag.gpgsign", "false")
	return clone
}

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

func TestIsGitRepo(t *testing.T) {
	bare := initBareRepo(t)
	clone := cloneRepo(t, bare)

	if !IsGitRepo(clone) {
		t.Fatal("expected cloned repo to be detected as git repo")
	}

	nonGit := t.TempDir()
	if IsGitRepo(nonGit) {
		t.Fatal("expected non-git directory to not be detected as git repo")
	}
}

func TestDefaultBranch(t *testing.T) {
	bare := initBareRepo(t)
	clone := cloneRepo(t, bare)

	branch, err := DefaultBranch(clone)
	if err != nil {
		t.Fatalf("DefaultBranch() error: %v", err)
	}
	if branch != "main" { //nolint:goconst
		t.Fatalf("expected default branch 'main', got %q", branch)
	}
}

func TestCreateAndRemove(t *testing.T) {
	bare := initBareRepo(t)
	clone := cloneRepo(t, bare)

	clonedPath := filepath.Join(t.TempDir(), "instance-workspace")

	if err := Create(clone, clonedPath); err != nil {
		t.Fatalf("Create() error: %v", err)
	}

	// Verify the clone directory exists and has the README.
	readme := filepath.Join(clonedPath, "README.md")
	data, err := os.ReadFile(readme) // #nosec G304 -- user-supplied or trusted local path; not exposed to untrusted input
	if err != nil {
		t.Fatalf("reading README in clone: %v", err)
	}
	if string(data) != "hello" { //nolint:goconst
		t.Fatalf("unexpected README content: %q", data)
	}

	// Verify it's a full clone (has .git directory, not a file).
	gitPath := filepath.Join(clonedPath, ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		t.Fatalf("stat .git in clone: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("expected .git to be a directory in clone, not a file")
	}

	// Verify the origin remote points to the upstream (bare repo), not the local clone.
	cmd := exec.Command("git", "remote", "get-url", "origin")
	cmd.Dir = clonedPath
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git remote get-url origin: %v", err)
	}
	originURL := strings.TrimSpace(string(out))
	if originURL != bare {
		t.Fatalf("expected origin URL %q, got %q", bare, originURL)
	}

	// Remove the clone.
	if err := Remove(clone, clonedPath); err != nil {
		t.Fatalf("Remove() error: %v", err)
	}

	if _, err := os.Stat(clonedPath); !os.IsNotExist(err) {
		t.Fatalf("expected clone directory to be removed, stat err: %v", err)
	}
}

func TestCreateProducesSelfContainedGitDir(t *testing.T) {
	bare := initBareRepo(t)
	clone := cloneRepo(t, bare)

	clonedPath := filepath.Join(t.TempDir(), "instance-workspace")

	if err := Create(clone, clonedPath); err != nil {
		t.Fatalf("Create() error: %v", err)
	}

	// The key invariant: git operations work in the clone without access to
	// the source repo. Simulate container isolation by verifying git commands
	// succeed using only the clone's .git directory.
	for _, args := range [][]string{
		{"rev-parse", "--is-inside-work-tree"},
		{"log", "--oneline", "-1"},
		{"remote", "-v"},
		{"status"},
	} {
		cmd := exec.Command("git", args...) // #nosec G204 -- container runtime CLI invocation with controlled args
		cmd.Dir = clonedPath
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s failed in clone: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
}

func TestCreateMultipleClones(t *testing.T) {
	bare := initBareRepo(t)
	clone := cloneRepo(t, bare)

	c1 := filepath.Join(t.TempDir(), "c1")
	c2 := filepath.Join(t.TempDir(), "c2")

	if err := Create(clone, c1); err != nil {
		t.Fatalf("Create(c1) error: %v", err)
	}
	if err := Create(clone, c2); err != nil {
		t.Fatalf("Create(c2) error: %v", err)
	}

	// Both should have the README.
	for _, c := range []string{c1, c2} {
		if _, err := os.ReadFile(filepath.Join(c, "README.md")); err != nil { // #nosec G304 -- user-supplied or trusted local path; not exposed to untrusted input
			t.Fatalf("missing README in %s: %v", c, err)
		}
	}

	// Clean up both.
	if err := Remove(clone, c1); err != nil {
		t.Fatalf("Remove(c1) error: %v", err)
	}
	if err := Remove(clone, c2); err != nil {
		t.Fatalf("Remove(c2) error: %v", err)
	}
}

func TestConcurrentCreate(t *testing.T) {
	bare := initBareRepo(t)
	clone := cloneRepo(t, bare)

	const n = 5
	clonePaths := make([]string, n)
	for i := range clonePaths {
		clonePaths[i] = filepath.Join(t.TempDir(), fmt.Sprintf("c%d", i))
	}

	// Create all clones concurrently.
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			errs[idx] = Create(clone, clonePaths[idx])
		}(i)
	}
	wg.Wait()

	// All creations must succeed.
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Create(c%d) error: %v", i, err)
		}
	}

	// Every clone must have the README and a valid git remote pointing upstream.
	for i, c := range clonePaths {
		data, err := os.ReadFile(filepath.Join(c, "README.md")) // #nosec G304 -- user-supplied or trusted local path; not exposed to untrusted input
		if err != nil {
			t.Fatalf("c%d: missing README: %v", i, err)
		}
		if string(data) != "hello" {
			t.Fatalf("c%d: unexpected README content: %q", i, data)
		}

		// Verify git remote points to upstream.
		cmd := exec.Command("git", "remote", "get-url", "origin")
		cmd.Dir = c
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("c%d: git remote get-url failed: %v\n%s", i, err, out)
		}
		url := strings.TrimSpace(string(out))
		if url != bare {
			t.Fatalf("c%d: expected origin URL %q, got %q", i, bare, url)
		}
	}

	// Clean up all clones.
	for i, c := range clonePaths {
		if err := Remove(clone, c); err != nil {
			t.Errorf("Remove(c%d) error: %v", i, err)
		}
	}
}

func TestCreateDeleteCreateCycle(t *testing.T) {
	bare := initBareRepo(t)
	clone := cloneRepo(t, bare)

	clonedPath := filepath.Join(t.TempDir(), "instance-workspace")

	// Run three full create-delete cycles on the same path.
	for i := 0; i < 3; i++ {
		if err := Create(clone, clonedPath); err != nil {
			t.Fatalf("cycle %d: Create() error: %v", i, err)
		}

		data, err := os.ReadFile(filepath.Join(clonedPath, "README.md")) // #nosec G304 -- user-supplied or trusted local path; not exposed to untrusted input
		if err != nil {
			t.Fatalf("cycle %d: reading README: %v", i, err)
		}
		if string(data) != "hello" {
			t.Fatalf("cycle %d: unexpected README content: %q", i, data)
		}

		if err := Remove(clone, clonedPath); err != nil {
			t.Fatalf("cycle %d: Remove() error: %v", i, err)
		}
	}
}

func TestRemoveAlreadyDeletedDirectory(t *testing.T) {
	bare := initBareRepo(t)
	clone := cloneRepo(t, bare)

	clonedPath := filepath.Join(t.TempDir(), "instance-workspace")

	if err := Create(clone, clonedPath); err != nil {
		t.Fatalf("Create() error: %v", err)
	}

	// Manually delete the directory to simulate partial cleanup.
	if err := os.RemoveAll(clonedPath); err != nil {
		t.Fatal(err)
	}

	// Remove should succeed (os.RemoveAll on non-existent path returns nil).
	if err := Remove(clone, clonedPath); err != nil {
		t.Fatalf("Remove() error: %v", err)
	}
}

func TestCreateNoFetch(t *testing.T) {
	bare := initBareRepo(t)
	clone := cloneRepo(t, bare)

	// Push a new commit to origin that the clone doesn't have locally.
	tmpClone := filepath.Join(t.TempDir(), "pusher")
	run(t, "", "git", "clone", bare, tmpClone)
	run(t, tmpClone, "git", "config", "user.email", "test@test.com")
	run(t, tmpClone, "git", "config", "user.name", "Test")
	run(t, tmpClone, "git", "config", "commit.gpgsign", "false")
	run(t, tmpClone, "git", "config", "tag.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(tmpClone, "new.txt"), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	run(t, tmpClone, "git", "add", ".")
	run(t, tmpClone, "git", "commit", "-m", "new file")
	run(t, tmpClone, "git", "push", "origin", "main")

	// Create with NoFetch: the new file should NOT appear because we skipped fetch.
	clonedPath := filepath.Join(t.TempDir(), "instance-workspace")
	opts := CreateOptions{NoFetch: true}
	if err := Create(clone, clonedPath, opts); err != nil {
		t.Fatalf("Create() with NoFetch error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(clonedPath, "new.txt")); !os.IsNotExist(err) {
		t.Fatal("expected new.txt to be absent when NoFetch is set")
	}

	// The original README should still be present.
	if _, err := os.ReadFile(filepath.Join(clonedPath, "README.md")); err != nil { // #nosec G304 -- user-supplied or trusted local path; not exposed to untrusted input
		t.Fatalf("missing README in clone: %v", err)
	}
}

func TestCreateFetchUpdatesRemoteRefs(t *testing.T) {
	bare := initBareRepo(t)
	clone := cloneRepo(t, bare)

	// Delete the symbolic-ref so DefaultBranch must use ls-remote,
	// which consults the fetch URL. Verify that Create still works
	// after fetching (i.e., origin is reachable and refs are current).
	run(t, clone, "git", "remote", "set-head", "origin", "--delete")

	clonedPath := filepath.Join(t.TempDir(), "instance-workspace")
	if err := Create(clone, clonedPath); err != nil {
		t.Fatalf("Create() error: %v", err)
	}

	// Verify the README is present and git operations work.
	if _, err := os.ReadFile(filepath.Join(clonedPath, "README.md")); err != nil { // #nosec G304 -- user-supplied or trusted local path; not exposed to untrusted input
		t.Fatalf("missing README in clone: %v", err)
	}

	cmd := exec.Command("git", "log", "--oneline", "-1")
	cmd.Dir = clonedPath
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git log failed in clone: %v\n%s", err, out)
	}
}

func TestCreateFetchFailureWarnsButContinues(t *testing.T) {
	bare := initBareRepo(t)
	clone := cloneRepo(t, bare)

	// Set origin to an unreachable URL so git fetch fails. The clone step
	// uses the local repoDir path (not the remote URL), so it succeeds.
	// upstreamURL() returns the configured URL string without connecting.
	run(t, clone, "git", "remote", "set-url", "origin", "https://invalid.example.com/repo.git")

	clonedPath := filepath.Join(t.TempDir(), "instance-workspace")
	var warnings strings.Builder
	opts := CreateOptions{Warnings: &warnings}

	err := Create(clone, clonedPath, opts)
	if err != nil {
		t.Fatalf("Create() error (fetch failure should be non-fatal): %v", err)
	}

	if !strings.Contains(warnings.String(), "warning: git fetch origin failed") {
		t.Fatalf("expected fetch warning, got: %q", warnings.String())
	}

	// The clone should still have the README from the original state.
	if _, err := os.ReadFile(filepath.Join(clonedPath, "README.md")); err != nil { // #nosec G304 -- user-supplied or trusted local path; not exposed to untrusted input
		t.Fatalf("missing README in clone: %v", err)
	}
}

func TestRemoveWithModifiedFiles(t *testing.T) {
	bare := initBareRepo(t)
	clone := cloneRepo(t, bare)

	clonedPath := filepath.Join(t.TempDir(), "instance-workspace")

	if err := Create(clone, clonedPath); err != nil {
		t.Fatalf("Create() error: %v", err)
	}

	// Modify a tracked file.
	if err := os.WriteFile(filepath.Join(clonedPath, "README.md"), []byte("modified"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Remove should succeed unconditionally (just deletes the directory).
	if err := Remove(clone, clonedPath); err != nil {
		t.Fatalf("Remove() error: %v", err)
	}

	if _, err := os.Stat(clonedPath); !os.IsNotExist(err) {
		t.Fatalf("expected clone directory to be removed, stat err: %v", err)
	}
}
