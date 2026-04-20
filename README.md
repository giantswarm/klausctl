# klausctl

CLI for managing local [klaus](https://github.com/giantswarm/klaus) instances.

klausctl is the local-mode counterpart to the Helm chart and the klaus-operator. It produces the same env vars, flags, and file mounts that the klaus Go binary expects, but through a developer-friendly CLI backed by Docker or Podman.

## Features

- **Container lifecycle management** -- start, stop, status, logs for local klaus instances
- **Plugin fetching via ORAS** -- pull Claude Code plugins from OCI registries before container start, with digest-based caching
- **Config rendering** -- generate `.mcp.json`, `settings.json`, `SKILL.md` files from a single config file
- **Container runtime auto-detection** -- Docker or Podman, with preference configurable
- **Environment variable forwarding** -- pass secrets from host to container
- **Self-update** -- `klausctl update` to fetch the latest release from GitHub

## Quick Start

```bash
# Create a default configuration
klausctl config init

# Edit the configuration
$EDITOR ~/.config/klausctl/instances/default/config.yaml

# Set your API key
export ANTHROPIC_API_KEY=sk-ant-...

# Start a klaus instance
klausctl start default

# Check status
klausctl status default

# View logs
klausctl logs default -f

# Stop the instance
klausctl stop default
```

To target a remote [klaus-gateway](https://github.com/giantswarm/klaus-gateway)
instead of a local container, authenticate once and then pass `--remote=URL`
to `run`, `prompt`, or `messages`. See [docs/remote.md](docs/remote.md) for
details.

```bash
klausctl auth login --remote=https://gw.example.com
klausctl run my-bot --remote=https://gw.example.com -m "summarise the week"
```

## Usage

```
klausctl create <name> [workspace]   # Create and start a named instance
klausctl list                         # List known instances
klausctl delete <name>                # Delete an instance (container + files)
klausctl start <name>                 # Start an instance
klausctl start <name> --workspace .   # Start with workspace override
klausctl stop <name>                  # Stop an instance
klausctl status <name>                # Show instance status (running, MCP endpoint, uptime)
klausctl logs <name>                  # Stream container logs (-f to follow, --tail N for last N lines)
klausctl config               # Manage configuration (init, show, path, validate)
klausctl self-update           # Update klausctl to the latest release (--yes to skip prompt)
klausctl version              # Show version information
```

`start`, `stop`, `status`, and `logs` currently default to `default` when `<name>` is omitted. This implicit default is deprecated; use `default` explicitly to avoid future breakage.

## OCI registry cache

klausctl keeps a persistent on-disk cache of OCI registry responses so that
repeated invocations don't have to re-walk the catalog, re-list tags, or
re-resolve references every time. The cache is driven by the shared
[klaus-oci cache store](https://github.com/giantswarm/klaus-oci/issues/25)
and is enabled by default.

Location (first match wins):

1. `--cache-dir <dir>` (global flag)
2. `KLAUSCTL_CACHE_DIR=<dir>` (env var)
3. `$XDG_CACHE_HOME/klausctl/oci` (XDG default)
4. `~/.cache/klausctl/oci` (fallback when XDG is unset)

The cache is safe to delete at any time; the next invocation will
repopulate the entries it needs.

### Commands

```bash
klausctl cache info              # show location, size, per-layer entry counts
klausctl cache prune             # remove stale entries (safe default)
klausctl cache prune --all       # wipe everything, including fresh entries and blobs
klausctl cache refresh           # invalidate all index entries (blobs kept)
klausctl cache refresh --registry gsoci.azurecr.io
klausctl cache refresh --repo   gsoci.azurecr.io/giantswarm/klaus-plugins/gs-platform
```

All three commands support `--output json` for scripting.

### Bypassing the cache

Occasionally you want to skip the cache for a single command — e.g. to
confirm a registry change is visible right now:

```bash
klausctl --no-cache create --personality coding my-instance
KLAUSCTL_NO_CACHE=1 klausctl list
```

Neither `--no-cache` nor the env var are persistent; later invocations
will use the cache again.

### MCP tools

`klausctl serve` exposes three tools so agents can manage the cache:

- `klausctl_cache_info` — same data as `klausctl cache info --output json`.
- `klausctl_cache_prune` — accepts a boolean `all` argument.
- `klausctl_cache_refresh` — accepts optional `registry` / `repo`
  strings (mutually exclusive).

See [docs/cache-reproduction.md](docs/cache-reproduction.md) for a
back-to-back reproduction that shows a second `klausctl create` skipping
catalog/tag/reference traffic entirely.

## Configuration

Config file at `~/.config/klausctl/instances/default/config.yaml`:

```yaml
# Container runtime (auto-detected if not set)
runtime: docker  # or: podman

# Klaus image
image: gsoci.azurecr.io/giantswarm/klaus:latest

# Workspace to mount
workspace: ~/projects/my-repo

# Port for the MCP endpoint
port: 8080

# Claude configuration
claude:
  model: sonnet
  systemPrompt: "You are a helpful coding assistant."
  maxBudgetUsd: 5.0
  permissionMode: plan

# Forward host environment variables
envForward:
  - GITHUB_TOKEN

# Inline skills
skills:
  api-conventions:
    description: "API design patterns"
    content: |
      When writing API endpoints...

# Subagents (JSON format, highest priority)
agents:
  reviewer:
    description: "Reviews code changes"
    prompt: "You are a senior code reviewer..."

# Lifecycle hooks
hooks:
  PreToolUse:
    - matcher: "Bash"
      hooks:
        - type: command
          command: /etc/klaus/hooks/block-dangerous.sh

# MCP servers
mcpServers:
  github:
    type: http
    url: https://api.githubcopilot.com/mcp/
    headers:
      Authorization: "Bearer ${GITHUB_TOKEN}"

# OCI plugins (pulled via ORAS before container start)
plugins:
  - repository: gsoci.azurecr.io/giantswarm/klaus-plugins/gs-platform
    tag: v1.2.0
```

The configuration intentionally mirrors the Helm chart values structure so that knowledge transfers between local, standalone, and operator-managed modes.

## Architecture

```
~/.config/klausctl/instances/default/config.yaml
         |
         v
    klausctl CLI
         |
         +-- ORAS client (pkg/oci/)
         |      Pull plugins to ~/.config/klausctl/plugins/
         |
         +-- Config renderer (pkg/renderer/)
         |      Generate mcp-config.json, settings.json, SKILL.md files
         |      in ~/.config/klausctl/instances/default/rendered/
         |
         +-- Container runtime (Docker or Podman, auto-detect)
                docker/podman run with:
                  -e ANTHROPIC_API_KEY=...
                  -e CLAUDE_AGENTS=...
                  -e CLAUDE_ADD_DIRS=/etc/klaus/extensions
                  -e CLAUDE_PLUGIN_DIRS=/var/lib/klaus/plugins/gs-platform,...
                  -v ~/.config/klausctl/plugins/gs-platform:/var/lib/klaus/plugins/gs-platform
                  -v ~/.config/klausctl/instances/default/rendered/extensions:/etc/klaus/extensions
                  -v ~/workspace:/workspace
                  -p 8080:8080
                  gsoci.azurecr.io/giantswarm/klaus:latest
```

## Local bridges

klausctl can manage two local background services so containerized agents
reach host-side tooling:

- **Muster bridge** (`klausctl muster start|stop|status|restart`) -- aggregates
  stdio MCP servers behind a single HTTP endpoint. See
  [docs/musterbridge.md](docs/musterbridge.md).
- **Gateway bridge** (`klausctl gateway start|stop|status`) -- manages a local
  `klaus-gateway` process. See [docs/gatewaybridge.md](docs/gatewaybridge.md).

## Development

```bash
# Build
go build -o klausctl .

# Run
./klausctl version

# Run tests
go test ./...
```

See [docs/development.md](docs/development.md) for development instructions.

## License

Apache 2.0 -- see [LICENSE](LICENSE).
