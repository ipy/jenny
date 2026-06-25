---
title: Clean and Sweep Subcommands
slug: clean-sweep
priority: P2
status: done
spec: complete
code: complete
package: cmd/jenny, internal/clean, internal/sweep, internal/session
depends_on:
  - jenny-directory-structure
  - session-resume
---
# `jenny clean` and `jenny sweep`

## Overview

Two housekeeping subcommands for `~/.jenny/sessions/` data. They give the user explicit, scriptable, safe control over disk usage without leaving the Jenny CLI.

| Subcommand | What it removes | What it preserves |
|------------|-----------------|-------------------|
| `jenny clean` | Every `sessions/<id>/` directory under `$JENNY_HOME` | All top-level config files: `.env`, `settings.json`, `skills/`, `.jennyignore`, top-level `scratchpad/`, top-level `spills/` |
| `jenny sweep` | The on-disk session directory (replaced with `<id>.tar.gz` archive) | All bytes inside the session, packaged under `<id>/` in the archive |

`clean` is destructive — it deletes the entire `sessions/` tree. `sweep` is lossless — it archives then deletes, with resume transparently re-extracting on demand.

## Target directory

Both commands resolve the jenny home directory via `constants.JennyHomeDir()` (which honors `JENNY_HOME`). The sessions directory is `<home>/sessions/`.

## `jenny clean`

### Flags

| Flag | Behavior |
|------|----------|
| `--dry-run` | Print `would remove: <absolute-path>` for each session directory. Do not mutate. |
| `--help` | Print usage, mention `sweep` and `--dry-run`. |

### Behavior

1. Walk `<home>/sessions/`. For each direct child that is a directory or a `*.tar.gz` file, print or delete it. Directories containing a `pid` file whose process is still alive are skipped with `skip (running): <id>` on stdout.
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
- **AC2.** Without flags, every session directory under `sessions/` is recursively removed. Top-level files outside `sessions/` (e.g. `.env`, `settings.json`, `skills/`) are byte-identical pre/post.
- **AC3.** Empty/missing `sessions/` prints `nothing to clean`, exits 0, no mutation. `--help` exits 0 and lists both subcommands and `--dry-run`.
- **AC4.** When `JENNY_HOME=/tmp/alt-jenny`, only `/tmp/alt-jenny/sessions/` is touched; the real `~/.jenny/sessions/` is left intact.

## `jenny sweep`

### Flags

| Flag | Behavior |
|------|----------|
| `--dry-run` | Print the per-session summary line. No archive written. |
| `--force` | Overwrite an existing `<id>.tar.gz` archive (default refuses). |
| `--help` | Print usage, mention `clean` and `--dry-run` / `--force`. |

### Usage

```
jenny sweep                  # sweep every session directory under sessions/
jenny sweep <id>            # sweep a single named session
jenny sweep --dry-run       # preview without writing
jenny sweep <id> --force    # overwrite existing archive
```

### Behavior

1. Determine target list:
   - If `<id>` is given, target the single child `sessions/<id>/`.
   - Otherwise, iterate every direct child of `sessions/`.
2. For each child:
   - **Directory containing `pid` file with a live process**: skip with `skip (running): <id>` on stdout.
   - **Directory containing `transcript.jsonl` or `config`**: sweep it.
   - **`*.tar.gz` archive**: skip with `skip (already archived): <id>.tar.gz` on stdout.
   - **Anything else (file or dir without `transcript.jsonl` or `config`)**: report `skip (unrecognized): <name>` on stderr, leave on disk.
3. Sweep flow for a session directory:
   1. If `<home>/sessions/<id>.tar.gz` exists and `--force` is not set, emit `refusing to overwrite existing archive: <id>.tar.gz` on stderr. For single-id invocation this is a hard failure; for batch mode the entry is skipped and processing continues.
   2. Stream the directory into `<home>/sessions/<id>.tar.gz.tmp` using `archive/tar` + `compress/gzip` (stdlib only).
   3. `fsync` the temp file, `os.Rename` to the final `<home>/sessions/<id>.tar.gz`, then `os.RemoveAll(sessions/<id>/)`.
   4. The archive root is `<id>/` — entries look like `<id>/transcript.jsonl`, `<id>/scratchpad/...`, etc.
4. `--dry-run` prints `would sweep: <id> (keep transcript.jsonl, config, memory.md; archive scratchpad/, spills/, mcp-resources/)` per session directory and exits 0 with no mutation.

### Archive format

- Container: `tar` (USTAR), compression: `gzip`. Stdlib only.
- Each session entry is stored under `<id>/<relative-path>`.
- File modes and sizes are preserved (regular files only — symlinks and devices are out of scope).

### Resume transparency (AC8)

When `-r <id>` (or `--resume <id>`) is invoked and `sessions/<id>/` does not exist but `sessions/<id>.tar.gz` does, the binary transparently extracts the archive into `sessions/<id>/` before continuing the resume flow.

Extraction behavior:

- Extract archive root `<id>/...` into `<home>/sessions/`.
- After successful extraction, by default the archive `<id>.tar.gz` is **removed** (deterministic, single-disk-state contract).
- Setting `JENNY_SWEEP_KEEP_ARCHIVE` to any non-empty value (or passing `--sweep-keep-archive`) keeps the archive in place after extraction. Both behaviors are deterministic; this is documented in `--help` output and in this spec.

### Acceptance

