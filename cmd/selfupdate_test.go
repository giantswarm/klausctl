package cmd

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func TestRunSelfUpdateRejectsDevVersion(t *testing.T) {
	original := rootCmd.Version
	defer func() { rootCmd.Version = original }()

	tests := []struct {
		name    string
		version string
	}{
		{"dev", "dev"},
		{"empty", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rootCmd.Version = tc.version
			err := runSelfUpdate(selfUpdateCmd, nil)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), "cannot self-update a development version") {
				t.Errorf("unexpected error: %s", err)
			}
		})
	}
}

func TestPrintReleaseNotes(t *testing.T) {
	t.Run("empty notes prints nothing", func(t *testing.T) {
		var buf bytes.Buffer
		printReleaseNotes(&buf, "")
		if buf.Len() != 0 {
			t.Errorf("expected empty output, got %q", buf.String())
		}
	})

	t.Run("short notes printed in full", func(t *testing.T) {
		var buf bytes.Buffer
		printReleaseNotes(&buf, "Fixed a bug.\nAdded a feature.")
		output := buf.String()
		if !strings.Contains(output, "Fixed a bug.") {
			t.Error("expected full notes to be printed")
		}
		if strings.Contains(output, "more lines") {
			t.Error("short notes should not be truncated")
		}
	})

	t.Run("long notes are truncated", func(t *testing.T) {
		lines := make([]string, 20)
		for i := range lines {
			lines[i] = fmt.Sprintf("Line %d", i+1)
		}
		var buf bytes.Buffer
		printReleaseNotes(&buf, strings.Join(lines, "\n"))
		output := buf.String()
		if !strings.Contains(output, "Line 1") {
			t.Error("expected first lines to be printed")
		}
		if !strings.Contains(output, "more lines") {
			t.Error("expected truncation indicator")
		}
		if strings.Contains(output, "Line 20") {
			t.Error("expected last lines to be omitted")
		}
	})
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

func TestSelfUpdateYesFlag(t *testing.T) {
	cmd := selfUpdateCmd
	f := cmd.Flags().Lookup("yes")
	if f == nil {
		t.Fatal("expected --yes flag to be registered")
	}
	if f.Shorthand != "y" {
		t.Errorf("expected shorthand 'y', got %q", f.Shorthand)
	}
}
