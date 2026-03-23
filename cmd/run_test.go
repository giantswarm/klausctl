package cmd

import (
	"context"
	"testing"
	"time"

	"github.com/giantswarm/klausctl/pkg/mcpclient"
)

func TestRunCommandHasAllCreateFlags(t *testing.T) {
	createFlags := []string{
		"personality",
		"toolchain",
		"plugin",
		"port",
		"env",
		"env-forward",
		"permission-mode",
		"model",
		"system-prompt",
		"max-budget",
		"secret-env",
		"secret-file",
		"mcpserver",
		"source",
		"persistent-mode",
		"no-isolate",
		"git-author",
		"git-credential-helper",
		"git-https-instead-of-ssh",
		"yes",
		"force",
		"generate-suffix",
	}

	for _, flag := range createFlags {
		assertFlagRegistered(t, runCmd, flag)
	}
}

func TestRunCommandHasPromptFlags(t *testing.T) {
	assertFlagRegistered(t, runCmd, "message")
	assertFlagRegistered(t, runCmd, "blocking")
	assertFlagRegistered(t, runCmd, "output")
}

func TestRunCommandMessageIsRequired(t *testing.T) {
	f := runCmd.Flags().Lookup("message")
	if f == nil {
		t.Fatal("message flag not found")
	}
	// Cobra marks required flags via annotations.
	annotations := f.Annotations
	if _, ok := annotations["cobra_annotation_bash_completion_one_required_flag"]; !ok {
		t.Error("expected message flag to be marked as required")
	}
}

func TestRunCommandMessageShorthand(t *testing.T) {
	f := runCmd.Flags().ShorthandLookup("m")
	if f == nil {
		t.Fatal("expected -m shorthand for --message")
	}
	if f.Name != "message" {
		t.Fatalf("expected -m to map to message, got %q", f.Name)
	}
}

func TestWaitForMCPReadyCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	client := mcpclient.New("test")
	defer client.Close()

	err := waitForMCPReady(ctx, "test-instance", "http://localhost:0/mcp", client)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestWaitForMCPReadyTimeout(t *testing.T) {
	// Use a very short timeout context to test the timeout path quickly.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	client := mcpclient.New("test")
	defer client.Close()

	err := waitForMCPReady(ctx, "test-instance", "http://localhost:0/mcp", client)
	if err == nil {
		t.Fatal("expected error for unreachable endpoint")
	}
}
