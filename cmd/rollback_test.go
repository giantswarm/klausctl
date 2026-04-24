package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	runtimepkg "github.com/giantswarm/klausctl/pkg/runtime"
)

// rollbackRuntime extends fakeRuntime with configurable errors for testing
// rollback behavior in the create and start paths.
type rollbackRuntime struct {
	runErr    error
	runID     string
	pullErr   error
	removeErr error

	removeCalls []string
	stopCalls   []string
}

func (r *rollbackRuntime) Name() string { return "fake" }
func (r *rollbackRuntime) Run(_ context.Context, opts runtimepkg.RunOptions) (string, error) {
	if r.runErr != nil {
		return "", r.runErr
	}
	id := r.runID
	if id == "" {
		id = "fake-container-id"
	}
	return id, nil
}
func (r *rollbackRuntime) Stop(_ context.Context, name string) error {
	r.stopCalls = append(r.stopCalls, name)
	return nil
}
func (r *rollbackRuntime) Remove(_ context.Context, name string) error {
	r.removeCalls = append(r.removeCalls, name)
	return r.removeErr
}
func (r *rollbackRuntime) Status(_ context.Context, _ string) (string, error) {
	return "", nil
}
func (r *rollbackRuntime) Inspect(_ context.Context, _ string) (*runtimepkg.ContainerInfo, error) {
	return nil, fmt.Errorf("not found")
}
func (r *rollbackRuntime) Logs(_ context.Context, _ string, _ bool, _ int) error { return nil }
func (r *rollbackRuntime) LogsCapture(_ context.Context, _ string, _ int) (string, error) {
	return "", nil
}
func (r *rollbackRuntime) Pull(_ context.Context, _ string, _ io.Writer) error {
	return r.pullErr
}
func (r *rollbackRuntime) Images(_ context.Context, _ string) ([]runtimepkg.ImageInfo, error) {
	return nil, nil
}

// setupCreateEnv prepares a temp config home and workspace directory and
// resets global create flags. Returns (configHome, workspace).
func setupCreateEnv(t *testing.T) (string, string) {
	t.Helper()
	configHome := filepath.Join(t.TempDir(), "config-home")
	t.Setenv("XDG_CONFIG_HOME", configHome)

	workspace := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(workspace, 0o750); err != nil {
		t.Fatal(err)
	}

	// Reset global flags to defaults.
	createPersonality = ""
	createToolchain = ""
	createPlugins = nil
	createPort = 0
	createEnv = nil
	createEnvForward = nil
	createSecretEnv = nil
	createSecretFile = nil
	createMcpServer = nil
	createPermMode = ""
	createModel = ""
	createSystemPrompt = ""
	createMaxBudget = 0
	createSource = ""
	createNoIsolate = true // skip worktree to avoid needing real git repos
	createGitAuthor = ""
	createGitCredHelper = ""
	createGitHTTPSInsteadOf = false

	return configHome, workspace
}

// overrideRuntime installs a rollbackRuntime as the runtime factory and
// registers a cleanup to restore the original. Returns the fake runtime.
func overrideRuntime(t *testing.T, rt *rollbackRuntime) {
	t.Helper()
	orig := newRuntime
	newRuntime = func(_ string) (runtimepkg.Runtime, error) { return rt, nil }
	t.Cleanup(func() { newRuntime = orig })
}

