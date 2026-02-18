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
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/oci"
	"github.com/giantswarm/klausctl/pkg/runtime"
)

// toolchainImageSubstring is the substring matched against repository names
// to identify klaus toolchain images. Docker's reference filter does not
// support wildcards across path separators, so we fetch all images and filter
// client-side.
const toolchainImageSubstring = "klaus-"

var (
	toolchainInitName    string
	toolchainInitDir     string
	toolchainValidateOut string
	toolchainPullOut     string
	toolchainListOut     string
	toolchainListWide    bool
	toolchainListRemote  bool
)

var toolchainCmd = &cobra.Command{
	Use:   "toolchain",
	Short: "Manage toolchain images",
	Long: `Commands for working with klaus toolchain images.

Toolchain images are pre-built by CI and published to the registry with
semver tags. klausctl does not build or push images -- that is CI's
responsibility.`,
}

var toolchainListCmd = &cobra.Command{
	Use:   "list",
	Short: "List toolchain images",
	Long: `List locally cached or remote klaus toolchain images.

Without --remote, shows all Docker/Podman images matching the klaus-*
naming pattern, typically pulled from the registry by CI or during
'klausctl start'.

With --remote, queries the registry for available tags of locally cached
toolchain images.`,
	RunE: runToolchainList,
}

var toolchainInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Scaffold a new toolchain image repository",
	Long: `Scaffold a new toolchain image repository with example Dockerfiles and CI config.

This is a one-time scaffolding command. After initialization, the repository
is maintained by the platform team using standard git and CI workflows.
klausctl is not involved in ongoing builds.`,
	RunE: runToolchainInit,
}

var toolchainValidateCmd = &cobra.Command{
	Use:   "validate <directory>",
	Short: "Validate a local toolchain directory",
	Long: `Validate a local toolchain image directory against the expected structure.

A valid toolchain directory must contain a Dockerfile.`,
	Args: cobra.ExactArgs(1),
	RunE: runToolchainValidate,
}

var toolchainPullCmd = &cobra.Command{
	Use:   "pull <reference>",
	Short: "Pull a toolchain image from the registry",
	Long: `Pull a toolchain container image from the registry using Docker/Podman.

The reference should be a full image reference:

  klausctl toolchain pull gsoci.azurecr.io/giantswarm/klaus-go:1.0.0`,
	Args: cobra.ExactArgs(1),
	RunE: runToolchainPull,
}

// toolchainValidation is the JSON representation of a successful toolchain validation.
type toolchainValidation struct {
	Valid     bool   `json:"valid"`
	Directory string `json:"directory"`
}

// toolchainPullResult is the JSON representation of a successful toolchain pull.
type toolchainPullResult struct {
	Ref    string `json:"ref"`
	Status string `json:"status"`
}

func init() {
	toolchainValidateCmd.Flags().StringVarP(&toolchainValidateOut, "output", "o", "text", "output format: text, json")
	toolchainPullCmd.Flags().StringVarP(&toolchainPullOut, "output", "o", "text", "output format: text, json")
	toolchainListCmd.Flags().StringVarP(&toolchainListOut, "output", "o", "text", "output format: text, json")
	toolchainListCmd.Flags().BoolVar(&toolchainListWide, "wide", false, "show additional columns (ID, size)")
	toolchainListCmd.Flags().BoolVar(&toolchainListRemote, "remote", false, "list remote registry tags instead of local images")

	toolchainInitCmd.Flags().StringVar(&toolchainInitName, "name", "", "toolchain name (required)")
	toolchainInitCmd.Flags().StringVar(&toolchainInitDir, "dir", "", "output directory (default: ./klaus-<name>)")
	_ = toolchainInitCmd.MarkFlagRequired("name")

	toolchainCmd.AddCommand(toolchainListCmd)
	toolchainCmd.AddCommand(toolchainInitCmd)
	toolchainCmd.AddCommand(toolchainValidateCmd)
	toolchainCmd.AddCommand(toolchainPullCmd)
	rootCmd.AddCommand(toolchainCmd)
}

