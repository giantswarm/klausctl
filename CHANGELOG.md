# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).



## [Unreleased]

### Added

- Add unified `validate`, `pull`, and `list` subcommands for plugins, personalities, and toolchains. ([#31](https://github.com/giantswarm/klausctl/issues/31))
  - `klausctl plugin validate|pull|list [--remote]` -- manage OCI plugin artifacts.
  - `klausctl personality validate|pull|list [--remote]` -- manage OCI personality artifacts.
  - `klausctl toolchain validate|pull` -- validate toolchain directories and pull images.
  - `klausctl toolchain list --remote` -- query remote registry tags for locally cached toolchain images.
- Add `PersonalitiesDir` to config paths for OCI personality cache storage. ([#31](https://github.com/giantswarm/klausctl/issues/31))

### Changed

- Refactor `pkg/oci/` to import shared `giantswarm/klaus-oci` library for media type constants, metadata types, OCI annotations, and the ORAS client. klausctl-specific helpers (cache paths, container mount paths) remain in `pkg/oci/`. ([#31](https://github.com/giantswarm/klausctl/issues/31))

### Added

- Add `klausctl completion` command for bash, zsh, fish, and powershell shell completions.
- Add `--output json` flag to `klausctl status` and `klausctl toolchain list` for scripting.
- Add `--wide` flag to `klausctl toolchain list` to show image ID and size columns.
- Add `--effective` flag to `klausctl config show` to display resolved config with defaults applied.
- Stream Docker/Podman pull progress during `klausctl start` instead of silent waits.
- Add color output for status indicators and warnings (respects `NO_COLOR` env var).
- Add `klausctl toolchain list` to list locally cached toolchain images with tabular output. ([#20](https://github.com/giantswarm/klausctl/issues/20))
- Add `klausctl toolchain init --name <name>` to scaffold a new toolchain image repository with Dockerfiles, Makefile, CI config, and README. ([#20](https://github.com/giantswarm/klausctl/issues/20))
- Add `Images()` and `Pull()` methods to `Runtime` interface. ([#20](https://github.com/giantswarm/klausctl/issues/20))

### Fixed

- Fix CircleCI config to use `klausctl` binary name instead of `template` from project scaffold. ([#9](https://github.com/giantswarm/klausctl/issues/9))

### Changed

- `klausctl stop` is now idempotent -- exits 0 when no instance is running.
- `klausctl status` returns exit code 1 when no instance is found, enabling `if klausctl status` in scripts.
- Runtime auto-detection now prefers Docker over Podman and shows a hint about the `runtime` config key.
- Warnings (e.g. missing `ANTHROPIC_API_KEY`) now appear after the success summary instead of before the action context.
- `klausctl toolchain list` default table now includes SIZE column.
- Integrate toolchain image into `klausctl start`: when a toolchain image (e.g., `giantswarm/klaus-go:1.0.0`) is configured via the `image` field, it is used directly for container run, tracked in instance state, and displayed in `klausctl status`. ([#19](https://github.com/giantswarm/klausctl/issues/19))
- Implement OCI plugin pulling via ORAS for `klausctl start`. ([#5](https://github.com/giantswarm/klausctl/issues/5))
  - ORAS-based client (`pkg/oci/`) with Pull, Push, Resolve, and List operations.
  - Registry auth from Docker config, Podman auth, or `KLAUSCTL_REGISTRY_AUTH` env var.
  - Digest-based caching -- unchanged plugins are skipped on subsequent pulls.
  - OCI artifact format with custom media types for plugin config and content layers.
- Add `klausctl self-update` command for self-updating to the latest GitHub release. ([#7](https://github.com/giantswarm/klausctl/issues/7))
- Implement klausctl CLI for managing local klaus instances. ([#3](https://github.com/giantswarm/klausctl/issues/3))
  - `klausctl start` -- start a local klaus container with configured settings.
  - `klausctl start --workspace` -- override workspace directory from CLI.
  - `klausctl stop` -- stop and remove the running instance.
  - `klausctl status` -- show instance status (running state, MCP endpoint, uptime).
  - `klausctl logs` -- stream container logs (with `-f` follow and `--tail N` support).
  - `klausctl config init` -- create a default configuration file.
  - `klausctl config show` -- display current configuration.
  - `klausctl config path` -- print configuration file path.
  - `klausctl config validate` -- validate configuration file syntax and values.
  - `klausctl version` -- show version information.
- Configuration file at `~/.config/klausctl/config.yaml` mirroring Helm chart values.
- Container runtime auto-detection (Podman preferred over Docker when both available).
- Config rendering: generate `mcp-config.json`, `settings.json`, `SKILL.md` files from config.
- Environment variable forwarding (ANTHROPIC_API_KEY auto-forwarded, custom vars configurable).
- OCI plugin directory structure for container mounts.

### Fixed

- Align plugin container mount path with Helm chart (`/var/lib/klaus/plugins/` instead of `/mnt/plugins/`).
- Preserve hook `timeout` field in rendered `settings.json` (was silently dropped).
- Add `loadAdditionalDirsMemory` toggle to control `CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD` (defaults to `true`, matching Helm chart).
- Validate that `hooks` and `claude.settingsFile` are mutually exclusive.

### Added

- Support for additional Claude config fields to match Helm chart parity:
  `jsonSchema`, `settingsFile`, `settingSources`, `includePartialMessages`,
  `mcpTimeout`, `maxMcpOutputTokens`.

[Unreleased]: https://github.com/giantswarm/klausctl/tree/main
