# Muster bridge

The muster bridge is klausctl's local lifecycle manager for
[muster](https://github.com/giantswarm/muster), an HTTP aggregator that
exposes stdio MCP servers behind a single HTTP endpoint. It spawns a `muster
serve` process, tracks PID/port, health-checks the `/mcp` endpoint, and
registers the resulting URL in `~/.config/klausctl/mcpservers.yaml` so
containerized klaus instances can reach it via `host.docker.internal`.

Containerized agents cannot reach stdio MCP servers directly. The muster
bridge solves this by aggregating the servers you configure in
`~/.config/klausctl/muster/mcpservers/` and making them available at a
single HTTP endpoint. Instance configs reference the bridge via
`mcpServerRefs: [muster]`.

For managing a local `klaus-gateway` process instead, see
[docs/gatewaybridge.md](gatewaybridge.md).

## CLI

```
klausctl muster start
klausctl muster stop
klausctl muster status
klausctl muster restart
```

`restart` is a convenience alias for stop followed by start; it is useful
after adding or editing files under `muster/mcpservers/`.

## MCP tools

Agents running inside `klausctl serve` see three tools:

- `klaus_muster_start` -- starts the bridge; returns the status JSON.
- `klaus_muster_stop` -- no arguments.
- `klaus_muster_status` -- returns the same JSON shape the CLI emits.

Unlike the gateway bridge, `muster start` accepts no flags. All
configuration lives in `muster/config.yaml` and the `muster/mcpservers/`
directory.

## Binary resolution

The bridge locates the muster binary via a PATH lookup:

```
exec.LookPath("muster")
```

When `muster` is not on `$PATH`, start fails with:

```
muster binary not found in PATH; install it with: go install github.com/giantswarm/muster@latest
```

There is no env-var or flag override for the binary path.

## State directory layout

```
~/.config/klausctl/
  mcpservers.yaml         # klausctl auto-registers muster on start
  muster/                 # mixed ownership -- see below
    config.yaml           # user-owned: aggregator port
    mcpservers/           # user-owned: one YAML file per MCP server
      *.yaml
  muster.pid              # klausctl-owned
  muster.port             # klausctl-owned
```

### Ownership boundaries

**klausctl owns**

- `muster.pid`
- `muster.port`

**The user owns**

- `muster/config.yaml`
- `muster/mcpservers/*.yaml`

klausctl never writes to user-owned files; the user should never touch
klausctl-owned files. `muster stop` cleans up the klausctl-owned PID/port
files and removes the `muster` entry from `mcpservers.yaml` but leaves
`muster/config.yaml` and `muster/mcpservers/` untouched.

### `muster/config.yaml` schema

All fields are optional. klausctl reads only `aggregator.port`; muster reads
the full file.

```yaml
aggregator:
  port: 8090          # overrides the default muster listen port
```

### `muster/mcpservers/*.yaml`

Each `.yaml` file in `muster/mcpservers/` describes one MCP server. Muster
reads these files directly; klausctl only checks that at least one file
exists before starting the bridge. Refer to the
[muster documentation](https://github.com/giantswarm/muster) for the
per-file schema.

## Bootstrap matrix

The bridge has two prerequisites. Start fails fast with a descriptive error
when either is unmet.

| Condition | Outcome |
|-----------|---------|
| `muster` not on `$PATH` | error: binary not found |
| `muster/mcpservers/` empty or missing | error: no MCP server files configured |
| Both met, bridge not yet running | start succeeds; PID/port files written |
| Both met, bridge already running | idempotent; existing process reused |

## Container networking

The bridge listens on the port muster binds to (default `0.0.0.0:8090`).
Containers reach it at `http://host.docker.internal:<port>/mcp` via the
`--add-host host.docker.internal:host-gateway` flag that klausctl already
adds to `docker run` / `podman run`. On Linux, the extra-host flag is
mandatory; macOS and Windows resolve `host.docker.internal` natively.

## Troubleshooting

**Stale PID file after an ungraceful shutdown.** `muster status` detects a
stale PID, removes the PID/port files, and reports `not running`. A fresh
`muster start` brings the bridge back up.

**Start fails because no server files exist.** klausctl refuses to start the
bridge when `muster/mcpservers/` is empty or absent. Create at least one
`.yaml` file there before running `muster start`.

**Port collision with an existing process.** Set `aggregator.port` in
`muster/config.yaml` to a free port and run `muster restart`. The
`mcpservers.yaml` entry is updated to match.

**New server files not picked up.** `muster restart` reloads
`muster/mcpservers/` without manual PID/port cleanup.