// loadRuntime creates a container runtime from the current config file.
func loadRuntime() (runtime.Runtime, error) {
	name := ""
	if cfg, err := config.Load(cfgFile); err == nil {
		name = cfg.Runtime
	}
	return runtime.New(name)
}

func runToolchainList(cmd *cobra.Command, _ []string) error {
	if err := validateOutputFormat(toolchainListOut); err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	out := cmd.OutOrStdout()

	if toolchainListRemote {
		return runToolchainListRemote(ctx, out)
	}

	rt, err := loadRuntime()
	if err != nil {
		return err
	}

	return toolchainList(ctx, out, rt, toolchainListOptions{
		output: toolchainListOut,
		wide:   toolchainListWide,
	})
}

// runToolchainListRemote queries remote tags for locally cached toolchain images.
func runToolchainListRemote(ctx context.Context, out io.Writer) error {
	rt, err := loadRuntime()
	if err != nil {
		return err
	}

	all, err := rt.Images(ctx, "")
	if err != nil {
		return fmt.Errorf("listing images: %w", err)
	}

	seen := make(map[string]bool)
	var repos []string
	for _, img := range all {
		if strings.Contains(img.Repository, toolchainImageSubstring) && !seen[img.Repository] {
			seen[img.Repository] = true
			repos = append(repos, img.Repository)
		}
	}

	if len(repos) == 0 {
		return printEmpty(out, toolchainListOut,
			"No locally cached toolchain images to query remote tags for.",
			"Use 'klausctl toolchain pull <ref>' to pull a toolchain image first.",
		)
	}

	client := oci.NewDefaultClient()
	var tags []remoteTag
	for _, repo := range repos {
		repoTags, err := client.List(ctx, repo)
		if err != nil {
			return fmt.Errorf("listing tags for %s: %w", repo, err)
		}
		for _, tag := range repoTags {
			tags = append(tags, remoteTag{Repository: repo, Tag: tag})
		}
	}

	return printRemoteTags(out, tags, toolchainListOut)
}

// toolchainListOptions controls output formatting for the toolchain list.
type toolchainListOptions struct {
	output string
	wide   bool
}

// toolchainList lists locally cached toolchain images using the given runtime.
func toolchainList(ctx context.Context, out io.Writer, rt runtime.Runtime, opts toolchainListOptions) error {
	all, err := rt.Images(ctx, "")
	if err != nil {
		return fmt.Errorf("listing images: %w", err)
	}

	var images []runtime.ImageInfo
	for _, img := range all {
		if strings.Contains(img.Repository, toolchainImageSubstring) {
			images = append(images, img)
		}
	}

	if len(images) == 0 {
		return printEmpty(out, opts.output,
			"No toolchain images found locally.",
			"Toolchain images are built and tagged by CI in the toolchain repository.",
		)
	}

	if opts.output == "json" {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(images)
	}

	return printImageTable(out, images, opts.wide)
}

func printImageTable(out io.Writer, images []runtime.ImageInfo, wide bool) error {
	w := tabwriter.NewWriter(out, 0, 0, 3, ' ', 0)
	if wide {
		fmt.Fprintln(w, "IMAGE\tTAG\tID\tCREATED\tSIZE")
		for _, img := range images {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", img.Repository, img.Tag, img.ID, img.CreatedSince, img.Size)
		}
	} else {
		fmt.Fprintln(w, "IMAGE\tTAG\tSIZE\tCREATED")
		for _, img := range images {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", img.Repository, img.Tag, img.Size, img.CreatedSince)
		}
	}
	return w.Flush()
}

func runToolchainValidate(cmd *cobra.Command, args []string) error {
	if err := validateOutputFormat(toolchainValidateOut); err != nil {
		return err
	}
	return validateToolchainDir(args[0], cmd.OutOrStdout(), toolchainValidateOut)
}

// validateToolchainDir checks that a directory has a valid toolchain structure.
func validateToolchainDir(dir string, out io.Writer, outputFmt string) error {
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

	dockerfilePath := filepath.Join(dir, "Dockerfile")
	if _, err := os.Stat(dockerfilePath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("Dockerfile not found in %s", dir)
		}
		return fmt.Errorf("checking Dockerfile: %w", err)
	}

	if outputFmt == "json" {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(toolchainValidation{
			Valid:     true,
			Directory: dir,
		})
	}

	fmt.Fprintf(out, "Valid toolchain directory: %s\n", dir)
	return nil
}

