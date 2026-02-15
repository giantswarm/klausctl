# klausctl

CLI for managing local [klaus](https://github.com/giantswarm/klaus) instances.

klausctl is the local-mode counterpart to the Helm chart and the klaus-operator. It produces the same env vars, flags, and file mounts that the klaus Go binary expects, but through a developer-friendly CLI backed by Docker or Podman.

## Features

- **Container lifecycle management** -- start, stop, status, logs for local klaus instances
- **Plugin fetching via ORAS** -- pull Claude Code plugins from OCI registries before container start
- **Config rendering** -- generate `.mcp.json`, `settings.json`, `SKILL.md` files from a single config file
- **Container runtime auto-detection** -- Docker or Podman, with preference configurable
- **Environment variable forwarding** -- pass secrets from host to container
- **Self-update** -- `klausctl update` to fetch the latest release

## Quick Start

```bash
# Create a default configuration
klausctl config init

# Edit the configuration
$EDITOR ~/.config/klausctl/config.yaml

# Set your API key
export ANTHROPIC_API_KEY=sk-ant-...

# Start a klaus instance
klausctl start

# Check status
klausctl status

# View logs
klausctl logs -f

# Stop the instance
klausctl stop
```

## Usage

```
klausctl start    # Start a klaus instance
klausctl stop     # Stop the running instance
klausctl status   # Show instance status (running, MCP endpoint, uptime)
klausctl logs     # Stream container logs (-f to follow)
klausctl config   # Manage configuration (init, show, path)
klausctl version  # Show version information
```

## Configuration

Config file at `~/.config/klausctl/config.yaml`:

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
~/.config/klausctl/config.yaml
         |
         v
    klausctl CLI
         |
         +-- ORAS client (pkg/oci/)
         |      Pull plugins to ~/.config/klausctl/plugins/
         |
         +-- Config renderer (pkg/renderer/)
         |      Generate .mcp.json, settings.json, SKILL.md files
         |      in ~/.config/klausctl/rendered/
         |
         +-- Container runtime (Docker or Podman, auto-detect)
                docker/podman run with:
                  -e ANTHROPIC_API_KEY=...
                  -e CLAUDE_AGENTS=...
                  -e CLAUDE_ADD_DIRS=/etc/klaus/extensions
                  -e CLAUDE_PLUGIN_DIRS=/mnt/plugins/gs-platform,...
                  -v ~/.config/klausctl/plugins/gs-platform:/mnt/plugins/gs-platform
                  -v ~/.config/klausctl/rendered/extensions:/etc/klaus/extensions
                  -v ~/workspace:/workspace
                  -p 8080:8080
                  gsoci.azurecr.io/giantswarm/klaus:latest
```

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
