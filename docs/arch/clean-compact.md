---
title: Clean and Compact Subcommands
slug: clean-compact
priority: P2
status: partial
spec: complete
code: partial
package: cmd/jenny, internal/clean, internal/compact, internal/session
depends_on:
  - jenny-directory-structure
  - session-resume
---
# `jenny clean` and `jenny compact`

## Overview

Two housekeeping subcommands for `~/.jenny/sessions/` data. They give the user explicit, scriptable, safe control over disk usage without leaving the Jenny CLI.

| Subcommand | What it removes | What it preserves |
|------------|-----------------|-------------------|
| `jenny clean` | Every `sessions/<id>/` directory under `$JENNY_HOME` | All top-level config files: `.env`, `routes.yaml`, `skills/`, `.jennyignore`, top-level `scratchpad/`, top-level `spills/` |
| `jenny compact` | The on-disk session directory (replaced with `<id>.tar.gz` archive) | All bytes inside the session, packaged under `<id>/` in the archive |

`clean` is destructive — it deletes the entire `sessions/` tree. `compact` is lossless — it archives then deletes, with resume transparently re-extracting on demand.

## Target directory

Both commands resolve the jenny home directory via `constants.JennyHomeDir()` (which honors `JENNY_HOME`). The sessions directory is `<home>/sessions/`.

## `jenny clean`

### Flags

| Flag | Behavior |
|------|----------|
| `--dry-run` | Print `would remove: <absolute-path>` for each session directory. Do not mutate. |
| `--help` | Print usage, mention `compact` and `--dry-run`. |

### Behavior

1. Walk `<home>/sessions/`. For each direct child that is a regular directory, print or delete it.
2. If `<home>/sessions/` does not exist or has no entries, print `nothing to clean` and exit 0.
3. After real deletion, recreate `<home>/sessions/` (empty) so future sessions can be appended without an extra `mkdir`.
4. Never touch files outside `<home>/sessions/`.

### Output

Dry-run, one line per session:

```
would remove: /Users/alice/.jenny/sessions/abc123
```

Real run: silent on success.

### Acceptance

- **AC1.** `--dry-run` is a no-op (no files removed) and prints one `would remove:` line per session directory. Files outside `sessions/` are never reported.
- **AC2.** Without flags, every session directory under `sessions/` is recursively removed. Top-level files outside `sessions/` (e.g. `.env`, `routes.yaml`, `skills/`) are byte-identical pre/post.
- **AC3.** Empty/missing `sessions/` prints `nothing to clean`, exits 0, no mutation. `--help` exits 0 and lists both subcommands and `--dry-run`.
- **AC4.** When `JENNY_HOME=/tmp/alt-jenny`, only `/tmp/alt-jenny/sessions/` is touched; the real `~/.jenny/sessions/` is left intact.

## `jenny compact`

### Flags

| Flag | Behavior |
|------|----------|
| `--dry-run` | Print the per-session summary line. No archive written. |
| `--force` | Overwrite an existing `<id>.tar.gz` archive (default refuses). |
| `--help` | Print usage, mention `clean` and `--dry-run` / `--force`. |

### Usage

```
jenny compact                  # compact every session directory under sessions/
jenny compact <id>            # compact a single named session
jenny compact --dry-run       # preview without writing
jenny compact <id> --force    # overwrite existing archive
```

### Behavior

1. Determine target list:
   - If `<id>` is given, target the single child `sessions/<id>/`.
   - Otherwise, iterate every direct child of `sessions/`.
2. For each child:
   - **Directory containing `transcript.jsonl`**: compact it.
   - **`*.tar.gz` archive**: skip with `skip (already archived): <id>.tar.gz` on stdout.
   - **Anything else (file or dir without `transcript.jsonl`)**: report `skip (unrecognized): <name>` on stderr, leave on disk.