// TestCreateRollbackRemovesContainerOnRunFailure verifies that when docker run
// fails (e.g. port conflict), the potentially-created container is removed.
func TestCreateRollbackRemovesContainerOnRunFailure(t *testing.T) {
	configHome, workspace := setupCreateEnv(t)

	rt := &rollbackRuntime{
		runErr: fmt.Errorf("port 8080 already in use"),
	}
	overrideRuntime(t, rt)

	cmd := &cobra.Command{}
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := runCreate(cmd, []string{"rollback-test", workspace})
	if err == nil {
		t.Fatal("expected error from docker run failure")
	}

	// The container should have been removed during rollback.
	expectedContainer := "klausctl-rollback-test"
	found := false
	for _, name := range rt.removeCalls {
		if name == expectedContainer {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected Remove call for %q, got: %v", expectedContainer, rt.removeCalls)
	}

	// The instance directory should have been cleaned up.
	instanceDir := filepath.Join(configHome, "klausctl", "instances", "rollback-test")
	if _, err := os.Stat(instanceDir); !os.IsNotExist(err) {
		t.Fatalf("expected instance directory to be removed, stat err: %v", err)
	}
}

// TestCreateRollbackRemovesContainerOnImagePullFailure verifies that when the
// image pull fails and no cached image exists, no orphaned container is left
// and the instance directory is cleaned up.
func TestCreateRollbackRemovesContainerOnImagePullFailure(t *testing.T) {
	configHome, workspace := setupCreateEnv(t)

	rt := &rollbackRuntime{
		pullErr: fmt.Errorf("image not found"),
	}
	overrideRuntime(t, rt)

	cmd := &cobra.Command{}
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := runCreate(cmd, []string{"pull-fail-test", workspace})
	if err == nil {
		t.Fatal("expected error from image pull failure")
	}

	// Pull failure happens before container creation, so no Remove calls
	// should have been made.
	if len(rt.removeCalls) != 0 {
		t.Fatalf("expected no Remove calls for pull failure, got: %v", rt.removeCalls)
	}

	// The instance directory should have been cleaned up.
	instanceDir := filepath.Join(configHome, "klausctl", "instances", "pull-fail-test")
	if _, err := os.Stat(instanceDir); !os.IsNotExist(err) {
		t.Fatalf("expected instance directory to be removed, stat err: %v", err)
	}
}

// TestCreateRollbackCleansUpInstanceDir verifies that if startInstance fails
// for any reason, the instance directory created by runCreate is removed so
// a retry of `klausctl create` with the same name succeeds.
func TestCreateRollbackCleansUpInstanceDir(t *testing.T) {
	configHome, workspace := setupCreateEnv(t)

	rt := &rollbackRuntime{
		runErr: fmt.Errorf("simulated failure"),
	}
	overrideRuntime(t, rt)

	cmd := &cobra.Command{}
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	// First create attempt fails.
	err := runCreate(cmd, []string{"retry-test", workspace})
	if err == nil {
		t.Fatal("expected error from simulated failure")
	}

	// Instance directory should be cleaned up.
	instanceDir := filepath.Join(configHome, "klausctl", "instances", "retry-test")
	if _, err := os.Stat(instanceDir); !os.IsNotExist(err) {
		t.Fatalf("expected instance directory to be removed after failed create, stat err: %v", err)
	}

	// A retry should not fail with "already exists".
	rt.runErr = nil
	err = runCreate(cmd, []string{"retry-test", workspace})
	// The retry may still fail (e.g., no real Docker), but should NOT fail
	// with "instance already exists".
	if err != nil && strings.Contains(err.Error(), "already exists") {
		t.Fatalf("retry should not fail with 'already exists': %v", err)
	}
}

// TestStartInstanceRollbackRemovesContainerOnSaveFailure verifies that if
// instance state cannot be saved after the container is started, the
// container is rolled back.
func TestStartInstanceRollbackRemovesContainerOnSaveFailure(t *testing.T) {
	configHome := filepath.Join(t.TempDir(), "config-home")
	t.Setenv("XDG_CONFIG_HOME", configHome)

	workspace := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(workspace, 0o750); err != nil {
		t.Fatal(err)
	}

	// Set up instance directory with config.
	instanceDir := filepath.Join(configHome, "klausctl", "instances", "save-fail")
	if err := os.MkdirAll(instanceDir, 0o750); err != nil {
		t.Fatal(err)
	}
	configContent := fmt.Sprintf("workspace: %s\nport: 9999\ntoolchain: fake-image:latest\n", workspace)
	if err := os.WriteFile(filepath.Join(instanceDir, "config.yaml"), []byte(configContent), 0o600); err != nil {
		t.Fatal(err)
	}

	rt := &rollbackRuntime{
		runID: "container-abc123",
	}
	overrideRuntime(t, rt)

	// Make the instance file path a directory so that writing to it fails.
	instanceFile := filepath.Join(instanceDir, "instance.json")
	if err := os.MkdirAll(instanceFile, 0o750); err != nil {
		t.Fatal(err)
	}

	cmd := &cobra.Command{}
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := startInstance(cmd, "save-fail", "", "")
	if err == nil {
		t.Fatal("expected error from instance state save failure")
	}

	// The container should have been removed during rollback.
	expectedContainer := "klausctl-save-fail"
	found := false
	for _, name := range rt.removeCalls {
		if name == expectedContainer {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected Remove call for %q, got: %v", expectedContainer, rt.removeCalls)
	}
}
