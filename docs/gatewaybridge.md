# Gateway bridge

The gateway bridge is klausctl's local lifecycle manager for `klaus-gateway`
(and optionally the upstream `agentgateway` that `klaus-gateway` can run
behind). It mirrors the shape of [the muster bridge](musterbridge.md): it
spawns the process, tracks PID/port, health-checks `/healthz`, and registers
the resulting MCP HTTP endpoint in `~/.config/klausctl/mcpservers.yaml` so
containerized klaus instances can reach it via `host.docker.internal`.

## CLI

```
klausctl gateway start [--with-agentgateway] [--port N] \
                       [--agentgateway-port N] \
                       [--klaus-gateway-bin PATH] \
                       [--agentgateway-bin PATH] \
                       [--log-level LEVEL]
klausctl gateway status [-o json]
klausctl gateway stop
```

The CLI is the user-facing surface. Every flag is also accepted by the MCP
tools below; a parity test (`cmd/gateway_parity_test.go`) fails the build if
the two surfaces drift.

## MCP tools

Agents running inside `klausctl serve` see three tools:

- `klaus_gateway_start` -- same arguments as `gateway start`, returns the
  status JSON.
- `klaus_gateway_stop` -- no arguments.
- `klaus_gateway_status` -- returns the same JSON shape the CLI emits with
  `-o json`.

## Runtime selection

The bridge looks up the gateway binary in this order:

1. `--klaus-gateway-bin` / `--agentgateway-bin` flags on `gateway start`.
2. `KLAUS_GATEWAY_BIN` / `KLAUS_AGENTGATEWAY_BIN` environment variables.
3. `klaus-gateway` / `agentgateway` on `$PATH`.

When none of these resolve, start fails with a clear error. Container-mode
start via Docker/Podman is reserved for a future change: it will be wired to
the existing runtime package, and the `--klaus-gateway-bin` escape hatch will
still override it.

## State directory layout

```
~/.config/klausctl/
  mcpservers.yaml          # klausctl auto-registers klaus-gateway on start
  gateway/                 # mixed ownership -- see below
    config.yaml            # user-owned: adapters, agentgateway on/off, log level
    routes.bolt            # klausctl-owned: passed to klaus-gateway as --bolt-path
    slack-secrets.yaml     # user-owned (populated when the slack adapter ships)
  klaus-gateway.pid        # klausctl-owned
  klaus-gateway.port       # klausctl-owned
  agentgateway.pid         # klausctl-owned (only when --with-agentgateway)
  agentgateway.port        # klausctl-owned
```

### Ownership boundaries

Strict ownership is part of the contract and is covered by the
`TestOwnership` unit test in `pkg/gatewaybridge`.

**klausctl owns**

- `klaus-gateway.pid` / `klaus-gateway.port`
- `agentgateway.pid` / `agentgateway.port`
- `gateway/routes.bolt`

**The user owns**

- `gateway/config.yaml`
- `gateway/slack-secrets.yaml`

klausctl never writes to user-owned files; the user should never touch
klausctl-owned files. `gateway stop` cleans up the klausctl-owned PID/port
files and removes the `klaus-gateway` entry from `mcpservers.yaml` but
leaves `gateway/config.yaml` and `gateway/slack-secrets.yaml` untouched.

### `gateway/config.yaml` schema

All fields are optional. klausctl reads only the keys below; the gateway
binary itself reads the full file.

```yaml
logLevel: info          # debug|info|warn|error
port: 8080              # overrides the default klaus-gateway listen port
adapters:
  slack:
    enabled: true       # surfaced in `gateway status` under "Adapters"
  github:
    enabled: false
agentGateway:
  enabled: false        # auto-start agentgateway alongside klaus-gateway
  port: 8090
```

## Auto-start on `klaus create`

When the resolved instance config declares `requires.gateway.enabled: true`,
`klausctl create` / `start` calls `gatewaybridge.EnsureRunning(...)` before
the instance container starts. `klaus-gateway` is then reachable from the
container at `http://host.docker.internal:<port>/mcp` via the standard
`mcpServerRefs` flow.

To also start agentgateway, set `requires.gateway.withAgentgateway: true`.

```yaml
# ~/.config/klausctl/instances/<name>/config.yaml
requires:
  gateway:
    enabled: true
    withAgentgateway: false
```

## Container networking

Host-binary mode listens on `0.0.0.0` so containers reach it through the
`--add-host host.docker.internal:host-gateway` plumbing klausctl already adds
to `docker run` / `podman run`. On Linux, the extra-host flag is mandatory;
macOS and Windows resolve `host.docker.internal` natively.

## Troubleshooting

**Stale PID file after an ungraceful shutdown.** `gateway status` detects a
stale PID, removes the PID/port files, and reports `not running`. A fresh
`gateway start` brings the bridge back up.

**Port collision with an existing process.** Pass `--port <N>` (or set
`port` in `gateway/config.yaml`) to pick a free port. The registered
`mcpservers.yaml` entry is updated to match.

**Container vs host-binary mismatch.** `gateway status` reports the start
mode as `host` or `container` so you can tell which branch is active.
