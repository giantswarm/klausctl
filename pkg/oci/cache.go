package oci

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const cacheFileName = ".klausctl-cache.json"

// CacheEntry holds metadata about a cached plugin.
type CacheEntry struct {
	// Digest is the OCI manifest digest.
	Digest string `json:"digest"`
	// Ref is the original OCI reference that was pulled.
	Ref string `json:"ref"`
	// PulledAt is when the plugin was last pulled.
	PulledAt time.Time `json:"pulledAt"`
}

// IsCached returns true if the plugin directory has a cache entry
// matching the given manifest digest.
func IsCached(pluginDir string, digest string) bool {
	entry, err := ReadCacheEntry(pluginDir)
	if err != nil {
		return false
	}
	return entry.Digest == digest
}

// ReadCacheEntry reads the cache metadata from a plugin directory.
func ReadCacheEntry(pluginDir string) (*CacheEntry, error) {
	path := filepath.Join(pluginDir, cacheFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var entry CacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, err
	}

	return &entry, nil
}

// WriteCacheEntry writes cache metadata to a plugin directory.
// The PulledAt timestamp is always set to the current time.
func WriteCacheEntry(pluginDir string, entry CacheEntry) error {
	entry.PulledAt = time.Now()

	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return err
	}

	path := filepath.Join(pluginDir, cacheFileName)
	return os.WriteFile(path, data, 0o644)
}
