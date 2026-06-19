---
title: E2E Test Harness
slug: e2e-test-harness
priority: P0
status: done
spec: complete
code: done
package: e2e
depends_on:
  - cli
  - stream-json-spec
  - anthropic-api-client
gaps:
  - test file listing needs periodic refresh
---
# E2E Test Harness

## Overview

Blackbox end-to-end test suite for jenny. The suite launches the compiled
`jenny` binary as a subprocess, drives it with CLI flags and stdin, and
asserts on its stdout, stderr, exit code, and the HTTP traffic it emits
against an in-process mock server. Tests are organized as Go test
functions under `e2e/`. No live API access is required.

## Directory Layout

```
e2e/
├── harness/                       # shared test infrastructure
│   ├── runner.go                  # jenny binary builder + spawner (RunJenny, RunTarget, etc.)
│   ├── types.go                   # TestCase, TargetInvocation, ExpectedBehavior, etc.
│   ├── comparator.go             # declarative comparison engine (Compare)
│   ├── suite.go                  # declarative SuiteRunner
│   └── reporter.go              # TextReporter / JSONReporter
├── fixtures/cassettes/           # SSE and JSON cassette files
├── api_protocol_test.go          # API request shape
├── cli_test.go                   # CLI flags
├── cost_tracking_test.go         # cost/usage
├── e2e_test.go                   # top-level test definitions
├── env_override_test.go          # env var override tests
├── helpers_test.go               # shared test helpers
├── max_tokens_clamp_test.go      # max_tokens clamping
├── minimax_test.go               # MiniMax provider parity
├── model_registry_test.go        # model registry
├── normalization_test.go         # message normalization
├── session_test.go               # session persistence
├── skill_plugin_test.go          # skills/plugin discovery
├── stream_json_test.go           # stream-json envelope
├── system_prompt_test.go         # system prompt assembly
├── tool_call_test.go             # tool call flows
├── tools_test.go                 # per-tool behavior
├── transcript_test.go            # transcript file tests
├── web_search_test.go            # web search tool
└── debt_*_test.go                # tech debt regression tests
```

All mock server, runner, and comparison infrastructure is consolidated
in `e2e/harness/`.

## System Prompt Verification

The `--print-system-prompt` flag allows verifying the assembled system prompt
without making any API calls. This is used by `e2e/system_prompt_test.go`
to assert on the presence of core tools, platform context, and overall
structure. This flag runs entirely offline and exits before any network or
session initialization.

## Cassette File Format

Cassettes are SSE text files (`.sse`) or JSON files (`.json`). One file per API exchange. The mock server streams SSE files verbatim as `Content-Type: text/event-stream`. The `Lookup()` function checks for `.sse` first, then `.json` as fallback.

Naming convention: `<cassette-id>.sse`, all lowercase, hyphen-separated,
unique across the suite. The cassette id is the only thing the mock
server needs to find a file; it is taken from the URL path prefix
`/cassette/<id>/v1/messages`.

## Mock Server

The mock server is started per-test via `harness.NewTestServer(t, cassetteID, opts...)` which delegates to `mockapi.NewMockServer(opts...)`. It returns a `*MockServer` handle for inspecting captured requests.

Captured requests:

Each POST appends a decoded copy of the request body to the mock
server's in-memory list. Tests call `Requests()` to retrieve a copy and
assert against the outbound request shape (model, stream flag, messages,
tools, etc.).

### Cassette sequences (multi-turn)

Single-turn flows are the common case, but tool use is a multi-turn
pattern: the model responds with `stop_reason: "tool_use"`, jenny runs
the tool, then makes a second `/v1/messages` call carrying the tool
result. The mock server supports this via per-cassette-id sequences:

```go
mock.SetSequence("tool-use", []string{"tool-use-turn1", "tool-use-turn2"})
```

## Binary Runner

The runner builds the jenny binary once per `go test` invocation using
`go build -o <tmpdir>/jenny ./cmd/jenny` and caches the result with
`sync.Once`.

