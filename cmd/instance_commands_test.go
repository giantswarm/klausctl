package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/giantswarm/klausctl/pkg/config"
	runtimepkg "github.com/giantswarm/klausctl/pkg/runtime"
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

func TestRunStopRejectsNameWithAll(t *testing.T) {
	configHome := filepath.Join(t.TempDir(), "config-home")
	t.Setenv("XDG_CONFIG_HOME", configHome)

	stopAll = true
	t.Cleanup(func() { stopAll = false })

	cmd := &cobra.Command{}
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := runStop(cmd, []string{"dev"})
	if err == nil {
		t.Fatal("expected argument conflict error")
	}
	if !strings.Contains(err.Error(), "--all cannot be used with an instance name") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApplyWorkspaceOverride(t *testing.T) {
	cfg := &config.Config{Workspace: "/tmp/original"}

	applyWorkspaceOverride(cfg, "")
	if cfg.Workspace != "/tmp/original" {
		t.Fatalf("workspace should remain unchanged, got %q", cfg.Workspace)
	}

	applyWorkspaceOverride(cfg, "/tmp/override")
	if cfg.Workspace != "/tmp/override" {
		t.Fatalf("workspace should be overridden, got %q", cfg.Workspace)
	}
}

func TestStopAndRemoveContainerIfExistsRunning(t *testing.T) {
	rt := &fakeRuntime{status: "running"}
	if err := stopAndRemoveContainerIfExists(context.Background(), rt, "klausctl-dev"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rt.stopCalls != 1 {
		t.Fatalf("expected stop to be called once, got %d", rt.stopCalls)
	}
	if rt.removeCalls != 1 {
		t.Fatalf("expected remove to be called once, got %d", rt.removeCalls)
	}
}

func TestStopAndRemoveContainerIfExistsMissing(t *testing.T) {
	rt := &fakeRuntime{status: ""}
	if err := stopAndRemoveContainerIfExists(context.Background(), rt, "klausctl-dev"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rt.stopCalls != 0 {
		t.Fatalf("expected stop not to be called, got %d", rt.stopCalls)
	}
	if rt.removeCalls != 0 {
		t.Fatalf("expected remove not to be called, got %d", rt.removeCalls)
	}
}

type fakeRuntime struct {
	status      string
	stopCalls   int
	removeCalls int
}

func (f *fakeRuntime) Name() string { return "fake" }
func (f *fakeRuntime) Run(_ context.Context, _ runtimepkg.RunOptions) (string, error) {
	return "", nil
}
func (f *fakeRuntime) Stop(_ context.Context, _ string) error {
	f.stopCalls++
	return nil
}
func (f *fakeRuntime) Remove(_ context.Context, _ string) error {
	f.removeCalls++
	return nil
}
func (f *fakeRuntime) Status(_ context.Context, _ string) (string, error) {
	return f.status, nil
}
func (f *fakeRuntime) Inspect(_ context.Context, _ string) (*runtimepkg.ContainerInfo, error) {
	return &runtimepkg.ContainerInfo{StartedAt: time.Now()}, nil
}
func (f *fakeRuntime) Logs(_ context.Context, _ string, _ bool, _ int) error { return nil }
func (f *fakeRuntime) Pull(_ context.Context, _ string, _ io.Writer) error   { return nil }
func (f *fakeRuntime) Images(_ context.Context, _ string) ([]runtimepkg.ImageInfo, error) {
	return nil, nil
}
