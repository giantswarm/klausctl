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

	klausoci "github.com/giantswarm/klaus-oci"

	"github.com/giantswarm/klausctl/pkg/config"
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
	assertSubcommandsRegistered(t, personalityCmd, []string{"validate", "pull", "push", "list", "describe"})
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
	assertFlagRegistered(t, personalityValidateCmd, "source")
	assertFlagRegistered(t, personalityValidateCmd, "resolve-deps")
	assertFlagRegistered(t, personalityPullCmd, "output")
	assertFlagRegistered(t, personalityPushCmd, "output")
	assertFlagRegistered(t, personalityPushCmd, "source")
	assertFlagRegistered(t, personalityPushCmd, "dry-run")
	assertFlagRegistered(t, personalityListCmd, "output")
	assertFlagRegistered(t, personalityListCmd, "local")
	assertFlagRegistered(t, personalityDescribeCmd, "output")
	assertFlagRegistered(t, personalityDescribeCmd, "source")
	assertFlagRegistered(t, personalityDescribeCmd, "deps")
}

func TestValidatePersonalityDepsNoDeps(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "personality.yaml"), []byte("name: minimal"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := validatePersonalityDeps(t.Context(), dir, ""); err != nil {
		t.Errorf("validatePersonalityDeps() error = %v, want nil for personality without deps", err)
	}
}

func TestResolvePersonalityDepsCollectsAllErrors(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	spec := klausoci.Personality{
		Name: "test",
		Toolchain: klausoci.ToolchainReference{
			Repository: "localhost:1/toolchains/go",
		},
		Plugins: []klausoci.PluginReference{
			{Repository: "localhost:1/plugins/alpha"},
			{Repository: "localhost:1/plugins/beta"},
		},
	}

	resolver := config.NewSourceResolver(nil)
	client := klausoci.NewClient(klausoci.WithPlainHTTP(true))

	err := resolvePersonalityDeps(ctx, spec, resolver, client)
	if err == nil {
		t.Fatal("expected error when all deps are unresolvable")
	}

	msg := err.Error()
	if !strings.Contains(msg, "unresolvable dependencies") {
		t.Errorf("error should mention unresolvable dependencies, got: %s", msg)
	}
	if !strings.Contains(msg, "resolving toolchain") {
		t.Errorf("error should report toolchain failure, got: %s", msg)
	}
	if !strings.Contains(msg, "resolving plugin") {
		t.Errorf("error should report plugin failures, got: %s", msg)
	}
	if count := strings.Count(msg, "resolving plugin"); count != 2 {
		t.Errorf("expected 2 plugin errors, got %d in: %s", count, msg)
	}
}

func TestPrintResolvedDeps(t *testing.T) {
	deps := &klausoci.ResolvedDependencies{
		Toolchain: &klausoci.DescribedToolchain{
			ArtifactInfo: klausoci.ArtifactInfo{Digest: "sha256:tc123"},
			Toolchain:    klausoci.Toolchain{Name: "go", Version: "v1.0.0"},
		},
		Plugins: []klausoci.DescribedPlugin{
			{
				ArtifactInfo: klausoci.ArtifactInfo{Digest: "sha256:p1"},
				Plugin:       klausoci.Plugin{Name: "gs-base", Version: "v0.1.0"},
			},
		},
		Warnings: []string{"plugin gs-sre: not found"},
	}

	var buf bytes.Buffer
	printResolvedDeps(&buf, deps)
	output := buf.String()

	for _, want := range []string{
		"Resolved Toolchain:",
		"Name:          go",
		"Resolved Plugin [gs-base]:",
		"Warning: plugin gs-sre: not found",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q\ngot:\n%s", want, output)
		}
	}
}

func TestPrintIndentedMeta(t *testing.T) {
	var buf bytes.Buffer
	printIndentedMeta(&buf, artifactMeta{
		Name:        "go",
		Version:     "v1.0.0",
		Description: "Go toolchain",
		Author:      "GS",
		Digest:      "sha256:abc",
	})
	output := buf.String()

	for _, want := range []string{"  Name:", "  Version:", "  Description:", "  Author:", "  Digest:"} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing indented %q", want)
		}
	}
}
