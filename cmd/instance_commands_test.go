package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestCreateFailsOnExplicitPortCollision(t *testing.T) {
	configHome := filepath.Join(t.TempDir(), "config-home")
	t.Setenv("XDG_CONFIG_HOME", configHome)

	workspace := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}

	conflictDir := filepath.Join(configHome, "klausctl", "instances", "other")
	if err := os.MkdirAll(conflictDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(conflictDir, "config.yaml"), []byte("workspace: /tmp\nport: 5050\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	createPersonality = ""
	createToolchain = ""
	createPlugins = nil
	createPort = 5050

	cmd := &cobra.Command{}
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := runCreate(cmd, []string{"dev", workspace})
	if err == nil {
		t.Fatal("expected port collision error")
	}
	if !strings.Contains(err.Error(), "already used") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestListJSONOutputIncludesContractFields(t *testing.T) {
	configHome := filepath.Join(t.TempDir(), "config-home")
	t.Setenv("XDG_CONFIG_HOME", configHome)

	instanceDir := filepath.Join(configHome, "klausctl", "instances", "dev")
	if err := os.MkdirAll(instanceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(instanceDir, "config.yaml"), []byte(
		"workspace: /tmp/dev\nport: 8181\ntoolchain: gsoci.azurecr.io/giantswarm/klaus-go:latest\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}

	listOutput = "json"
	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)

	if err := runList(cmd, nil); err != nil {
		t.Fatalf("runList() error = %v", err)
	}

	var entries []map[string]any
	if err := json.Unmarshal(out.Bytes(), &entries); err != nil {
		t.Fatalf("invalid JSON output: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	entry := entries[0]
	if entry["name"] != "dev" {
		t.Fatalf("unexpected name: %v", entry["name"])
	}
	if entry["status"] != "stopped" {
		t.Fatalf("unexpected status: %v", entry["status"])
	}
	if entry["toolchain"] != "go" {
		t.Fatalf("unexpected toolchain: %v", entry["toolchain"])
	}
	if entry["workspace"] != "/tmp/dev" {
		t.Fatalf("unexpected workspace: %v", entry["workspace"])
	}
}

func TestDeleteRemovesInstanceDirectoryWithYes(t *testing.T) {
	configHome := filepath.Join(t.TempDir(), "config-home")
	t.Setenv("XDG_CONFIG_HOME", configHome)

	instanceDir := filepath.Join(configHome, "klausctl", "instances", "dev")
	if err := os.MkdirAll(instanceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(instanceDir, "config.yaml"), []byte("workspace: /tmp/dev\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	deleteYes = true
	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)

	if err := runDelete(cmd, []string{"dev"}); err != nil {
		t.Fatalf("runDelete() error = %v", err)
	}

	if _, err := os.Stat(instanceDir); !os.IsNotExist(err) {
		t.Fatalf("expected instance directory to be deleted, stat err: %v", err)
	}
}

func TestResolveOptionalInstanceNameWarnsAboutDeprecatedImplicitDefault(t *testing.T) {
	var errOut bytes.Buffer

	name, err := resolveOptionalInstanceName(nil, "status", &errOut)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "default" {
		t.Fatalf("expected default instance name, got %q", name)
	}
	if !strings.Contains(errOut.String(), "omitting <name> is deprecated") {
		t.Fatalf("expected deprecation warning, got: %q", errOut.String())
	}
}
