# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).



## [Unreleased]

### Added

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
- OCI plugin directory structure (ORAS pull is a placeholder for now).

[Unreleased]: https://github.com/giantswarm/klausctl/tree/main
