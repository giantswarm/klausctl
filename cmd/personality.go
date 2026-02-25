package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"

	klausoci "github.com/giantswarm/klaus-oci"
	"github.com/spf13/cobra"

	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/orchestrator"
)

var (
	personalityValidateOut string
	personalityPullOut     string
	personalityPullSource  string
	personalityPushOut     string
	personalityPushSource  string
	personalityPushDryRun  bool
	personalityListOut     string
	personalityListLocal   bool
	personalityListSource  string
	personalityListAll     bool
)

var personalityCmd = &cobra.Command{
	Use:   "personality",
	Short: "Manage OCI personalities",
	Long: `Commands for working with klaus OCI personalities.

Personalities are OCI artifacts that bundle a curated set of plugins,
a recommended toolchain image, and persona-specific configuration.
They are published to the registry by CI and can be pulled locally.`,
}

var personalityValidateCmd = &cobra.Command{
	Use:   "validate <directory>",
	Short: "Validate a local personality directory",
	Long: `Validate a local personality directory against the expected structure.

A valid personality directory must contain a personality.yaml file
that defines the personality's plugins, image, and description.`,
	Args: cobra.ExactArgs(1),
	RunE: runPersonalityValidate,
}

var personalityPullCmd = &cobra.Command{
	Use:   "pull <reference>",
	Short: "Pull a personality from the OCI registry",
	Long: `Pull a personality OCI artifact from the registry to the local cache.

Accepts a short name, short name with tag, or full OCI reference:

  klausctl personality pull sre              (resolves latest version)
  klausctl personality pull sre:v0.0.7       (specific version)
  klausctl personality pull gsoci.azurecr.io/giantswarm/klaus-personalities/sre:v0.0.7`,
	Args: cobra.ExactArgs(1),
	RunE: runPersonalityPull,
}

var personalityPushCmd = &cobra.Command{
	Use:   "push <directory> <reference>",
	Short: "Push a personality to the OCI registry",
	Long: `Push a local personality directory as an OCI artifact to the registry.

The directory must contain a valid personality.yaml file.

Accepts a full OCI reference with tag or a short name with tag:

  klausctl personality push ./my-personality sre:v1.0.0
  klausctl personality push ./my-personality gsoci.azurecr.io/giantswarm/klaus-personalities/sre:v1.0.0`,
	Args: cobra.ExactArgs(2),
	RunE: runPersonalityPush,
}

var personalityListCmd = &cobra.Command{
	Use:   "list",
	Short: "List personalities",
	Long: `List available personalities from the remote OCI registry.

By default, discovers personalities from the registry, shows the latest version
of each, and indicates whether it is cached locally.

With --local, shows only locally cached personalities with full detail.`,
	RunE: runPersonalityList,
}

// personalityValidation is the JSON representation of a successful personality validation.
type personalityValidation struct {
	Valid       bool   `json:"valid"`
	Directory   string `json:"directory"`
	Description string `json:"description,omitempty"`
	Image       string `json:"image,omitempty"`
	Plugins     int    `json:"plugins,omitempty"`
}

func init() {
	personalityValidateCmd.Flags().StringVarP(&personalityValidateOut, "output", "o", "text", "output format: text, json")
	personalityPullCmd.Flags().StringVarP(&personalityPullOut, "output", "o", "text", "output format: text, json")
	personalityPullCmd.Flags().StringVar(&personalityPullSource, "source", "", "resolve against a specific source")
	personalityPushCmd.Flags().StringVarP(&personalityPushOut, "output", "o", "text", "output format: text, json")
	personalityPushCmd.Flags().StringVar(&personalityPushSource, "source", "", "use a specific source registry for the push destination")
	personalityPushCmd.Flags().BoolVar(&personalityPushDryRun, "dry-run", false, "validate and resolve without pushing")
	personalityListCmd.Flags().StringVarP(&personalityListOut, "output", "o", "text", "output format: text, json")
	personalityListCmd.Flags().BoolVar(&personalityListLocal, "local", false, "list only locally cached personalities")
	personalityListCmd.Flags().StringVar(&personalityListSource, "source", "", "list personalities from a specific source only")
	personalityListCmd.Flags().BoolVar(&personalityListAll, "all", false, "list personalities from all configured sources")

	personalityCmd.AddCommand(personalityValidateCmd)
	personalityCmd.AddCommand(personalityPullCmd)
	personalityCmd.AddCommand(personalityPushCmd)
	personalityCmd.AddCommand(personalityListCmd)
	rootCmd.AddCommand(personalityCmd)
}

