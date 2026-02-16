package oci

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// maxExtractFileSize is the per-file size limit during extraction (100 MB).
const maxExtractFileSize = 100 << 20

// Pull downloads a plugin from an OCI registry and extracts it to destDir.
// If the plugin is already cached with a matching digest, the pull is skipped
// and PullResult.Cached is set to true.
func (c *Client) Pull(ctx context.Context, ref string, destDir string) (*PullResult, error) {
	repo, tag, err := c.newRepository(ref)
	if err != nil {
		return nil, err
	}

	if tag == "" {
		return nil, fmt.Errorf("reference %q must include a tag or digest", ref)
	}

	// Resolve to manifest descriptor.
	manifestDesc, err := repo.Resolve(ctx, tag)
	if err != nil {
		return nil, fmt.Errorf("resolving %s: %w", ref, err)
	}

	digest := manifestDesc.Digest.String()

	// Check cache -- skip pull if digest matches.
	if IsCached(destDir, digest) {
		return &PullResult{Digest: digest, Ref: ref, Cached: true}, nil
	}

	// Fetch manifest.
	manifestRC, err := repo.Fetch(ctx, manifestDesc)
	if err != nil {
		return nil, fmt.Errorf("fetching manifest for %s: %w", ref, err)
	}
	defer manifestRC.Close()

	var manifest ocispec.Manifest
	if err := json.NewDecoder(manifestRC).Decode(&manifest); err != nil {
		return nil, fmt.Errorf("parsing manifest for %s: %w", ref, err)
	}

	// Find the content layer by media type.
	var contentLayer *ocispec.Descriptor
	for i := range manifest.Layers {
		if manifest.Layers[i].MediaType == MediaTypePluginContent {
			contentLayer = &manifest.Layers[i]
			break
		}
	}
	if contentLayer == nil {
		return nil, fmt.Errorf("no content layer found in %s (expected media type %s)", ref, MediaTypePluginContent)
	}

	// Fetch the content layer blob.
	layerRC, err := repo.Fetch(ctx, *contentLayer)
	if err != nil {
		return nil, fmt.Errorf("fetching content layer for %s: %w", ref, err)
	}
	defer layerRC.Close()

	// Clean the destination before extracting to avoid stale files.
	if err := os.RemoveAll(destDir); err != nil {
		return nil, fmt.Errorf("cleaning destination %s: %w", destDir, err)
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating destination %s: %w", destDir, err)
	}

	if err := extractTarGz(layerRC, destDir); err != nil {
		return nil, fmt.Errorf("extracting content for %s: %w", ref, err)
	}

	// Write cache metadata so subsequent pulls with the same digest are skipped.
	if err := WriteCacheEntry(destDir, CacheEntry{Digest: digest, Ref: ref}); err != nil {
		return nil, fmt.Errorf("writing cache entry: %w", err)
	}

	return &PullResult{Digest: digest, Ref: ref}, nil
}

// extractTarGz extracts a gzip-compressed tar archive to destDir.
// It validates paths to prevent directory traversal attacks and limits
// individual file sizes.
func extractTarGz(r io.Reader, destDir string) error {
	gzr, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("creating gzip reader: %w", err)
	}
	defer gzr.Close()

	cleanDest := filepath.Clean(destDir)
	tr := tar.NewReader(gzr)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading tar entry: %w", err)
		}

		// Sanitize path to prevent directory traversal.
		name := filepath.Clean(header.Name)
		if strings.HasPrefix(name, "..") || filepath.IsAbs(name) {
			return fmt.Errorf("invalid path in archive: %s", header.Name)
		}

		target := filepath.Join(destDir, name)

		// Verify target stays within destDir.
		if !strings.HasPrefix(filepath.Clean(target), cleanDest) {
			return fmt.Errorf("path escapes destination: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("creating directory %s: %w", target, err)
			}

		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("creating parent directory for %s: %w", target, err)
			}

			mode := os.FileMode(header.Mode) & 0o777
			if mode == 0 {
				mode = 0o644
			}

			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
			if err != nil {
				return fmt.Errorf("creating file %s: %w", target, err)
			}

			n, err := io.Copy(f, io.LimitReader(tr, maxExtractFileSize+1))
			if err != nil {
				f.Close()
				return fmt.Errorf("extracting file %s: %w", target, err)
			}
			f.Close()

			if n > maxExtractFileSize {
				return fmt.Errorf("file %s exceeds max size (%d bytes)", header.Name, maxExtractFileSize)
			}

		default:
			// Skip symlinks and other types for security.
		}
	}

	return nil
}
