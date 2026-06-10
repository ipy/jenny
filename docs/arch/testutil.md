---
title: Test Utilities
slug: testutil
priority: P3
status: done
spec: done
code: done
package: internal/testutil
gaps: []
depends_on: []
---
# Test Utilities

## Overview

`internal/testutil` is a shared test helper package that provides utilities used by
tests across multiple internal packages. It exists as a neutral location to avoid
import cycles between packages that need the same helpers.

## Purpose

The package was introduced to break the import cycle between `internal/agent` and
`internal/tool`: both packages needed `CaptureStdout` and `SSELine` helpers, but
neither could import the other. Moving these helpers to a neutral package
(`internal/testutil`) that neither `agent` nor `tool` depends on resolved the cycle.

## API

### CaptureStdout

```go
func CaptureStdout(t *testing.T, fn func()) string
```

Redirects `os.Stdout` to a pipe for the duration of `fn` and returns everything
written. Uses a background goroutine to drain the pipe, avoiding deadlocks when
`fn` produces large output. Calls `t.Helper()`.

### SSELine

```go
func SSELine(event, data string) string
```

Formats a Server-Sent Events (SSE) line in the format:
`event: <event>\ndata: <data>\n\n`. Used by agent streaming tests to construct
mock SSE responses.

## Usage

Packages import `internal/testutil` and call the functions directly:

```go
import "github.com/ipy/jenny/internal/testutil"

// In a test:
output := testutil.CaptureStdout(t, func() {
    // code that writes to stdout
})
```

## Headless Protocol Compatibility

Test utilities do not affect runtime behavior. They are compile-test-time only.