func runPersonalityValidate(cmd *cobra.Command, args []string) error {
	if err := validateOutputFormat(personalityValidateOut); err != nil {
		return err
	}
	return validatePersonalityDir(args[0], cmd.OutOrStdout(), personalityValidateOut)
}

// validatePersonalityDir checks that a directory has a valid personality structure.
func validatePersonalityDir(dir string, out io.Writer, outputFmt string) error {
	info, err := os.Stat(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("directory does not exist: %s", dir)
		}
		return fmt.Errorf("checking directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("not a directory: %s", dir)
	}

	spec, err := orchestrator.LoadPersonalitySpec(dir)
	if err != nil {
		return err
	}

	if outputFmt == "json" {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(personalityValidation{
			Valid:       true,
			Directory:   dir,
			Description: spec.Description,
			Image:       spec.Toolchain.Ref(),
			Plugins:     len(spec.Plugins),
		})
	}

	fmt.Fprintf(out, "Valid personality directory: %s\n", dir)
	if spec.Description != "" {
		fmt.Fprintf(out, "  Description: %s\n", spec.Description)
	}
	if spec.Toolchain.Repository != "" {
		fmt.Fprintf(out, "  Image: %s\n", spec.Toolchain.Ref())
	}
	if len(spec.Plugins) > 0 {
		fmt.Fprintf(out, "  Plugins: %d\n", len(spec.Plugins))
	}
	return nil
}

// pullPersonalityFn wraps the typed PullPersonality method for use with pullArtifact.
var pullPersonalityFn pullFn = func(ctx context.Context, client *klausoci.Client, ref, destDir string) (string, bool, error) {
	result, err := client.PullPersonality(ctx, ref, destDir)
	if err != nil {
		return "", false, err
	}
	return result.Digest, result.Cached, nil
}

// listPersonalitiesFn wraps the typed ListPersonalities method for use with listLatestRemoteArtifacts.
var listPersonalitiesFn listFn = func(ctx context.Context, client *klausoci.Client, opts ...klausoci.ListOption) ([]klausoci.ListEntry, error) {
	return client.ListPersonalities(ctx, opts...)
}

// pushPersonalityFn reads personality metadata from sourceDir and pushes it as an OCI artifact.
var pushPersonalityFn pushFn = func(ctx context.Context, client *klausoci.Client, sourceDir, ref string) (string, error) {
	personality, err := klausoci.ReadPersonalityFromDir(sourceDir)
	if err != nil {
		return "", err
	}
	result, err := client.PushPersonality(ctx, sourceDir, ref, *personality)
	if err != nil {
		return "", err
	}
	return result.Digest, nil
}

func runPersonalityPush(cmd *cobra.Command, args []string) error {
	if err := validateOutputFormat(personalityPushOut); err != nil {
		return err
	}

	dir := args[0]
	if err := validatePersonalityDir(dir, io.Discard, "text"); err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	resolver, err := buildSourceResolver(personalityPushSource)
	if err != nil {
		return err
	}

	ref := resolver.ResolvePersonalityRef(args[1])
	if err := validatePushRef(ref); err != nil {
		return err
	}

	return pushArtifact(ctx, dir, ref, pushPersonalityFn, cmd.OutOrStdout(), personalityPushOut, pushOpts{dryRun: personalityPushDryRun})
}

func runPersonalityPull(cmd *cobra.Command, args []string) error {
	if err := validateOutputFormat(personalityPullOut); err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}

	if err := config.EnsureDir(paths.PersonalitiesDir); err != nil {
		return fmt.Errorf("creating personalities directory: %w", err)
	}

	resolver, err := buildSourceResolver(personalityPullSource)
	if err != nil {
		return err
	}

	resolved := resolver.ResolvePersonalityRef(args[0])
	client := orchestrator.NewDefaultClient()
	ref, err := client.ResolvePersonalityRef(ctx, resolved)
	if err != nil {
		return err
	}

	return pullArtifact(ctx, ref, paths.PersonalitiesDir, pullPersonalityFn, cmd.OutOrStdout(), personalityPullOut)
}

func runPersonalityList(cmd *cobra.Command, _ []string) error {
	if err := validateOutputFormat(personalityListOut); err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}

	resolver, err := buildListSourceResolver(personalityListSource, personalityListAll)
	if err != nil {
		return err
	}

	return listOCIArtifacts(ctx, cmd.OutOrStdout(), paths.PersonalitiesDir, personalityListOut, "personality", "personalities", resolver.PersonalityRegistries(), personalityListLocal, listPersonalitiesFn)
}
