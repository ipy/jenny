---
title: Sandbox Abstraction
slug: sandbox
priority: P2
status: done
spec: complete
code: done
package: internal/sandbox
gaps:
  - AllowUnsandboxedCommands field exists but is never consumed by any backend
depends_on:
  - bash
---
# Sandbox Abstraction

## Overview

Pluggable OS-level sandbox wraps Bash and optionally Grep ripgrep. Policy from settings: filesystem, network, managed-domains-only.

## Pluggable Backend

Interface `SandboxManager` wrapping external sandbox-runtime.

Platforms: macOS, Linux, WSL2 (not WSL1). `none` backend represents sandbox disabled.

- `initialize()` builds config from settings.
- `wrapWithSandbox(command)` returns wrapped shell command.
- `refreshConfig()` after permission changes.
- `failIfUnavailable`: clear error when sandbox enabled but deps missing.

## Per-Invocation Opt-Out

- `sandbox.excludedCommands[]`: patterns not wrapped.
- `sandbox.allowUnsandboxedCommands`: config field exists but is not consumed by any backend in production.
- Bash `dangerouslyDisableSandbox` per call.

## Network Policy

| Mode | Behavior |
|------|----------|
| Normal | Merge allowedDomains + WebFetch allow rules |
| managed-domains-only | **Only** policy domains; block interactive ask |

Denied domains from permission deny rules always applied.

## Sandboxed Ripgrep

Config `sandbox.ripgrep`: `{ command, args, argv0 }`.

Grep tool uses sandboxed ripgrep when sandbox active.

## Filesystem Policy

- Allow write: paths in `FilesystemAllowedDirs` (populated by caller from `.`, temp dir, `--add-dir` paths).
- Deny write: paths in `FilesystemDenyDirs` (populated by caller from settings files, skills dir).

## Edge Cases

| Case | Expected behavior |
|------|-------------------|
| Sandbox off | Grep uses host ripgrep |

## Acceptance Criteria

- **AC1:** Bash wrapped unless excluded pattern matches.
- **AC2:** Managed-domains-only restricts network to policy list.
- **AC3:** Grep uses sandboxed ripgrep when sandbox on.
- **AC4:** Missing deps yield clear unavailable reason.
- **AC5:** refreshConfig without restart.
