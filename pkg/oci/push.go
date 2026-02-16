package oci

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	godigest "github.com/opencontainers/go-digest"
	specs "github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// Push packages a plugin directory and pushes it to an OCI registry.
// The ref must include a tag (e.g. "registry.example.com/plugins/my-plugin:v1.0.0").
func (c *Client) Push(ctx context.Context, pluginDir string, ref string, meta PluginMeta) (*PushResult, error) {
	repo, tag, err := c.newRepository(ref)
	if err != nil {
		return nil, err
	}

	if tag == "" {
		return nil, fmt.Errorf("reference %q must include a tag", ref)
	}

	// Create config blob from metadata.
	configJSON, err := json.Marshal(meta)
	if err != nil {
		return nil, fmt.Errorf("marshaling plugin metadata: %w", err)
	}
	configDesc := ocispec.Descriptor{
		MediaType: MediaTypePluginConfig,
		Digest:    godigest.FromBytes(configJSON),
		Size:      int64(len(configJSON)),
	}

	// Push config blob.
	if err := repo.Push(ctx, configDesc, bytes.NewReader(configJSON)); err != nil {
		return nil, fmt.Errorf("pushing config blob: %w", err)
	}

	// Create tar.gz layer from the plugin directory.
	layerData, err := createTarGz(pluginDir)
	if err != nil {
		return nil, fmt.Errorf("creating plugin archive: %w", err)
	}
	layerDesc := ocispec.Descriptor{
		MediaType: MediaTypePluginContent,
		Digest:    godigest.FromBytes(layerData),
		Size:      int64(len(layerData)),
	}

	// Push content layer.
	if err := repo.Push(ctx, layerDesc, bytes.NewReader(layerData)); err != nil {
		return nil, fmt.Errorf("pushing content layer: %w", err)
	}

	// Build and push manifest.
	annotations := map[string]string{
		ocispec.AnnotationTitle:       meta.Name,
		ocispec.AnnotationVersion:     meta.Version,
		ocispec.AnnotationDescription: meta.Description,
	}

	manifest := ocispec.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ocispec.MediaTypeImageManifest,
		Config:    configDesc,
		Layers:    []ocispec.Descriptor{layerDesc},
		Annotations: func() map[string]string {
			// Filter out empty annotations.
			clean := make(map[string]string)
			for k, v := range annotations {
				if v != "" {
					clean[k] = v
				}
			}
			if len(clean) == 0 {
				return nil
			}
			return clean
		}(),
	}

	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		return nil, fmt.Errorf("marshaling manifest: %w", err)
	}
	manifestDesc := ocispec.Descriptor{
		MediaType: ocispec.MediaTypeImageManifest,
		Digest:    godigest.FromBytes(manifestJSON),
		Size:      int64(len(manifestJSON)),
	}

	if err := repo.Push(ctx, manifestDesc, bytes.NewReader(manifestJSON)); err != nil {
		return nil, fmt.Errorf("pushing manifest: %w", err)
	}

	// Tag the manifest.
	if err := repo.Tag(ctx, manifestDesc, tag); err != nil {
		return nil, fmt.Errorf("tagging manifest as %s: %w", tag, err)
	}

	return &PushResult{Digest: manifestDesc.Digest.String()}, nil
}

// createTarGz creates a gzip-compressed tar archive of the given directory.
// Hidden files starting with ".klausctl-" (cache metadata) are excluded.
func createTarGz(sourceDir string) ([]byte, error) {
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)

	err := filepath.WalkDir(sourceDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}

		// Skip root directory entry.
		if relPath == "." {
			return nil
		}

		// Skip cache metadata files.
		if strings.HasPrefix(filepath.Base(relPath), ".klausctl-") {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		// Use relative path with forward slashes for cross-platform compatibility.
		header.Name = filepath.ToSlash(relPath)

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()

		_, err = io.Copy(tw, f)
		return err
	})

	if err != nil {
		return nil, err
	}

	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gzw.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
