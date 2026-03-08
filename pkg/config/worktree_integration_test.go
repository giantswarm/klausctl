package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// setupGitWorkspace creates a bare repo and a clone suitable for testing.
func setupGitWorkspace(t *testing.T) (bare, clone string) {
	t.Helper()

	bare = filepath.Join(t.TempDir(), "origin.git")
	gitRun(t, "", "init", "--bare", "--initial-branch=main", bare)

	clone = filepath.Join(t.TempDir(), "workspace")
	gitRun(t, "", "clone", bare, clone)
	gitRun(t, clone, "config", "user.email", "test@test.com")
	gitRun(t, clone, "config", "user.name", "Test")
	gitRun(t, clone, "checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(clone, "README.md"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, clone, "add", ".")
	gitRun(t, clone, "commit", "-m", "init")
	gitRun(t, clone, "push", "-u", "origin", "main")
	return bare, clone
}

func TestGenerateInstanceConfig_CreatesWorktreeForGitRepo(t *testing.T) {
	_, clone := setupGitWorkspace(t)

	base := t.TempDir()
	paths := &Paths{
		ConfigDir:        base,
		InstancesDir:     filepath.Join(base, "instances"),
		PluginsDir:       filepath.Join(base, "plugins"),
		PersonalitiesDir: filepath.Join(base, "personalities"),
	}

	cfg, err := GenerateInstanceConfig(paths, CreateOptions{
		Name:      "dev",
		Workspace: clone,
	})
	if err != nil {
		t.Fatalf("GenerateInstanceConfig() error: %v", err)
	}

	if cfg.WorktreePath == "" {
		t.Fatal("expected WorktreePath to be set for git workspace")
	}

	expectedPath := filepath.Join(base, "instances", "dev", "workspace")
	if cfg.WorktreePath != expectedPath {
		t.Fatalf("expected WorktreePath=%q, got %q", expectedPath, cfg.WorktreePath)
	}

	// Verify the worktree was actually created with the expected content.
	readme, err := os.ReadFile(filepath.Join(cfg.WorktreePath, "README.md"))
	if err != nil {
		t.Fatalf("reading README in worktree: %v", err)
	}
	if string(readme) != "hello" {
		t.Fatalf("unexpected README content: %q", readme)
	}

	// Original workspace should be stored in cfg.Workspace.
	if cfg.Workspace != clone {
		t.Fatalf("expected Workspace=%q (original repo), got %q", clone, cfg.Workspace)
	}

	// Verify the clone has a self-contained .git directory (not a file).
	info, err := os.Stat(filepath.Join(cfg.WorktreePath, ".git"))
	if err != nil {
		t.Fatalf("stat .git in clone: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("expected .git to be a directory in clone, not a file")
	}

	// Clean up the clone.
	if err := os.RemoveAll(cfg.WorktreePath); err != nil {
		t.Fatalf("removing clone: %v", err)
	}
}

func TestGenerateInstanceConfig_NoIsolateSkipsWorktree(t *testing.T) {
	_, clone := setupGitWorkspace(t)

	base := t.TempDir()
	paths := &Paths{
		ConfigDir:        base,
		InstancesDir:     filepath.Join(base, "instances"),
		PluginsDir:       filepath.Join(base, "plugins"),
		PersonalitiesDir: filepath.Join(base, "personalities"),
	}

	cfg, err := GenerateInstanceConfig(paths, CreateOptions{
		Name:      "dev",
		Workspace: clone,
		NoIsolate: true,
	})
	if err != nil {
		t.Fatalf("GenerateInstanceConfig() error: %v", err)
	}

	if cfg.WorktreePath != "" {
		t.Fatalf("expected WorktreePath to be empty with NoIsolate, got %q", cfg.WorktreePath)
	}
}

func TestGenerateInstanceConfig_NonGitWorkspaceSkipsWorktree(t *testing.T) {
	base := t.TempDir()
	workspace := filepath.Join(base, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}

	paths := &Paths{
		ConfigDir:        base,
		InstancesDir:     filepath.Join(base, "instances"),
		PluginsDir:       filepath.Join(base, "plugins"),
		PersonalitiesDir: filepath.Join(base, "personalities"),
	}

	cfg, err := GenerateInstanceConfig(paths, CreateOptions{
		Name:      "dev",
		Workspace: workspace,
	})
	if err != nil {
		t.Fatalf("GenerateInstanceConfig() error: %v", err)
	}

	if cfg.WorktreePath != "" {
		t.Fatalf("expected WorktreePath to be empty for non-git workspace, got %q", cfg.WorktreePath)
	}
}
