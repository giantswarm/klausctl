package oci

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
)

// DiscoverRepositories queries the OCI registry catalog to find all
// repositories under the given base path. The base path format is
// "registry.example.com/org/prefix" (e.g.,
// "gsoci.azurecr.io/giantswarm/klaus-plugins"). Returns fully-qualified
// repository references sorted by name.
func DiscoverRepositories(ctx context.Context, registryBase string, plainHTTP bool) ([]string, error) {
	host, prefix := SplitRegistryBase(registryBase)

	reg, err := remote.NewRegistry(host)
	if err != nil {
		return nil, fmt.Errorf("creating registry client for %s: %w", host, err)
	}
	reg.PlainHTTP = plainHTTP
	reg.Client = newRegistryAuthClient()

	var repos []string
	err = reg.Repositories(ctx, "", func(batch []string) error {
		for _, name := range batch {
			if strings.HasPrefix(name, prefix) {
				repos = append(repos, host+"/"+name)
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("listing repositories in %s: %w", registryBase, err)
	}

	return repos, nil
}

// SplitRegistryBase splits a registry base path into the registry host and
// the repository name prefix (with trailing slash). For example,
// "gsoci.azurecr.io/giantswarm/klaus-plugins" returns
// ("gsoci.azurecr.io", "giantswarm/klaus-plugins/").
func SplitRegistryBase(base string) (host, prefix string) {
	idx := strings.Index(base, "/")
	if idx < 0 {
		return base, ""
	}
	return base[:idx], base[idx+1:] + "/"
}

// registryDockerConfig represents the Docker/Podman credential config file format.
type registryDockerConfig struct {
	Auths map[string]registryDockerAuth `json:"auths"`
}

type registryDockerAuth struct {
	Auth string `json:"auth"`
}

// newRegistryAuthClient creates an ORAS auth client for registry-level
// operations. It mirrors the credential resolution chain used by the
// klaus-oci Client: env var, Docker config, Podman auth, anonymous.
func newRegistryAuthClient() *auth.Client {
	return &auth.Client{
		Client: http.DefaultClient,
		Cache:  auth.NewCache(),
		Credential: func(_ context.Context, hostport string) (auth.Credential, error) {
			return resolveRegistryCredential(hostport)
		},
	}
}

func resolveRegistryCredential(hostport string) (auth.Credential, error) {
	if envAuth := os.Getenv(RegistryAuthEnvVar); envAuth != "" {
		if cred, ok := registryCredFromBase64(envAuth, hostport); ok {
			return cred, nil
		}
	}

	if home, err := os.UserHomeDir(); err == nil {
		if cred, ok := registryCredFromFile(filepath.Join(home, ".docker", "config.json"), hostport); ok {
			return cred, nil
		}
	}

	if runtimeDir := os.Getenv("XDG_RUNTIME_DIR"); runtimeDir != "" {
		if cred, ok := registryCredFromFile(filepath.Join(runtimeDir, "containers", "auth.json"), hostport); ok {
			return cred, nil
		}
	}

	return auth.EmptyCredential, nil
}

func registryCredFromBase64(envValue, hostport string) (auth.Credential, bool) {
	data, err := base64.StdEncoding.DecodeString(envValue)
	if err != nil {
		return auth.EmptyCredential, false
	}
	return registryCredFromJSON(data, hostport)
}

func registryCredFromFile(path, hostport string) (auth.Credential, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return auth.EmptyCredential, false
	}
	return registryCredFromJSON(data, hostport)
}

func registryCredFromJSON(data []byte, hostport string) (auth.Credential, bool) {
	var cfg registryDockerConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return auth.EmptyCredential, false
	}

	entry, ok := cfg.Auths[hostport]
	if !ok {
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
