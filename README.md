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

## Usage

```
klausctl start    # Start a klaus instance
klausctl stop     # Stop the running instance
klausctl status   # Show instance status (running, MCP endpoint, cost, etc.)
klausctl logs     # Stream container logs
klausctl config   # Manage configuration
klausctl update   # Self-update to latest version
```

## Configuration

Config file at `~/.config/klausctl/config.yaml`:

```yaml
# Container runtime (auto-detected if not set)
runtime: docker  # or: podman

# Klaus image
image: ghcr.io/giantswarm/klaus:latest

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

# OCI plugins (pulled via ORAS before container start)
ociPlugins:
  - repository: gsoci.azurecr.io/giantswarm/claude-plugins/gs-platform
    tag: v1.2.0
```

See the [configuration documentation](docs/configuration.md) for the full reference.

## Development

See [docs/development.md](docs/development.md) for development instructions.

## License

Apache 2.0 -- see [LICENSE](LICENSE).
