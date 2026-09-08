package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/creativeprojects/go-selfupdate"
)

func TestRunSelfUpdateRejectsDevVersion(t *testing.T) {
	original := rootCmd.Version
	defer func() { rootCmd.Version = original }()

	tests := []struct {
		name    string
		version string
	}{
		{versionDev, versionDev},
		{testCaseEmpty, ""},
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

func TestSelfUpdateHelpSaysReleasesAreVerified(t *testing.T) {
	if !strings.Contains(selfUpdateCmd.Long, "Sigstore bundle") {
		t.Errorf("the long help should say that releases are verified, got: %q", selfUpdateCmd.Long)
	}
}

// The signature check itself (a bundle that verifies, a tampered binary, a
// bundle for another repository) is tested where it lives, in
// github.com/giantswarm/selfupdate-cosign. What follows checks that klausctl
// wires it in so that an unsigned or unverifiable release never reaches the
// disk.

// fakeSource stands in for GitHub: one release, and the bytes every asset
// download returns.
type fakeSource struct {
	release fakeRelease
	assets  map[int64][]byte
}

func (s *fakeSource) ListReleases(context.Context, selfupdate.Repository) ([]selfupdate.SourceRelease, error) {
	return []selfupdate.SourceRelease{s.release}, nil
}

func (s *fakeSource) DownloadReleaseAsset(_ context.Context, _ *selfupdate.Release, id int64) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(s.assets[id])), nil
}

type fakeAsset struct {
	id   int64
	name string
}

func (a fakeAsset) GetID() int64                  { return a.id }
func (a fakeAsset) GetName() string               { return a.name }
func (a fakeAsset) GetSize() int                  { return 3 }
func (a fakeAsset) GetBrowserDownloadURL() string { return "https://example.test/" + a.name }

type fakeRelease struct {
	tag    string
	assets []selfupdate.SourceAsset
}

func (r fakeRelease) GetID() int64              { return 1 }
func (r fakeRelease) GetTagName() string        { return r.tag }
func (r fakeRelease) GetDraft() bool            { return false }
func (r fakeRelease) GetPrerelease() bool       { return false }
func (r fakeRelease) GetPublishedAt() time.Time { return time.Date(2026, 9, 8, 10, 0, 0, 0, time.UTC) }
func (r fakeRelease) GetReleaseNotes() string   { return "notes" }
func (r fakeRelease) GetName() string           { return r.tag }
func (r fakeRelease) GetURL() string {
	return "https://github.com/giantswarm/klausctl/releases/tag/" + r.tag
}
func (r fakeRelease) GetAssets() []selfupdate.SourceAsset { return r.assets }

// binaryAsset is the asset name architect publishes for this platform.
func binaryAsset() string {
	name := "klausctl-" + runtime.GOOS + "-" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

// selfUpdateFixture points the command at src instead of GitHub and at a
// throwaway file instead of the running executable, skips the [y/N] prompt,
// and captures the command's output; it returns the file's path and content,
// so a test can assert the file survived, and the output buffer.
func selfUpdateFixture(t *testing.T, src *fakeSource) (string, []byte, *bytes.Buffer) {
	t.Helper()
	installed := []byte("the klausctl that is installed right now")
	exe := filepath.Join(t.TempDir(), "klausctl")
	if err := os.WriteFile(exe, installed, 0o755); err != nil { //nolint:gosec // an executable
		t.Fatal(err)
	}
	prevSource, prevExe, prevVersion, prevYes := selfUpdateSource, selfUpdateExecutable, rootCmd.Version, selfUpdateYes
	selfUpdateSource = src
	selfUpdateExecutable = func() (string, error) { return exe, nil }
	rootCmd.Version = "1.0.0"
	selfUpdateYes = true
	var out bytes.Buffer
	selfUpdateCmd.SetOut(&out)
	t.Cleanup(func() {
		selfUpdateSource, selfUpdateExecutable, rootCmd.Version, selfUpdateYes = prevSource, prevExe, prevVersion, prevYes
		selfUpdateCmd.SetOut(nil)
	})
	return exe, installed, &out
}

func assertUnchanged(t *testing.T, exe string, installed []byte) {
	t.Helper()
	got, err := os.ReadFile(exe) //nolint:gosec // the test's own temp file
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, installed) {
		t.Fatalf("the installed binary was replaced: %q", got)
	}
}

func TestRunSelfUpdateRefusesAReleaseWithoutASignatureBundle(t *testing.T) {
	src := &fakeSource{
		release: fakeRelease{tag: "v99.0.0", assets: []selfupdate.SourceAsset{fakeAsset{1, binaryAsset()}}},
		assets:  map[int64][]byte{1: []byte("a newer klausctl, unsigned")},
	}
	exe, installed, _ := selfUpdateFixture(t, src)

	err := runSelfUpdate(selfUpdateCmd, nil)
	if err == nil {
		t.Fatal("a release without a bundle must be refused")
	}
	if !strings.Contains(err.Error(), "no signature bundle") {
		t.Errorf("the error should say what is missing, got: %v", err)
	}
	assertUnchanged(t, exe, installed)
}

func TestRunSelfUpdateRefusesADownloadThatDoesNotVerify(t *testing.T) {
	src := &fakeSource{
		release: fakeRelease{tag: "v99.0.0", assets: []selfupdate.SourceAsset{
			fakeAsset{1, binaryAsset()},
			fakeAsset{2, binaryAsset() + ".bundle"},
		}},
		assets: map[int64][]byte{
			1: []byte("a newer klausctl"),
			2: []byte("{}"), // not a Sigstore bundle
		},
	}
	exe, installed, out := selfUpdateFixture(t, src)

	err := runSelfUpdate(selfUpdateCmd, nil)
	if err == nil {
		t.Fatal("a download whose bundle does not verify must be refused")
	}
	if !strings.Contains(err.Error(), "is unchanged") || !strings.Contains(err.Error(), "is not a Sigstore bundle") {
		t.Errorf("the error should say the binary was refused and why, got: %v", err)
	}
	if !strings.Contains(out.String(), "Found newer version: 99.0.0") {
		t.Errorf("the newer release should have been announced before the refusal, got:\n%s", out.String())
	}
	assertUnchanged(t, exe, installed)
}
