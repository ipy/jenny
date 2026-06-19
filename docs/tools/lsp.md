---
title: LSP Tool
slug: lsp
priority: P3
status: done
spec: partial
code: done
package: internal/tool
gaps:
  - Gitignore filtering only applied to findReferences and workspaceSymbol, not all operations
depends_on:
  - tool-registry
---
# LSP Tool

## Overview

Language Server Protocol operations for code intelligence. Read-only, concurrency-safe.

## Gating

Requires LSP client to be configured and connected (via `WithLSPEnabled(true)` + `WithLSPClient(client)` on the registry). Returns immediate error if not connected.

## Limits

Max file size: **10 MB**.

Coordinates: **1-based** line and character (editor style); convert to LSP 0-based internally.

## Operations

goToDefinition, findReferences, hover, documentSymbol, workspaceSymbol, goToImplementation, prepareCallHierarchy, incomingCalls, outgoingCalls.

Returns immediate error if LSP server not connected (no wait-for-init logic).

Filter gitignored locations from findReferences and workspaceSymbol results only.

## Acceptance Criteria

- **AC1:** 1-based coordinates in tool API.
- **AC2:** Clear error when LSP disconnected.
- **AC3:** Files >10MB rejected.
- **AC4:** Concurrency-safe read-only.
- **AC5:** Gitignored paths filtered from findReferences and workspaceSymbol results.
