package session

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ipy/jenny/internal/compact"
	"github.com/ipy/jenny/internal/constants"
)

// withJennyHome overrides constants.JennyHomeDirFunc for the test, restoring
// via t.Cleanup.
func withJennyHomeCompact(t *testing.T, dir string) {
	t.Helper()
	orig := constants.JennyHomeDirFunc
	constants.JennyHomeDirFunc = func() string { return dir }
	t.Cleanup(func() { constants.JennyHomeDirFunc = orig })
}

// TestMaybeExtractArchive_ExtractsAndRestoresTranscript (AC8)
// When the session directory is absent but the archive is present,
// MaybeExtractArchive extracts the archive and the Manager can then load
// the original transcript entries.
func TestMaybeExtractArchive_ExtractsAndRestoresTranscript(t *testing.T) {
	home := t.TempDir()
	withJennyHomeCompact(t, home)

	sessionsDir := filepath.Join(home, "sessions")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	sessDir := filepath.Join(sessionsDir, "sess-1")
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	transcriptContent := []byte(`{"type":"user","content":"hello"}` + "\n" +
		`{"type":"assistant","content":"hi"}` + "\n")
	if err := os.WriteFile(filepath.Join(sessDir, "transcript.jsonl"), transcriptContent, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sessDir, "memory.md"), []byte("# mem\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Compact first — produces sess-1.tar.gz and removes sess-1/.
	if err := compact.CompactOne(sessionsDir, "sess-1", false, os.Stdout, os.Stderr); err != nil {
		t.Fatalf("CompactOne: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sessionsDir, "sess-1.tar.gz")); err != nil {
		t.Fatalf("archive missing: %v", err)
	}

	// MaybeExtractArchive should rebuild the directory and consume the archive.
	if err := MaybeExtractArchive("sess-1"); err != nil {
		t.Fatalf("MaybeExtractArchive: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sessionsDir, "sess-1.tar.gz")); !os.IsNotExist(err) {
		t.Errorf("archive should have been removed (default behavior), err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(sessDir, "transcript.jsonl")); err != nil {
		t.Fatalf("transcript not restored: %v", err)
	}

	// Manager can now load the transcript with the original entries.
	mgr, err := NewManager(home, false)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	entries, err := mgr.LoadTranscript("sess-1")
	if err != nil {
		t.Fatalf("LoadTranscript: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("LoadTranscript returned %d entries, want 2", len(entries))
	}
	if entries[0].Type != "user" || entries[0].Content != "hello" {
		t.Errorf("entries[0] = %+v, want user/hello", entries[0])
	}
	if entries[1].Type != "assistant" || entries[1].Content != "hi" {
		t.Errorf("entries[1] = %+v, want assistant/hi", entries[1])
	}
}

// TestMaybeExtractArchive_NoOpWhenDirectoryExists (AC8 inverse):
// If the session directory already exists, MaybeExtractArchive is a no-op even
// if an archive with the same id is sitting next to it.
func TestMaybeExtractArchive_NoOpWhenDirectoryExists(t *testing.T) {
	home := t.TempDir()
	withJennyHomeCompact(t, home)
	sessionsDir := filepath.Join(home, "sessions")
	if err := os.MkdirAll(filepath.Join(sessionsDir, "sess-1"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sessionsDir, "sess-1", "transcript.jsonl"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	// Drop a stale archive that must NOT be deleted.
	stale := filepath.Join(sessionsDir, "sess-1.tar.gz")
	if err := os.WriteFile(stale, []byte("STALE"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := MaybeExtractArchive("sess-1"); err != nil {
		t.Fatalf("MaybeExtractArchive: %v", err)
	}
	if _, err := os.Stat(stale); err != nil {
		t.Errorf("stale archive was deleted despite directory existing: %v", err)
	}
}

// TestMaybeExtractArchive_NoOpWhenNothingToExtract: no directory, no archive
// → no-op success.
func TestMaybeExtractArchive_NoOpWhenNothingToExtract(t *testing.T) {
	home := t.TempDir()
	withJennyHomeCompact(t, home)
	if err := os.MkdirAll(filepath.Join(home, "sessions"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := MaybeExtractArchive("sess-missing"); err != nil {
		t.Errorf("MaybeExtractArchive should be a no-op for missing session, got: %v", err)
	}
}
