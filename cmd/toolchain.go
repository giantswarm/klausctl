package cmd

import (
	"context"
	"encoding/json"
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
	"github.com/giantswarm/klausctl/pkg/runtime"
)

// toolchainImageSubstring is the substring matched against repository names
// to identify klaus toolchain images. Docker's reference filter does not
// support wildcards across path separators, so we fetch all images and filter
// client-side.
const toolchainImageSubstring = "klaus-"

var (
	toolchainInitName string
	toolchainInitDir  string
	toolchainListOut  string
	toolchainListWide bool
)

var toolchainCmd = &cobra.Command{
	Use:   "toolchain",
	Short: "Manage toolchain images",
	Long: `Commands for working with klaus toolchain images locally.

Toolchain images are pre-built by CI and published to the registry with
semver tags. klausctl does not build or push images -- that is CI's
responsibility.`,
}

var toolchainListCmd = &cobra.Command{
	Use:   "list",
	Short: "List locally cached toolchain images",
	Long: `List locally cached klaus toolchain images.

Shows all Docker/Podman images matching the klaus-* naming pattern,
typically pulled from the registry by CI or during 'klausctl start'.`,
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

func init() {
	toolchainListCmd.Flags().StringVarP(&toolchainListOut, "output", "o", "text", "output format: text, json")
	toolchainListCmd.Flags().BoolVar(&toolchainListWide, "wide", false, "show additional columns (ID, size)")

	toolchainInitCmd.Flags().StringVar(&toolchainInitName, "name", "", "toolchain name (required)")
	toolchainInitCmd.Flags().StringVar(&toolchainInitDir, "dir", "", "output directory (default: ./klaus-<name>)")
	_ = toolchainInitCmd.MarkFlagRequired("name")

	toolchainCmd.AddCommand(toolchainListCmd)
	toolchainCmd.AddCommand(toolchainInitCmd)
	rootCmd.AddCommand(toolchainCmd)
}

func runToolchainList(cmd *cobra.Command, _ []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	// Detect runtime; respect config if available.
	runtimeName := ""
	cfg, err := config.Load(cfgFile)
	if err == nil {
		runtimeName = cfg.Runtime
	}

	rt, err := runtime.New(runtimeName)
	if err != nil {
		return err
	}

	return toolchainList(ctx, cmd.OutOrStdout(), rt, toolchainListOptions{
		output: toolchainListOut,
		wide:   toolchainListWide,
	})
}

// toolchainListOptions controls output formatting for the toolchain list.
type toolchainListOptions struct {
	output string
	wide   bool
}

// toolchainList lists locally cached toolchain images using the given runtime.
func toolchainList(ctx context.Context, out io.Writer, rt runtime.Runtime, opts toolchainListOptions) error {
	// Fetch all images and filter client-side. Docker's reference filter
	// does not support wildcards across path separators, so server-side
	// filtering misses registry-qualified names like gsoci.azurecr.io/giantswarm/klaus-go.
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
		if opts.output == "json" {
			fmt.Fprintln(out, "[]")
			return nil
		}
		fmt.Fprintln(out, "No toolchain images found locally.")
		fmt.Fprintln(out, "Toolchain images are built and tagged by CI in the toolchain repository.")
		return nil
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

func runToolchainInit(cmd *cobra.Command, _ []string) error {
	out := cmd.OutOrStdout()

	dir := toolchainInitDir
	if dir == "" {
		dir = filepath.Join(".", "klaus-"+toolchainInitName)
	}

	// Check if directory already exists.
	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("directory already exists: %s", dir)
	}

	// Create the directory tree.
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
