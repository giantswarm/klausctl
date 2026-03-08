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

func TestRemoveFallsBackToForce(t *testing.T) {
	bare := initBareRepo(t)
	clone := cloneRepo(t, bare)

	wtPath := filepath.Join(t.TempDir(), "worktree")

	if err := Create(clone, wtPath); err != nil {
		t.Fatalf("Create() error: %v", err)
	}

	// Modify a tracked file so `git worktree remove` (without --force) fails
	// due to uncommitted changes.
	if err := os.WriteFile(filepath.Join(wtPath, "README.md"), []byte("modified"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Remove should succeed via the --force fallback.
	if err := Remove(clone, wtPath); err != nil {
		t.Fatalf("Remove() error: %v", err)
	}

	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Fatalf("expected worktree directory to be removed, stat err: %v", err)
	}
}

func TestRemoveFallsBackToManualCleanup(t *testing.T) {
	bare := initBareRepo(t)
	clone := cloneRepo(t, bare)

	wtPath := filepath.Join(t.TempDir(), "worktree")

	if err := Create(clone, wtPath); err != nil {
		t.Fatalf("Create() error: %v", err)
	}

	// Corrupt the worktree by replacing the .git file with garbage.
	// This makes both `git worktree remove` and `git worktree remove --force`
	// fail because git cannot validate the worktree.
	gitFile := filepath.Join(wtPath, ".git")
	if err := os.Remove(gitFile); err != nil {
		t.Fatalf("removing .git file: %v", err)
	}
	if err := os.WriteFile(gitFile, []byte("garbage"), 0o644); err != nil {
		t.Fatalf("writing corrupt .git file: %v", err)
	}

	// Remove should succeed via the manual removal + prune fallback.
	if err := Remove(clone, wtPath); err != nil {
		t.Fatalf("Remove() error: %v", err)
	}

	// Verify the worktree directory is gone.
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Fatalf("expected worktree directory to be removed, stat err: %v", err)
	}

	// Verify git no longer lists the worktree.
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	cmd.Dir = clone
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git worktree list: %v", err)
	}
	if strings.Contains(string(out), wtPath) {
		t.Fatalf("expected worktree to be pruned from git, but still listed:\n%s", out)
	}
}

func TestRemoveAlreadyDeletedDirectory(t *testing.T) {
	bare := initBareRepo(t)
	clone := cloneRepo(t, bare)

	wtPath := filepath.Join(t.TempDir(), "worktree")

	if err := Create(clone, wtPath); err != nil {
		t.Fatalf("Create() error: %v", err)
	}

	// Manually delete the worktree directory to simulate partial cleanup.
	if err := os.RemoveAll(wtPath); err != nil {
		t.Fatal(err)
	}

	// Remove should still succeed (manual fallback prunes the stale reference).
	if err := Remove(clone, wtPath); err != nil {
		t.Fatalf("Remove() error: %v", err)
	}

	// Verify git no longer lists the worktree.
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	cmd.Dir = clone
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git worktree list: %v", err)
	}
	if strings.Contains(string(out), wtPath) {
		t.Fatalf("expected stale worktree to be pruned, but still listed:\n%s", out)
	}
}

func TestCreateAutoRecoveryFromStaleRegistration(t *testing.T) {
	bare := initBareRepo(t)
	clone := cloneRepo(t, bare)

	wtPath := filepath.Join(t.TempDir(), "worktree")

	// Create a worktree normally.
	if err := Create(clone, wtPath); err != nil {
		t.Fatalf("Create() error: %v", err)
	}

	// Simulate a stale registration: delete the worktree directory but leave
	// the .git/worktrees entry intact (as happens on SIGKILL or power loss).
	if err := os.RemoveAll(wtPath); err != nil {
		t.Fatalf("removing worktree dir: %v", err)
	}
	// Verify the stale registration exists.
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	cmd.Dir = clone
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git worktree list: %v", err)
	}
	if !strings.Contains(string(out), wtPath) {
		t.Fatal("expected stale worktree registration to still exist")
	}

	// Create again at the same path — should auto-recover via prune + retry.
	if err := Create(clone, wtPath); err != nil {
		t.Fatalf("Create() with stale registration error: %v", err)
	}

	// Verify the new worktree is functional.
	data, err := os.ReadFile(filepath.Join(wtPath, "README.md"))
	if err != nil {
		t.Fatalf("reading README in recovered worktree: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("unexpected README content: %q", data)
	}

	// Clean up.
	if err := Remove(clone, wtPath); err != nil {
		t.Fatalf("Remove() error: %v", err)
	}
}

func TestCreateDeleteCreateCycle(t *testing.T) {
	bare := initBareRepo(t)
	clone := cloneRepo(t, bare)

	wtPath := filepath.Join(t.TempDir(), "worktree")

	// Run three full create-delete cycles on the same path to verify no
	// stale registrations accumulate.
	for i := 0; i < 3; i++ {
		if err := Create(clone, wtPath); err != nil {
			t.Fatalf("cycle %d: Create() error: %v", i, err)
		}

		data, err := os.ReadFile(filepath.Join(wtPath, "README.md"))
		if err != nil {
			t.Fatalf("cycle %d: reading README: %v", i, err)
		}
		if string(data) != "hello" {
			t.Fatalf("cycle %d: unexpected README content: %q", i, data)
		}

		if err := Remove(clone, wtPath); err != nil {
			t.Fatalf("cycle %d: Remove() error: %v", i, err)
		}
	}
}

func TestCreateNonStaleErrorFailsImmediately(t *testing.T) {
	bare := initBareRepo(t)
	clone := cloneRepo(t, bare)

	wtPath := filepath.Join(t.TempDir(), "worktree")

	// Create a live worktree at this path.
	if err := Create(clone, wtPath); err != nil {
		t.Fatalf("initial Create() error: %v", err)
	}

	// Attempt to create again at the same path while the worktree is still
	// live. Git produces "already exists" rather than "already registered
	// worktree", so the prune+retry recovery must NOT be triggered.
	wtPath2 := filepath.Join(t.TempDir(), "worktree2")
	err := Create(clone, wtPath2)
	// This should succeed (different path). Now test the actual guard: try
	// creating at wtPath again while it is live.
	if err != nil {
		t.Fatalf("Create(wtPath2) error: %v", err)
	}
	_ = Remove(clone, wtPath2)

	// The real test: create at the same path as a live worktree. The error
	// from git will NOT contain "already registered worktree" so prune+retry
	// must not trigger.
	err = Create(clone, wtPath)
	if err == nil {
		// Some git versions may succeed by creating at an occupied path;
		// in that case the guard is moot. Clean up and skip.
		_ = Remove(clone, wtPath)
		t.Skip("git worktree add succeeded on occupied path; cannot test guard")
	}
	if strings.Contains(err.Error(), "retry after prune") {
		t.Fatalf("non-stale error incorrectly triggered prune+retry: %v", err)
	}
	if !strings.Contains(err.Error(), "git worktree add") {
		t.Fatalf("unexpected error format: %v", err)
	}

	// Clean up.
	if err := Remove(clone, wtPath); err != nil {
		t.Fatalf("Remove() error: %v", err)
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
