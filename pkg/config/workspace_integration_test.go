package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateInstanceConfig_RepoIdentifier(t *testing.T) {
	// Set up a bare repo and a clone to serve as the cached workspace.
	bare, _ := setupGitWorkspace(t)

	base := t.TempDir()
	reposDir := filepath.Join(base, "repos")
	cacheDir := filepath.Join(reposDir, "testorg", "testrepo")

	// Pre-populate the cache directory with a clone of the bare repo.
	gitRun(t, "", "clone", bare, cacheDir)

	// Write a workspace registry that allows the org.
	wsFile := filepath.Join(base, "workspaces.yaml")
	if err := os.WriteFile(wsFile, []byte("orgs:\n  - testorg\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	paths := &Paths{
		ConfigDir:        base,
		InstancesDir:     filepath.Join(base, "instances"),
		PluginsDir:       filepath.Join(base, "plugins"),
		PersonalitiesDir: filepath.Join(base, "personalities"),
		WorkspacesFile:   wsFile,
		ReposDir:         reposDir,
	}

	cfg, err := GenerateInstanceConfig(paths, CreateOptions{
		Name:      "dev",
		Workspace: "testorg/testrepo",
		NoFetch:   true,
	})
	if err != nil {
		t.Fatalf("GenerateInstanceConfig() error: %v", err)
	}

	// cfg.Workspace should preserve the original owner/repo identifier.
	if cfg.Workspace != "testorg/testrepo" {
		t.Fatalf("expected Workspace=%q, got %q", "testorg/testrepo", cfg.Workspace)
	}

	// A worktree should have been created from the cached clone.
	if cfg.WorktreePath == "" {
		t.Fatal("expected WorktreePath to be set for repo identifier workspace")
	}

	expectedWT := filepath.Join(base, "instances", "dev", "workspace")
	if cfg.WorktreePath != expectedWT {
		t.Fatalf("expected WorktreePath=%q, got %q", expectedWT, cfg.WorktreePath)
	}

	// Verify the worktree has the expected content.
	readme, err := os.ReadFile(filepath.Join(cfg.WorktreePath, "README.md"))
	if err != nil {
		t.Fatalf("reading README in worktree: %v", err)
	}
	if string(readme) != "hello" {
		t.Fatalf("unexpected README content: %q", readme)
	}

	// Clean up.
	if err := os.RemoveAll(cfg.WorktreePath); err != nil {
		t.Fatalf("removing worktree: %v", err)
	}
}

func TestGenerateInstanceConfig_RepoIdentifierNotRegistered(t *testing.T) {
	base := t.TempDir()

	// Write a workspace registry that only allows "allowed-org".
	wsFile := filepath.Join(base, "workspaces.yaml")
	if err := os.WriteFile(wsFile, []byte("orgs:\n  - allowed-org\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	paths := &Paths{
		ConfigDir:        base,
		InstancesDir:     filepath.Join(base, "instances"),
		PluginsDir:       filepath.Join(base, "plugins"),
		PersonalitiesDir: filepath.Join(base, "personalities"),
		WorkspacesFile:   wsFile,
		ReposDir:         filepath.Join(base, "repos"),
	}

	_, err := GenerateInstanceConfig(paths, CreateOptions{
		Name:      "dev",
		Workspace: "unknown-org/somerepo",
	})
	if err == nil {
		t.Fatal("expected error for unregistered repo identifier")
	}
	if !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("unexpected error message: %v", err)
	}
}
