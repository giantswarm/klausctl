package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/oci"
)

var (
	personalityListOut    string
	personalityListRemote bool
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
With --remote, shows available tags for locally cached personality repositories.`,
	RunE: runPersonalityList,
}

func init() {
	personalityListCmd.Flags().StringVarP(&personalityListOut, "output", "o", "text", "output format: text, json")
	personalityListCmd.Flags().BoolVar(&personalityListRemote, "remote", false, "list remote registry tags instead of local cache")

	personalityCmd.AddCommand(personalityValidateCmd)
	personalityCmd.AddCommand(personalityPullCmd)
	personalityCmd.AddCommand(personalityListCmd)
	rootCmd.AddCommand(personalityCmd)
}

func runPersonalityValidate(_ *cobra.Command, args []string) error {
	dir := args[0]
	return validatePersonalityDir(dir)
}

// validatePersonalityDir checks that a directory has a valid personality structure.
func validatePersonalityDir(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
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
		if os.IsNotExist(err) {
			return fmt.Errorf("personality.yaml not found in %s", dir)
		}
		return fmt.Errorf("reading personality.yaml: %w", err)
	}

	var spec oci.PersonalitySpec
	if err := yaml.Unmarshal(data, &spec); err != nil {
		return fmt.Errorf("parsing personality.yaml: %w", err)
	}

	fmt.Printf("Valid personality directory: %s\n", dir)
	if spec.Description != "" {
		fmt.Printf("  Description: %s\n", spec.Description)
	}
	if spec.Image != "" {
		fmt.Printf("  Image: %s\n", spec.Image)
	}
	if len(spec.Plugins) > 0 {
		fmt.Printf("  Plugins: %d\n", len(spec.Plugins))
	}
	return nil
}

func runPersonalityPull(cmd *cobra.Command, args []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}

	if err := config.EnsureDir(paths.PersonalitiesDir); err != nil {
		return fmt.Errorf("creating personalities directory: %w", err)
	}

	return pullArtifact(ctx, args[0], paths.PersonalitiesDir, oci.PersonalityArtifact, cmd.OutOrStdout())
}

func runPersonalityList(cmd *cobra.Command, _ []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	out := cmd.OutOrStdout()

	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}

	if personalityListRemote {
		tags, err := listRemoteTags(ctx, paths.PersonalitiesDir)
		if err != nil {
			return err
		}
		if len(tags) == 0 {
			if personalityListOut != "json" {
				fmt.Fprintln(out, "No locally cached personalities to query remote tags for.")
				fmt.Fprintln(out, "Use 'klausctl personality pull <ref>' to pull a personality first.")
			} else {
				fmt.Fprintln(out, "[]")
			}
			return nil
		}
		return printRemoteTags(out, tags, personalityListOut)
	}

	artifacts, err := listLocalArtifacts(paths.PersonalitiesDir)
	if err != nil {
		return err
	}

	if len(artifacts) == 0 && personalityListOut != "json" {
		fmt.Fprintln(out, "No personalities cached locally.")
		fmt.Fprintln(out, "Use 'klausctl personality pull <ref>' to pull a personality.")
		return nil
	}

	return printLocalArtifacts(out, artifacts, personalityListOut)
}
