package cmd

import (
	"testing"
)

func TestStartSubcommandRegistered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "start" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'start' subcommand to be registered on rootCmd")
	}
}

func TestStartWorkspaceFlag(t *testing.T) {
	f := startCmd.Flags().Lookup("workspace")
	if f == nil {
		t.Fatal("expected --workspace flag to be registered")
	}
}
