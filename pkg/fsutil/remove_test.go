package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveAllReadOnlyTree(t *testing.T) {
	root := filepath.Join(t.TempDir(), "instance")
	modDir := filepath.Join(root, "workspace", ".gomodcache", "gopkg.in", "inf.v0@v0.9.1")
	if err := os.MkdirAll(modDir, 0o755); err != nil { // #nosec G301 G302 G306 -- the test builds read-only trees on purpose
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modDir, "rounder_test.go"), []byte("package inf\n"), 0o444); err != nil { // #nosec G301 G302 G306 -- the test builds read-only trees on purpose
		t.Fatal(err)
	}
	// The Go toolchain leaves the module cache read-only, deepest first.
	for _, dir := range []string{modDir, filepath.Dir(modDir), filepath.Join(root, "workspace", ".gomodcache")} {
		if err := os.Chmod(dir, 0o555); err != nil { // #nosec G301 G302 G306 -- the test builds read-only trees on purpose
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Dir(modDir), 0o755); _ = os.Chmod(modDir, 0o755) }) // #nosec G301 G302 G306 -- the test builds read-only trees on purpose

	if os.Geteuid() != 0 {
		if err := os.RemoveAll(root); err == nil {
			t.Fatal("os.RemoveAll unexpectedly removed a read-only tree; the test no longer exercises the fix")
		}
	}

	if err := RemoveAll(root); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("%s still exists: %v", root, err)
	}
}

func TestRemoveAllMissingPath(t *testing.T) {
	if err := RemoveAll(filepath.Join(t.TempDir(), "missing")); err != nil {
		t.Fatalf("RemoveAll on a missing path: %v", err)
	}
}

func TestRemoveAllKeepsSymlinkTarget(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "target")
	if err := os.Mkdir(target, 0o555); err != nil { // #nosec G301 G302 G306 -- the test builds read-only trees on purpose
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(target, 0o755) }) // #nosec G301 G302 G306 -- the test builds read-only trees on purpose
	root := filepath.Join(base, "instance")
	ro := filepath.Join(root, "ro")
	if err := os.MkdirAll(ro, 0o755); err != nil { // #nosec G301 G302 G306 -- the test builds read-only trees on purpose
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(ro, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(ro, 0o555); err != nil { // #nosec G301 G302 G306 -- the test builds read-only trees on purpose
		t.Fatal(err)
	}

	if err := RemoveAll(root); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("symlink target removed: %v", err)
	}
	if info.Mode().Perm() != 0o555 {
		t.Fatalf("symlink target mode changed to %v", info.Mode().Perm())
	}
}
