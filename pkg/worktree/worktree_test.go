package worktree

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
