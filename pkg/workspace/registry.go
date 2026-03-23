// Package workspace manages a registry of GitHub repositories and a local
// clone cache. The registry maps owner/repo identifiers to locally managed
// bare clones, providing the foundation for klausctl-managed workspaces
// that don't rely on users' personal checkouts.
package workspace

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// RepoEntry represents a single repository in the workspace registry.
type RepoEntry struct {
	Name string `yaml:"name"` // owner/repo format
}

// WorkspaceConfig holds the workspace registry configuration, serialized as
// workspaces.yaml. Organizations grant access to all repos under an owner;
// Repos grants access to individual repositories.
type WorkspaceConfig struct {
	Organizations []string    `yaml:"organizations,omitempty"`
	Repos         []RepoEntry `yaml:"repos,omitempty"`
}

// CachedRepo describes a locally cached repository clone.
type CachedRepo struct {
	Owner      string
	Repo       string
	Identifier string // "owner/repo"
}

// Load reads a WorkspaceConfig from the given path. If the file does not
// exist, an empty config is returned without error.
func Load(path string) (*WorkspaceConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &WorkspaceConfig{}, nil
		}
		return nil, fmt.Errorf("reading workspace config: %w", err)
	}
	var cfg WorkspaceConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing workspace config: %w", err)
	}
	return &cfg, nil
}

// Save writes a WorkspaceConfig to the given path, creating parent
// directories as needed.
func Save(path string, cfg *WorkspaceConfig) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling workspace config: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing workspace config: %w", err)
	}
	return nil
}

// IsRepoIdentifier reports whether workspace looks like an owner/repo
// identifier rather than a filesystem path. A repo identifier does NOT
// start with '/', '~', or '.' and contains exactly one '/'.
func IsRepoIdentifier(workspace string) bool {
	if workspace == "" {
		return false
	}
	if workspace[0] == '/' || workspace[0] == '~' || workspace[0] == '.' {
		return false
	}
	return strings.Count(workspace, "/") == 1
}

// IsAllowed reports whether the given owner/repo is permitted by the
// workspace config. A repo is allowed if its owner appears in the
// Organizations list or the full "owner/repo" appears in the Repos list.
func IsAllowed(cfg *WorkspaceConfig, owner, repo string) bool {
	for _, org := range cfg.Organizations {
		if strings.EqualFold(org, owner) {
			return true
		}
	}
	identifier := owner + "/" + repo
	for _, r := range cfg.Repos {
		if strings.EqualFold(r.Name, identifier) {
			return true
		}
	}
	return false
}

// EnsureCached ensures a bare clone of the repository exists at
// reposDir/owner/repo/. If the clone does not exist it is created with
// `git clone --no-checkout`. If it exists and noFetch is false, origin is
// fetched to update remote refs. The returned path is the cache directory.
func EnsureCached(reposDir, owner, repo string, noFetch bool) (string, error) {
	cacheDir := filepath.Join(reposDir, owner, repo)
	gitDir := filepath.Join(cacheDir, ".git")

	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		// Clone from GitHub with --no-checkout so no working tree files
		// are created, but the full .git/ with refs/remotes/origin/* exists.
		url := "https://github.com/" + owner + "/" + repo + ".git"
		if err := os.MkdirAll(filepath.Dir(cacheDir), 0o755); err != nil {
			return "", fmt.Errorf("creating cache parent directory: %w", err)
		}
		cmd := exec.Command("git", "clone", "--no-checkout", url, cacheDir)
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("git clone --no-checkout %s: %s: %w", url, strings.TrimSpace(string(out)), err)
		}
		return cacheDir, nil
	} else if err != nil {
		return "", fmt.Errorf("checking cache directory: %w", err)
	}

	// Cache exists; optionally fetch.
	if !noFetch {
		if err := FetchRepo(reposDir, owner, repo); err != nil {
			return "", err
		}
	}
	return cacheDir, nil
}

// FetchRepo runs `git fetch origin` in the cached clone at
// reposDir/owner/repo/.
func FetchRepo(reposDir, owner, repo string) error {
	cacheDir := filepath.Join(reposDir, owner, repo)
	cmd := exec.Command("git", "fetch", "origin")
	cmd.Dir = cacheDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git fetch origin in %s/%s: %s: %w", owner, repo, strings.TrimSpace(string(out)), err)
	}
	return nil
}

// FetchAll walks reposDir and fetches origin for every cached repository.
func FetchAll(reposDir string) error {
	repos, err := ListCached(reposDir)
	if err != nil {
		return err
	}
	for _, r := range repos {
		if err := FetchRepo(reposDir, r.Owner, r.Repo); err != nil {
			return err
		}
	}
	return nil
}

// ListCached walks reposDir looking for <owner>/<repo>/ directories that
// contain a .git directory, and returns them as CachedRepo entries.
func ListCached(reposDir string) ([]CachedRepo, error) {
	owners, err := os.ReadDir(reposDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading repos directory: %w", err)
	}

	var repos []CachedRepo
	for _, ownerEntry := range owners {
		if !ownerEntry.IsDir() {
			continue
		}
		ownerName := ownerEntry.Name()
		ownerPath := filepath.Join(reposDir, ownerName)
		repoEntries, err := os.ReadDir(ownerPath)
		if err != nil {
			return nil, fmt.Errorf("reading owner directory %s: %w", ownerName, err)
		}
		for _, repoEntry := range repoEntries {
			if !repoEntry.IsDir() {
				continue
			}
			repoName := repoEntry.Name()
			gitDir := filepath.Join(ownerPath, repoName, ".git")
			info, err := os.Stat(gitDir)
			if err != nil || !info.IsDir() {
				continue
			}
			repos = append(repos, CachedRepo{
				Owner:      ownerName,
				Repo:       repoName,
				Identifier: ownerName + "/" + repoName,
			})
		}
	}
	return repos, nil
}
