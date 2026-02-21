package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/secret"
)

var secretSetValue string

var secretCmd = &cobra.Command{
	Use:   "secret",
	Short: "Manage secrets",
	Long: `Commands for managing klausctl secrets.

Secrets are stored in ~/.config/klausctl/secrets.yaml with owner-only
permissions (0600). They can be referenced by name in instance configs
via secretEnvVars, secretFiles, and mcpServerRefs.`,
}

var secretSetCmd = &cobra.Command{
	Use:   "set <name>",
	Short: "Set a secret value",
	Long: `Store a secret under the given name.

The value can be provided inline with --value or piped via stdin:

  klausctl secret set anthropic-key --value sk-ant-...
  echo "sk-ant-..." | klausctl secret set anthropic-key`,
	Args: cobra.ExactArgs(1),
	RunE: runSecretSet,
}

var secretListCmd = &cobra.Command{
	Use:   "list",
	Short: "List secret names",
	Long:  `List all stored secret names. Values are never displayed.`,
	Args:  cobra.NoArgs,
	RunE:  runSecretList,
}

var secretDeleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Delete a secret",
	Args:  cobra.ExactArgs(1),
	RunE:  runSecretDelete,
}

func init() {
	secretSetCmd.Flags().StringVar(&secretSetValue, "value", "", "secret value (reads from stdin if omitted)")

	secretCmd.AddCommand(secretSetCmd)
	secretCmd.AddCommand(secretListCmd)
	secretCmd.AddCommand(secretDeleteCmd)
	rootCmd.AddCommand(secretCmd)
}

func loadSecretStore() (*secret.Store, error) {
	paths, err := config.DefaultPaths()
	if err != nil {
		return nil, err
	}
	return secret.Load(paths.SecretsFile)
}

func runSecretSet(cmd *cobra.Command, args []string) error {
	name := args[0]

	value := secretSetValue
	if value == "" {
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			value = strings.TrimSpace(scanner.Text())
		}
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("reading from stdin: %w", err)
		}
		if value == "" {
			return fmt.Errorf("no value provided; use --value or pipe via stdin")
		}
	}

	store, err := loadSecretStore()
	if err != nil {
		return err
	}

	if err := store.Set(name, value); err != nil {
		return err
	}
	if err := store.Save(); err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Secret %q saved.\n", name)
	return nil
}

func runSecretList(cmd *cobra.Command, _ []string) error {
	store, err := loadSecretStore()
	if err != nil {
		return err
	}

	names := store.List()
	if len(names) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No secrets stored.")
		return nil
	}

	for _, n := range names {
		fmt.Fprintln(cmd.OutOrStdout(), n)
	}
	return nil
}

func runSecretDelete(cmd *cobra.Command, args []string) error {
	name := args[0]

	store, err := loadSecretStore()
	if err != nil {
		return err
	}

	if err := store.Delete(name); err != nil {
		return err
	}
	if err := store.Save(); err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Secret %q deleted.\n", name)
	return nil
}
