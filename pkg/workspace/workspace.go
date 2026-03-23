// Package workspace manages cached git repository clones used as workspaces.
// It provides functions for identifying owner/repo format strings, checking
// access against a workspace registry, and ensuring local clones exist.
package workspace

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Registry is the workspace registry stored in workspaces.yaml.
// It tracks which orgs and individual repos are allowed as workspaces.
type Registry struct {
	Orgs  []string   `yaml:"orgs,omitempty"`
	Repos []RepoSpec `yaml:"repos,omitempty"`
}

// RepoSpec identifies a single repository by owner and name.
type RepoSpec struct {
	Owner string `yaml:"owner"`
	Repo  string `yaml:"repo"`
}

// IsRepoIdentifier reports whether s looks like an owner/repo identifier
// (exactly one slash separating two non-empty parts, no path separators or
// special characters that would indicate a filesystem path).
func IsRepoIdentifier(s string) bool {
	if s == "" {
		return false
	}
	// Filesystem paths: absolute, relative, or home-relative.
	if strings.HasPrefix(s, "/") || strings.HasPrefix(s, ".") || strings.HasPrefix(s, "~") {
		return false
	}
	parts := strings.SplitN(s, "/", 3)
	if len(parts) != 2 {
		return false
	}
	return parts[0] != "" && parts[1] != ""
}

// Load reads a workspace registry from the given YAML file.
// If the file does not exist, an empty registry is returned.
func Load(path string) (*Registry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Registry{}, nil
		}
		return nil, fmt.Errorf("reading workspace registry: %w", err)
	}
	var reg Registry
	if err := yaml.Unmarshal(data, &reg); err != nil {
		return nil, fmt.Errorf("parsing workspace registry: %w", err)
	}
	return &reg, nil
}

// IsAllowed reports whether the given owner/repo is permitted by the registry,
// either because the org is listed or the specific repo is listed.
func IsAllowed(reg *Registry, owner, repo string) bool {
	for _, org := range reg.Orgs {
		if strings.EqualFold(org, owner) {
			return true
		}
	}
	for _, r := range reg.Repos {
		if strings.EqualFold(r.Owner, owner) && strings.EqualFold(r.Repo, repo) {
			return true
		}
	}
	return false
}

// EnsureCached ensures a bare clone of owner/repo exists under reposDir.
// If noFetch is false and the clone already exists, it fetches updates.
// Returns the path to the cached clone.
func EnsureCached(reposDir, owner, repo string, noFetch bool) (string, error) {
	cacheDir := filepath.Join(reposDir, owner, repo)

	if info, err := os.Stat(filepath.Join(cacheDir, ".git")); err == nil && info.IsDir() {
		// Clone exists -- optionally fetch.
		if !noFetch {
			cmd := exec.Command("git", "-C", cacheDir, "fetch", "origin")
			if out, err := cmd.CombinedOutput(); err != nil {
				return "", fmt.Errorf("fetching %s/%s: %w\n%s", owner, repo, err, out)
			}
		}
		return cacheDir, nil
	}

	// Clone the repo.
	if err := os.MkdirAll(filepath.Dir(cacheDir), 0o755); err != nil {
		return "", fmt.Errorf("creating cache directory: %w", err)
	}
	url := fmt.Sprintf("https://github.com/%s/%s.git", owner, repo)
	cmd := exec.Command("git", "clone", url, cacheDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("cloning %s/%s: %w\n%s", owner, repo, err, out)
	}

	return cacheDir, nil
}
