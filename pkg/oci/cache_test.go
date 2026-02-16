package oci

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAndReadCacheEntry(t *testing.T) {
	dir := t.TempDir()

	entry := &CacheEntry{
		Digest: "sha256:abc123def456",
		Ref:    "example.com/plugin:v1.0.0",
	}

	if err := WriteCacheEntry(dir, entry); err != nil {
		t.Fatalf("WriteCacheEntry() error = %v", err)
	}

	got, err := ReadCacheEntry(dir)
	if err != nil {
		t.Fatalf("ReadCacheEntry() error = %v", err)
	}

	if got.Digest != entry.Digest {
		t.Errorf("Digest = %q, want %q", got.Digest, entry.Digest)
	}
	if got.Ref != entry.Ref {
		t.Errorf("Ref = %q, want %q", got.Ref, entry.Ref)
	}
	if got.PulledAt.IsZero() {
		t.Error("PulledAt should not be zero")
	}
}

func TestReadCacheEntryMissing(t *testing.T) {
	dir := t.TempDir()

	_, err := ReadCacheEntry(dir)
	if err == nil {
		t.Fatal("ReadCacheEntry() should return error for missing file")
	}
}

func TestReadCacheEntryInvalid(t *testing.T) {
	dir := t.TempDir()

	path := filepath.Join(dir, cacheFileName)
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := ReadCacheEntry(dir)
	if err == nil {
		t.Fatal("ReadCacheEntry() should return error for invalid JSON")
	}
}

func TestIsCached(t *testing.T) {
	dir := t.TempDir()

	digest := "sha256:abc123def456"

	// No cache file -- should not be cached.
	if IsCached(dir, digest) {
		t.Error("IsCached() should return false when no cache file exists")
	}

	// Write cache entry.
	if err := WriteCacheEntry(dir, &CacheEntry{Digest: digest, Ref: "example.com/p:v1"}); err != nil {
		t.Fatal(err)
	}

	// Matching digest -- should be cached.
	if !IsCached(dir, digest) {
		t.Error("IsCached() should return true for matching digest")
	}

	// Different digest -- should not be cached.
	if IsCached(dir, "sha256:different") {
		t.Error("IsCached() should return false for different digest")
	}
}
