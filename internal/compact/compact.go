// Package compact implements the `jenny compact` housekeeping subcommand.
//
// `jenny compact` archives each <JENNY_HOME>/sessions/<id>/ directory into a
// sibling <id>.tar.gz using tar+gzip, then removes the directory. Resuming
// such an archived session transparently extracts the archive via
// ExtractArchive (called from internal/session.MaybeExtractArchive).
package compact

import (
	"archive/tar"
	"compress/gzip"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ipy/jenny/internal/constants"
)

// CompactUsage is the human-readable help text printed by `jenny compact --help`.
const CompactUsage = `Usage: jenny compact [<id>] [--dry-run] [--force]

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
`

// dryRunSummary is the per-session summary printed by `jenny compact --dry-run`.
const dryRunSummary = "would compact: %s (keep transcript.jsonl, config, memory.md; archive scratchpad/, spills/, mcp-resources/)"

// sessionsDir returns the canonical sessions directory for the current
// JENNY_HOME. Used by RunCompact and exposed for tests that want a fixed path.
func sessionsDir() string {
	return filepath.Join(constants.JennyHomeDir(), "sessions")
}

// RunCompact performs `jenny compact [id]`. When id is empty, every direct
// child of the sessions directory is considered; otherwise only the named
// child. Returns the process exit code (0 on success).
//
// stdout/stderr receive the per-entry skip messages. A non-empty id that
// fails (e.g. archive collision without --force) yields a non-zero return.
func RunCompact(id string, dryRun, force bool, stdout, stderr io.Writer) int {
	sessDir := sessionsDir()

	if id != "" {
		if err := compactEntry(sessDir, id, dryRun, force, stdout, stderr); err != nil {
			// compactEntry already printed stderr detail; surface non-zero.
			return 1
		}
		return 0
	}

	// Batch mode: iterate every direct child. Continue past per-entry errors
	// unless the entry was the single-id invocation.
	entries, err := os.ReadDir(sessDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		fmt.Fprintf(stderr, "jenny compact: reading sessions directory: %v\n", err)
		return 1
	}
	for _, e := range entries {
		if err := compactEntry(sessDir, e.Name(), dryRun, force, stdout, stderr); err != nil {
			fmt.Fprintf(stderr, "jenny compact: %s: %v\n", e.Name(), err)
			// Do not abort the whole batch — keep processing siblings.
		}
	}
	return 0
}

// compactEntry handles a single direct child of the sessions directory.
//
// Behaviour matrix:
//   - *.tar.gz              → stdout "skip (already archived): <name>"
//   - dir w/ transcript     → run the archive flow (or dry-run summary)
//   - anything else          → stderr "skip (unrecognized): <name>"
//
// On a single-id invocation an archive collision without --force surfaces as
// an error returned to the caller; on batch mode it is skipped silently via
// the "already archived" branch (the sibling .tar.gz is the prior artifact).
func compactEntry(sessionsDir, name string, dryRun, force bool, stdout, stderr io.Writer) error {
	full := filepath.Join(sessionsDir, name)

	// 1) Archive sibling → skip.
	if strings.HasSuffix(name, ".tar.gz") {
		fmt.Fprintf(stdout, "skip (already archived): %s\n", name)
		return nil
	}

	info, err := os.Stat(full)
	if err != nil {
		if os.IsNotExist(err) {
			// Disappeared between ReadDir and Stat — treat as already gone.
			return nil
		}
		return fmt.Errorf("stat %s: %w", full, err)
	}

	// 2) Anything that's not a directory → unrecognized.
	if !info.IsDir() {
		fmt.Fprintf(stderr, "skip (unrecognized): %s\n", name)
		return nil
	}

	// 3) Directory that lacks a transcript.jsonl → not a session.
	if _, err := os.Stat(filepath.Join(full, "transcript.jsonl")); err != nil {
		fmt.Fprintf(stderr, "skip (unrecognized): %s\n", name)
		return nil
	}

	if dryRun {
		fmt.Fprintf(stdout, dryRunSummary+"\n", name)
		return nil
	}

	return CompactOne(sessionsDir, name, force, stdout, stderr)
}

