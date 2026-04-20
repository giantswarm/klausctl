package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/remote"
)

var (
	authLoginRemote string
	authLogoutRemote string
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Authenticate with remote klaus-gateway endpoints",
	Long: `Manage OAuth credentials for remote klaus-gateway endpoints used by
'klausctl run/prompt/messages --remote=URL'.

Credentials are stored under ~/.config/klausctl/auth/<host>.yaml with file
mode 0600 and refreshed automatically on 401 responses.`,
}

var authLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Log in to a remote klaus-gateway via OAuth",
	Long: `Perform browser-based OAuth login for the given klaus-gateway URL.

The gateway is probed for OAuth metadata, a browser window is opened for
authentication, and the resulting credentials are persisted locally.

  klausctl auth login --remote=https://gw.example.com`,
	Args: cobra.NoArgs,
	RunE: runAuthLogin,
}

var authLogoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Remove stored credentials for a remote klaus-gateway",
	Args:  cobra.NoArgs,
	RunE:  runAuthLogout,
}

var authStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show authentication status for remote klaus-gateway endpoints",
	Args:  cobra.NoArgs,
	RunE:  runAuthStatus,
}

func init() {
	authLoginCmd.Flags().StringVar(&authLoginRemote, "remote", "", "remote klaus-gateway URL (required)")
	_ = authLoginCmd.MarkFlagRequired("remote")

	authLogoutCmd.Flags().StringVar(&authLogoutRemote, "remote", "", "remote klaus-gateway URL (required)")
	_ = authLogoutCmd.MarkFlagRequired("remote")

	authCmd.AddCommand(authLoginCmd)
	authCmd.AddCommand(authLogoutCmd)
	authCmd.AddCommand(authStatusCmd)
	rootCmd.AddCommand(authCmd)
}

func runAuthLogin(cmd *cobra.Command, _ []string) error {
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}

	store := remote.NewAuthStore(paths.AuthDir)
	rec, err := remote.Login(cmd.Context(), store, authLoginRemote, remote.LoginOptions{})
	if err != nil {
		return fmt.Errorf("login failed for %s: %w", authLoginRemote, err)
	}

	out := cmd.OutOrStdout()
	if !rec.ExpiresAt.IsZero() {
		fmt.Fprintf(out, "Login successful for %s. Token expires at %s.\n", rec.ServerURL, rec.ExpiresAt.Format(time.RFC3339))
	} else {
		fmt.Fprintf(out, "Login successful for %s.\n", rec.ServerURL)
	}
	return nil
}

func runAuthLogout(cmd *cobra.Command, _ []string) error {
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}

	normURL, err := remote.NormalizeBaseURL(authLogoutRemote)
	if err != nil {
		return err
	}

	store := remote.NewAuthStore(paths.AuthDir)
	if err := store.Delete(normURL); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Logged out from %s.\n", normURL)
	return nil
}

func runAuthStatus(cmd *cobra.Command, _ []string) error {
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}

	store := remote.NewAuthStore(paths.AuthDir)
	records, err := store.List()
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	if len(records) == 0 {
		fmt.Fprintln(out, "No remote klaus-gateway credentials stored.")
		return nil
	}

	for _, rec := range records {
		state := "valid"
		if rec.IsExpired(0) {
			state = "expired"
		}
		fmt.Fprintf(out, "%s  [%s]", rec.ServerURL, state)
		if !rec.ExpiresAt.IsZero() {
			fmt.Fprintf(out, "  expires=%s", rec.ExpiresAt.Format(time.RFC3339))
		}
		fmt.Fprintln(out)
	}
	return nil
}
