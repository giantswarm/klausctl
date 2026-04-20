# Reproducing the zero-network second invocation

This is a manual reproduction that demonstrates the persistent
`klaus-oci` cache: a second `klausctl create --personality coding`
invocation against an unchanged registry performs zero registry traffic
for catalog, tag-list, and reference-resolution lookups while the entries
are still within the default `fresh_ttl` (30s).

## Prerequisites

- A working `klausctl` binary (either installed or built via `go build .`).
- Docker or Podman available locally so `create` can start an instance.
- Network access to the default klaus registry.

## Steps

1. Start from a clean cache so the first invocation has to populate it:

   ```shell
   klausctl cache prune --all
   ```

2. (Optional) Start a capturing HTTP proxy to confirm zero traffic on the
   second run. This is optional — the cache files on disk are the
   authoritative signal.

   ```shell
   HTTPS_PROXY=http://localhost:8080 mitmproxy -p 8080
   ```

3. Run the first create. This populates the catalog, tag-list, and
   reference index entries in the cache.

   ```shell
   klausctl create --personality coding my-instance-1
   klausctl delete my-instance-1 --force
   ```

4. Run the second create **within 30 seconds** of the first. The
   klaus-oci fresh TTL is 30s; within that window no HEAD or catalog
   request is issued.

   ```shell
   klausctl create --personality coding my-instance-2
   klausctl delete my-instance-2 --force
   ```

5. Inspect the cache to see what was reused:

   ```shell
   klausctl cache info --output json | jq
   ```

   The output will show the cache directory (under
   `$XDG_CACHE_HOME/klausctl/oci`), the per-layer entry counts
   (`catalog`, `tags`, `refs`, `blobs`), and the most recent write time.

## What "zero traffic" means

With the default TTLs:

- `fresh_ttl = 30s` — during this window the cache returns immediately
  without issuing any HEAD/GET against the registry.
- `stale_ttl = 24h` (refs/tags) or `7d` (catalog) — between fresh and
  stale, entries are still returned instantly, but a **background**
  revalidation is kicked off (not counted against the user-visible
  latency budget).

The acceptance scenario targets the fresh window. If the second
invocation is more than 30s after the first, the cache still avoids a
full round-trip — it issues a conditional HEAD/GET with the stored ETag
and the registry almost always replies `304 Not Modified`.

## How to force network traffic again

- `klausctl --no-cache create ...` — bypass for a single invocation.
- `KLAUSCTL_NO_CACHE=1 klausctl create ...` — env-var equivalent.
- `klausctl cache refresh --repo <host/repo>` — invalidate one
  repository's tag and ref entries.
- `klausctl cache refresh --registry <host>` — invalidate one registry
  catalog entry.
- `klausctl cache prune --all` — wipe everything and start over.

## Companion library

The cache implementation lives in
[giantswarm/klaus-oci#25](https://github.com/giantswarm/klaus-oci/issues/25).
The klausctl side (this repo) is responsible for:

- Choosing the cache directory and honouring `--no-cache` /
  `--cache-dir`.
- Exposing `cache info`, `cache prune`, and `cache refresh` commands.
- Surfacing the same operations as MCP tools (`klausctl_cache_info`,
  `klausctl_cache_prune`, `klausctl_cache_refresh`).
