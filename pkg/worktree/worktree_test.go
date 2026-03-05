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
	run(t, clone, "git", "checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(clone, "README.md"), []byte("hello"), 0o644); err != nil {
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
	return clone
}

func run(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
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
	if branch != "main" {
		t.Fatalf("expected default branch 'main', got %q", branch)
	}
}

func TestCreateAndRemove(t *testing.T) {
	bare := initBareRepo(t)
	clone := cloneRepo(t, bare)

	wtPath := filepath.Join(t.TempDir(), "worktree")

	if err := Create(clone, wtPath); err != nil {
		t.Fatalf("Create() error: %v", err)
	}

	// Verify the worktree directory exists and has the README.
	readme := filepath.Join(wtPath, "README.md")
	data, err := os.ReadFile(readme)
	if err != nil {
		t.Fatalf("reading README in worktree: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("unexpected README content: %q", data)
	}

	// Verify it's a git worktree (has .git file, not directory).
	gitPath := filepath.Join(wtPath, ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		t.Fatalf("stat .git in worktree: %v", err)
	}
	if info.IsDir() {
		t.Fatal("expected .git to be a file in worktree, not a directory")
	}

	// Remove the worktree.
	if err := Remove(clone, wtPath); err != nil {
		t.Fatalf("Remove() error: %v", err)
	}

	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Fatalf("expected worktree directory to be removed, stat err: %v", err)
	}
}

func TestCreateMultipleWorktrees(t *testing.T) {
	bare := initBareRepo(t)
	clone := cloneRepo(t, bare)

	wt1 := filepath.Join(t.TempDir(), "wt1")
	wt2 := filepath.Join(t.TempDir(), "wt2")

	if err := Create(clone, wt1); err != nil {
		t.Fatalf("Create(wt1) error: %v", err)
	}
	if err := Create(clone, wt2); err != nil {
		t.Fatalf("Create(wt2) error: %v", err)
	}

	// Both should have the README.
	for _, wt := range []string{wt1, wt2} {
		if _, err := os.ReadFile(filepath.Join(wt, "README.md")); err != nil {
			t.Fatalf("missing README in %s: %v", wt, err)
		}
	}

	// Clean up both.
	if err := Remove(clone, wt1); err != nil {
		t.Fatalf("Remove(wt1) error: %v", err)
	}
	if err := Remove(clone, wt2); err != nil {
		t.Fatalf("Remove(wt2) error: %v", err)
	}
}

func TestConcurrentCreate(t *testing.T) {
	bare := initBareRepo(t)
	clone := cloneRepo(t, bare)

	const n = 5
	wtPaths := make([]string, n)
	for i := range wtPaths {
		wtPaths[i] = filepath.Join(t.TempDir(), fmt.Sprintf("wt%d", i))
	}

	// Create all worktrees concurrently.
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			errs[idx] = Create(clone, wtPaths[idx])
		}(i)
	}
	wg.Wait()

	// All creations must succeed.
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Create(wt%d) error: %v", i, err)
		}
	}

	// Every worktree must have the README and a valid git remote.
	for i, wt := range wtPaths {
		data, err := os.ReadFile(filepath.Join(wt, "README.md"))
		if err != nil {
			t.Fatalf("wt%d: missing README: %v", i, err)
		}
		if string(data) != "hello" {
			t.Fatalf("wt%d: unexpected README content: %q", i, data)
		}

		// Verify git remote is intact.
		cmd := exec.Command("git", "remote", "-v")
		cmd.Dir = wt
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("wt%d: git remote -v failed: %v\n%s", i, err, out)
		}
		if !strings.Contains(string(out), "origin") {
			t.Fatalf("wt%d: missing origin remote: %s", i, out)
		}
	}

	// Clean up all worktrees.
	for i, wt := range wtPaths {
		if err := Remove(clone, wt); err != nil {
			t.Errorf("Remove(wt%d) error: %v", i, err)
		}
	}
}

func TestLockRepo(t *testing.T) {
	bare := initBareRepo(t)
	clone := cloneRepo(t, bare)

	// Acquire lock.
	unlock, err := lockRepo(clone)
	if err != nil {
		t.Fatalf("lockRepo() error: %v", err)
	}

	// Verify lock file was created.
	lockPath := filepath.Join(clone, ".git", "klausctl.lock")
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("lock file not created: %v", err)
	}

	// Release lock.
	unlock()

	// Lock can be acquired again after release.
	unlock2, err := lockRepo(clone)
	if err != nil {
		t.Fatalf("lockRepo() after release error: %v", err)
	}
	unlock2()
}
