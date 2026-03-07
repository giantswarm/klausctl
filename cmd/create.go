package cmd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/instance"
	"github.com/giantswarm/klausctl/pkg/orchestrator"
	"github.com/giantswarm/klausctl/pkg/worktree"
)

var (
	createPersonality       string
	createToolchain         string
	createPlugins           []string
	createPort              int
	createEnv               []string
	createEnvForward        []string
	createSecretEnv         []string
	createSecretFile        []string
	createMcpServer         []string
	createPermMode          string
	createModel             string
	createSystemPrompt      string
	createMaxBudget         float64
	createSource            string
	createNoIsolate         bool
	createGitAuthor         string
	createGitCredHelper     string
	createGitHTTPSInsteadOf bool
	createYes               bool
	createForce             bool
	createGenerateSuffix    bool
)

var createCmd = &cobra.Command{
	Use:   "create <name> [workspace]",
	Short: "Create and start a named klaus instance",
	Long: `Create and start a named klaus instance.

Override flags (--env, --env-forward, --permission-mode, --model, etc.) are
applied on top of any values defined by the resolved personality. Map-like
fields (envVars, envForward) are merged; scalar fields (model, permissionMode,
systemPrompt, maxBudget) replace the personality default.

By default, a random 4-character suffix is appended to the instance name to
avoid collisions (e.g. "myproject" becomes "myproject-a3f7"). Use
--no-generate-suffix to preserve the exact name.

If a name collision occurs with a stopped instance, you will be prompted to
replace it (use -y to auto-confirm). If the collision is with a running
instance, the command aborts unless --force is used.

MCP server configurations can be supplied via the MCP tool interface
(mcpServers parameter) or by editing the instance config file directly.`,
	Args: cobra.RangeArgs(1, 2),
	RunE: runCreate,
}

func init() {
	createCmd.Flags().StringVar(&createPersonality, "personality", "", "personality short name or OCI reference")
	createCmd.Flags().StringVar(&createToolchain, "toolchain", "", "toolchain short name or OCI reference")
	createCmd.Flags().StringSliceVar(&createPlugins, "plugin", nil, "additional plugin short name or OCI reference (repeatable)")
	createCmd.Flags().IntVar(&createPort, "port", 0, "override auto-selected port")
	createCmd.Flags().StringArrayVar(&createEnv, "env", nil, "environment variable KEY=VALUE (repeatable)")
	createCmd.Flags().StringArrayVar(&createEnvForward, "env-forward", nil, "host environment variable name to forward (repeatable)")
	createCmd.Flags().StringVar(&createPermMode, "permission-mode", "", "Claude permission mode: default, acceptEdits, bypassPermissions, dontAsk, plan, delegate")
	createCmd.Flags().StringVar(&createModel, "model", "", "Claude model (e.g. sonnet, opus)")
	createCmd.Flags().StringVar(&createSystemPrompt, "system-prompt", "", "system prompt override for the Claude agent")
	createCmd.Flags().Float64Var(&createMaxBudget, "max-budget", 0, "maximum dollar budget per invocation (0 = no limit)")
	createCmd.Flags().StringArrayVar(&createSecretEnv, "secret-env", nil, "secret env var ENV_NAME=secret-name (repeatable)")
	createCmd.Flags().StringArrayVar(&createSecretFile, "secret-file", nil, "secret file /container/path=secret-name (repeatable)")
	createCmd.Flags().StringArrayVar(&createMcpServer, "mcpserver", nil, "managed MCP server name (repeatable)")
	createCmd.Flags().StringVar(&createSource, "source", "", "resolve artifact short names against a specific source")
	createCmd.Flags().BoolVar(&createNoIsolate, "no-isolate", false, "skip git worktree creation and bind-mount workspace directly")
	createCmd.Flags().StringVar(&createGitAuthor, "git-author", "", `git author identity "Name <email>"`)
	createCmd.Flags().StringVar(&createGitCredHelper, "git-credential-helper", "", "git credential helper (currently only 'gh')")
	createCmd.Flags().BoolVar(&createGitHTTPSInsteadOf, "git-https-instead-of-ssh", false, "rewrite SSH git URLs to HTTPS via container-local gitconfig")
	createCmd.Flags().BoolVarP(&createYes, "yes", "y", false, "auto-confirm replacement of existing instances")
	createCmd.Flags().BoolVar(&createForce, "force", false, "allow replacing a running instance (prompts for confirmation unless -y is also set)")
	createCmd.Flags().BoolVar(&createGenerateSuffix, "generate-suffix", true, "append a random 4-character suffix to the instance name (use --no-generate-suffix to disable)")
	rootCmd.AddCommand(createCmd)
}