- **AC5.** `--dry-run` prints the canonical summary line and does not mutate disk. No `.tar.gz` file is created and the layout is unchanged.
- **AC6.** `jenny sweep <id>` produces `<id>.tar.gz` whose `tar -tzf` listing contains exactly: `<id>/transcript.jsonl`, `<id>/config`, `<id>/memory.md`, `<id>/scratchpad/notes.md`, `<id>/spills/spill-1.txt`, `<id>/mcp-resources/blob.bin`. The session directory is removed. Byte-identical preservation of `transcript.jsonl`, `config`, `memory.md`.
- **AC7.** `jenny sweep` (no id) iterates: directories → archive + remove; sibling `.tar.gz` → stdout `skip (already archived): <id>.tar.gz`; anything else → stderr `skip (unrecognized): <name>`.
- **AC8.** `jenny --resume <id>` against an archive transparently extracts and runs as if the session directory had been there. Transcript contents identical to the pre-archive state.
- **AC9.** Default refusal on archive collision: stderr `refusing to overwrite existing archive: <id>.tar.gz`, exit non-zero, no mutation. `--force` replaces and exits 0.
- **AC10.** `JENNY_HOME=/tmp/alt-jenny` redirects the archive path to `/tmp/alt-jenny/sessions/<id>.tar.gz` and leaves the real `~/.jenny/sessions/` untouched.

## Implementation

| Concern | Responsibility |
|---------|---------------|
| CLI subcommand dispatch + `clean` and `sweep` parsers | CLI argument parsing |
| `jenny clean` logic | Walk sessions directory; print or remove entries |
| `jenny sweep` archive write + directory walk | Stream directory into tar.gz archive |
| Resume-time archive extraction | Transparent extraction when resuming from archived session |
| CLI subcommand routing | Detect `clean`/`sweep` subcommands before main agent flow |
| Env knob for archive retention after resume | `JENNY_SWEEP_KEEP_ARCHIVE` (any non-empty value = keep) or `--sweep-keep-archive` CLI flag (see [koanf-config.md](./koanf-config.md)) |

### Clean

The clean function walks the sessions directory under Jenny home. If the directory does not exist or has no entries, it prints `nothing to clean` and exits successfully. Otherwise it iterates entries and either prints (dry-run) or removes them. Returns exit code 0 on success.

### Sweep

The sweep function iterates session directories and archives each one:

- Use stdlib `archive/tar` and `compress/gzip` only — no third-party deps.
- Stream the directory into a temporary file, sync, then atomically rename to the final archive path.
- The archive root is `<id>/` to keep the archive self-contained.

Key exported operations:

- **RunSweep**: Iterate and sweep all or a single session directory
- **SweepOne**: Sweep a single session directory (exposed for tests)
- **ExtractArchive**: Extract an archive to a destination directory; optionally remove the archive after success

### Resume extraction

Before checking if a session exists on resume, if the session directory is missing but an archive exists, the archive is transparently extracted. This ensures the existing resume flow continues unmodified.

### CLI plumbing

The CLI parser detects `clean` and `sweep` subcommands via dedicated helper functions. The main entry point calls these after other subcommand checks.

### Help output

`jenny clean --help`:

```
Usage: jenny clean [--dry-run]

Remove every session directory under $JENNY_HOME/sessions/.
Config files at the top of $JENNY_HOME (.env, settings.json, skills/) are preserved.

Flags:
  --dry-run   Print "would remove:" lines and exit without deleting anything.

See also: jenny sweep — archive each session directory into a single .tar.gz file.
```

`jenny sweep --help`:

```
Usage: jenny sweep [<id>] [--dry-run] [--force]

Sweep session directories under $JENNY_HOME/sessions/. Each session directory
is archived into <id>.tar.gz using tar+gzip, then removed. Resume transparently
extracts the archive on demand.

Flags:
  --dry-run   Print "would sweep:" lines and exit without writing any archive.
  --force     Overwrite an existing <id>.tar.gz archive (default refuses).

Archive retention after resume:
  By default the archive is removed after a successful resume extraction.
  Set JENNY_SWEEP_KEEP_ARCHIVE to a non-empty value (or pass --sweep-keep-archive) to keep the archive.

See also: jenny clean — remove every session directory without archiving.
```

## Edge cases

- **Concurrent `sweep` and `resume`**: the resume path uses `os.Rename` (atomic on POSIX) when replacing a half-written `.tar.gz.tmp`; never writes to `<id>.tar.gz` in place.
- **Archive-name collision with a real session id `foo.tar.gz`**: any child matching the literal `*.tar.gz` suffix is treated as an archive (AC7). Add a regression test.
- **Filesystem without `os.Rename` atomicity (Windows)**: documented limitation in this spec; no manual cross-platform extraction.
- **`JENNY_HOME` not set**: defaults to `~/.jenny/` via `constants.JennyHomeDir()`.

## Out of scope

- Selective deletion by session id for `jenny clean` (no `jenny clean <id>` in this iteration; `clean` is all-or-nothing on `sessions/`).
- Compression algorithm choice (always gzip; no `zstd` / `xz`).
- Encrypting the archive.
- Remote/cloud archival (`s3`, `gs`).
- Any change to the on-disk format of `transcript.jsonl`, `config`, or `memory.md`.
- Any change to MCP or provider routing code.
- Automated scheduling of `clean` / `sweep` (cron-like behavior).
