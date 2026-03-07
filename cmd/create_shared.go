package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/instance"
	"github.com/giantswarm/klausctl/pkg/orchestrator"
	"github.com/giantswarm/klausctl/pkg/worktree"
)

// CLICreateParams holds the flag values and resolved inputs for creating an
// instance via the CLI. Both create and run populate this from their flags.
type CLICreateParams struct {
	BaseName        string
	Workspace       string
	Personality     string
	Toolchain       string
	Plugins         []string
	Port            int
	Env             []string
	EnvForward      []string
	SecretEnv       []string
	SecretFile      []string
	McpServer       []string
	PermMode        string
	Model           string
	SystemPrompt    string
	MaxBudget       float64
	MaxBudgetSet    bool
	Source          string
	NoIsolate       bool
	GitAuthor       string
	GitCredHelper   string
	GitHTTPSInstead bool
	Yes             bool
	Force           bool
	GenerateSuffix  bool
}

// cliCreateInstance creates and starts a new instance from CLI parameters.
// It handles name suffix generation, collision detection, config generation,
// directory setup, and starting the container. On error, it cleans up any
// partially-created state. Returns the resolved instance name.
func cliCreateInstance(ctx context.Context, cmd *cobra.Command, params CLICreateParams) (_ string, retErr error) {
	baseName := params.BaseName
	if err := config.ValidateInstanceName(baseName); err != nil {
		return "", err
	}

	instanceName := baseName
	if params.GenerateSuffix {
		suffixed, err := instance.AppendSuffix(baseName)
		if err != nil {
			return "", fmt.Errorf("generating name suffix: %w", err)
		}
		instanceName = suffixed
	}

	if err := config.ValidateInstanceName(instanceName); err != nil {
		return "", err
	}

	workspace := params.Workspace
	if workspace == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("determining current directory: %w", err)
		}
		workspace = cwd
	}

	if params.Port < 0 || params.Port > 65535 {
		return "", fmt.Errorf("port must be between 0 and 65535, got %d", params.Port)
	}

	paths, err := config.DefaultPaths()
	if err != nil {
		return "", err
	}
	if err := config.MigrateLayout(paths); err != nil {
		return "", fmt.Errorf("migrating config layout: %w", err)
	}

	instancePaths := paths.ForInstance(instanceName)

	// Check for name collision with an existing instance.
	collision, err := instance.CheckCollision(ctx, instancePaths)
	if err != nil {
		return "", fmt.Errorf("checking for existing instance: %w", err)
	}

	if err := handleCLICollision(cmd, instanceName, collision, params.Force, params.Yes, ctx, instancePaths); err != nil {
		return "", err
	}

	resolver, err := buildSourceResolver(params.Source)
	if err != nil {
		return "", err
	}

	if params.Source != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "Using source %q for artifact resolution\n", params.Source)
	}

	personality, toolchain, plugins, err := orchestrator.ResolveCreateRefs(ctx, resolver, params.Personality, params.Toolchain, params.Plugins)
	if err != nil {
		return "", err
	}

	envVars, err := parseEnvFlags(params.Env)
	if err != nil {
		return "", err
	}

	secretEnvVars, err := parseEnvFlags(params.SecretEnv)
	if err != nil {
		return "", fmt.Errorf("parsing --secret-env: %w", err)
	}

	secretFiles, err := parseEnvFlags(params.SecretFile)
	if err != nil {
		return "", fmt.Errorf("parsing --secret-file: %w", err)
	}

	gitName, gitEmail, err := parseGitAuthor(params.GitAuthor)
	if err != nil {
		return "", err
	}

	opts := config.CreateOptions{
		Name:                 instanceName,
		Workspace:            workspace,
		NoIsolate:            params.NoIsolate,
		Personality:          personality,
		Toolchain:            toolchain,
		Plugins:              plugins,
		Port:                 params.Port,
		GitAuthorName:        gitName,
		GitAuthorEmail:       gitEmail,
		GitCredentialHelper:  params.GitCredHelper,
		GitHTTPSInsteadOfSSH: params.GitHTTPSInstead,
		EnvVars:              envVars,
		EnvForward:           params.EnvForward,
		SecretEnvVars:        secretEnvVars,
		SecretFiles:          secretFiles,
		McpServerRefs:        params.McpServer,
		PermissionMode:       params.PermMode,
		Model:                params.Model,
		SystemPrompt:         params.SystemPrompt,
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
	if params.MaxBudgetSet {
		opts.MaxBudgetUSD = &params.MaxBudget
	}

	cfg, err := config.GenerateInstanceConfig(paths, opts)
	if err != nil {
		return "", err
	}

	if err := config.EnsureDir(instancePaths.InstanceDir); err != nil {
		return "", fmt.Errorf("creating instance directory: %w", err)
	}

	// Clean up the instance directory if any subsequent step fails.
	defer func() {
		if retErr != nil {
			_ = os.RemoveAll(instancePaths.InstanceDir)
		}
	}()

	// Clean up the worktree if any subsequent step fails. Registered after
	// the instance dir defer so it runs first (LIFO).
	if cfg.WorktreePath != "" {
		defer func() {
			if retErr != nil {
				_ = worktree.Remove(cfg.Workspace, cfg.WorktreePath)
			}
		}()
	}

	data, err := cfg.Marshal()
	if err != nil {
		return "", fmt.Errorf("serializing config: %w", err)
	}
	if err := os.WriteFile(instancePaths.ConfigFile, data, 0o644); err != nil {
		return "", fmt.Errorf("writing instance config: %w", err)
	}

	if err := config.EnsureDir(filepath.Dir(instancePaths.RenderedDir)); err != nil {
		return "", fmt.Errorf("creating rendered directory parent: %w", err)
	}

	if err := startInstance(cmd, instanceName, "", instancePaths.ConfigFile); err != nil {
		return "", err
	}

	return instanceName, nil
}
