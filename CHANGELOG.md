# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).



## [Unreleased]

### Added

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
