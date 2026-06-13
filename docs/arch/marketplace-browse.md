---
title: Marketplace Browsing
slug: marketplace-browse
priority: P1
status: not_started
spec: complete
code: not_started
package: internal/portal
gaps:
  - Default marketplace URL
  - tar.gz extraction logic
  - MCP install to mcp.json
depends_on:
  - webui-portal
---

# Marketplace Browsing

## Overview

The Marketplace tab shows only installed assets. This iteration adds the ability to browse and install new skills, plugins, and MCP servers from external sources.

## API Endpoints

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
- `400 Bad Request`: Invalid URL (non-http/https scheme)
- `502 Bad Gateway`: Unreachable URL or fetch failure

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
- `plugin` → `<cwd>/.jenny-plugin/<name>/`
- `mcp` → updates `~/.jenny/mcp.json`

**Response:**
```json
{
  "status": "installed",
  "path": "string"
}
```

**Error Responses:**
- `409 Conflict`: Already installed (directory exists)

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

1. **handler.go**: Add `MarketplaceItem` type, `handleMarketplaceBrowse`, `handleMarketplaceInstall` handlers
2. **install.go**: New file with `downloadAndExtract` function using `archive/tar` and `compress/gzip`

### Frontend

1. **App.tsx**: Add `MarketplaceBrowseView` component
2. Marketplace tab shows "Browse" button
3. Browse view fetches items via API, shows Install buttons
4. Install updates view state (button → "Installed" / disabled)

## Out of Scope

- Version bump
- Marketplace source management UI
- Package versioning/updates
- Uninstall from UI
- Private marketplace authentication
- Search/filtering
- Zip format support (tar.gz only)
