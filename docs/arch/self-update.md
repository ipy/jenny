---
title: Self-Update and CDN Download
slug: self-update
priority: P2
status: done
spec: complete
code: done
package: internal/update, cmd/jenny
depends_on:
  - cli
  - jenny-directory-structure
---

# Self-Update and CDN Download

Jenny fetches its own binary updates and model registry data from a remote info endpoint backed by geo-sorted CDN mirrors. The system optimizes for reliability across regions with poor connectivity to GitHub.

## Overview

On startup (when not offline), jenny contacts an info service with its current version, platform, and models version. The response indicates whether updates are available and provides a list of CDN base URLs to try. Jenny downloads resources from the CDN, verifies SHA-256 integrity, and installs them locally.

## Info Endpoint

Jenny sends a single HTTP GET to the info service. The path structure:

```
/v1/{jenny_version}/{os}/{arch}/{models_version}
```

- `jenny_version`: current jenny binary version (e.g. `0.4.0`)
- `os`: runtime.GOOS (e.g. `darwin`, `linux`, `windows`)
- `arch`: runtime.GOARCH (e.g. `arm64`, `amd64`)
- `models_version`: version identifier from local models cache (or `none` if no cache)

## Info Response Schema

```json
{
  "executable": {
    "version": "string",
    "path": "string (relative)",
    "sha256": "hex string"
  },
  "models": {
    "version": "string",
    "path": "string (relative)",
    "sha256": "hex string"
  },
  "cdn": ["string (URL prefix with trailing slash)"]
}
```

- `executable` is `null` when the running version is already latest.
- `models` is `null` when the models cache is already latest.
- `cdn` is an ordered list of base URL prefixes. Each prefix is combined with the resource `path` to form a full download URL. Order reflects geo-priority (nearest/most-reliable first).

## CDN Download Strategy

For each resource that needs updating:

1. Iterate through CDN prefixes in order.
2. For each prefix, attempt HTTP GET of `{prefix}{path}`.
3. On successful download, compute SHA-256 of the response body.
4. If SHA matches, write to disk atomically (write to `.tmp`, rename).
5. If SHA mismatches or download fails, try the next CDN prefix.
6. If all CDN prefixes fail, log a warning and continue with stale data.

Downloads use a 60-second timeout per attempt.

## Managed Binary Location

Updated executables are stored at:

```
$JENNY_HOME/bin/jenny
```

Binaries are served raw from CDN (UPX-compressed at build time, no additional gzip layer). The SHA-256 is computed on the raw binary as downloaded.

This path is never in `$PATH` directly. The PATH-installed jenny binary acts as a launcher.

## Launcher Behavior

On startup, before any other initialization:

1. Determine the managed binary path: `$JENNY_HOME/bin/jenny`.
2. If that file exists and is executable, exec it (replacing the current process), forwarding all arguments and environment.
3. If not, continue normal execution.

The launcher check happens unconditionally (even in offline mode) because it's a local filesystem check with no network dependency.

To prevent infinite exec loops, the launched binary sets an environment variable `_JENNY_MANAGED=1`. The launcher only execs if this variable is NOT already set.

## Models Update

When a newer models version is available:

1. Download `models.json.gz` from CDN.
2. Verify SHA-256 of the compressed payload.
3. Decompress (gzip) and write to the registry cache path (`$JENNY_HOME/models.json`).
4. Update `$JENNY_HOME/meta.json` with the new version string and timestamp.
5. Notify the in-memory model registry to reload from the updated cache file.

This replaces the direct GitHub fetch in the registry when the CDN path succeeds. The existing `DefaultRegistryURL` fetch remains as a fallback when the info endpoint is unreachable.

## Update Flow Timing

- The info check runs in the background (goroutine) at startup, similar to the existing registry background fetch.
- Binary updates are downloaded but not applied until the next invocation (no hot-swap during a running session).
- Models updates are applied immediately to the cache file (the in-memory registry may already be loaded from the old cache for the current session).

## Configuration

| Source | Key | Effect |
|--------|-----|--------|
| `--offline` | N/A | Disables all update checks |
| `JENNY_INFO_URL` | env | Override info endpoint base URL |
| `JENNY_NO_UPDATE` | env | Set to `1` to disable self-update checks entirely |

## Meta File Extension

The existing `$JENNY_HOME/meta.json` gains a `modelsVersion` field:

```json
{
  "fetchedAt": "...",
  "etag": "...",
  "schemaVersion": 1,
  "modelsVersion": "20250625"
}
```

This is read on startup to populate the models_version path segment in the info request.
