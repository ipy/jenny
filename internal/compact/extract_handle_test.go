package compact

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// archiveSession creates the canonical session layout under
// <root>/sessions/<id>/ (transcript.jsonl, config, memory.md,
// scratchpad/notes.md) and then runs CompactOne so the on-disk layout is
// exactly what `jenny compact` would produce. Returns the archive path.
//
// The session directory is created at <root>/sessions/<id>/ (not <root>/<id>/)
// because CompactOne looks it up as <sessionsDir>/<id>.
func archiveSession(t *testing.T, root, id string, transcriptBytes []byte) string {
	t.Helper()
	sessionsDir := filepath.Join(root, "sessions")
	dir := filepath.Join(sessionsDir, id)
	if err := os.MkdirAll(filepath.Join(dir, "scratchpad"), 0o755); err != nil {
		t.Fatalf("MkdirAll(scratchpad): %v", err)
	}
	files := map[string][]byte{
		"transcript.jsonl":    transcriptBytes,
		"config":              []byte("cost=0\n"),
		"memory.md":           []byte("# memory\n"),
		"scratchpad/notes.md": []byte("scratch\n"),
	}
	for rel, data := range files {
		if err := os.WriteFile(filepath.Join(dir, rel), data, 0o644); err != nil {
			t.Fatalf("WriteFile(%q): %v", rel, err)
		}
	}
	if err := CompactOne(sessionsDir, id, false, os.Stdout, os.Stderr); err != nil {
		t.Fatalf("CompactOne: %v", err)
	}
	return filepath.Join(sessionsDir, id+".tar.gz")
}

// TestAC2_ExtractArchiveClosesHandleBeforeRemove (AC2):
// ExtractArchive must close the underlying file handle before calling
// os.Remove, so that Windows DeleteFileW does not return
// ERROR_SHARING_VIOLATION. On POSIX the same code path also succeeds, but
// the test is portable — it does NOT depend on OS behaviour.
func TestAC2_ExtractArchiveClosesHandleBeforeRemove(t *testing.T) {
	root := t.TempDir()
	transcript := []byte(`{"type":"user","content":"hello"}` + "\n")
	archive := archiveSession(t, root, "sess-1", transcript)

	destParent := filepath.Join(root, "resume")
	if err := os.MkdirAll(destParent, 0o755); err != nil {
		t.Fatalf("MkdirAll(destParent): %v", err)
	}

	if err := ExtractArchive(archive, destParent, "sess-1", false); err != nil {
		t.Fatalf("ExtractArchive: %v", err)
	}

	if _, err := os.Stat(archive); !os.IsNotExist(err) {
		t.Errorf("archive should be removed after ExtractArchive(keepArchive=false); os.Stat err=%v", err)
	}

	restored, err := os.ReadFile(filepath.Join(destParent, "sess-1", "transcript.jsonl"))
	if err != nil {
		t.Fatalf("ReadFile(transcript.jsonl): %v", err)
	}
	if string(restored) != string(transcript) {
		t.Errorf("transcript.jsonl content mismatch:\n got=%q\nwant=%q", restored, transcript)
	}
}

