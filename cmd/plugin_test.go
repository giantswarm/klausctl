package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPluginSubcommandsRegistered(t *testing.T) {
	subs := []string{"validate", "pull", "list"}
	for _, name := range subs {
		t.Run(name, func(t *testing.T) {
			for _, cmd := range pluginCmd.Commands() {
				if cmd.Name() == name {
					return
				}
			}
			t.Errorf("expected %q subcommand on plugin", name)
		})
	}
}

func TestPluginCommandRegisteredOnRoot(t *testing.T) {
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "plugin" {
			return
		}
	}
	t.Error("expected 'plugin' command to be registered on root")
}

func TestValidatePluginDirValid(t *testing.T) {
	dir := t.TempDir()

	if err := os.MkdirAll(filepath.Join(dir, "skills", "k8s"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skills", "k8s", "SKILL.md"), []byte("# Skill"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := validatePluginDir(dir)
	if err != nil {
		t.Errorf("validatePluginDir() error = %v", err)
	}
}

func TestValidatePluginDirWithAgents(t *testing.T) {
	dir := t.TempDir()

	if err := os.MkdirAll(filepath.Join(dir, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agents", "helper.md"), []byte("# Agent"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := validatePluginDir(dir)
	if err != nil {
		t.Errorf("validatePluginDir() error = %v", err)
	}
}

func TestValidatePluginDirWithMCPConfig(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	err := validatePluginDir(dir)
	if err != nil {
		t.Errorf("validatePluginDir() error = %v", err)
	}
}

func TestValidatePluginDirEmpty(t *testing.T) {
	dir := t.TempDir()

	err := validatePluginDir(dir)
	if err == nil {
		t.Fatal("expected error for empty directory")
	}
	if !strings.Contains(err.Error(), "no recognized plugin content") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidatePluginDirNotExist(t *testing.T) {
	err := validatePluginDir("/nonexistent/path")
	if err == nil {
		t.Fatal("expected error for nonexistent directory")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidatePluginDirNotADirectory(t *testing.T) {
	f := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(f, []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := validatePluginDir(f)
	if err == nil {
		t.Fatal("expected error for file (not directory)")
	}
	if !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestPluginListFlagsRegistered(t *testing.T) {
	if f := pluginListCmd.Flags().Lookup("output"); f == nil {
		t.Error("expected --output flag")
	}
	if f := pluginListCmd.Flags().Lookup("remote"); f == nil {
		t.Error("expected --remote flag")
	}
}