func runToolchainPull(cmd *cobra.Command, args []string) error {
	if err := validateOutputFormat(toolchainPullOut); err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	out := cmd.OutOrStdout()

	rt, err := loadRuntime()
	if err != nil {
		return err
	}

	ref := args[0]

	progressOut := out
	if toolchainPullOut == "json" {
		progressOut = cmd.ErrOrStderr()
	}

	fmt.Fprintf(progressOut, "Pulling %s...\n", ref)
	if err := rt.Pull(ctx, ref, progressOut); err != nil {
		return fmt.Errorf("pulling image: %w", err)
	}

	if toolchainPullOut == "json" {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(toolchainPullResult{
			Ref:    ref,
			Status: "pulled",
		})
	}

	fmt.Fprintf(out, "Successfully pulled %s\n", ref)
	return nil
}

func runToolchainInit(cmd *cobra.Command, _ []string) error {
	out := cmd.OutOrStdout()

	dir := toolchainInitDir
	if dir == "" {
		dir = filepath.Join(".", "klaus-"+toolchainInitName)
	}

	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("directory already exists: %s", dir)
	}

	if err := os.MkdirAll(filepath.Join(dir, ".circleci"), 0o755); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}

	files := scaffoldFiles(toolchainInitName)
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("creating directory for %s: %w", name, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", name, err)
		}
	}

	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)

	fmt.Fprintf(out, "Created %s/\n", dir)
	for _, name := range names {
		fmt.Fprintf(out, "  %s\n", name)
	}

	return nil
}

// scaffoldFiles returns the scaffold file contents keyed by relative path.
func scaffoldFiles(name string) map[string]string {
	imageName := "gsoci.azurecr.io/giantswarm/klaus-" + name

	return map[string]string{
		"Dockerfile": fmt.Sprintf(`# Toolchain image: klaus-%s
# Based on the klaus-git base image (Alpine).
FROM gsoci.azurecr.io/giantswarm/klaus-git:latest

# Install toolchain-specific packages.
# RUN apk add --no-cache <your-packages>

# Add custom configuration files if needed.
# COPY config/ /etc/klaus/
`, name),

		"Dockerfile.debian": fmt.Sprintf(`# Toolchain image: klaus-%s (Debian variant)
# Based on the klaus-git base image (Debian).
FROM gsoci.azurecr.io/giantswarm/klaus-git:latest-debian

# Install toolchain-specific packages.
# RUN apt-get update && apt-get install -y --no-install-recommends \
#     <your-packages> \
#     && rm -rf /var/lib/apt/lists/*
`, name),

		"Makefile": fmt.Sprintf(`IMAGE_NAME ?= %s
TAG ?= dev

.PHONY: docker-build docker-build-debian

docker-build:
	docker build -t $(IMAGE_NAME):$(TAG) -f Dockerfile .

docker-build-debian:
	docker build -t $(IMAGE_NAME):$(TAG)-debian -f Dockerfile.debian .
`, imageName),

		".circleci/config.yml": fmt.Sprintf(`version: 2.1

# CI configuration for the klaus-%s toolchain image.
# Builds are triggered on semver tags and publish to the registry.
# See: https://github.com/giantswarm/klaus-images
`, name),

		"README.md": fmt.Sprintf(`# klaus-%s

Klaus toolchain image for %s.

## Overview

This repository contains the Dockerfile and CI configuration for the
`+"`klaus-%s`"+` toolchain image, published to `+"`%s`"+`.

Toolchain images extend the base `+"`klaus-git`"+` image with language-specific
or project-specific tooling.

## Building

`+"```"+`bash
# Alpine variant (default)
make docker-build

# Debian variant
make docker-build-debian
`+"```"+`

## CI

Images are built and published automatically by CircleCI on semver tags.
`, name, name, name, imageName),
	}
}
