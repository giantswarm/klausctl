package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/mcpserverstore"
	"github.com/giantswarm/klausctl/pkg/oauth"
)

var mcpserverAddURL string
var mcpserverAddSecret string

var mcpserverCmd = &cobra.Command{
	Use:   "mcpserver",
	Short: "Manage managed MCP servers",
	Long: `Commands for managing global MCP server definitions.

Managed MCP servers are stored in ~/.config/klausctl/mcpservers.yaml.
They can be referenced by name via --mcpserver or mcpServerRefs in instance
configs. At start time, each referenced server is merged into the instance's
mcpServers config with an optional Bearer token from the secrets store.`,
}

var mcpserverAddCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Add a managed MCP server",
	Long: `Register a managed MCP server definition.

  klausctl mcpserver add muster --url https://muster.example.com/mcp
  klausctl mcpserver add muster --url https://muster.example.com/mcp --secret muster-token`,
	Args: cobra.ExactArgs(1),
	RunE: runMcpserverAdd,
}

var mcpserverListCmd = &cobra.Command{
	Use:   "list",
	Short: "List managed MCP servers",
	Args:  cobra.NoArgs,
	RunE:  runMcpserverList,
}

var mcpserverRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Remove a managed MCP server",
	Args:  cobra.ExactArgs(1),
	RunE:  runMcpserverRemove,
}

var mcpserverLoginCmd = &cobra.Command{
	Use:   "login <name>",
	Short: "Authenticate with a managed MCP server via OAuth",
	Long: `Perform browser-based OAuth login for a managed MCP server.

The server is probed for OAuth requirements, a browser window is opened
for authentication, and the resulting token is stored locally.

  klausctl mcpserver login muster-gs`,
	Args: cobra.ExactArgs(1),
	RunE: runMcpserverLogin,
}

var mcpserverAuthStatusCmd = &cobra.Command{
	Use:   "auth-status [name]",
	Short: "Show authentication status for managed MCP servers",
	Long: `Show the OAuth authentication status for managed MCP servers.

Without arguments, shows the status for all servers. With a name argument,
shows detailed status for that specific server.

  klausctl mcpserver auth-status
  klausctl mcpserver auth-status muster-gs`,
	Args: cobra.MaximumNArgs(1),
	RunE: runMcpserverAuthStatus,
}

func init() {
	mcpserverAddCmd.Flags().StringVar(&mcpserverAddURL, "url", "", "MCP server URL (required)")
	_ = mcpserverAddCmd.MarkFlagRequired("url")
	mcpserverAddCmd.Flags().StringVar(&mcpserverAddSecret, "secret", "", "secret name for Bearer token authentication")

	mcpserverCmd.AddCommand(mcpserverAddCmd)
	mcpserverCmd.AddCommand(mcpserverListCmd)
	mcpserverCmd.AddCommand(mcpserverRemoveCmd)
	mcpserverCmd.AddCommand(mcpserverLoginCmd)
	mcpserverCmd.AddCommand(mcpserverAuthStatusCmd)
	rootCmd.AddCommand(mcpserverCmd)
}

func loadMcpServerStore() (*mcpserverstore.Store, error) {
	paths, err := config.DefaultPaths()
	if err != nil {
		return nil, err
	}
	return mcpserverstore.Load(paths.McpServersFile)
}

func runMcpserverAdd(cmd *cobra.Command, args []string) error {
	name := args[0]

	store, err := loadMcpServerStore()
	if err != nil {
		return err
	}

	store.Add(name, mcpserverstore.McpServerDef{
		URL:    mcpserverAddURL,
		Secret: mcpserverAddSecret,
	})

	if err := store.Save(); err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "MCP server %q added.\n", name)
	return nil
}

