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
	assertSubcommandsRegistered(t, pluginCmd, []string{"validate", "pull", "list"})
}

func TestPluginCommandRegisteredOnRoot(t *testing.T) {
	assertCommandOnRoot(t, "plugin")
}

func TestValidatePluginDirValid(t *testing.T) {
	dir := t.TempDir()

	if err := os.MkdirAll(filepath.Join(dir, "skills", "k8s"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skills", "k8s", "SKILL.md"), []byte("# Skill"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := validatePluginDir(dir, io.Discard, "text"); err != nil {
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

	if err := validatePluginDir(dir, io.Discard, "text"); err != nil {
		t.Errorf("validatePluginDir() error = %v", err)
	}
}

func TestValidatePluginDirWithMCPConfig(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := validatePluginDir(dir, io.Discard, "text"); err != nil {
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
	testValidateDirNotExist(t, validatePluginDir)
}

func TestValidatePluginDirNotADirectory(t *testing.T) {
	testValidateDirNotADirectory(t, validatePluginDir)
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

	if !strings.Contains(buf.String(), "Valid plugin directory") {
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
	assertFlagRegistered(t, pluginValidateCmd, "output")
	assertFlagRegistered(t, pluginPullCmd, "output")
	assertFlagRegistered(t, pluginListCmd, "output")
	assertFlagRegistered(t, pluginListCmd, "remote")
}
