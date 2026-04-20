# Remote klaus-gateway targeting

`klausctl run`, `klausctl prompt`, and `klausctl messages` can target a
remote `klaus-gateway` instead of a local klaus container. When `--remote=URL`
is set, the named instance is treated as a logical instance hosted by the
gateway: no local container is created, no local port file is read, and no
workspace is mounted. Requests flow directly to the gateway's
`/v1/{instance}/chat/completions` and `/v1/{instance}/mcp` endpoints.

This is the counterpart to the [local gateway bridge](gatewaybridge.md):
the bridge lets a local agent reach a local gateway; `--remote` lets a CLI
user reach someone else's gateway.

## Quick start

```bash
# Authenticate once per gateway host (browser-based OAuth).
klausctl auth login --remote=https://gw.example.com

# Run a prompt against a gateway-hosted instance.
klausctl run my-bot --remote=https://gw.example.com -m "summarise the week"

# Follow an existing conversation.
klausctl prompt my-bot --remote=https://gw.example.com -m "and last Friday?"

# Inspect saved messages for an instance.
klausctl messages my-bot --remote=https://gw.example.com
```

The same inputs are available on the MCP tools served by `klausctl serve`:
`klaus_run`, `klaus_prompt`, and `klaus_messages` each accept `remote` and
`session` alongside their existing parameters. A parity test in
`cmd/remote_parity_test.go` fails the build if the CLI and MCP surfaces
drift.

## Flags

Both `--remote` and `--session` are declared once in
`internal/remotesurface` so the three subcommands and three MCP tools stay
in lock-step.

- `--remote=URL` -- gateway base URL. When set, the target is the gateway
  and no local container is inspected or spawned. A trailing `/v1` is
  stripped so `https://gw.example.com` and `https://gw.example.com/v1`
  both work.
- `--session=NAME` -- session (thread) identity forwarded as
  `X-Klaus-Thread-ID`. When omitted, klausctl derives a stable value from
  a SHA-256 of the current working directory so repeated invocations from
  the same checkout land on the same conversation thread.

## Authentication

`klausctl auth login --remote=URL` runs the standard browser-based OAuth
flow (the same PKCE/CIMD client used for muster federations) and stores
the resulting tokens under:

```
~/.config/klausctl/auth/<host>[_port].yaml
```

with directory mode `0700` and file mode `0600`. Each record captures the
access token, refresh token, expiry, granted scopes, and the cached token
endpoint + client ID so refresh does not need to repeat discovery.

Related commands:

```
klausctl auth login  --remote=URL
klausctl auth status
klausctl auth logout --remote=URL
```

### Token refresh

Tokens are refreshed in two places so a stale access token never surfaces
to the user as a cryptic 401:

1. **Proactive (pre-call):** `resolveRemoteTarget` consults the cached
   record and refreshes when `ExpiresAt` is within 60 seconds.
2. **Reactive (on 401):** `streamRemoteCompletion` and the equivalent MCP
   handler retry the request exactly once after refreshing.

If either refresh path is rejected by the OAuth server (typically because
the refresh token was revoked or expired beyond its grace period),
klausctl returns a `re-run 'klausctl auth login --remote=URL'` hint
rather than a generic OAuth error.

## Routing headers

Every gateway call carries the following headers so operators can route
and log requests consistently:

| Header                 | Value                                                                |
|------------------------|----------------------------------------------------------------------|
| `X-Klaus-Channel`      | `cli` (constant; identifies klausctl)                                |
| `X-Klaus-Channel-ID`   | `os.Hostname()` (falls back to `unknown`)                            |
| `X-Klaus-User-ID`      | JWT `sub` claim when the bearer parses, else `$USER` / `$LOGNAME`    |
| `X-Klaus-Thread-ID`    | `--session` value, or `cwd-<sha256(cwd)[0:16]>`                      |
| `Authorization`        | `Bearer <access_token>` when an auth record exists                   |

The same header set is applied to both the completions stream (HTTP POST
with SSE response) and the MCP streamable-HTTP session used by
`klausctl messages`. See `pkg/remote/target.go` for the resolution rules
and `pkg/mcpclient/client.go` (`NewWithHeaders`) for the MCP integration.

## URL composition

For a base URL `https://gw.example.com` and instance name `my-bot`:

- Chat completions: `POST https://gw.example.com/v1/my-bot/chat/completions`
- MCP endpoint:     `https://gw.example.com/v1/my-bot/mcp`

The instance name is percent-encoded so names containing characters
outside the DNS-label grammar still produce a valid URL.

## Local mode remains unchanged

When `--remote` is **not** set, the three subcommands keep the exact
byte-for-byte wire format they had before this feature: the same local
port lookup, the same `http://localhost:<port>/v1/chat/completions`
endpoint, no `Authorization` header, and no `X-Klaus-*` headers. Nothing
in the local path checks any auth state.

## File layout

```
~/.config/klausctl/
  auth/                    # 0700
    gw.example.com.yaml    # 0600 per-host OAuth record
    gw.other.example:8080.yaml
```

Delete the directory (or use `klausctl auth logout`) to sign out of a
gateway. The next remote call will fail with the re-login hint.