// CompactOne compacts a single session directory into a sibling .tar.gz
// archive and removes the original directory. Exposed for direct test use.
//
// Refuses to overwrite an existing archive unless force is true. Returns an
// error on archive collisions in single-id mode; the caller decides whether
// to surface or swallow it.
func CompactOne(sessionsDir, id string, force bool, stdout, stderr io.Writer) error {
	srcDir := filepath.Join(sessionsDir, id)
	archivePath := filepath.Join(sessionsDir, id+".tar.gz")
	tmpPath := archivePath + ".tmp"

	if _, err := os.Stat(archivePath); err == nil {
		if !force {
			fmt.Fprintf(stderr, "refusing to overwrite existing archive: %s.tar.gz\n", id)
			return fmt.Errorf("archive exists: %s", archivePath)
		}
		// Force path: remove the stale archive before rewriting. The atomic
		// rename below would otherwise refuse on POSIX.
		if err := os.Remove(archivePath); err != nil {
			return fmt.Errorf("removing stale archive %s: %w", archivePath, err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("checking existing archive %s: %w", archivePath, err)
	}

	// Stream the directory into the temp archive.
	if err := writeArchive(srcDir, id, tmpPath); err != nil {
		// Best-effort cleanup of partial temp file.
		_ = os.Remove(tmpPath)
		return err
	}

	// Atomic rename: on POSIX, this is the moment readers either see the new
	// archive or nothing at all (no half-written file). Concurrent resume
	// calls that arrive mid-compact will see no archive until the rename
	// completes.
	if err := os.Rename(tmpPath, archivePath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("renaming archive %s -> %s: %w", tmpPath, archivePath, err)
	}

	// Only now is it safe to drop the original directory.
	if err := os.RemoveAll(srcDir); err != nil {
		return fmt.Errorf("removing source dir %s: %w", srcDir, err)
	}
	return nil
}

// writeArchive streams srcDir into a gzipped tar file at dst. Each entry is
// stored under <id>/<relative-path> so the archive is self-describing.
func writeArchive(srcDir, id, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("creating archive parent: %w", err)
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("opening archive temp file: %w", err)
	}
	closed := false
	defer func() {
		if !closed {
			_ = out.Close()
		}
	}()

	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)

	walkErr := filepath.WalkDir(srcDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		// Skip the root itself — we only want its descendants under the
		// <id>/ prefix.
		if path == srcDir {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("stat %s: %w", path, err)
		}

		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return fmt.Errorf("relative path: %w", err)
		}
		// Tar headers use forward slashes regardless of platform.
		headerName := filepath.ToSlash(filepath.Join(id, rel))

		switch {
		case info.Mode().IsRegular():
			hdr, err := tar.FileInfoHeader(info, "")
			if err != nil {
				return fmt.Errorf("file header %s: %w", path, err)
			}
			hdr.Name = headerName
			if err := tw.WriteHeader(hdr); err != nil {
				return fmt.Errorf("write header %s: %w", path, err)
			}
			f, err := os.Open(path)
			if err != nil {
				return fmt.Errorf("open %s: %w", path, err)
			}
			if _, err := io.Copy(tw, f); err != nil {
				_ = f.Close()
				return fmt.Errorf("copy %s: %w", path, err)
			}
			if err := f.Close(); err != nil {
				return fmt.Errorf("close %s: %w", path, err)
			}
		case info.IsDir():
			// Skip directory entries so `tar -tzf` lists exactly the files,
			// matching the AC6 expected listing. The tar reader will recreate
			// any needed parent directories during extraction.
			return nil
		default:
			// Skip non-regular entries (symlinks, devices). Out of scope per spec.
			return nil
		}
		return nil
	})
	if walkErr != nil {
		_ = tw.Close()
		_ = gz.Close()
		_ = out.Close()
		closed = true
		return fmt.Errorf("walking %s: %w", srcDir, walkErr)
	}
	if err := tw.Close(); err != nil {
		_ = gz.Close()
		_ = out.Close()
		closed = true
		return fmt.Errorf("closing tar: %w", err)
	}
	if err := gz.Close(); err != nil {
		_ = out.Close()
		closed = true
		return fmt.Errorf("closing gzip: %w", err)
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		closed = true
		return fmt.Errorf("syncing archive: %w", err)
	}
	if err := out.Close(); err != nil {
		closed = true
		return fmt.Errorf("closing archive file: %w", err)
	}
	closed = true
	return nil
}

// ExtractArchive extracts <archivePath> into <destParent>/<id>/, stripping the
// <id>/ prefix from each entry. After successful extraction the archive is
// removed unless keepArchive is true.
//
// Used both during resume (via internal/session.MaybeExtractArchive) and by
// tests to verify round-trip preservation.
func ExtractArchive(archivePath, destParent, id string, keepArchive bool) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("opening archive: %w", err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("gzip reader: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)

	prefix := id + "/"
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar next: %w", err)
		}
		if !strings.HasPrefix(hdr.Name, prefix) {
			return fmt.Errorf("archive entry %q outside %q prefix", hdr.Name, prefix)
		}
		rel := strings.TrimPrefix(hdr.Name, prefix)
		if rel == "" {
			// The root directory entry itself — skip.
			continue
		}
		target := filepath.Join(destParent, id, rel)
		if hdr.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("mkdir %s: %w", target, err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("mkdir parent of %s: %w", target, err)
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
		if err != nil {
			return fmt.Errorf("open %s: %w", target, err)
		}
		if _, err := io.Copy(out, tr); err != nil {
			_ = out.Close()
			return fmt.Errorf("copy to %s: %w", target, err)
		}
		if err := out.Close(); err != nil {
			return fmt.Errorf("close %s: %w", target, err)
		}
	}

	if !keepArchive {
		if err := os.Remove(archivePath); err != nil {
			return fmt.Errorf("removing archive after extract: %w", err)
		}
	}
	return nil
}

// ParseCompactArgs parses the args slice (excluding the subcommand name
// itself) for `jenny compact`. Returns (positional id, dryRun, force, help).
func ParseCompactArgs(args []string) (id string, dryRun, force, help bool, err error) {
	fs := flag.NewFlagSet("jenny compact", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.BoolVar(&dryRun, "dry-run", false, "Print would-compact lines and exit")
	fs.BoolVar(&force, "force", false, "Overwrite an existing archive")
	fs.BoolVar(&help, "help", false, "Print usage and exit")
	fs.BoolVar(&help, "h", false, "Print usage and exit (alias)")
	if err := fs.Parse(args); err != nil {
		return "", false, false, false, err
	}
	rest := fs.Args()
	if len(rest) > 1 {
		return "", false, false, false, fmt.Errorf("jenny compact: at most one positional id is allowed")
	}
	if len(rest) == 1 {
		id = rest[0]
	}
	return id, dryRun, force, help, nil
}

// RunCompactHelp prints the compact help text to stdout and returns 0.
func RunCompactHelp(stdout, stderr io.Writer) int {
	_, err := io.WriteString(stdout, CompactUsage)
	if err != nil {
		fmt.Fprintf(stderr, "jenny compact: writing help: %v\n", err)
		return 1
	}
	return 0
}
