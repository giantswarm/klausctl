// Package oci handles pulling and pushing OCI plugin artifacts using ORAS.
//
// Plugins are OCI artifacts containing skills, hooks, agents, and MCP server
// configurations. They are pulled to the local plugins directory before
// the klaus container is started, then bind-mounted into the container.
package oci

import (
	"context"
	"fmt"

	"oras.land/oras-go/v2/registry/remote"
)

const (
	// MediaTypePluginConfig is the OCI media type for the plugin config blob.
	MediaTypePluginConfig = "application/vnd.giantswarm.klaus-plugin.config.v1+json"

	// MediaTypePluginContent is the OCI media type for the plugin content layer.
	MediaTypePluginContent = "application/vnd.giantswarm.klaus-plugin.content.v1.tar+gzip"
)

// PluginMeta holds metadata stored in the OCI config blob.
type PluginMeta struct {
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description,omitempty"`
	Skills      []string `json:"skills,omitempty"`
	Commands    []string `json:"commands,omitempty"`
}

// PullResult holds the result of a successful pull.
type PullResult struct {
	// Digest is the resolved manifest digest.
	Digest string
	// Ref is the original reference string.
	Ref string
	// Cached is true if the pull was skipped because the local cache was fresh.
	Cached bool
}

// PushResult holds the result of a successful push.
type PushResult struct {
	// Digest is the manifest digest of the pushed artifact.
	Digest string
}

// Client is an ORAS-based client for interacting with OCI registries.
type Client struct {
	plainHTTP bool
}

// ClientOption configures the OCI client.
type ClientOption func(*Client)

// WithPlainHTTP disables TLS for registry communication.
// This is useful for local testing with insecure registries.
func WithPlainHTTP(plain bool) ClientOption {
	return func(c *Client) { c.plainHTTP = plain }
}

// NewClient creates a new OCI client.
func NewClient(opts ...ClientOption) *Client {
	c := &Client{}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Resolve resolves a reference (tag or digest) to its manifest digest.
func (c *Client) Resolve(ctx context.Context, ref string) (string, error) {
	repo, tag, err := c.newRepository(ref)
	if err != nil {
		return "", err
	}

	desc, err := repo.Resolve(ctx, tag)
	if err != nil {
		return "", fmt.Errorf("resolving %s: %w", ref, err)
	}

	return desc.Digest.String(), nil
}

// List returns all tags in the given repository.
func (c *Client) List(ctx context.Context, repository string) ([]string, error) {
	repo, err := c.newRepositoryFromName(repository)
	if err != nil {
		return nil, err
	}

	var tags []string
	err = repo.Tags(ctx, "", func(t []string) error {
		tags = append(tags, t...)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("listing tags for %s: %w", repository, err)
	}

	return tags, nil
}

// newRepository creates a remote.Repository from a full OCI reference string
// (e.g. "registry.example.com/repo:tag") and returns the repository client
// and the tag/digest portion.
func (c *Client) newRepository(ref string) (*remote.Repository, string, error) {
	repo, err := remote.NewRepository(ref)
	if err != nil {
		return nil, "", fmt.Errorf("parsing reference %q: %w", ref, err)
	}

	tag := repo.Reference.Reference
	repo.PlainHTTP = c.plainHTTP
	repo.Client = newAuthClient()

	return repo, tag, nil
}

// newRepositoryFromName creates a remote.Repository from a repository name
// (without tag or digest), used for listing tags.
func (c *Client) newRepositoryFromName(name string) (*remote.Repository, error) {
	repo, err := remote.NewRepository(name)
	if err != nil {
		return nil, fmt.Errorf("creating repository for %q: %w", name, err)
	}

	repo.PlainHTTP = c.plainHTTP
	repo.Client = newAuthClient()

	return repo, nil
}
