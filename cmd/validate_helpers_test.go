package cmd

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// validateFunc is the signature shared by all validate*Dir functions.
type validateFunc func(dir string, out io.Writer, outputFmt string) error

// assertSubcommandsRegistered checks that all named subcommands exist on parent.
func assertSubcommandsRegistered(t *testing.T, parent *cobra.Command, names []string) {
	t.Helper()
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			for _, cmd := range parent.Commands() {
				if cmd.Name() == name {
					return
				}
			}
			t.Errorf("expected %q subcommand on %s", name, parent.Name())
		})
	}
}

// assertCommandOnRoot checks that a command with the given name is registered on rootCmd.
func assertCommandOnRoot(t *testing.T, name string) {
	t.Helper()
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == name {
			return
		}
	}
	t.Errorf("expected %q command to be registered on root", name)
}

// assertFlagRegistered checks that a flag is present on the given command.
func assertFlagRegistered(t *testing.T, cmd *cobra.Command, flagName string) {
	t.Helper()
	if cmd.Flags().Lookup(flagName) == nil {
		t.Errorf("expected --%s flag on %s", flagName, cmd.Name())
	}
}

// testValidateDirNotExist verifies that the validator returns an error for a
// non-existent path.
func testValidateDirNotExist(t *testing.T, fn validateFunc) {
	t.Helper()
	err := fn("/nonexistent/path", io.Discard, "text")
	if err == nil {
		t.Fatal("expected error for nonexistent directory")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("unexpected error: %v", err)
	}
}

// testValidateDirNotADirectory verifies that the validator returns an error
// when given a file instead of a directory.
func testValidateDirNotADirectory(t *testing.T, fn validateFunc) {
	t.Helper()
	f := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(f, []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := fn(f, io.Discard, "text")
	if err == nil {
		t.Fatal("expected error for file (not directory)")
	}
	if !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("unexpected error: %v", err)
	}
}
