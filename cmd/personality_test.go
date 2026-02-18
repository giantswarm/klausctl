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

	err := validatePersonalityDir(dir, io.Discard, "text")
	if err != nil {
		t.Errorf("validatePersonalityDir() error = %v", err)
	}
}

func TestValidatePersonalityDirMinimal(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "personality.yaml"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := validatePersonalityDir(dir, io.Discard, "text")
	if err != nil {
		t.Errorf("validatePersonalityDir() error = %v", err)
	}
}

func TestValidatePersonalityDirMissingSpec(t *testing.T) {
	dir := t.TempDir()

	err := validatePersonalityDir(dir, io.Discard, "text")
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

	err := validatePersonalityDir(dir, io.Discard, "text")
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
	if !strings.Contains(err.Error(), "parsing personality.yaml") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidatePersonalityDirNotExist(t *testing.T) {
	err := validatePersonalityDir("/nonexistent/path", io.Discard, "text")
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

	err := validatePersonalityDir(f, io.Discard, "text")
	if err == nil {
		t.Fatal("expected error for file (not directory)")
	}
	if !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidatePersonalityDirTextOutput(t *testing.T) {
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

	var buf bytes.Buffer
	if err := validatePersonalityDir(dir, &buf, "text"); err != nil {
		t.Fatalf("validatePersonalityDir() error = %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Valid personality directory") {
		t.Error("expected text output to contain 'Valid personality directory'")
	}
	if !strings.Contains(output, "Description: SRE personality") {
		t.Error("expected text output to contain description")
	}
}

func TestValidatePersonalityDirJSONOutput(t *testing.T) {
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

	var buf bytes.Buffer
	if err := validatePersonalityDir(dir, &buf, "json"); err != nil {
		t.Fatalf("validatePersonalityDir() error = %v", err)
	}

	var result personalityValidation
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v", err)
	}
	if !result.Valid {
		t.Error("expected valid=true")
	}
	if result.Description != "SRE personality" {
		t.Errorf("description = %q, want %q", result.Description, "SRE personality")
	}
	if result.Plugins != 1 {
		t.Errorf("plugins = %d, want 1", result.Plugins)
	}
}

func TestPersonalityFlagsRegistered(t *testing.T) {
	if f := personalityValidateCmd.Flags().Lookup("output"); f == nil {
		t.Error("expected --output flag on validate")
	}
	if f := personalityPullCmd.Flags().Lookup("output"); f == nil {
		t.Error("expected --output flag on pull")
	}
	if f := personalityListCmd.Flags().Lookup("output"); f == nil {
		t.Error("expected --output flag on list")
	}
	if f := personalityListCmd.Flags().Lookup("remote"); f == nil {
		t.Error("expected --remote flag on list")
	}
}
