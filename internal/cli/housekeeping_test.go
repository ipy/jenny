// Housekeeping regression tests for the docs/arch/clean-compact spec.
// These tests guard against re-introducing dead helpers and against
// doc front-matter / EOF regressions in `docs/arch/clean-compact.md`.
//
// Style notes (matching internal/cli/cli_test.go):
//   - No third-party test deps; stdlib only.
//   - Independent tests, no shared mutable state, no t.Skip.
//   - Fast (< 50 ms each): no subprocesses, no network, no fsync.
package cli

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// repoRoot returns the absolute path to the repository root by walking up
// from this test file's directory until a `go.mod` is found. This keeps
// the tests runnable from any working directory:
//
//	cd internal/cli && go test ./...
//	go test ./...
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	cur, err := filepath.Abs(dir)
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(cur, "go.mod")); err == nil {
			return cur
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			t.Fatalf("could not locate repo root (no go.mod) starting at %s", dir)
		}
		cur = parent
	}
}

// collectGoFiles returns every .go file under the given root-relative dirs.
func collectGoFiles(t *testing.T, root string, relDirs []string) []string {
	t.Helper()
	var out []string
	for _, rel := range relDirs {
		base := filepath.Join(root, rel)
		err := filepath.WalkDir(base, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if strings.HasSuffix(p, ".go") {
				out = append(out, p)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", base, err)
		}
	}
	return out
}

// readFileOrFail is a tiny helper that reads p or fails the test.
func readFileOrFail(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return b
}

// selfFile returns the absolute path of this test file. It is used to
// exclude this file from the in-process grep walks below — the test
// file necessarily references the identifiers it is checking, and we
// do not want a self-match to fail the test.
func selfFile(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	abs, err := filepath.Abs(file)
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	return abs
}

// TestAC1_NoDeadCleanCompactHelpers encodes AC1 of the clean-compact
// tech-debt spec: after this iteration, IsCleanSubcommand and
// IsCompactSubcommand must not be defined anywhere in the repository.
//
// Note: if a future contributor revives the helpers under a different name
// (e.g. cli.IsCleanCmd), extend the regex below. If the helpers are
// intentionally reintroduced, update or delete this test as part of that
// change — do not silently let the regex drift.
func TestAC1_NoDeadCleanCompactHelpers(t *testing.T) {
	root := repoRoot(t)
	self := selfFile(t)
	re := regexp.MustCompile(`func\s+(IsCleanSubcommand|IsCompactSubcommand)\s*\(`)

	for _, p := range collectGoFiles(t, root, []string{"internal", "cmd"}) {
		// Skip this test file: it necessarily mentions the identifiers in
		// the regex it is enforcing.
		if p == self {
			continue
		}
		content := readFileOrFail(t, p)
		if re.Match(content) {
			t.Errorf("AC1: dead helper redefined in %s\n"+
				"Delete it; shouldRunClean/shouldRunCompact in cmd/jenny/main.go "+
				"is the single source of truth.", p)
		}
	}
}

// TestAC2_CleanCompactDetectionLivesInMain encodes AC2: shouldRunClean and
// shouldRunCompact are defined in cmd/jenny/main.go, and no other Go
// file in the tree mentions these identifiers.
//
// Note: this test enforces the spec literally — "the only file that
// mentions those identifiers is `cmd/jenny/main.go`". If the helpers are
// renamed, update this test and the corresponding callsite together.
func TestAC2_CleanCompactDetectionLivesInMain(t *testing.T) {
	root := repoRoot(t)
	mainPath := filepath.Join(root, "cmd", "jenny", "main.go")
	mainContent := readFileOrFail(t, mainPath)

	// main.go must contain the function definitions and the call sites.
	mustContain := []string{
		"func shouldRunClean(",
		"func shouldRunCompact(",
		"shouldRunClean()",
		"shouldRunCompact()",
	}
	for _, frag := range mustContain {
		if !bytes.Contains(mainContent, []byte(frag)) {
			t.Errorf("AC2: cmd/jenny/main.go missing %q", frag)
		}
	}

	// No other Go file may mention either identifier. We deliberately use
	// a substring match (not a regex) to mirror the spec's "the only file
	// that mentions those identifiers" wording. Skip this test file
	// because it necessarily mentions the identifiers it is enforcing.
	otherFiles := collectGoFiles(t, root, []string{"internal", "cmd"})
	self := selfFile(t)
	for _, p := range otherFiles {
		if p == mainPath || p == self {
			continue
		}
		content := readFileOrFail(t, p)
		for _, ident := range []string{"shouldRunClean", "shouldRunCompact"} {
			if bytes.Contains(content, []byte(ident)) {
				t.Errorf("AC2: %s mentions %s; "+
					"this identifier must live only in cmd/jenny/main.go",
					p, ident)
			}
		}
	}
}

// TestAC3_CleanCompactDocFrontMatter encodes AC3: docs/arch/clean-compact.md
// must contain `status: done`, `spec: complete`, and `code: complete`,
// and no other status:/spec:/code: line.
func TestAC3_CleanCompactDocFrontMatter(t *testing.T) {
	root := repoRoot(t)
	docPath := filepath.Join(root, "docs", "arch", "clean-compact.md")
	content := readFileOrFail(t, docPath)

	scanner := bufio.NewScanner(bytes.NewReader(content))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	allowed := map[string]string{
		"status:": "done",
		"spec:":   "complete",
		"code:":   "complete",
	}
	for scanner.Scan() {
		line := scanner.Text()
		for prefix, expectedValue := range allowed {
			if !strings.HasPrefix(line, prefix) {
				continue
			}
			got := strings.TrimSpace(strings.TrimPrefix(line, prefix))
			if got != expectedValue {
				t.Errorf("AC3: line %q has %q, want %q",
					line, prefix+" "+got, prefix+" "+expectedValue)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}
}

// TestAC4_CleanCompactDocHasEofNewline encodes AC4:
// docs/arch/clean-compact.md must end with a single LF (0x0a) so POSIX
// text-file tools (wc -l, git diff, markdownlint) behave deterministically.
func TestAC4_CleanCompactDocHasEofNewline(t *testing.T) {
	root := repoRoot(t)
	docPath := filepath.Join(root, "docs", "arch", "clean-compact.md")
	content := readFileOrFail(t, docPath)

	if len(content) == 0 {
		t.Fatal("AC4: docs/arch/clean-compact.md is empty")
	}
	if content[len(content)-1] != '\n' {
		t.Errorf("AC4: docs/arch/clean-compact.md does not end with LF "+
			"(last byte is 0x%02x); append a trailing newline",
			content[len(content)-1])
	}
}
