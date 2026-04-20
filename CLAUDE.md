# klausctl – agent context

## Repository layout

- `cmd/` -- Cobra command definitions (one file per command group)
- `pkg/` -- shared library packages
- `internal/` -- packages not intended for external import
- `docs/` -- narrative documentation

## Key packages

| Package | Purpose |
|---------|---------|
| `pkg/musterbridge` | Local lifecycle manager for the muster aggregator |
| `pkg/gatewaybridge` | Local lifecycle manager for `klaus-gateway` |
| `pkg/config` | Instance config loading, path resolution (`Paths` struct) |
| `pkg/mcpserverstore` | Read/write `mcpservers.yaml` |
| `pkg/runtime` | Docker/Podman detection and container operations |
| `internal/tools/muster` | MCP tool registration for the muster bridge |
| `internal/tools/gateway` | MCP tool registration for the gateway bridge |

## Bridge documentation

- [docs/musterbridge.md](docs/musterbridge.md) -- state files, ownership, MCP tools, troubleshooting
- [docs/gatewaybridge.md](docs/gatewaybridge.md) -- state files, ownership, MCP tools, troubleshooting

## Conventions

- Bridge packages expose `Start`, `Stop`, `GetStatus`, `EnsureRunning` functions.
- CLI surface and MCP tool inputs must stay in sync. The gateway bridge enforces
  this with a parity test in `cmd/gateway_parity_test.go`.
- State files follow strict ownership: klausctl owns PID/port files; users own
  config files. klausctl never writes to user-owned files.
- New tests go in `_test.go` files in the same package as the code under test.