3. Compact flow for a session directory:
   1. If `<home>/sessions/<id>.tar.gz` exists and `--force` is not set, emit `refusing to overwrite existing archive: <id>.tar.gz` on stderr. For single-id invocation this is a hard failure; for batch mode the entry is skipped and processing continues.
   2. Stream the directory into `<home>/sessions/<id>.tar.gz.tmp` using `archive/tar` + `compress/gzip` (stdlib only).
   3. `fsync` the temp file, `os.Rename` to the final `<home>/sessions/<id>.tar.gz`, then `os.RemoveAll(sessions/<id>/)`.
   4. The archive root is `<id>/` — entries look like `<id>/transcript.jsonl`, `<id>/scratchpad/...`, etc.
4. `--dry-run` prints `would compact: <id> (keep transcript.jsonl, config, memory.md; archive scratchpad/, spills/, mcp-resources/)` per session directory and exits 0 with no mutation.

### Archive format

- Container: `tar` (USTAR), compression: `gzip`. Stdlib only.
- Each session entry is stored under `<id>/<relative-path>`.
- File modes and sizes are preserved (regular files only — symlinks and devices are out of scope).

### Resume transparency (AC8)

When `-r <id>` (or `--resume <id>`) is invoked and `sessions/<id>/` does not exist but `sessions/<id>.tar.gz` does, the binary transparently extracts the archive into `sessions/<id>/` before continuing the resume flow.

Extraction behavior:

- Extract archive root `<id>/...` into `<home>/sessions/`.
- After successful extraction, by default the archive `<id>.tar.gz` is **removed** (deterministic, single-disk-state contract).
- Setting `JENNY_COMPACT_KEEP_ARCHIVE` to any non-empty value keeps the archive in place after extraction. Both behaviors are deterministic; this is documented in `--help` output and in this spec.

### Acceptance

- **AC5.** `--dry-run` prints the canonical summary line and does not mutate disk. No `.tar.gz` file is created and the layout is unchanged.
- **AC6.** `jenny compact <id>` produces `<id>.tar.gz` whose `tar -tzf` listing contains exactly: `<id>/transcript.jsonl`, `<id>/config`, `<id>/memory.md`, `<id>/scratchpad/notes.md`, `<id>/spills/spill-1.txt`, `<id>/mcp-resources/blob.bin`. The session directory is removed. Byte-identical preservation of `transcript.jsonl`, `config`, `memory.md`.
- **AC7.** `jenny compact` (no id) iterates: directories → archive + remove; sibling `.tar.gz` → stdout `skip (already archived): <id>.tar.gz`; anything else → stderr `skip (unrecognized): <name>`.
- **AC8.** `jenny --resume <id>` against an archive transparently extracts and runs as if the session directory had been there. Transcript contents identical to the pre-archive state.
- **AC9.** Default refusal on archive collision: stderr `refusing to overwrite existing archive: <id>.tar.gz`, exit non-zero, no mutation. `--force` replaces and exits 0.
- **AC10.** `JENNY_HOME=/tmp/alt-jenny` redirects the archive path to `/tmp/alt-jenny/sessions/<id>.tar.gz` and leaves the real `~/.jenny/sessions/` untouched.

## Implementation

| Concern | Location |
|---------|----------|
| CLI subcommand dispatch + `clean` and `compact` parsers | `internal/cli/cli.go` |
| `jenny clean` logic | `internal/clean/clean.go` |
| `jenny compact` archive write + directory walk | `internal/compact/compact.go` |
| Resume-time archive extraction | `internal/session/manager.go` (or a thin wrapper called from `cmd/jenny/main.go` before resume) |
| CLI subcommand routing from `cmd/jenny/main.go` | `shouldRunClean()`, `shouldRunCompact()` mirror `shouldLaunchPortal()` |
| Env knob for archive retention after resume | `JENNY_COMPACT_KEEP_ARCHIVE` (any non-empty value = keep) |

### `internal/clean/clean.go`

Exports:

```go
// RunClean performs `jenny clean`.
//   - dryRun: when true, print "would remove:" lines and exit without mutation.
//   - stdout, stderr: writers for output.
// Returns exit code (0 = success).
func RunClean(dryRun bool, stdout, stderr io.Writer) int
```

The implementation walks `filepath.Join(constants.JennyHomeDir(), "sessions")`. If the directory does not exist or `os.ReadDir` returns no entries, print `nothing to clean` and return 0. Otherwise iterate entries and either print or `os.RemoveAll`.