func runCreate(cmd *cobra.Command, args []string) (retErr error) {
	baseName := args[0]
	if err := config.ValidateInstanceName(baseName); err != nil {
		return err
	}

	instanceName := baseName
	if createGenerateSuffix {
		suffixed, err := instance.AppendSuffix(baseName)
		if err != nil {
			return fmt.Errorf("generating name suffix: %w", err)
		}
		instanceName = suffixed
	}

	if err := config.ValidateInstanceName(instanceName); err != nil {
		return err
	}

	workspace := ""
	if len(args) > 1 {
		workspace = args[1]
	} else {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("determining current directory: %w", err)
		}
		workspace = cwd
	}

	ctx := context.Background()

	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	if err := config.MigrateLayout(paths); err != nil {
		return fmt.Errorf("migrating config layout: %w", err)
	}

	instancePaths := paths.ForInstance(instanceName)

	// Check for name collision with an existing instance.
	collision, err := instance.CheckCollision(ctx, instancePaths)
	if err != nil {
		return fmt.Errorf("checking for existing instance: %w", err)
	}

	if err := handleCLICollision(cmd, instanceName, collision, ctx, instancePaths); err != nil {
		return err
	}

	resolver, err := buildSourceResolver(createSource)
	if err != nil {
		return err
	}

	if createSource != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "Using source %q for artifact resolution\n", createSource)
	}

	personality, toolchain, plugins, err := orchestrator.ResolveCreateRefs(ctx, resolver, createPersonality, createToolchain, createPlugins)
	if err != nil {
		return err
	}

	envVars, err := parseEnvFlags(createEnv)
	if err != nil {
		return err
	}

	secretEnvVars, err := parseEnvFlags(createSecretEnv)
	if err != nil {
		return fmt.Errorf("parsing --secret-env: %w", err)
	}

	secretFiles, err := parseEnvFlags(createSecretFile)
	if err != nil {
		return fmt.Errorf("parsing --secret-file: %w", err)
	}

	gitName, gitEmail, err := parseGitAuthor(createGitAuthor)
	if err != nil {
		return err
	}

	opts := config.CreateOptions{
		Name:                 instanceName,
		Workspace:            workspace,
		NoIsolate:            createNoIsolate,
		Personality:          personality,
		Toolchain:            toolchain,
		Plugins:              plugins,
		Port:                 createPort,
		GitAuthorName:        gitName,
		GitAuthorEmail:       gitEmail,
		GitCredentialHelper:  createGitCredHelper,
		GitHTTPSInsteadOfSSH: createGitHTTPSInsteadOf,
		EnvVars:              envVars,
		EnvForward:           createEnvForward,
		SecretEnvVars:        secretEnvVars,
		SecretFiles:          secretFiles,
		McpServerRefs:        createMcpServer,
		PermissionMode:       createPermMode,
		Model:                createModel,
		SystemPrompt:         createSystemPrompt,
		SourceResolver:       resolver,
		Context:              ctx,
		Output:               cmd.OutOrStdout(),
		ResolvePersonality: func(ctx context.Context, ref string, outWriter io.Writer) (*config.ResolvedPersonality, error) {
			if err := config.EnsureDir(paths.PersonalitiesDir); err != nil {
				return nil, fmt.Errorf("creating personalities directory: %w", err)
			}
			client := orchestrator.NewDefaultClient()
			pr, err := orchestrator.ResolvePersonality(ctx, client, ref, paths.PersonalitiesDir, outWriter)
			if err != nil {
				return nil, err
			}

			plugins, err := orchestrator.ResolvePluginRefs(ctx, client, pr.Spec.Plugins)
			if err != nil {
				return nil, fmt.Errorf("resolving personality plugins: %w", err)
			}

			image, err := client.ResolveToolchainRef(ctx, pr.Spec.Toolchain.Ref())
			if err != nil {
				return nil, fmt.Errorf("resolving personality image: %w", err)
			}

			return &config.ResolvedPersonality{
				Plugins: plugins,
				Image:   image,
			}, nil
		},
	}
	if cmd.Flags().Changed("max-budget") {
		opts.MaxBudgetUSD = &createMaxBudget
	}

	cfg, err := config.GenerateInstanceConfig(paths, opts)
	if err != nil {
		return err
	}

	if err := config.EnsureDir(instancePaths.InstanceDir); err != nil {
		return fmt.Errorf("creating instance directory: %w", err)
	}

	// Clean up the instance directory if any subsequent step fails. This
	// prevents leftover config/state that would block a retry of create.
	defer func() {
		if retErr != nil {
			_ = os.RemoveAll(instancePaths.InstanceDir)
		}
	}()

	// Clean up the worktree if any subsequent step fails. Registered after
	// the instance dir defer so it runs first (LIFO), ensuring git worktree
	// metadata is cleaned up before the directory is removed.
	if cfg.WorktreePath != "" {
		defer func() {
			if retErr != nil {
				_ = worktree.Remove(cfg.Workspace, cfg.WorktreePath)
			}
		}()
	}

	data, err := cfg.Marshal()
	if err != nil {
		return fmt.Errorf("serializing config: %w", err)
	}
	if err := os.WriteFile(instancePaths.ConfigFile, data, 0o644); err != nil {
		return fmt.Errorf("writing instance config: %w", err)
	}

	// Ensure rendered output stays under the instance directory.
	if err := config.EnsureDir(filepath.Dir(instancePaths.RenderedDir)); err != nil {
		return fmt.Errorf("creating rendered directory parent: %w", err)
	}

	if err := startInstance(cmd, instanceName, "", instancePaths.ConfigFile); err != nil {
		return err
	}

	// Print the resolved instance name so callers can reference it.
	// Printed after successful start to avoid advertising a name that
	// was rolled back on failure.
	fmt.Fprintln(cmd.OutOrStdout(), instanceName)
	return nil
}

