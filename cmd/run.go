package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"

	"github.com/spf13/cobra"

	"github.com/giantswarm/klausctl/pkg/agentclient"
	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/instance"
)

var (
	runPersonality       string
	runToolchain         string
	runPlugins           []string
	runPort              int
	runEnv               []string
	runEnvForward        []string
	runSecretEnv         []string
	runSecretFile        []string
	runMcpServer         []string
	runPermMode          string
	runModel             string
	runSystemPrompt      string
	runMaxBudget         float64
	runSource            string
	runMode              string
	runNoIsolate         bool
	runNoFetch           bool
	runGitAuthor         string
	runGitCredHelper     string
	runGitHTTPSInsteadOf bool
	runYes               bool
	runForce             bool
	runGenerateSuffix    bool

	runMessage  string
	runBlocking bool
	runOutput   string

	runRemote  string
	runSession string
)

var runCmd = &cobra.Command{
	Use:   "run <name> [workspace]",
	Short: "Create an instance and send a prompt in one step",
	Long: `Create a new klaus instance, wait for it to become ready, and send a
prompt — all in a single command.

This combines 'klausctl create' and 'klausctl prompt' into one operation,
reducing boilerplate in agent workflows. All flags from 'create' are
supported, plus -m/--message to supply the prompt.

By default the command returns once the prompt is accepted. Use --blocking
to wait for the agent to finish and print the result.

Examples:

  klausctl run dev ~/myproject -m "Fix the failing tests"
  klausctl run dev ~/myproject --personality go -m "List all TODO comments" --blocking
  klausctl run dev -m "Refactor the handler" --blocking -o json`,
	Args: cobra.RangeArgs(1, 2),
	RunE: runRun,
}

func init() {
	runCmd.Flags().StringVar(&runPersonality, "personality", "", "personality short name or OCI reference")
	runCmd.Flags().StringVar(&runToolchain, "toolchain", "", "toolchain short name or OCI reference")
	runCmd.Flags().StringSliceVar(&runPlugins, "plugin", nil, "additional plugin short name or OCI reference (repeatable)")
	runCmd.Flags().IntVar(&runPort, "port", 0, "override auto-selected port")
	runCmd.Flags().StringArrayVar(&runEnv, "env", nil, "environment variable KEY=VALUE (repeatable)")
	runCmd.Flags().StringArrayVar(&runEnvForward, "env-forward", nil, "host environment variable name to forward (repeatable)")
	runCmd.Flags().StringVar(&runPermMode, "permission-mode", "", "Claude permission mode: default, acceptEdits, bypassPermissions, dontAsk, plan, delegate")
	runCmd.Flags().StringVar(&runModel, "model", "", "Claude model (e.g. sonnet, opus)")
	runCmd.Flags().StringVar(&runSystemPrompt, "system-prompt", "", "system prompt override for the Claude agent")
	runCmd.Flags().Float64Var(&runMaxBudget, "max-budget", 0, "maximum dollar budget per invocation (0 = no limit)")
	runCmd.Flags().StringArrayVar(&runSecretEnv, "secret-env", nil, "secret env var ENV_NAME=secret-name (repeatable)")
	runCmd.Flags().StringArrayVar(&runSecretFile, "secret-file", nil, "secret file /container/path=secret-name (repeatable)")
	runCmd.Flags().StringArrayVar(&runMcpServer, "mcpserver", nil, "managed MCP server name (repeatable)")
	runCmd.Flags().StringVar(&runSource, "source", "", "resolve artifact short names against a specific source")
	runCmd.Flags().StringVar(&runMode, "mode", "agent", `operating mode: "agent" (autonomous coding, new process per prompt) or "chat" (interactive, persistent process, saved sessions)`)
	runCmd.Flags().BoolVar(&runNoIsolate, "no-isolate", false, "skip git worktree creation and bind-mount workspace directly")
	runCmd.Flags().BoolVar(&runNoFetch, "no-fetch", false, "skip git fetch origin before cloning the workspace")
	runCmd.Flags().StringVar(&runGitAuthor, "git-author", "", `git author identity "Name <email>"`)
	runCmd.Flags().StringVar(&runGitCredHelper, "git-credential-helper", "", "git credential helper (currently only 'gh')")
	runCmd.Flags().BoolVar(&runGitHTTPSInsteadOf, "git-https-instead-of-ssh", false, "rewrite SSH git URLs to HTTPS via container-local gitconfig")
	runCmd.Flags().BoolVarP(&runYes, "yes", "y", false, "auto-confirm replacement of existing instances")
	runCmd.Flags().BoolVar(&runForce, "force", false, "allow replacing a running instance (prompts for confirmation unless -y is also set)")
	runCmd.Flags().BoolVar(&runGenerateSuffix, "generate-suffix", true, "append a random 4-character suffix to the instance name (use --no-generate-suffix to disable)")

	runCmd.Flags().StringVarP(&runMessage, "message", "m", "", "prompt message to send to the agent (required)")
	runCmd.Flags().BoolVar(&runBlocking, "blocking", false, "wait for the agent to complete and return the result")
	runCmd.Flags().StringVarP(&runOutput, "output", "o", "text", "output format: text, json")

	runCmd.Flags().StringVar(&runRemote, remoteFlagName("remote"), "", remoteFlagDesc("remote"))
	runCmd.Flags().StringVar(&runSession, remoteFlagName("session"), "", remoteFlagDesc("session"))

	_ = runCmd.MarkFlagRequired("message")
	rootCmd.AddCommand(runCmd)
}

