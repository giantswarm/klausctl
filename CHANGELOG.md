# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).



## [Unreleased]

### Changed

- Upgrade `klaus-oci` to v0.0.8 for the unified `Personality` type and typed artifact operations:
  - `PersonalitySpec` replaced by `Personality` with `Toolchain` field (`ToolchainReference`) instead of `Image` string.
  - Generic `Pull`/`ListArtifacts` replaced by typed `PullPlugin`/`PullPersonality` and `ListPlugins`/`ListPersonalities`/`ListToolchains`.
  - Batch `ResolvePluginRefs` replaced by individual `ResolvePluginRef` calls.
  - `ArtifactKind` constants removed; registry base passed via `WithRegistry` list option.

### Added

- Add secrets management with `klausctl secret set|list|delete` CLI commands and `klaus_secret_list` MCP tool. Secrets are stored in `~/.config/klausctl/secrets.yaml` with 0600 permissions and can be referenced by name in instance configs.
- Add managed MCP servers with `klausctl mcpserver add|list|remove` CLI commands and `klaus_mcpserver_add`, `klaus_mcpserver_list`, `klaus_mcpserver_remove` MCP tools. Managed servers are stored in `~/.config/klausctl/mcpservers.yaml` and resolved with optional Bearer token authentication at start time.
- Add `secretEnvVars`, `secretFiles`, and `mcpServerRefs` config fields for referencing secrets by name in instance configurations. At start time, `secretEnvVars` injects resolved secret values as container environment variables, `secretFiles` writes secret values to files and mounts them read-only, and `mcpServerRefs` merges managed MCP server definitions into `mcpServers` with Bearer token headers.
- Add `--secret-env`, `--secret-file`, and `--mcpserver` flags to `klausctl create` and corresponding `secretEnvVars`, `secretFiles`, `mcpServerRefs` parameters to the `klaus_create` MCP tool.

### Changed

