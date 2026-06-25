---
title: LSP Tool
slug: lsp
priority: P3
status: done
spec: complete
code: done
package: internal/tool
gaps: []
depends_on:
  - tool-registry
---
# LSP Tool

## Overview

Language Server Protocol operations for code intelligence. Read-only, concurrency-safe. Backed by a server manager that discovers language servers from plugins and routes requests by file extension.

## Architecture

The LSP system has three layers:

- **Server Manager** — maintains language server processes, routes requests by file extension, and syncs file state via didOpen/didChange/didSave/didClose notifications.
- **Real Client** — JSON-RPC 2.0 client over stdio. Manages a single language server process with request/response multiplexing, timeouts, and graceful shutdown.
- **Tool** — exposes LSP operations as a single `lsp` tool with an `operation` parameter.

Server configurations are discovered from plugins via `.lsp.json` files referenced by the manifest's `lspServers` field. Each config specifies a `command`, `args`, and `extensionToLanguage` mapping.

## Gating

Enabled via `--lsp` CLI flag or `JENNY_LSP` environment variable. The registry method `WithLSPEnabled(true)` and `WithLSPManager(manager)` must both be set.

Returns immediate error if no language server is connected when a request arrives.

## Write/Edit Integration

Write and Edit tools notify the LSP server manager after successful file modifications via didChange and didSave notifications. Errors from LSP notifications do not affect the tool result.

## Limits

Max file size: **10 MB**.

Coordinates: **1-based** line and character (editor style); convert to LSP 0-based internally.

## Operations

goToDefinition, findReferences, hover, documentSymbol, workspaceSymbol, goToImplementation, prepareCallHierarchy, incomingCalls, outgoingCalls.

The tool opens files in the LSP server via didOpen before performing file-based operations if the file has not been opened yet.

Filter gitignored locations from findReferences and workspaceSymbol results.

## Acceptance Criteria

- **AC1:** 1-based coordinates in tool API.
- **AC2:** Clear error when LSP disconnected.
- **AC3:** Files >10MB rejected.
- **AC4:** Concurrency-safe read-only.
- **AC5:** Gitignored paths filtered from findReferences and workspaceSymbol results.
- **AC6:** Language server processes launched on demand by file extension routing.
- **AC7:** Write and Edit tools send didChange and didSave notifications to LSP servers.
- **AC8:** Server configurations loaded from plugins via .lsp.json files.
