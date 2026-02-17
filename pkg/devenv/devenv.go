// Package devenv builds composite container images for toolchain environments.
// It layers Klaus agent capabilities (Node.js, Claude Code CLI, klaus binary)
// on top of a user-chosen language base image to produce a single image where
// language tools are native.
package devenv

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"text/template"

	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/runtime"
)

// nodeSetupVersion is the Node.js major version installed in composite images
// when the base image does not already provide Node.js.
const nodeSetupVersion = "24"

// packageNameRe validates Debian/Ubuntu package names.
// See: https://www.debian.org/doc/debian-policy/ch-controlfields.html#source
var packageNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9.+\-]+$`)

// dockerfileData holds the template data for Dockerfile generation.
type dockerfileData struct {
	KlausImage       string
	BaseImage        string
	Packages         []string
	NodeSetupVersion string
}

var dockerfileTmpl = template.Must(
	template.New("Dockerfile.toolchain").Funcs(template.FuncMap{
		"join": strings.Join,
	}).Parse(dockerfileContent),
)

const dockerfileContent = `FROM {{.KlausImage}} AS klaus-source
FROM {{.BaseImage}}

# System dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates curl git openssh-client \
  && rm -rf /var/lib/apt/lists/*

# Node.js (skip if already present)
RUN command -v node >/dev/null 2>&1 || \
  (curl -fsSL https://deb.nodesource.com/setup_{{.NodeSetupVersion}}.x | bash - \
   && apt-get install -y --no-install-recommends nodejs \
   && rm -rf /var/lib/apt/lists/*)
{{- if .Packages}}

# Additional packages
RUN apt-get update && apt-get install -y --no-install-recommends \
    {{join .Packages " "}} \
  && rm -rf /var/lib/apt/lists/*
{{- end}}

# Copy Klaus agent from source image
COPY --from=klaus-source /usr/local/lib/node_modules/@anthropic-ai /usr/local/lib/node_modules/@anthropic-ai
COPY --from=klaus-source /usr/local/bin/claude /usr/local/bin/claude
COPY --from=klaus-source /usr/local/bin/klaus /usr/local/bin/klaus

WORKDIR /workspace
EXPOSE 8080
ENTRYPOINT ["klaus"]
`

// ValidatePackages checks that all package names are valid Debian package
// names. This prevents shell injection through the Dockerfile template.
func ValidatePackages(packages []string) error {
	for _, p := range packages {
		if !packageNameRe.MatchString(p) {
			return fmt.Errorf("invalid package name %q: must match %s", p, packageNameRe.String())
		}
	}
	return nil
}

// GenerateDockerfile renders a Dockerfile that builds a composite image
// layering Klaus agent capabilities on top of the given base image.
// The generated Dockerfile uses a multi-stage build: it copies the klaus
// binary and Claude Code CLI from the klaus image into the base image,
// installs system dependencies and Node.js, and optionally installs
// additional apt packages.
func GenerateDockerfile(klausImage, baseImage string, packages []string) (string, error) {
	if err := ValidatePackages(packages); err != nil {
		return "", fmt.Errorf("validating packages: %w", err)
	}

	var buf bytes.Buffer
	data := dockerfileData{
		KlausImage:       klausImage,
		BaseImage:        baseImage,
		Packages:         packages,
		NodeSetupVersion: nodeSetupVersion,
	}
	if err := dockerfileTmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("executing Dockerfile template: %w", err)
	}
	return buf.String(), nil
}

// CompositeTag computes a deterministic image tag from the build inputs.
// The tag format is "klausctl-toolchain:<content-hash>" where the hash is
// derived from the Dockerfile template, Klaus image ref, base image ref,
// and sorted package list. Including the template ensures that upgrading
// klausctl with a changed template invalidates the cache.
// Package order does not affect the resulting tag.
func CompositeTag(klausImage, baseImage string, packages []string) string {
	sorted := make([]string, len(packages))
	copy(sorted, packages)
	sort.Strings(sorted)

	h := sha256.New()
	fmt.Fprintf(h, "tmpl=%s\n", dockerfileContent)
	fmt.Fprintf(h, "klaus=%s\n", klausImage)
	fmt.Fprintf(h, "base=%s\n", baseImage)
	for _, p := range sorted {
		fmt.Fprintf(h, "pkg=%s\n", p)
	}
	return fmt.Sprintf("klausctl-toolchain:%x", h.Sum(nil)[:12])
}

// Build orchestrates the composite image build for a toolchain configuration.
// When the toolchain is marked as prebuilt, the image is used directly without
// building. Otherwise it generates a Dockerfile, checks if the image already
// exists locally, and builds it if necessary. The Dockerfile is written to the
// rendered directory for debugging. Docker layer caching makes subsequent
// builds instant after the first run.
func Build(ctx context.Context, rt runtime.Runtime, klausImage string, toolchain *config.Toolchain, renderedDir string) (string, error) {
	if toolchain.Prebuilt {
		return toolchain.Image, nil
	}

	dockerfile, err := GenerateDockerfile(klausImage, toolchain.Image, toolchain.Packages)
	if err != nil {
		return "", fmt.Errorf("generating toolchain Dockerfile: %w", err)
	}

	// Write the Dockerfile to the rendered directory for debugging.
	dfPath := filepath.Join(renderedDir, "Dockerfile.toolchain")
	if err := os.WriteFile(dfPath, []byte(dockerfile), 0o644); err != nil {
		return "", fmt.Errorf("writing toolchain Dockerfile: %w", err)
	}

	tag := CompositeTag(klausImage, toolchain.Image, toolchain.Packages)

	// Fast path: skip build if the image already exists locally.
	exists, err := rt.ImageExists(ctx, tag)
	if err != nil {
		return "", fmt.Errorf("checking for existing image: %w", err)
	}
	if exists {
		return tag, nil
	}

	// Build the composite image.
	if _, err := rt.BuildImage(ctx, runtime.BuildOptions{
		Tag:        tag,
		Dockerfile: dfPath,
		Context:    renderedDir,
	}); err != nil {
		return "", fmt.Errorf("building toolchain image: %w", err)
	}

	return tag, nil
}
