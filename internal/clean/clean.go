// Package clean implements the `jenny clean` housekeeping subcommand.
//
// `jenny clean` recursively removes every direct child of
// $JENNY_HOME/sessions/. Config files at the top of $JENNY_HOME are not
// touched. The sessions/ directory is recreated (empty) on success so future
// runs can append without an extra mkdir.
package clean

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ipy/jenny/internal/constants"
)

// CleanUsage is the human-readable help text printed by `jenny clean --help`
// and referenced from the compact help text.
const CleanUsage = `Usage: jenny clean [--dry-run]

Remove every session directory under $JENNY_HOME/sessions/.
Config files at the top of $JENNY_HOME (.env, routes.yaml, skills/) are preserved.

Flags:
  --dry-run   Print "would remove:" lines and exit without deleting anything.

See also: jenny compact — archive each session directory into a single .tar.gz file.
`

// RunClean performs `jenny clean`. Returns the process exit code (0 on success).
//
//   - dryRun: when true, print "would remove:" lines and exit without mutation.
//   - stdout, stderr: writers for output.
//
// Files outside <JENNY_HOME>/sessions/ are never touched or reported.
func RunClean(dryRun bool, stdout, stderr io.Writer) int {
	home := constants.JennyHomeDir()
	sessionsDir := filepath.Join(home, "sessions")

	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintln(stdout, "nothing to clean")
			return 0
		}
		fmt.Fprintf(stderr, "jenny clean: reading sessions directory: %v\n", err)
		return 1
	}

	// Filter to directories only — files at the top of sessions/ are unexpected
	// but we do not delete them silently.
	targets := make([]os.DirEntry, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			targets = append(targets, e)
		}
	}

	if len(targets) == 0 {
		fmt.Fprintln(stdout, "nothing to clean")
		return 0
	}

	for _, e := range targets {
		full := filepath.Join(sessionsDir, e.Name())
		if dryRun {
			fmt.Fprintf(stdout, "would remove: %s\n", full)
			continue
		}
		if err := os.RemoveAll(full); err != nil {
			fmt.Fprintf(stderr, "jenny clean: removing %s: %v\n", full, err)
			return 1
		}
	}

	// Recreate the (empty) sessions/ directory so future runs do not need to
	// mkdir first. If it already exists, MkdirAll is a no-op.
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		fmt.Fprintf(stderr, "jenny clean: recreating sessions directory: %v\n", err)
		return 1
	}

	return 0
}

// RunCleanHelp prints the clean help text to stdout and returns 0.
func RunCleanHelp(stdout, stderr io.Writer) int {
	_, err := io.WriteString(stdout, CleanUsage)
	if err != nil {
		fmt.Fprintf(stderr, "jenny clean: writing help: %v\n", err)
		return 1
	}
	return 0
}

// ParseCleanArgs parses the args slice (excluding the subcommand name itself)
// for `jenny clean`. Returns (dryRun, help, error). Help wins over dryRun.
func ParseCleanArgs(args []string) (dryRun, help bool, err error) {
	fs := flag.NewFlagSet("jenny clean", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.BoolVar(&dryRun, "dry-run", false, "Print would-remove lines and exit without deleting")
	fs.BoolVar(&help, "help", false, "Print usage and exit")
	fs.BoolVar(&help, "h", false, "Print usage and exit (alias)")
	if err := fs.Parse(args); err != nil {
		return false, false, err
	}
	return dryRun, help, nil
}