`harness.RunJenny(t, env, args...)` returns a `RunResult`:

```go
type RunResult struct {
    Lines      []string         // raw stdout lines (newline-split)
    Parsed     []map[string]any // lines parsed as JSON (blanks skipped)
    Stdout     string           // raw stdout
    Stderr     string           // captured stderr
    ExitCode   int              // process exit code
    Dir        string           // working directory of the process
    DurationMs int64            // total execution time
}
```

## Declarative Test Infrastructure

The primary test-writing interface is declarative, defined in `harness/types.go`:

### TargetInvocation
Defines how to invoke jenny:
- `Kind`: `"cli"`, `"prompt"`, or `"subprocess"`
- `Prompt`: prompt string
- `Format`: output format (`"text"`, `"stream-json"`)
- `Cassette` / `CassetteSequence`: mock API responses
- `Args`, `Env`: additional CLI args and env vars (supports `${WORK_DIR}` and `${MOCK_URL}` macros)
- `WorkDirFiles`: map of relative path → content to provision before running
- `MockBehavior`: customize mock server behavior (e.g., `RejectEmptyToolProperties`)
- `TimeoutMs`: per-invocation timeout override

### ExpectedBehavior
Defines assertions:
- `ExitCode`: expected exit code
- `Stdout`, `Stderr`: substring/regex assertions
- `StreamJSON`: `StreamJSONExpectation` for stream-json assertions
- `APIRequests`: `APIRequestExpectation` for outbound request shape
- `FileSystem`: `FileSystemExpectation` for work directory file assertions

### SuiteRunner and Compare
`SuiteRunner` iterates `TestCase` definitions, builds args, launches the binary, and passes results to `Compare()` which runs all expectation checks.

### Reference Binary Comparison
When `REFERENCE_BIN` is set, the suite also runs the reference binary and compares stream-json output between builds via `CompareToReference` / `CompareJSONLines`.

### Runner Functions
- `RunJenny(t, env, args...)` — convenience wrapper using built jenny binary
- `RunJennyInDir(t, dir, env, args...)` — same, with custom working directory
- `RunTarget(t, target, env, args...)` — run arbitrary binary
- `RunTargetInDir(t, dir, target, env, args...)` — same, with custom working directory
- `RunReferenceTarget(t, env, args...)` — run reference binary for comparison
- `RunReferenceTargetInDir(t, dir, env, args...)` — same, with custom working directory

Note: `--verbose` is auto-injected when `--output-format stream-json` is used.

## Running the Suite

From the repo root:

```bash
go test ./e2e/...
```

The suite is hermetic: with `ANTHROPIC_AUTH_TOKEN` and
`ANTHROPIC_BASE_URL` unset in the environment, the mock server is the
sole destination of all HTTP traffic, and no network access is
required.

## Acceptance Criteria

### API protocol conformance (api_protocol_test.go)

- **AC1 — `max_tokens` is 64000:** the captured request has a numeric
  `max_tokens` field equal to 64000.
- **AC2 — `system` field is present and substantial:** the request body
  has a top-level `system` field.
- **AC3 — `system` prompt includes the working directory:** the system
  prompt content contains the absolute path of the directory from which
  the jenny subprocess is spawned.
- **AC4 — `tools` array is present and non-empty:** the request body
  has a `tools` key whose value is a JSON array with at least one
  element.
- **AC5 — core tools present by name:** the `tools` array always
  includes tools named `"Bash"` and `"Read"`.

## go fix Constraint: e2e/harness

`go fix ./e2e/harness/...` requires multiple consecutive runs and exits 1.
This is a documented constraint of the test infrastructure.

## Process Timeout

`RunTargetInDir` applies a per-run timeout via `context.WithTimeout`.
When `Config.TimeoutMs > 0`, it is used as the timeout. Otherwise a
default of 60 000 ms is applied. If the binary exceeds the deadline,
`cmd.Process.Kill()` is called and the run is treated as an error.
This prevents a single hanging subprocess from blocking the entire suite.
