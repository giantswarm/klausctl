package oci

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"oras.land/oras-go/v2/registry/remote/auth"
)

// dockerConfig represents the Docker/Podman credential config file format.
type dockerConfig struct {
	Auths map[string]dockerAuthEntry `json:"auths"`
}

// dockerAuthEntry holds a single registry credential.
type dockerAuthEntry struct {
	Auth string `json:"auth"` // base64(username:password)
}

// newAuthClient creates an auth.Client that resolves credentials from
// Docker/Podman config files or the KLAUSCTL_REGISTRY_AUTH env var.
func newAuthClient() *auth.Client {
	return &auth.Client{
		Client:     http.DefaultClient,
		Cache:      auth.NewCache(),
		Credential: resolveCredential,
	}
}

// resolveCredential resolves registry credentials in priority order:
//  1. KLAUSCTL_REGISTRY_AUTH env var (base64-encoded Docker config JSON)
//  2. Docker config at ~/.docker/config.json
//  3. Podman auth at $XDG_RUNTIME_DIR/containers/auth.json
//  4. Anonymous (empty credential)
func resolveCredential(_ context.Context, hostport string) (auth.Credential, error) {
	// 1. Environment variable override.
	if envAuth := os.Getenv("KLAUSCTL_REGISTRY_AUTH"); envAuth != "" {
		if cred, ok := credentialFromEnv(envAuth, hostport); ok {
			return cred, nil
		}
	}

	// 2. Docker config.
	if home, err := os.UserHomeDir(); err == nil {
		dockerCfg := filepath.Join(home, ".docker", "config.json")
		if cred, ok := credentialFromFile(dockerCfg, hostport); ok {
			return cred, nil
		}
	}

	// 3. Podman auth.
	if runtimeDir := os.Getenv("XDG_RUNTIME_DIR"); runtimeDir != "" {
		podmanAuth := filepath.Join(runtimeDir, "containers", "auth.json")
		if cred, ok := credentialFromFile(podmanAuth, hostport); ok {
			return cred, nil
		}
	}

	// 4. Anonymous access.
	return auth.EmptyCredential, nil
}

// credentialFromEnv decodes a base64 Docker config JSON from the env var.
func credentialFromEnv(envValue, hostport string) (auth.Credential, bool) {
	data, err := base64.StdEncoding.DecodeString(envValue)
	if err != nil {
		return auth.EmptyCredential, false
	}
	return credentialFromJSON(data, hostport)
}

// credentialFromFile reads a Docker/Podman config file and extracts
// credentials for the given registry host.
func credentialFromFile(path, hostport string) (auth.Credential, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return auth.EmptyCredential, false
	}
	return credentialFromJSON(data, hostport)
}

// credentialFromJSON extracts credentials for a specific host from
// a Docker-format config JSON.
func credentialFromJSON(data []byte, hostport string) (auth.Credential, bool) {
	var cfg dockerConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return auth.EmptyCredential, false
	}

	// Try exact match first.
	entry, ok := cfg.Auths[hostport]
	if !ok {
		// Try without port (e.g. "registry.example.com" for "registry.example.com:443").
		host := hostport
		if idx := strings.LastIndex(host, ":"); idx > 0 {
			host = host[:idx]
		}
		entry, ok = cfg.Auths[host]
	}
	if !ok {
		return auth.EmptyCredential, false
	}

	decoded, err := base64.StdEncoding.DecodeString(entry.Auth)
	if err != nil {
		return auth.EmptyCredential, false
	}

	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 {
		return auth.EmptyCredential, false
	}

	return auth.Credential{
		Username: parts[0],
		Password: parts[1],
	}, true
}