- Migrate to `klaus-oci` v0.0.5 and delete the `pkg/oci/` wrapper package entirely. All callers now import `klaus-oci` directly for OCI types, constants, and helpers. Klausctl-specific orchestration (artifact resolution, plugin pulling, personality merging) moves to `pkg/orchestrator/`. ([#55](https://github.com/giantswarm/klausctl/issues/55))
  - Use typed convenience methods (`ResolveToolchainRef`, `ResolvePersonalityRef`, `ResolvePluginRef`) instead of generic `ResolveArtifactRef`.
  - Replace the 5-step manual listing flow with `client.ListArtifacts()` for concurrent artifact discovery.
  - Drop `Masterminds/semver/v3` as a direct dependency.
- Pass `WithFilter()` to `ListArtifacts` for toolchain listing, filtering repos at the library level before version resolution instead of after. Reduces toolchain list from ~68s to ~4s on gsoci.
- Upgrade `klaus-oci` to v0.0.7 for the `klaus-toolchains/` sub-namespace migration. Toolchain images now resolve to `gsoci.azurecr.io/giantswarm/klaus-toolchains/<name>` instead of `gsoci.azurecr.io/giantswarm/klaus-<name>`, narrowing `ListRepositories` from the entire `giantswarm/` catalog to just the toolchains sub-namespace. `ShortToolchainName` replaced by `ShortName`.

### Fixed

- Fix MCP config rendering for HTTP servers: auto-infer `"type": "http"` for URL-based entries and `"type": "stdio"` for command-based entries. Without the explicit type field, Claude Code misidentifies HTTP servers as stdio and hangs during initialization.

### Added

- Add `--env`, `--env-forward`, `--permission-mode`, `--model`, `--system-prompt`, and `--max-budget` flags to `klausctl create`, unifying config overrides between the CLI and MCP tool.
- Add `klausctl prompt` and `klausctl result` CLI commands to send prompts to and retrieve results from running klaus instances, mirroring the `klaus_prompt` and `klaus_result` MCP tools. `prompt` supports `--blocking` to wait for completion and `--message`/`-m` for the prompt text; both support `--output json`.
- Expose full config options in `klaus_create` MCP tool: `envVars`, `envForward`, `mcpServers`, `maxBudgetUsd`, `permissionMode`, `model`, and `systemPrompt` parameters, enabling programmatic instance creation without manual config editing. ([#46](https://github.com/giantswarm/klausctl/issues/46))
- Add `klaus_prompt` and `klaus_result` MCP tools to bridge the management and agent planes. `klaus_prompt` sends prompts to running instances with optional blocking mode, and `klaus_result` retrieves the agent's last response. Includes a lightweight MCP HTTP client (`pkg/mcpclient/`) with per-instance session caching. ([#45](https://github.com/giantswarm/klausctl/issues/45))
- Enhance `klaus_status` to include the agent's internal status (`agent_status` field) alongside the container status when the instance is running, eliminating an extra round-trip. ([#45](https://github.com/giantswarm/klausctl/issues/45))
- Add `klausctl serve` command that runs an MCP (Model Context Protocol) server over stdio, exposing klausctl's container lifecycle and artifact management as MCP tools for IDE integration (Cursor, Claude Code). ([#35](https://github.com/giantswarm/klausctl/issues/35))
  - Instance lifecycle tools: `klaus_create`, `klaus_start`, `klaus_stop`, `klaus_delete`, `klaus_status`, `klaus_logs`, `klaus_list`.
  - Artifact discovery tools: `klaus_toolchain_list`, `klaus_personality_list`, `klaus_plugin_list`.
- Add `LogsCapture` method to `Runtime` interface for programmatic log retrieval (returns string instead of streaming to stdout). ([#35](https://github.com/giantswarm/klausctl/issues/35))
- Add `pkg/orchestrator` package extracting shared container orchestration logic (`BuildRunOptions`, `BuildEnvVars`, `BuildVolumes`) for reuse by both CLI commands and MCP tool handlers. ([#35](https://github.com/giantswarm/klausctl/issues/35))
- Add `github.com/mark3labs/mcp-go` dependency for MCP protocol support. ([#35](https://github.com/giantswarm/klausctl/issues/35))

### Changed

- `plugin list` and `personality list` now default to remote registry listing, showing the latest version of each artifact and whether it is cached locally. Use `--local` for the previous local-only behavior. ([#42](https://github.com/giantswarm/klausctl/issues/42))

### Fixed

- Fix `:latest` tag resolution for personality, plugin, and toolchain short names. Short names without explicit tags (e.g. `--personality sre`) now query the registry for the highest semver tag instead of blindly appending `:latest`. Plugin references and toolchain images from personality specs with `:latest` or empty tags are also resolved. Config-level ref expansion no longer appends `:latest`; tag resolution is deferred to start time. Personality spec image resolution now consistently applies the `klaus-` name prefix for short toolchain names. Plugin validation no longer requires a tag at config load time since tags are resolved at start time. Path traversal is now rejected in rendered file names (skills, agents, hooks). ([#47](https://github.com/giantswarm/klausctl/issues/47))
- Fix `plugin list` and `personality list` remote discovery failing when no local cache exists. The commands now discover repositories directly from the OCI registry catalog via `klaus-oci` v0.0.3. ([#42](https://github.com/giantswarm/klausctl/issues/42))

### Added

- Add named instance lifecycle commands for local multi-instance workflows. ([#29](https://github.com/giantswarm/klausctl/issues/29))
  - `klausctl create <name> [workspace]` creates and starts a named instance.
  - `klausctl list` lists instance metadata in table or JSON format.
  - `klausctl delete <name>` removes an instance directory and runtime container.
- Add personality support for `klausctl start`. When `personality` is set in config, the personality OCI artifact is pulled, its `SOUL.md` is mounted into the container at `/etc/klaus/SOUL.md`, its plugins are merged with user-configured plugins (user wins on conflict), and its toolchain image is used unless the user explicitly overrides `image`. ([#30](https://github.com/giantswarm/klausctl/issues/30))
- Add unified `validate`, `pull`, and `list` subcommands for plugins, personalities, and toolchains. ([#31](https://github.com/giantswarm/klausctl/issues/31))
  - `klausctl plugin validate|pull|list [--remote]` -- manage OCI plugin artifacts.
  - `klausctl personality validate|pull|list [--remote]` -- manage OCI personality artifacts.
  - `klausctl toolchain validate|pull` -- validate toolchain directories and pull images.
  - `klausctl toolchain list --remote` -- query remote registry tags for locally cached toolchain images.
  - All subcommands support `--output json` for scripting (consistent with `status` and `toolchain list`).
- Add `PersonalitiesDir` to config paths for OCI personality cache storage. ([#31](https://github.com/giantswarm/klausctl/issues/31))

### Changed

- Add deprecation hints for implicit `default` instance selection when `<name>` is omitted in `start`, `stop`, `status`, and `logs`; users are now guided to pass `default` explicitly.
- Validate explicit `klausctl create --port` assignments against existing instance ports and fail on collisions.
- Persist per-instance `toolchain` metadata in `config.yaml` alongside resolved `image`, and prefer `toolchain` for `klausctl list` output.
- Move create-time personality plugin/image merge behavior into `GenerateInstanceConfig(...)` via a resolver callback, keeping command handlers thinner.
- Refactor `pkg/oci/` to import shared `giantswarm/klaus-oci` library for media type constants, metadata types, OCI annotations, and the ORAS client. klausctl-specific helpers (cache paths, container mount paths) remain in `pkg/oci/`. ([#31](https://github.com/giantswarm/klausctl/issues/31))

### Fixed

- Wire up `KLAUSCTL_REGISTRY_AUTH` env var for OCI credential resolution. After the migration to `klaus-oci`, the env var was no longer passed to the client. All OCI operations now use `NewDefaultClient()` which includes both Docker/Podman config file auth and the env var. ([#31](https://github.com/giantswarm/klausctl/issues/31))
- Fix `ShortName` extracting name with tag suffix when called on a full OCI reference (e.g. `gs-base:v0.6.0` instead of `gs-base`). The repository portion is now stripped of tag/digest before name extraction. ([#31](https://github.com/giantswarm/klausctl/issues/31))
- Fix validate commands writing to `os.Stdout` instead of `cmd.OutOrStdout()`, making output testable and consistent with the rest of the CLI. ([#31](https://github.com/giantswarm/klausctl/issues/31))

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
