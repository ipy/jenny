---
title: Marketplace Browsing
slug: marketplace-browse
priority: P1
status: done
spec: complete
code: done
package: internal/portal
gaps: []
depends_on: []
---

# Marketplace Browsing

## Overview

The Marketplace tab shows only installed assets. This iteration adds the ability to browse and install new skills, plugins, and MCP servers from external sources.

## API Endpoints

### Authentication

Both endpoints require authentication via `token` query parameter or `Authorization: Bearer` header (same as all `/api/` endpoints).

### GET /api/marketplace/browse

Fetches a JSON marketplace index and returns parsed items.

**Query Parameters:**
- `source` (optional): URL to marketplace index JSON. Defaults to `https://raw.githubusercontent.com/ipy/jenny-marketplace/main/index.json`

**Response:** JSON array of `MarketplaceItem`:
```json
[
  {
    "name": "string",
    "type": "skill|plugin|mcp",
    "description": "string",
    "version": "string",
    "download_url": "string"
  }
]
```

**Error Responses:**
- `400 Bad Request`: Invalid URL (non-http/https scheme) or invalid marketplace index JSON
- `502 Bad Gateway`: Unreachable URL, fetch failure, or non-200 response from source

### POST /api/marketplace/install

Downloads and extracts a package to the correct directory.

**Request Body:**
```json
{
  "type": "skill|plugin|mcp",
  "name": "string",
  "download_url": "string"
}
```

**Install Paths:**
- `skill` → `~/.jenny/skills/<name>/`
- `plugin` → `<git-root>/.jenny-plugin/<name>/` (falls back to `<cwd>/.jenny-plugin/<name>/` if not in a git repo)
- `mcp` → downloads to temp directory, extracts, reads `manifest.json` (requires `command` field, optional `args`), validates, then writes structured config to `~/.jenny/mcp.json`

**Response:**
```json
{
  "status": "installed",
  "path": "string"
}
```

**Error Responses:**
- `400 Bad Request`: Missing required fields, invalid type, download_url must end with `.tar.gz`, invalid/missing manifest.json (MCP), empty command in manifest (MCP)
- `409 Conflict`: Already installed (directory exists)
- `500 Internal Server Error`: Download or extraction failure

Note: For MCP installs, the response `path` field returns the path to `mcp.json` (a file), not a directory.

## Marketplace Index Format

The marketplace index JSON format:
```json
{
  "skills": [...],
  "plugins": [...],
  "mcp": [...]
}
```

Each item has: `name`, `description`, `version`, `download_url`.

## Implementation Details

### Backend

- Marketplace item type and browse/install HTTP handlers
- Download and extract logic using tar/gzip (stdlib only)

### Frontend

- Marketplace browse view component
- Marketplace tab shows "Browse" button
- Browse view fetches items via API, shows Install buttons
- Install updates view state (button → "Installed" / disabled)

## Out of Scope

- Version bump
- Marketplace source management UI
- Package versioning/updates
- Uninstall from UI
- Private marketplace authentication
- Search/filtering
- Zip format support (only `.tar.gz` archives are accepted; non-`.tar.gz` URLs are rejected with 400)
