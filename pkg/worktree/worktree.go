// Package worktree manages cloned workspaces for instance isolation.
// When a workspace path is a git repository, a local clone is created
// so each instance gets its own self-contained .git directory that
// works inside containers without additional volume mounts.
package worktree

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// IsGitRepo reports whether the given directory is a git repository root
// (has a .git directory). Clones (.git file pointing elsewhere) are not
// considered since they are not suitable as clone sources.
func IsGitRepo(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ".git"))
	if err != nil {
		return false
	}
	return info.IsDir()
}

// DefaultBranch returns the default branch of the remote "origin" in the given
// git repository. It runs `git symbolic-ref` and parses the HEAD branch.
// Falls back to "main" if the default branch cannot be determined.
func DefaultBranch(repoDir string) (string, error) {
	cmd := exec.Command("git", "symbolic-ref", "refs/remotes/origin/HEAD")
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err == nil {
		ref := strings.TrimSpace(string(out))
		// refs/remotes/origin/main -> main
		if branch, ok := strings.CutPrefix(ref, "refs/remotes/origin/"); ok {
			return branch, nil
		}
	}

	// Fallback: try ls-remote to determine HEAD.
	cmd = exec.Command("git", "ls-remote", "--symref", "origin", "HEAD")
	cmd.Dir = repoDir
	out, err = cmd.Output()
	if err != nil {
		return "main", nil
	}
	// Output format: "ref: refs/heads/main\tHEAD\n..."
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "ref:") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				ref := parts[1]
				if branch, ok := strings.CutPrefix(ref, "refs/heads/"); ok {
					return branch, nil
				}
			}
		}
	}

	return "main", nil
}

// upstreamURL returns the URL of the "origin" remote in the given repository.
func upstreamURL(repoDir string) (string, error) {
	cmd := exec.Command("git", "remote", "get-url", "origin")
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git remote get-url origin: %w", err)
	}
	url := strings.TrimSpace(string(out))
	if url == "" {
		return "", fmt.Errorf("origin remote URL is empty")
	}
	return url, nil
}

// Create creates a local clone at clonePath from the given repository
// directory. It uses `git clone --local --no-checkout` for efficiency
// (hardlinks objects on the same filesystem), then checks out a detached
// HEAD at origin/<default-branch> and fixes up the origin remote URL to
// point at the upstream remote rather than the local repo path.
//
// The resulting clone has a self-contained .git directory that works
// inside containers without additional volume mounts.
func Create(repoDir, clonePath string) error {
	// Fetch latest from origin before cloning.
	fetchCmd := exec.Command("git", "fetch", "origin")
	fetchCmd.Dir = repoDir
	var fetchErr bytes.Buffer
	fetchCmd.Stderr = &fetchErr
	if err := fetchCmd.Run(); err != nil {
		return fmt.Errorf("git fetch origin: %s: %w", strings.TrimSpace(fetchErr.String()), err)
	}

	// Get the upstream remote URL before cloning (--local sets origin to the
	// local path, so we need the real upstream URL to fix it up afterward).
	upstream, err := upstreamURL(repoDir)
	if err != nil {
		return fmt.Errorf("determining upstream URL: %w", err)
	}

	branch, err := DefaultBranch(repoDir)
	if err != nil {
		return fmt.Errorf("determining default branch: %w", err)
	}

	// Clone locally with --no-checkout so we can detach at the right ref.
	cloneCmd := exec.Command("git", "clone", "--local", "--no-checkout", repoDir, clonePath)
	var cloneErr bytes.Buffer
	cloneCmd.Stderr = &cloneErr
	if err := cloneCmd.Run(); err != nil {
		return fmt.Errorf("git clone --local %s: %s: %w", repoDir, strings.TrimSpace(cloneErr.String()), err)
	}

	// Checkout detached HEAD at origin/<default-branch>.
	ref := "origin/" + branch
	checkoutCmd := exec.Command("git", "checkout", "--detach", ref)
	checkoutCmd.Dir = clonePath
	var checkoutErr bytes.Buffer
	checkoutCmd.Stderr = &checkoutErr
	if err := checkoutCmd.Run(); err != nil {
		// Clean up the partial clone on failure.
		_ = os.RemoveAll(clonePath)
		return fmt.Errorf("git checkout --detach %s: %s: %w", ref, strings.TrimSpace(checkoutErr.String()), err)
	}

	// Fix up the origin remote to point at the real upstream, not the local path.
	setURLCmd := exec.Command("git", "remote", "set-url", "origin", upstream)
	setURLCmd.Dir = clonePath
	var setURLErr bytes.Buffer
	setURLCmd.Stderr = &setURLErr
	if err := setURLCmd.Run(); err != nil {
		_ = os.RemoveAll(clonePath)
		return fmt.Errorf("git remote set-url origin %s: %s: %w", upstream, strings.TrimSpace(setURLErr.String()), err)
	}

	return nil
}

// Remove removes a cloned workspace by deleting its directory.
// The repoDir parameter is accepted for backward compatibility but ignored,
// since clones are self-contained and do not require cleanup in the source repo.
func Remove(repoDir, clonePath string) error {
	_ = repoDir // kept for backward compatibility; clones are self-contained
	return os.RemoveAll(clonePath)
}