func runMcpserverList(cmd *cobra.Command, _ []string) error {
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}

	store, err := mcpserverstore.Load(paths.McpServersFile)
	if err != nil {
		return err
	}

	names := store.List()
	if len(names) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No managed MCP servers configured.")
		return nil
	}

	tokenStore := oauth.NewTokenStore(paths.TokensDir)
	all := store.All()
	for _, name := range names {
		def := all[name]
		auth := authLabel(def, tokenStore)
		fmt.Fprintf(cmd.OutOrStdout(), "%s  %s  [%s]\n", name, def.URL, auth)
	}
	return nil
}

func authLabel(def mcpserverstore.McpServerDef, tokenStore *oauth.TokenStore) string {
	if def.Secret != "" {
		return "secret"
	}
	st := tokenStore.GetToken(def.URL)
	if st == nil {
		return "-"
	}
	if st.IsExpired() {
		return "oauth (expired)"
	}
	return "oauth"
}

func runMcpserverRemove(cmd *cobra.Command, args []string) error {
	name := args[0]

	store, err := loadMcpServerStore()
	if err != nil {
		return err
	}

	if err := store.Remove(name); err != nil {
		return err
	}
	if err := store.Save(); err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "MCP server %q removed.\n", name)
	return nil
}

func runMcpserverLogin(cmd *cobra.Command, args []string) error {
	name := args[0]

	store, err := loadMcpServerStore()
	if err != nil {
		return err
	}

	def, err := store.Get(name)
	if err != nil {
		return err
	}

	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}

	tokenStore := oauth.NewTokenStore(paths.TokensDir)
	client := oauth.NewClient(tokenStore)

	if err := client.Login(cmd.Context(), def.URL); err != nil {
		return fmt.Errorf("login failed for %q: %w", name, err)
	}

	st := tokenStore.GetToken(def.URL)
	if st != nil && st.Token.ExpiresIn > 0 {
		expiry := st.CreatedAt.Add(time.Duration(st.Token.ExpiresIn) * time.Second)
		fmt.Fprintf(cmd.OutOrStdout(), "Login successful for %q. Token expires at %s.\n", name, expiry.Format(time.RFC3339))
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "Login successful for %q.\n", name)
	}

	return nil
}

func runMcpserverAuthStatus(cmd *cobra.Command, args []string) error {
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}

	store, err := mcpserverstore.Load(paths.McpServersFile)
	if err != nil {
		return err
	}

	tokenStore := oauth.NewTokenStore(paths.TokensDir)

	if len(args) == 1 {
		return printServerAuthStatus(cmd, store, tokenStore, args[0])
	}

	return printAllAuthStatus(cmd, store, tokenStore)
}

func printServerAuthStatus(cmd *cobra.Command, store *mcpserverstore.Store, tokenStore *oauth.TokenStore, name string) error {
	def, err := store.Get(name)
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "Server:  %s\n", name)
	fmt.Fprintf(w, "URL:     %s\n", def.URL)

	if def.Secret != "" {
		fmt.Fprintf(w, "Auth:    secret (%s)\n", def.Secret)
		return nil
	}

	st := tokenStore.GetToken(def.URL)
	if st == nil {
		fmt.Fprintf(w, "Auth:    none\n")
		return nil
	}

	if st.IsExpired() {
		fmt.Fprintf(w, "Auth:    oauth (expired)\n")
	} else {
		fmt.Fprintf(w, "Auth:    oauth (valid)\n")
	}

	fmt.Fprintf(w, "Issuer:  %s\n", st.Issuer)

	if st.Token.ExpiresIn > 0 {
		expiry := st.CreatedAt.Add(time.Duration(st.Token.ExpiresIn) * time.Second)
		fmt.Fprintf(w, "Expires: %s\n", expiry.Format(time.RFC3339))
	}

	return nil
}

func printAllAuthStatus(cmd *cobra.Command, store *mcpserverstore.Store, tokenStore *oauth.TokenStore) error {
	names := store.List()
	if len(names) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No managed MCP servers configured.")
		return nil
	}

	all := store.All()
	for _, name := range names {
		def := all[name]
		auth := authLabel(def, tokenStore)
		fmt.Fprintf(cmd.OutOrStdout(), "%s  %s  [%s]\n", name, def.URL, auth)
	}
	return nil
}