### `internal/compact/compact.go`

Exports:

```go
// RunCompact performs `jenny compact [id]`. Iterates every direct child of
// the sessions directory when id is empty.
func RunCompact(id string, dryRun, force bool, stdout, stderr io.Writer) int

// CompactOne compacts a single session directory. Exposed for tests.
func CompactOne(sessionsDir, id string, force bool, stdout, stderr io.Writer) error

// ExtractArchive extracts <archivePath> into <destParent>/<id>. Used both
// during resume and by tests. The archive is removed on success unless
// keepArchive is true.
func ExtractArchive(archivePath, destParent, id string, keepArchive bool) error
```

Implementation notes:

- Use `archive/tar` and `compress/gzip` only — no third-party deps.
- Stream the directory into `<id>.tar.gz.tmp`, `fsync`, `os.Rename` to `<id>.tar.gz`, then `os.RemoveAll(<id>/)`.
- The tar header `Name` is `<id>/<relative-path>` to keep the archive self-contained.

### Resume extraction

A helper `internal/session.MaybeExtractArchive(id string) error` checks: if `sessions/<id>/transcript.jsonl` is missing but `sessions/<id>.tar.gz` exists, call `compact.ExtractArchive` and return any error. Called from `cmd/jenny/main.go` immediately before the `sessionManager.SessionExists` check, so the existing flow continues unmodified.

### CLI plumbing

`internal/cli/cli.go` `Parse()` is extended with two helpers:

- `IsCleanSubcommand(args []string) bool` — true when `args[0] == "clean"`.
- `IsCompactSubcommand(args []string) bool` — true when `args[0] == "compact"`.

These mirror the existing `shouldLaunchPortal()` pattern. The `Parse()` function itself does not dispatch to them; `cmd/jenny/main.go` calls them after the `shouldLaunchPortal` check.

### Help output

`jenny clean --help`:

```
Usage: jenny clean [--dry-run]

Remove every session directory under $JENNY_HOME/sessions/.
Config files at the top of $JENNY_HOME (.env, routes.yaml, skills/) are preserved.

Flags:
  --dry-run   Print "would remove:" lines and exit without deleting anything.

See also: jenny compact — archive each session directory into a single .tar.gz file.
```

`jenny compact --help`:

```
Usage: jenny compact [<id>] [--dry-run] [--force]

Compact session directories under $JENNY_HOME/sessions/. Each session directory
is archived into <id>.tar.gz using tar+gzip, then removed. Resume transparently
extracts the archive on demand.

Flags:
  --dry-run   Print "would compact:" lines and exit without writing any archive.
  --force     Overwrite an existing <id>.tar.gz archive (default refuses).

Archive retention after resume:
  By default the archive is removed after a successful resume extraction.
  Set JENNY_COMPACT_KEEP_ARCHIVE to a non-empty value to keep the archive.

See also: jenny clean — remove every session directory without archiving.
```

## Edge cases

- **Concurrent `compact` and `resume`**: the resume path uses `os.Rename` (atomic on POSIX) when replacing a half-written `.tar.gz.tmp`; never writes to `<id>.tar.gz` in place.
- **Archive-name collision with a real session id `foo.tar.gz`**: any child matching the literal `*.tar.gz` suffix is treated as an archive (AC7). Add a regression test.
- **Filesystem without `os.Rename` atomicity (Windows)**: documented limitation in this spec; no manual cross-platform extraction.
- **`JENNY_HOME` not set**: defaults to `~/.jenny/` via `constants.JennyHomeDir()`.

## Out of scope

- Selective deletion by session id for `jenny clean` (no `jenny clean <id>` in this iteration; `clean` is all-or-nothing on `sessions/`).
- Compression algorithm choice (always gzip; no `zstd` / `xz`).
- Encrypting the archive.
- Remote/cloud archival (`s3`, `gs`).
- Migrating data currently in the legacy `~/.jenny/transcripts/` directory; that path is left as-is.
- Any change to the on-disk format of `transcript.jsonl`, `config`, or `memory.md`.
- Any change to portal, MCP, or provider routing code.
- Automated scheduling of `clean` / `compact` (cron-like behavior).