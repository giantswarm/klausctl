package oci

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractTarGz(t *testing.T) {
	// Create a tar.gz with a known structure.
	buf := createTestTarGz(t, map[string]string{
		"skills/test/SKILL.md": "# Test Skill\nContent here.",
		"agents/helper.md":     "Helper agent content.",
		"hooks/hooks.json":     `{"PreToolUse":[]}`,
	})

	destDir := t.TempDir()
	if err := extractTarGz(buf, destDir); err != nil {
		t.Fatalf("extractTarGz() error = %v", err)
	}

	// Verify extracted files.
	assertFileContent(t, filepath.Join(destDir, "skills", "test", "SKILL.md"), "# Test Skill\nContent here.")
	assertFileContent(t, filepath.Join(destDir, "agents", "helper.md"), "Helper agent content.")
	assertFileContent(t, filepath.Join(destDir, "hooks", "hooks.json"), `{"PreToolUse":[]}`)
}

func TestExtractTarGzRejectsTraversal(t *testing.T) {
	// Create a tar.gz with a path traversal entry.
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)

	content := []byte("malicious")
	header := &tar.Header{
		Name: "../../../etc/passwd",
		Size: int64(len(content)),
		Mode: 0o644,
	}
	if err := tw.WriteHeader(header); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}

	tw.Close()
	gzw.Close()

	destDir := t.TempDir()
	err := extractTarGz(&buf, destDir)
	if err == nil {
		t.Fatal("extractTarGz() should reject path traversal")
	}
}

func TestExtractTarGzEmpty(t *testing.T) {
	// An empty archive should succeed without creating files.
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)
	tw.Close()
	gzw.Close()

	destDir := t.TempDir()
	if err := extractTarGz(&buf, destDir); err != nil {
		t.Fatalf("extractTarGz() error = %v", err)
	}
}

func TestExtractTarGzDirectories(t *testing.T) {
	// Tar with explicit directory entries.
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)

	// Add a directory entry.
	tw.WriteHeader(&tar.Header{
		Name:     "subdir/",
		Typeflag: tar.TypeDir,
		Mode:     0o755,
	})

	// Add a file in the directory.
	content := []byte("file content")
	tw.WriteHeader(&tar.Header{
		Name:     "subdir/file.txt",
		Size:     int64(len(content)),
		Mode:     0o644,
		Typeflag: tar.TypeReg,
	})
	tw.Write(content)

	tw.Close()
	gzw.Close()

	destDir := t.TempDir()
	if err := extractTarGz(&buf, destDir); err != nil {
		t.Fatalf("extractTarGz() error = %v", err)
	}

	// Verify directory was created.
	info, err := os.Stat(filepath.Join(destDir, "subdir"))
	if err != nil {
		t.Fatalf("directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("subdir should be a directory")
	}

	assertFileContent(t, filepath.Join(destDir, "subdir", "file.txt"), "file content")
}

// createTestTarGz creates a gzip-compressed tar archive from a map of
// path -> content pairs.
func createTestTarGz(t *testing.T, files map[string]string) *bytes.Buffer {
	t.Helper()

	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)

	for name, content := range files {
		data := []byte(content)
		header := &tar.Header{
			Name:     name,
			Size:     int64(len(data)),
			Mode:     0o644,
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(header); err != nil {
			t.Fatalf("writing tar header for %s: %v", name, err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatalf("writing tar content for %s: %v", name, err)
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatal(err)
	}

	return &buf
}

func assertFileContent(t *testing.T, path, expected string) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	if string(data) != expected {
		t.Errorf("content of %s = %q, want %q", path, string(data), expected)
	}
}
