package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPersonalitySubcommandsRegistered(t *testing.T) {
	subs := []string{"validate", "pull", "list"}
	for _, name := range subs {
		t.Run(name, func(t *testing.T) {
			for _, cmd := range personalityCmd.Commands() {
				if cmd.Name() == name {
					return
				}
			}
			t.Errorf("expected %q subcommand on personality", name)
		})
	}
}

func TestPersonalityCommandRegisteredOnRoot(t *testing.T) {
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "personality" {
			return
		}
	}
	t.Error("expected 'personality' command to be registered on root")
}

func TestValidatePersonalityDirValid(t *testing.T) {
	dir := t.TempDir()

	spec := `description: SRE personality
image: gsoci.azurecr.io/giantswarm/klaus-go:1.0.0
plugins:
  - repository: gsoci.azurecr.io/giantswarm/klaus-plugins/gs-base
    tag: v0.6.0
`
	if err := os.WriteFile(filepath.Join(dir, "personality.yaml"), []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}

	err := validatePersonalityDir(dir)
	if err != nil {
		t.Errorf("validatePersonalityDir() error = %v", err)
	}
}

func TestValidatePersonalityDirMinimal(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "personality.yaml"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := validatePersonalityDir(dir)
	if err != nil {
		t.Errorf("validatePersonalityDir() error = %v", err)
	}
}

func TestValidatePersonalityDirMissingSpec(t *testing.T) {
	dir := t.TempDir()

	err := validatePersonalityDir(dir)
	if err == nil {
		t.Fatal("expected error for missing personality.yaml")
	}
	if !strings.Contains(err.Error(), "personality.yaml not found") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidatePersonalityDirInvalidYAML(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "personality.yaml"), []byte("{{invalid"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := validatePersonalityDir(dir)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
	if !strings.Contains(err.Error(), "parsing personality.yaml") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidatePersonalityDirNotExist(t *testing.T) {
	err := validatePersonalityDir("/nonexistent/path")
	if err == nil {
		t.Fatal("expected error for nonexistent directory")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidatePersonalityDirNotADirectory(t *testing.T) {
	f := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(f, []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := validatePersonalityDir(f)
	if err == nil {
		t.Fatal("expected error for file (not directory)")
	}
	if !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestPersonalityListFlagsRegistered(t *testing.T) {
	if f := personalityListCmd.Flags().Lookup("output"); f == nil {
		t.Error("expected --output flag")
	}
	if f := personalityListCmd.Flags().Lookup("remote"); f == nil {
		t.Error("expected --remote flag")
	}
}
