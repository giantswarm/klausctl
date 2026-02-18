package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/oci"
)

var (
	personalityValidateOut string
	personalityPullOut     string
	personalityListOut     string
	personalityListRemote  bool
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

The reference must include a tag or digest:

  klausctl personality pull gsoci.azurecr.io/giantswarm/klaus-personalities/sre:v1.0.0`,
	Args: cobra.ExactArgs(1),
	RunE: runPersonalityPull,
}

var personalityListCmd = &cobra.Command{
	Use:   "list",
	Short: "List personalities",
	Long: `List locally cached personalities, or query the remote registry with --remote.

Without --remote, shows personalities downloaded to the local cache.
With --remote, discovers available personalities directly from the OCI registry
and lists their tags. Works on a clean machine with no local cache.`,
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
	personalityListCmd.Flags().StringVarP(&personalityListOut, "output", "o", "text", "output format: text, json")
	personalityListCmd.Flags().BoolVar(&personalityListRemote, "remote", false, "list remote registry tags instead of local cache")

	personalityCmd.AddCommand(personalityValidateCmd)
	personalityCmd.AddCommand(personalityPullCmd)
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

	specPath := filepath.Join(dir, "personality.yaml")
	data, err := os.ReadFile(specPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("personality.yaml not found in %s", dir)
		}
		return fmt.Errorf("reading personality.yaml: %w", err)
	}

	var spec oci.PersonalitySpec
	if err := yaml.Unmarshal(data, &spec); err != nil {
		return fmt.Errorf("parsing personality.yaml: %w", err)
	}

	if outputFmt == "json" {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(personalityValidation{
			Valid:       true,
			Directory:   dir,
			Description: spec.Description,
			Image:       spec.Image,
			Plugins:     len(spec.Plugins),
		})
	}

	fmt.Fprintf(out, "Valid personality directory: %s\n", dir)
	if spec.Description != "" {
		fmt.Fprintf(out, "  Description: %s\n", spec.Description)
	}
	if spec.Image != "" {
		fmt.Fprintf(out, "  Image: %s\n", spec.Image)
	}
	if len(spec.Plugins) > 0 {
		fmt.Fprintf(out, "  Plugins: %d\n", len(spec.Plugins))
	}
	return nil
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

	return pullArtifact(ctx, args[0], paths.PersonalitiesDir, oci.PersonalityArtifact, cmd.OutOrStdout(), personalityPullOut)
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

	return listOCIArtifacts(ctx, cmd.OutOrStdout(), paths.PersonalitiesDir, personalityListOut, "personality", "personalities", oci.DefaultPersonalityRegistry, personalityListRemote)
}
