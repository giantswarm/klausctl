package cmd

import (
	"bytes"
	"encoding/json"
	"io"
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

	err := validatePluginDir(dir, io.Discard, "text")
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

	err := validatePluginDir(dir, io.Discard, "text")
	if err != nil {
		t.Errorf("validatePluginDir() error = %v", err)
	}
}

func TestValidatePluginDirWithMCPConfig(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	err := validatePluginDir(dir, io.Discard, "text")
	if err != nil {
		t.Errorf("validatePluginDir() error = %v", err)
	}
}

func TestValidatePluginDirEmpty(t *testing.T) {
	dir := t.TempDir()

	err := validatePluginDir(dir, io.Discard, "text")
	if err == nil {
		t.Fatal("expected error for empty directory")
	}
	if !strings.Contains(err.Error(), "no recognized plugin content") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidatePluginDirNotExist(t *testing.T) {
	err := validatePluginDir("/nonexistent/path", io.Discard, "text")
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

	err := validatePluginDir(f, io.Discard, "text")
	if err == nil {
		t.Fatal("expected error for file (not directory)")
	}
	if !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidatePluginDirTextOutput(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := validatePluginDir(dir, &buf, "text"); err != nil {
		t.Fatalf("validatePluginDir() error = %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Valid plugin directory") {
		t.Error("expected text output to contain 'Valid plugin directory'")
	}
}

func TestValidatePluginDirJSONOutput(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := validatePluginDir(dir, &buf, "json"); err != nil {
		t.Fatalf("validatePluginDir() error = %v", err)
	}

	var result pluginValidation
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v", err)
	}
	if !result.Valid {
		t.Error("expected valid=true")
	}
	if len(result.Found) != 2 {
		t.Errorf("expected 2 found items, got %d", len(result.Found))
	}
}

func TestPluginFlagsRegistered(t *testing.T) {
	if f := pluginValidateCmd.Flags().Lookup("output"); f == nil {
		t.Error("expected --output flag on validate")
	}
	if f := pluginPullCmd.Flags().Lookup("output"); f == nil {
		t.Error("expected --output flag on pull")
	}
	if f := pluginListCmd.Flags().Lookup("output"); f == nil {
		t.Error("expected --output flag on list")
	}
	if f := pluginListCmd.Flags().Lookup("remote"); f == nil {
		t.Error("expected --remote flag on list")
	}
}