// handleCLICollision handles name collision for the CLI create command.
// It prompts or errors based on the collision state, --force, and -y flags.
// On confirmation, it fully cleans up the old instance before returning nil.
func handleCLICollision(cmd *cobra.Command, name string, collision instance.CollisionState, ctx context.Context, paths *config.Paths) error {
	switch collision {
	case instance.NoCollision:
		return nil

	case instance.CollisionStopped:
		if !createYes {
			fmt.Fprintf(cmd.OutOrStdout(), "Instance %q already exists (stopped). Replace it? [y/N]: ", name)
			reader := bufio.NewReader(cmd.InOrStdin())
			answer, err := reader.ReadString('\n')
			if err != nil {
				return err
			}
			answer = strings.ToLower(strings.TrimSpace(answer))
			if answer != "y" && answer != "yes" {
				return fmt.Errorf("create cancelled")
			}
		}
		return cleanupExistingInstance(ctx, name, paths)

	case instance.CollisionRunning:
		if !createForce {
			return fmt.Errorf("instance %q is still running; use --force to replace it", name)
		}
		if !createYes {
			fmt.Fprintf(cmd.OutOrStdout(), "Instance %q is still running. Stop and replace it? [y/N]: ", name)
			reader := bufio.NewReader(cmd.InOrStdin())
			answer, err := reader.ReadString('\n')
			if err != nil {
				return err
			}
			answer = strings.ToLower(strings.TrimSpace(answer))
			if answer != "y" && answer != "yes" {
				return fmt.Errorf("create cancelled")
			}
		}
		return cleanupExistingInstance(ctx, name, paths)
	}

	return nil
}

// cleanupExistingInstance fully removes an existing instance: stops the
// container if running, removes the container, cleans up the worktree,
// and deletes the instance directory.
func cleanupExistingInstance(ctx context.Context, name string, paths *config.Paths) error {
	cfg, _ := config.Load(paths.ConfigFile)
	if cfg != nil && cfg.WorktreePath != "" {
		_ = worktree.Remove(cfg.Workspace, cfg.WorktreePath)
	}

	inst, _ := instance.Load(paths)
	if err := cleanupInstanceContainer(ctx, name, inst); err != nil {
		return fmt.Errorf("cleaning up existing instance: %w", err)
	}

	if err := os.RemoveAll(paths.InstanceDir); err != nil {
		return fmt.Errorf("removing existing instance directory: %w", err)
	}

	return nil
}

// parseGitAuthor parses a "Name <email>" string into separate name and email.
// Returns empty strings if the input is empty.
func parseGitAuthor(s string) (name, email string, err error) {
	if s == "" {
		return "", "", nil
	}
	lt := strings.Index(s, "<")
	gt := strings.Index(s, ">")
	if lt < 0 || gt < 0 || gt < lt {
		return "", "", fmt.Errorf("invalid --git-author format %q: expected \"Name <email>\"", s)
	}
	name = strings.TrimSpace(s[:lt])
	email = strings.TrimSpace(s[lt+1 : gt])
	if name == "" || email == "" {
		return "", "", fmt.Errorf("invalid --git-author format %q: name and email must not be empty", s)
	}
	return name, email, nil
}

// parseEnvFlags parses KEY=VALUE pairs from --env flag values into a map.
func parseEnvFlags(envFlags []string) (map[string]string, error) {
	if len(envFlags) == 0 {
		return nil, nil
	}
	m := make(map[string]string, len(envFlags))
	for _, kv := range envFlags {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			return nil, fmt.Errorf("invalid --env value %q: expected KEY=VALUE format", kv)
		}
		m[k] = v
	}
	return m, nil
}
