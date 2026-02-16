package oci

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestCreateTarGz(t *testing.T) {
	// Set up a plugin directory with a known structure.
	srcDir := t.TempDir()

	writeFile(t, filepath.Join(srcDir, "skills", "k8s", "SKILL.md"), "# Kubernetes skill")
	writeFile(t, filepath.Join(srcDir, "agents", "helper.md"), "Helper agent")
	writeFile(t, filepath.Join(srcDir, ".mcp.json"), `{"mcpServers":{}}`)

	// Also write a cache file that should be excluded.
	writeFile(t, filepath.Join(srcDir, ".klausctl-cache.json"), `{"digest":"old"}`)

	data, err := createTarGz(srcDir)
	if err != nil {
		t.Fatalf("createTarGz() error = %v", err)
	}

	// Extract and verify.
	files := listTarGzEntries(t, data)
	sort.Strings(files)

	expected := []string{
		".mcp.json",
		"agents",
		"agents/helper.md",
		"skills",
		"skills/k8s",
		"skills/k8s/SKILL.md",
	}

	if len(files) != len(expected) {
		t.Fatalf("archive contains %d entries, want %d\ngot:  %v\nwant: %v", len(files), len(expected), files, expected)
	}

	for i, name := range files {
		if name != expected[i] {
			t.Errorf("entry[%d] = %q, want %q", i, name, expected[i])
		}
	}
}

func TestCreateTarGzExcludesCacheFiles(t *testing.T) {
	srcDir := t.TempDir()

	writeFile(t, filepath.Join(srcDir, "SKILL.md"), "content")
	writeFile(t, filepath.Join(srcDir, ".klausctl-cache.json"), "cache data")
	writeFile(t, filepath.Join(srcDir, ".klausctl-meta.json"), "meta data")

	data, err := createTarGz(srcDir)
	if err != nil {
		t.Fatalf("createTarGz() error = %v", err)
	}

	files := listTarGzEntries(t, data)
	for _, f := range files {
		if f == ".klausctl-cache.json" || f == ".klausctl-meta.json" {
			t.Errorf("archive should not contain %s", f)
		}
	}
}

func TestCreateTarGzRoundTrip(t *testing.T) {
	srcDir := t.TempDir()

	writeFile(t, filepath.Join(srcDir, "file.txt"), "hello world")
	writeFile(t, filepath.Join(srcDir, "sub", "nested.txt"), "nested content")

	data, err := createTarGz(srcDir)
	if err != nil {
		t.Fatalf("createTarGz() error = %v", err)
	}

	// Extract to a new directory.
	destDir := t.TempDir()
	if err := extractTarGz(bytes.NewReader(data), destDir); err != nil {
		t.Fatalf("extractTarGz() error = %v", err)
	}

	// Verify round-trip.
	assertFileContent(t, filepath.Join(destDir, "file.txt"), "hello world")
	assertFileContent(t, filepath.Join(destDir, "sub", "nested.txt"), "nested content")
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func listTarGzEntries(t *testing.T, data []byte) []string {
	t.Helper()

	gzr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	var names []string
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, header.Name)
	}
	return names
}
