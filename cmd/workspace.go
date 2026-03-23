package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/workspace"
)

var workspaceCmd = &cobra.Command{
	Use:   "workspace",
	Short: "Manage workspace registry and repo cache",
	Long: `Commands for managing the workspace registry of GitHub organizations and
repositories, and the local clone cache used by klausctl workspaces.

Configuration is stored in: ~/.config/klausctl/workspaces.yaml
Cached clones live under:   ~/.config/klausctl/repos/`,
}

var workspaceListCmd = &cobra.Command{
	Use:   "list",
	Short: "List registered orgs, repos, and cached clones",
	Args:  cobra.NoArgs,
	RunE:  runWorkspaceList,
}

var workspaceAddOrgCmd = &cobra.Command{
	Use:   "add-org <org>",
	Short: "Register a GitHub organization",
	Args:  cobra.ExactArgs(1),
	RunE:  runWorkspaceAddOrg,
}

var workspaceAddRepoCmd = &cobra.Command{
	Use:   "add-repo <owner/repo>",
	Short: "Register a specific repository",
	Args:  cobra.ExactArgs(1),
	RunE:  runWorkspaceAddRepo,
}

var workspaceRemoveCmd = &cobra.Command{
	Use:   "remove <identifier>",
	Short: "Unregister an organization or repository",
	Long: `Remove an organization or repository from the workspace registry.

If the identifier contains a "/" it is treated as a repository (owner/repo).
Otherwise it is treated as an organization name.`,
	Args: cobra.ExactArgs(1),
	RunE: runWorkspaceRemove,
}

var workspaceFetchCmd = &cobra.Command{
	Use:   "fetch [owner/repo]",
	Short: "Fetch latest from remote for cached repos",
	Long: `Fetch the latest commits from origin for cached repositories.

Without arguments, all cached repos are fetched. With a specific owner/repo
argument, only that repository is fetched.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runWorkspaceFetch,
}

func init() {
	workspaceCmd.AddCommand(workspaceListCmd)
	workspaceCmd.AddCommand(workspaceAddOrgCmd)
	workspaceCmd.AddCommand(workspaceAddRepoCmd)
	workspaceCmd.AddCommand(workspaceRemoveCmd)
	workspaceCmd.AddCommand(workspaceFetchCmd)
	rootCmd.AddCommand(workspaceCmd)
}

func loadWorkspacePaths() (*config.Paths, error) {
	return config.DefaultPaths()
}

func loadWorkspaceConfig(paths *config.Paths) (*workspace.WorkspaceConfig, error) {
	return workspace.Load(paths.WorkspacesFile)
}

func runWorkspaceList(cmd *cobra.Command, _ []string) error {
	paths, err := loadWorkspacePaths()
	if err != nil {
		return err
	}

	cfg, err := loadWorkspaceConfig(paths)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()

	fmt.Fprintln(out, "Organizations:")
	if len(cfg.Organizations) == 0 {
		fmt.Fprintln(out, "  No organizations registered.")
	} else {
		for _, org := range cfg.Organizations {
			fmt.Fprintf(out, "  %s\n", org)
		}
	}

	fmt.Fprintln(out)

	cached, err := workspace.ListCached(paths.ReposDir)
	if err != nil {
		return err
	}

	fmt.Fprintln(out, "Cached repos:")
	if len(cached) == 0 {
		fmt.Fprintln(out, "  No cached repos.")
	} else {
		for _, r := range cached {
			fmt.Fprintf(out, "  %s\n", r.Identifier)
		}
	}

	return nil
}

func runWorkspaceAddOrg(cmd *cobra.Command, args []string) error {
	org := args[0]

	paths, err := loadWorkspacePaths()
	if err != nil {
		return err
	}

	cfg, err := loadWorkspaceConfig(paths)
	if err != nil {
		return err
	}

	for _, existing := range cfg.Organizations {
		if strings.EqualFold(existing, org) {
			fmt.Fprintf(cmd.OutOrStdout(), "Organization %q is already registered.\n", org)
			return nil
		}
	}

	cfg.Organizations = append(cfg.Organizations, org)
	if err := workspace.Save(paths.WorkspacesFile, cfg); err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Added organization: %s\n", org)
	return nil
}

func runWorkspaceAddRepo(cmd *cobra.Command, args []string) error {
	identifier := args[0]

	if strings.Count(identifier, "/") != 1 {
		return fmt.Errorf("invalid repo format %q: expected owner/repo", identifier)
	}

	paths, err := loadWorkspacePaths()
	if err != nil {
		return err
	}

	cfg, err := loadWorkspaceConfig(paths)
	if err != nil {
		return err
	}

	for _, existing := range cfg.Repos {
		if strings.EqualFold(existing.Name, identifier) {
			fmt.Fprintf(cmd.OutOrStdout(), "Repository %q is already registered.\n", identifier)
			return nil
		}
	}

	cfg.Repos = append(cfg.Repos, workspace.RepoEntry{Name: identifier})
	if err := workspace.Save(paths.WorkspacesFile, cfg); err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Added repository: %s\n", identifier)
	return nil
}

func runWorkspaceRemove(cmd *cobra.Command, args []string) error {
	identifier := args[0]

	paths, err := loadWorkspacePaths()
	if err != nil {
		return err
	}

	cfg, err := loadWorkspaceConfig(paths)
	if err != nil {
		return err
	}

	if strings.Contains(identifier, "/") {
		for i, r := range cfg.Repos {
			if strings.EqualFold(r.Name, identifier) {
				cfg.Repos = append(cfg.Repos[:i], cfg.Repos[i+1:]...)
				if err := workspace.Save(paths.WorkspacesFile, cfg); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Removed: %s\n", identifier)
				return nil
			}
		}
		return fmt.Errorf("repository %q not found in workspace config", identifier)
	}

	for i, org := range cfg.Organizations {
		if strings.EqualFold(org, identifier) {
			cfg.Organizations = append(cfg.Organizations[:i], cfg.Organizations[i+1:]...)
			if err := workspace.Save(paths.WorkspacesFile, cfg); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Removed: %s\n", identifier)
			return nil
		}
	}
	return fmt.Errorf("organization %q not found in workspace config", identifier)
}

func runWorkspaceFetch(cmd *cobra.Command, args []string) error {
	paths, err := loadWorkspacePaths()
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()

	if len(args) == 1 {
		identifier := args[0]
		if strings.Count(identifier, "/") != 1 {
			return fmt.Errorf("invalid repo format %q: expected owner/repo", identifier)
		}
		parts := strings.SplitN(identifier, "/", 2)
		owner, repo := parts[0], parts[1]

		fmt.Fprintf(out, "Fetching %s/%s...\n", owner, repo)
		_, err := workspace.EnsureCached(paths.ReposDir, owner, repo, false)
		return err
	}

	cfg, err := loadWorkspaceConfig(paths)
	if err != nil {
		return err
	}

	if len(cfg.Repos) == 0 {
		fmt.Fprintln(out, "No repos registered in workspace config.")
		return nil
	}

	for _, r := range cfg.Repos {
		repoParts := strings.SplitN(r.Name, "/", 2)
		if len(repoParts) != 2 {
			continue
		}
		fmt.Fprintf(out, "Fetching %s...\n", r.Name)
		if _, err := workspace.EnsureCached(paths.ReposDir, repoParts[0], repoParts[1], false); err != nil {
			return err
		}
	}

	return nil
}
