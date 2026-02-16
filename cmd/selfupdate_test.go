package cmd

import (
	"strings"
	"testing"
)

func TestNewSelfUpdateCmd(t *testing.T) {
	cmd := newSelfUpdateCmd()

	if cmd.Use != "self-update" {
		t.Errorf("expected Use to be 'self-update', got %q", cmd.Use)
	}
	if cmd.Short == "" {
		t.Error("expected Short description to be set")
	}
	if cmd.Long == "" {
		t.Error("expected Long description to be set")
	}
	if cmd.RunE == nil {
		t.Error("expected RunE to be set")
	}
}

func TestRunSelfUpdateWithDevVersion(t *testing.T) {
	original := rootCmd.Version
	defer func() { rootCmd.Version = original }()

	rootCmd.Version = "dev"
	err := runSelfUpdate(nil, nil)
	if err == nil {
		t.Fatal("expected error for dev version")
	}
	if !strings.Contains(err.Error(), "cannot self-update a development version") {
		t.Errorf("unexpected error message: %s", err)
	}
}

func TestRunSelfUpdateWithEmptyVersion(t *testing.T) {
	original := rootCmd.Version
	defer func() { rootCmd.Version = original }()

	rootCmd.Version = ""
	err := runSelfUpdate(nil, nil)
	if err == nil {
		t.Fatal("expected error for empty version")
	}
	if !strings.Contains(err.Error(), "cannot self-update a development version") {
		t.Errorf("unexpected error message: %s", err)
	}
}

func TestSelfUpdateCommandHelp(t *testing.T) {
	cmd := newSelfUpdateCmd()
	if !strings.Contains(cmd.Long, "latest release") {
		t.Error("expected Long description to mention 'latest release'")
	}
	if !strings.Contains(cmd.Long, "klausctl") {
		t.Error("expected Long description to mention 'klausctl'")
	}
}

func TestGithubRepoSlug(t *testing.T) {
	if githubRepoSlug != "giantswarm/klausctl" {
		t.Errorf("expected repo slug to be 'giantswarm/klausctl', got %q", githubRepoSlug)
	}
}

func TestSelfUpdateSubcommandRegistered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "self-update" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'self-update' subcommand to be registered on rootCmd")
	}
}