func runRun(cmd *cobra.Command, args []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	if runRemote != "" {
		return runRunRemote(ctx, cmd, args)
	}

	workspace := ""
	if len(args) > 1 {
		workspace = args[1]
	}

	params := CLICreateParams{
		BaseName:        args[0],
		Workspace:       workspace,
		Personality:     runPersonality,
		Toolchain:       runToolchain,
		Plugins:         runPlugins,
		Port:            runPort,
		Env:             runEnv,
		EnvForward:      runEnvForward,
		SecretEnv:       runSecretEnv,
		SecretFile:      runSecretFile,
		McpServer:       runMcpServer,
		PermMode:        runPermMode,
		Model:           runModel,
		SystemPrompt:    runSystemPrompt,
		MaxBudget:       runMaxBudget,
		MaxBudgetSet:    cmd.Flags().Changed("max-budget"),
		Source:          runSource,
		Mode:            runMode,
		NoIsolate:       runNoIsolate,
		NoFetch:         runNoFetch,
		GitAuthor:       runGitAuthor,
		GitCredHelper:   runGitCredHelper,
		GitHTTPSInstead: runGitHTTPSInsteadOf,
		Yes:             runYes,
		Force:           runForce,
		GenerateSuffix:  runGenerateSuffix,
	}

	instanceName, err := cliCreateInstance(ctx, cmd, params)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()

	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	instancePaths := paths.ForInstance(instanceName)

	inst, err := instance.Load(instancePaths)
	if err != nil {
		return fmt.Errorf("loading instance state after create: %w", err)
	}

	agentURL := fmt.Sprintf("http://localhost:%d", inst.Port)
	httpClient := &http.Client{}

	if err := agentclient.WaitForReady(ctx, httpClient, agentURL); err != nil {
		return fmt.Errorf("waiting for instance %q to become ready: %w", instanceName, err)
	}

	_, _ = fmt.Fprintf(out, "\nSending prompt to %s...\n", instanceName)

	if runBlocking {
		return runBlocked(ctx, out, httpClient, agentURL, instanceName)
	}

	return runNonBlocking(ctx, out, httpClient, agentURL, instanceName)
}

// runBlocked sends the prompt via the chat completions streaming API and
// prints content deltas as they arrive.
func runBlocked(ctx context.Context, out io.Writer, httpClient *http.Client, agentURL, instanceName string) error {
	compCh, err := agentclient.StreamCompletion(ctx, httpClient, agentclient.CompletionRequest{
		URL:    agentURL + "/v1/chat/completions",
		Prompt: runMessage,
	})
	if err != nil {
		return fmt.Errorf("sending prompt to %q: %w", instanceName, err)
	}

	for delta := range compCh {
		if delta.Err != nil {
			return fmt.Errorf("streaming from %q: %w", instanceName, delta.Err)
		}
		_, _ = fmt.Fprint(out, delta.Content)
	}
	_, _ = fmt.Fprintln(out)

	return renderRunResult(out, promptCLIResult{
		Instance: instanceName,
		Status:   "completed",
	})
}

// runNonBlocking sends the prompt via the chat completions API without
// waiting for the agent to finish.
func runNonBlocking(ctx context.Context, out io.Writer, httpClient *http.Client, agentURL, instanceName string) error {
	compCh, err := agentclient.StreamCompletion(ctx, httpClient, agentclient.CompletionRequest{
		URL:    agentURL + "/v1/chat/completions",
		Prompt: runMessage,
	})
	if err != nil {
		return fmt.Errorf("sending prompt to %q: %w", instanceName, err)
	}
	go func() {
		for range compCh {
		}
	}()

	result := promptCLIResult{
		Instance: instanceName,
		Status:   "started",
	}
	return renderRunResult(out, result)
}

// runRunRemote sends the prompt to a remote klaus-gateway endpoint,
// skipping the local create/container-wait path entirely.
func runRunRemote(ctx context.Context, cmd *cobra.Command, args []string) error {
	out := cmd.OutOrStdout()
	instanceName := args[0]

	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}

	target, store, rec, err := resolveRemoteTarget(ctx, runRemote, instanceName, runSession, paths)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(out, "Sending prompt to %s (remote: %s)...\n", instanceName, target.BaseURL)

	httpClient := &http.Client{}
	compCh, err := streamRemoteCompletion(ctx, httpClient, &target, store, rec, runMessage)
	if err != nil {
		return fmt.Errorf("sending prompt to %q: %w", instanceName, err)
	}

	if !runBlocking {
		go func() {
			for range compCh {
			}
		}()
		return renderRunResult(out, promptCLIResult{
			Instance: instanceName,
			Status:   "started",
		})
	}

	for delta := range compCh {
		if delta.Err != nil {
			return fmt.Errorf("streaming from %q: %w", instanceName, delta.Err)
		}
		_, _ = fmt.Fprint(out, delta.Content)
	}
	_, _ = fmt.Fprintln(out)

	return renderRunResult(out, promptCLIResult{
		Instance: instanceName,
		Status:   "completed",
	})
}

func renderRunResult(out io.Writer, result promptCLIResult) error {
	if runOutput == "json" { //nolint:goconst
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	_, _ = fmt.Fprintf(out, "Instance: %s\n", result.Instance)
	_, _ = fmt.Fprintf(out, "Status:   %s\n", colorStatus(result.Status))
	if result.Result != "" {
		_, _ = fmt.Fprintf(out, "\n%s\n", result.Result)
	}
	return nil
}