// TestAC3_ExtractArchiveRemoveOrdering (AC3):
// Regression guard against a future contributor re-introducing the
// `defer f.Close()` / `defer gz.Close()` pattern that broke Windows resume.
// Reads compact.go as a string, locates the ExtractArchive function body,
// and asserts:
//   - os.Remove(archivePath) appears AFTER every reachable Close() call
//   - The body does NOT contain a literal `defer f.Close()` or
//     `defer gz.Close()` (those would still hold the archive handle at the
//     os.Remove point and break Windows resume).
//   - os.Remove(archivePath) is the last filesystem operation in the
//     function body when keepArchive=false (no other os.* call follows it).
func TestAC3_ExtractArchiveRemoveOrdering(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("compact.go"))
	if err != nil {
		t.Fatalf("ReadFile(compact.go): %v", err)
	}
	body := extractFuncBody(string(src), "ExtractArchive")
	if body == "" {
		t.Fatalf("could not locate ExtractArchive body in compact.go")
	}

	removeIdx := strings.Index(body, "os.Remove(archivePath)")
	if removeIdx < 0 {
		t.Fatalf("ExtractArchive body does not call os.Remove(archivePath); the keepArchive branch must live in this function:\n%s", body)
	}
	// Advance past the `os.Remove(archivePath)` call itself so the
	// post-remove scan does not match its own body text.
	afterRemoveStart := removeIdx + len("os.Remove(archivePath)")
	afterRemove := body[afterRemoveStart:]

	for _, banned := range []string{"defer f.Close()", "defer gz.Close()"} {
		if strings.Contains(body, banned) {
			t.Errorf("ExtractArchive body contains %q — this defer would still hold the archive handle when os.Remove runs and breaks Windows resume:\n%s", banned, body)
		}
	}

	// Find every Close() call reachable in the body. We require the
	// preceding character to be a word boundary so `out.Close()` matches
	// (its `defer out.Close()` placement is fine — it closes the extracted
	// file, not the archive).
	closeRe := regexp.MustCompile(`\b(f|gz|out|tw)\.Close\(\)`)
	matches := closeRe.FindAllStringIndex(body, -1)
	for _, m := range matches {
		if m[0] > removeIdx {
			t.Errorf("Close() at byte offset %d appears AFTER os.Remove(archivePath) at byte offset %d — the archive handle must be closed before deletion:\n%s",
				m[0], removeIdx, body)
		}
	}

	// Verify os.Remove is the final filesystem operation when
	// keepArchive=false. extractFuncBody already stripped the outer function
	// braces, so the remainder of `body` after os.Remove(archivePath) is
	// the tail of the function body. We scan for filesystem calls in that
	// tail (no nested function literals follow os.Remove in ExtractArchive).
	if stray := regexp.MustCompile(`\bos\.(Remove|RemoveAll|Rename|Mkdir|MkdirAll|WriteFile|Truncate)\b`).FindString(afterRemove); stray != "" {
		t.Errorf("os.Remove(archivePath) is followed by another filesystem op %q — it must be the last filesystem operation in ExtractArchive:\n%s", stray, afterRemove)
	}
}

// extractFuncBody returns the body of the named top-level function in src,
// i.e. the text between the opening `{` and its matching `}`. Nested
// function literals are handled by depth counting, so this works correctly
// even when ExtractArchive contains a `defer func() { ... }()` block.
func extractFuncBody(src, name string) string {
	header := "func " + name + "("
	start := strings.Index(src, header)
	if start < 0 {
		return ""
	}
	braceOpen := strings.IndexByte(src[start:], '{')
	if braceOpen < 0 {
		return ""
	}
	openAbs := start + braceOpen
	depth := 0
	for i := openAbs; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[openAbs+1 : i]
			}
		}
	}
	return ""
}

// TestAC5_ExtractArchiveKeepArchiveTrue (AC5):
// When keepArchive is true, ExtractArchive MUST leave the archive on disk
// and its gzip magic header intact. Guards against an over-zealous refactor
// that removes the archive unconditionally.
func TestAC5_ExtractArchiveKeepArchiveTrue(t *testing.T) {
	root := t.TempDir()
	transcript := []byte(`{"type":"user","content":"hello"}` + "\n")
	archive := archiveSession(t, root, "sess-1", transcript)

	destParent := filepath.Join(root, "resume")
	if err := os.MkdirAll(destParent, 0o755); err != nil {
		t.Fatalf("MkdirAll(destParent): %v", err)
	}
	if err := ExtractArchive(archive, destParent, "sess-1", true); err != nil {
		t.Fatalf("ExtractArchive(keepArchive=true): %v", err)
	}

	info, err := os.Stat(archive)
	if err != nil {
		t.Fatalf("archive was removed despite keepArchive=true: %v", err)
	}
	if info.Size() < 2 {
		t.Fatalf("archive size=%d, want >=2 (gzip magic)", info.Size())
	}
	magic, err := os.ReadFile(archive)
	if err != nil {
		t.Fatalf("ReadFile(archive): %v", err)
	}
	if len(magic) < 2 || magic[0] != 0x1f || magic[1] != 0x8b {
		t.Errorf("gzip magic missing: got=% x, want 1f 8b …", magic[:min(len(magic), 8)])
	}

	// Sanity: extraction itself still succeeded.
	if _, err := os.Stat(filepath.Join(destParent, "sess-1", "transcript.jsonl")); err != nil {
		t.Errorf("transcript.jsonl missing after extract: %v", err)
	}
}
