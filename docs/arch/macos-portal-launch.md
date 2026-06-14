---
title: macOS Double-Click Portal Launch (WONTFIX)
slug: macos-portal-launch
priority: P1
status: complete
spec: complete
code: complete
package: cmd/jenny
depends_on:
  - webui-portal
---

# macOS Double-Click Portal Launch (WONTFIX)

## Overview

This document explains why double-clicking the `jenny` executable on macOS cannot launch the portal without opening a Terminal window, and documents the accepted workaround.

## macOS Limitation

**Double-clicking a bare Unix executable on macOS ALWAYS opens a Terminal window.** This is a fundamental macOS operating system behavior — the system provides no mechanism to suppress Terminal for bare Mach-O binaries.

When you double-click `jenny`:
1. macOS Finder launches Terminal.app
2. Terminal runs `/path/to/jenny` with no arguments
3. `jenny` with no arguments enters CLI mode (shows help)

This is not a bug in `jenny` — it is a macOS OS limitation.

## Solution

### Only `jenny portal` launches the portal

The `shouldLaunchPortal()` function now only returns true for the explicit `portal` subcommand:

```go
func shouldLaunchPortal() bool {
    return len(os.Args) >= 2 && os.Args[1] == "portal"
}
```

### Optional: Create a `.app` bundle manually

Users who want to launch the portal without Terminal must wrap the binary in a macOS `.app` bundle. This is the standard macOS practice for GUI applications.

Use `scripts/make-portal-app.sh` to create the bundle:

```bash
bash scripts/make-portal-app.sh
# or with custom binary path:
bash scripts/make-portal-app.sh /path/to/jenny
```

This creates `dist/Jenny Portal.app` which can be double-clicked to launch the portal without Terminal.

## Behavior Matrix

| Scenario | Command | Result |
|----------|---------|--------|
| `./jenny portal` | Terminal | Portal launches |
| `./jenny` | Terminal | CLI mode (shows help) |
| `./jenny -p "hello"` | Terminal | CLI mode |
| Double-click `jenny` | Terminal opens | CLI mode |
| Double-click `Jenny Portal.app` | No Terminal | Portal launches |

## Cross-Platform Notes

- This limitation applies to all bare executables on macOS (not just `jenny`)
- Linux/Windows double-click behavior follows their respective OS conventions
- The `.app` bundle approach is macOS-specific but harmless on other platforms
- `os.Executable()` may return different paths depending on how the binary was invoked

## Testable Acceptance Criteria

| AC | Description | Verification |
|----|-------------|--------------|
| AC1 | `./jenny` (no args, terminal) shows help | Does NOT launch portal |
| AC2 | `./jenny portal` launches portal | Backward compatible |
| AC3 | `go test ./cmd/jenny/` passes | Tests green |
| AC4 | `go test ./internal/portal/` passes | Tests green |
| AC5 | macOS double-click limitation documented | This doc |

## References

- `cmd/jenny/main.go` - `shouldLaunchPortal()` implementation
- `scripts/make-portal-app.sh` - Optional `.app` bundle generator
- `docs/arch/webui-portal.md` - Portal specification
