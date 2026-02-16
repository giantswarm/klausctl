package cmd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/creativeprojects/go-selfupdate"
	"github.com/spf13/cobra"
)

// githubRepoSlug specifies the GitHub repository (owner/repo) to check for updates.
const githubRepoSlug = "giantswarm/klausctl"

// maxReleaseNotesLines limits the number of release note lines printed by default.
const maxReleaseNotesLines = 10

var selfUpdateYes bool

var selfUpdateCmd = &cobra.Command{
	Use:   "self-update",
	Short: "Update klausctl to the latest version",
	Long: `Checks for the latest release of klausctl on GitHub and
updates the current binary if a newer version is found.`,
	RunE: runSelfUpdate,
}

func init() {
	selfUpdateCmd.Flags().BoolVarP(&selfUpdateYes, "yes", "y", false, "skip confirmation prompt")
	rootCmd.AddCommand(selfUpdateCmd)
}

// runSelfUpdate performs the self-update logic.
// It checks the current version against the latest GitHub release and updates if necessary.
func runSelfUpdate(cmd *cobra.Command, _ []string) error {
	out := cmd.OutOrStdout()

	currentVersion := rootCmd.Version
	if currentVersion == "" || currentVersion == "dev" {
		return fmt.Errorf("cannot self-update a development version")
	}

	fmt.Fprintf(out, "Current version: %s\n", currentVersion)
	fmt.Fprintln(out, "Checking for updates...")

	updater, err := selfupdate.NewUpdater(selfupdate.Config{})
	if err != nil {
		return fmt.Errorf("failed to create updater: %w", err)
	}

	latest, found, err := updater.DetectLatest(context.Background(), selfupdate.ParseSlug(githubRepoSlug))
	if err != nil {
		return fmt.Errorf("error detecting latest version: %w", err)
	}
	if !found {
		return fmt.Errorf("latest release for %s could not be found", githubRepoSlug)
	}

	if !latest.GreaterThan(currentVersion) {
		fmt.Fprintln(out, "Current version is the latest.")
		return nil
	}

	fmt.Fprintf(out, "Found newer version: %s (published at %s)\n", latest.Version(), latest.PublishedAt)
	printReleaseNotes(out, latest.ReleaseNotes)

	if !selfUpdateYes {
		fmt.Fprintf(out, "\nUpdate to version %s? [y/N] ", latest.Version())
		reader := bufio.NewReader(cmd.InOrStdin())
		answer, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("reading confirmation: %w", err)
		}
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			fmt.Fprintln(out, "Update cancelled.")
			return nil
		}
	}

	exe, err := selfupdate.ExecutablePath()
	if err != nil {
		return fmt.Errorf("could not locate executable path: %w", err)
	}

	fmt.Fprintf(out, "Updating %s to version %s...\n", exe, latest.Version())

	if err := updater.UpdateTo(context.Background(), latest, exe); err != nil {
		return fmt.Errorf("update failed: %w", err)
	}

	fmt.Fprintf(out, "Successfully updated to version %s\n", latest.Version())
	return nil
}

// printReleaseNotes writes release notes to out, truncating after maxReleaseNotesLines.
func printReleaseNotes(out io.Writer, notes string) {
	if notes == "" {
		return
	}
	lines := strings.Split(notes, "\n")
	if len(lines) <= maxReleaseNotesLines {
		fmt.Fprintf(out, "Release notes:\n%s\n", notes)
		return
	}
	fmt.Fprintln(out, "Release notes:")
	for _, line := range lines[:maxReleaseNotesLines] {
		fmt.Fprintln(out, line)
	}
	fmt.Fprintf(out, "... (%d more lines)\n", len(lines)-maxReleaseNotesLines)
}
