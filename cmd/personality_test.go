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

const personalitySpecYAML = `name: sre
description: SRE personality
toolchain:
  repository: gsoci.azurecr.io/giantswarm/klaus-toolchains/go
  tag: "1.0.0"
plugins:
  - repository: gsoci.azurecr.io/giantswarm/klaus-plugins/gs-base
    tag: v0.6.0
`

func TestPersonalitySubcommandsRegistered(t *testing.T) {
	assertSubcommandsRegistered(t, personalityCmd, []string{"validate", "pull", "list"})
}

func TestPersonalityCommandRegisteredOnRoot(t *testing.T) {
	assertCommandOnRoot(t, "personality")
}

func TestValidatePersonalityDirValid(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "personality.yaml"), []byte(personalitySpecYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := validatePersonalityDir(dir, io.Discard, "text"); err != nil {
		t.Errorf("validatePersonalityDir() error = %v", err)
	}
}

func TestValidatePersonalityDirMinimal(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "personality.yaml"), []byte("name: minimal"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := validatePersonalityDir(dir, io.Discard, "text"); err != nil {
		t.Errorf("validatePersonalityDir() error = %v", err)
	}
}

func TestValidatePersonalityDirMissingSpec(t *testing.T) {
	dir := t.TempDir()

	err := validatePersonalityDir(dir, io.Discard, "text")
	if err == nil {
		t.Fatal("expected error for missing personality.yaml")
	}
	if !strings.Contains(err.Error(), "personality.yaml") {
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
	testValidateDirNotExist(t, validatePersonalityDir)
}

func TestValidatePersonalityDirNotADirectory(t *testing.T) {
	testValidateDirNotADirectory(t, validatePersonalityDir)
}

func TestValidatePersonalityDirTextOutput(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "personality.yaml"), []byte(personalitySpecYAML), 0o644); err != nil {
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
	if err := os.WriteFile(filepath.Join(dir, "personality.yaml"), []byte(personalitySpecYAML), 0o644); err != nil {
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
	assertFlagRegistered(t, personalityValidateCmd, "output")
	assertFlagRegistered(t, personalityPullCmd, "output")
	assertFlagRegistered(t, personalityListCmd, "output")
	assertFlagRegistered(t, personalityListCmd, "local")
}
