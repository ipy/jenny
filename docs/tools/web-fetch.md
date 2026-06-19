---
title: WebFetch Tool
slug: web-fetch
priority: P3
status: done
spec: complete
code: done
package: internal/tool
gaps:
  []
depends_on:
  - tool-registry
---
# WebFetch Tool

## Overview

Fetches URL content and converts HTML to markdown.

## Parameters

| Param | Description |
|-------|-------------|
| `url` | HTTP(S) URL (max 2000 chars) |

## Limits

| Limit | Value |
|-------|-------|
| Response body | 10 MB |
| Timeout | 60s |
| Redirect hops | 10 |
| Result markdown | 100K chars |
| URL length | 2000 chars |

Reject credentials in URL.

## Behavior

- HTML → markdown (html-to-markdown).
- Domain blocklist preflight (10s timeout).
- Cache: 15 min / 50 MB LRU; hostname cache 5 min.
- Cross-host redirect → instruct model to re-fetch redirect URL (no auto cross-host follow).
- Binary saved to disk with path note.

## SSRF Protection

No interactive per-hostname permission UI. Instead, a **SSRF blocklist** runs before fetch:

- Hardcoded blocklist for common SSRF targets (localhost, metadata endpoints, …).
- DNS resolution with private/loopback range detection.
- Per-hostname cache (5 min TTL) for blocked hosts.
- Pinned dialer uses pre-resolved IPs to prevent DNS rebinding.

Auth warning in tool description.

## Acceptance Criteria

- **AC1:** 10MB body limit enforced.
- **AC2:** HTML capped at 100K chars markdown.
- **AC3:** Blocklist preflight before fetch.
- **AC4:** Cross-host redirect returns re-fetch instruction.
- **AC5:** Credentials in URL rejected.
