// Package worktree manages git worktrees for workspace isolation.
// When a workspace path is a git repository, a worktree can be created
// so each instance gets its own working tree while sharing .git/objects.
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
// (has a .git directory). Nested worktrees (.git file) are not considered
// since they cannot be used as the parent repository for new worktrees.
func IsGitRepo(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ".git"))
	if err != nil {
		return false
	}
	return info.IsDir()
}

// DefaultBranch returns the default branch of the remote "origin" in the given
// git repository. It runs `git remote show origin` and parses the HEAD branch.
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

// Create creates a git worktree at worktreePath from the given repository
// directory. It fetches origin, determines the default branch, and creates
// a detached worktree at origin/<default-branch>.
func Create(repoDir, worktreePath string) error {
	// Fetch latest from origin.
	fetchCmd := exec.Command("git", "fetch", "origin")
	fetchCmd.Dir = repoDir
	var fetchErr bytes.Buffer
	fetchCmd.Stderr = &fetchErr
	if err := fetchCmd.Run(); err != nil {
		return fmt.Errorf("git fetch origin: %s: %w", strings.TrimSpace(fetchErr.String()), err)
	}

	branch, err := DefaultBranch(repoDir)
	if err != nil {
		return fmt.Errorf("determining default branch: %w", err)
	}

	// Create the worktree in detached HEAD mode pointing at origin/<branch>.
	ref := "origin/" + branch
	wtCmd := exec.Command("git", "worktree", "add", "--detach", worktreePath, ref)
	wtCmd.Dir = repoDir
	var wtErr bytes.Buffer
	wtCmd.Stderr = &wtErr
	if err := wtCmd.Run(); err != nil {
		return fmt.Errorf("git worktree add %s: %s: %w", worktreePath, strings.TrimSpace(wtErr.String()), err)
	}

	return nil
}

// Remove removes a git worktree. It runs `git worktree remove --force` from
// the parent repository. The repoDir is the original repository (not the
// worktree itself).
func Remove(repoDir, worktreePath string) error {
	cmd := exec.Command("git", "worktree", "remove", "--force", worktreePath)
	cmd.Dir = repoDir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git worktree remove %s: %s: %w", worktreePath, strings.TrimSpace(stderr.String()), err)
	}
	return nil
}
