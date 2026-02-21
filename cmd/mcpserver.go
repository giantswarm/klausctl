package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/mcpserverstore"
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

func init() {
	mcpserverAddCmd.Flags().StringVar(&mcpserverAddURL, "url", "", "MCP server URL (required)")
	_ = mcpserverAddCmd.MarkFlagRequired("url")
	mcpserverAddCmd.Flags().StringVar(&mcpserverAddSecret, "secret", "", "secret name for Bearer token authentication")

	mcpserverCmd.AddCommand(mcpserverAddCmd)
	mcpserverCmd.AddCommand(mcpserverListCmd)
	mcpserverCmd.AddCommand(mcpserverRemoveCmd)
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
	store, err := loadMcpServerStore()
	if err != nil {
		return err
	}

	names := store.List()
	if len(names) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No managed MCP servers configured.")
		return nil
	}

	all := store.All()
	for _, name := range names {
		def := all[name]
		if def.Secret != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "%s  %s  (secret: %s)\n", name, def.URL, def.Secret)
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "%s  %s\n", name, def.URL)
		}
	}
	return nil
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
