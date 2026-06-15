package compact

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/ipy/jenny/internal/constants"
)

// withJennyHome overrides constants.JennyHomeDirFunc to point at dir for the
// duration of the test. Restored via t.Cleanup.
func withJennyHome(t *testing.T, dir string) {
	t.Helper()
	orig := constants.JennyHomeDirFunc
	constants.JennyHomeDirFunc = func() string { return dir }
	t.Cleanup(func() { constants.JennyHomeDirFunc = orig })
}

// makeFullSessionLayout creates the exact layout described in AC5/AC6.
// Returns the session directory path.
func makeFullSessionLayout(t *testing.T, home, id string) string {
	t.Helper()
	dir := filepath.Join(home, "sessions", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", dir, err)
	}
	files := map[string][]byte{
		"transcript.jsonl":       []byte(`{"type":"user","content":"hello"}` + "\n"),
		"config":                 []byte("cost=0\n"),
		"memory.md":              []byte("# memory\n"),
		"scratchpad/notes.md":    []byte("scratch\n"),
		"spills/spill-1.txt":     []byte("spill contents\n"),
		"mcp-resources/blob.bin": []byte{0x00, 0x01, 0x02, 0x03, 'B', 'L', 'O', 'B'},
	}
	for rel, data := range files {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q): %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, data, 0o644); err != nil {
			t.Fatalf("WriteFile(%q): %v", full, err)
		}
	}
	return dir
}

// listArchive returns the names of every entry in the tar.gz archive at path.
func listArchive(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open(%q): %v", path, err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var names []string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar.Next: %v", err)
		}
		names = append(names, hdr.Name)
	}
	return names
}

// TestCompactDryRun_NoMutation (AC5)
func TestCompactDryRun_NoMutation(t *testing.T) {
	home := t.TempDir()
	withJennyHome(t, home)
	dir := makeFullSessionLayout(t, home, "sess-1")

	var stdout, stderr bytes.Buffer
	rc := RunCompact("", true, false, &stdout, &stderr)
	if rc != 0 {
		t.Errorf("RunCompact(dryRun) rc = %d, want 0 (stderr=%q)", rc, stderr.String())
	}
	out := stdout.String()
	want := "would compact: sess-1 (keep transcript.jsonl, config, memory.md; archive scratchpad/, spills/, mcp-resources/)"
	if !strings.Contains(out, want) {
		t.Errorf("stdout missing %q:\n%s", want, out)
	}
	// Layout unchanged.
	for rel := range map[string]struct{}{
		"transcript.jsonl": {}, "config": {}, "memory.md": {},
		"scratchpad/notes.md": {}, "spills/spill-1.txt": {}, "mcp-resources/blob.bin": {},
	} {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Errorf("layout changed: %s gone after dry-run: %v", rel, err)
		}
	}
	// No archive was created.
	if _, err := os.Stat(filepath.Join(home, "sessions", "sess-1.tar.gz")); !os.IsNotExist(err) {
		t.Errorf("dry-run created an archive: err=%v", err)
	}
}

// TestCompactSingle_ProducesArchiveAndRemovesDir (AC6)
func TestCompactSingle_ProducesArchiveAndRemovesDir(t *testing.T) {
	home := t.TempDir()
	withJennyHome(t, home)
	dir := makeFullSessionLayout(t, home, "sess-1")

	originals := map[string][]byte{}
	for rel := range map[string]struct{}{
		"transcript.jsonl": {}, "config": {}, "memory.md": {},
	} {
		data, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil {
			t.Fatalf("ReadFile(%q): %v", rel, err)
		}
		originals[rel] = data
	}

	sessionsDir := filepath.Join(home, "sessions")
	var stdout, stderr bytes.Buffer
	if err := CompactOne(sessionsDir, "sess-1", false, &stdout, &stderr); err != nil {
		t.Fatalf("CompactOne: %v (stderr=%q)", err, stderr.String())
	}

	archivePath := filepath.Join(sessionsDir, "sess-1.tar.gz")
	if _, err := os.Stat(archivePath); err != nil {
		t.Fatalf("archive not created: %v", err)
	}
	// Session directory removed.
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("session dir not removed: err=%v", err)
	}

	// Archive content check: exactly the 6 entries.
	got := listArchive(t, archivePath)
	sort.Strings(got)
	want := []string{
		"sess-1/config",
		"sess-1/mcp-resources/blob.bin",
		"sess-1/memory.md",
		"sess-1/scratchpad/notes.md",
		"sess-1/spills/spill-1.txt",
		"sess-1/transcript.jsonl",
	}
	if !equalStringSlices(got, want) {
		t.Errorf("archive entries = %v, want %v", got, want)
	}

	// Byte-identical preservation of the three essential files.
	for rel, orig := range originals {
		got, err := readArchiveEntry(t, archivePath, "sess-1/"+rel)
		if err != nil {
			t.Fatalf("read %s from archive: %v", rel, err)
		}
		if !bytes.Equal(got, orig) {
			t.Errorf("%s contents changed: got=%q want=%q", rel, got, orig)
		}
	}
}

// TestCompactBatch_SkipsArchivesAndUnrecognized (AC7)
func TestCompactBatch_SkipsArchivesAndUnrecognized(t *testing.T) {
	home := t.TempDir()
	withJennyHome(t, home)
	sessionsDir := filepath.Join(home, "sessions")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// 1) Real session dir — should be compacted.
	makeFullSessionLayout(t, home, "real-1")

	// 2) Existing .tar.gz archive — should be skipped on stdout.
	orphanArchive := filepath.Join(sessionsDir, "orphan.tar.gz")
	if err := os.WriteFile(orphanArchive, []byte("not really a tarball"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// 3) Unrecognized entry — reported on stderr.
	unknown := filepath.Join(sessionsDir, "loose.txt")
	if err := os.WriteFile(unknown, []byte("junk"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var stdout, stderr bytes.Buffer
	rc := RunCompact("", false, false, &stdout, &stderr)
	if rc != 0 {
		t.Errorf("RunCompact rc = %d, want 0 (stderr=%q)", rc, stderr.String())
	}

	// Real session compacted.
	if _, err := os.Stat(filepath.Join(sessionsDir, "real-1")); !os.IsNotExist(err) {
		t.Errorf("real-1 dir survived: err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(sessionsDir, "real-1.tar.gz")); err != nil {
		t.Errorf("real-1.tar.gz missing: %v", err)
	}

	// Orphan archive left alone but reported on stdout.
	if _, err := os.Stat(orphanArchive); err != nil {
		t.Errorf("orphan.tar.gz vanished: %v", err)
	}
	if !strings.Contains(stdout.String(), "skip (already archived): orphan.tar.gz") {
		t.Errorf("stdout missing skip line for orphan.tar.gz:\n%s", stdout.String())
	}

	// Unrecognized left alone, reported on stderr.
	if _, err := os.Stat(unknown); err != nil {
		t.Errorf("loose.txt vanished: %v", err)
	}
	if !strings.Contains(stderr.String(), "skip (unrecognized): loose.txt") {
		t.Errorf("stderr missing skip line for loose.txt:\n%s", stderr.String())
	}
}

// TestCompact_ArchiveNameCollision_Refuses (AC9)
func TestCompact_ArchiveNameCollision_Refuses(t *testing.T) {
	home := t.TempDir()
	withJennyHome(t, home)
	sessionsDir := filepath.Join(home, "sessions")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	makeFullSessionLayout(t, home, "sess-1")

	// Pre-create an archive of the same name to force a collision.
	collision := filepath.Join(sessionsDir, "sess-1.tar.gz")
	originalContents := []byte("EXISTING ARCHIVE — must not be overwritten")
	if err := os.WriteFile(collision, originalContents, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var stdout, stderr bytes.Buffer
	err := CompactOne(sessionsDir, "sess-1", false, &stdout, &stderr)
	if err == nil {
		t.Fatalf("CompactOne without --force: expected error, got nil")
	}
	if !strings.Contains(stderr.String(), "refusing to overwrite existing archive: sess-1.tar.gz") {
		t.Errorf("stderr missing refusal message:\n%s", stderr.String())
	}
	// Original archive preserved.
	got, rerr := os.ReadFile(collision)
	if rerr != nil {
		t.Fatalf("ReadFile: %v", rerr)
	}
	if !bytes.Equal(got, originalContents) {
		t.Errorf("archive was overwritten despite refusal: got=%q want=%q", got, originalContents)
	}
}

// TestCompact_ArchiveNameCollision_ForceOverwrites (AC9)
func TestCompact_ArchiveNameCollision_ForceOverwrites(t *testing.T) {
	home := t.TempDir()
	withJennyHome(t, home)
	sessionsDir := filepath.Join(home, "sessions")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	makeFullSessionLayout(t, home, "sess-1")
	collision := filepath.Join(sessionsDir, "sess-1.tar.gz")
	if err := os.WriteFile(collision, []byte("STALE"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if err := CompactOne(sessionsDir, "sess-1", true, &stdout, &stderr); err != nil {
		t.Fatalf("CompactOne with --force: %v (stderr=%q)", err, stderr.String())
	}
	// Should now be a real tarball.
	names := listArchive(t, collision)
	if len(names) == 0 {
		t.Errorf("force-overwritten archive is empty")
	}
}

// TestCompact_HonorsJennyHome (AC10)
func TestCompact_HonorsJennyHome(t *testing.T) {
	realHome := t.TempDir()
	altHome := t.TempDir()
	withJennyHome(t, altHome)

	altSessions := filepath.Join(altHome, "sessions")
	if err := os.MkdirAll(altSessions, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	makeFullSessionLayout(t, altHome, "alpha")

	// Plant a sentinel in the real home that must NOT be touched.
	realSessions := filepath.Join(realHome, "sessions")
	if err := os.MkdirAll(realSessions, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	sentinelDir := filepath.Join(realSessions, "realSession")
	if err := os.MkdirAll(sentinelDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	sentinelTranscript := filepath.Join(sentinelDir, "transcript.jsonl")
	if err := os.WriteFile(sentinelTranscript, []byte(`{}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var stdout, stderr bytes.Buffer
	rc := RunCompact("", false, false, &stdout, &stderr)
	if rc != 0 {
		t.Errorf("RunCompact rc = %d, want 0 (stderr=%q)", rc, stderr.String())
	}

	// alt home compacted.
	if _, err := os.Stat(filepath.Join(altSessions, "alpha.tar.gz")); err != nil {
		t.Errorf("alpha.tar.gz missing in alt home: %v", err)
	}
	// real home untouched.
	if _, err := os.Stat(sentinelTranscript); err != nil {
		t.Errorf("real home sentinel was disturbed: %v", err)
	}
}

// TestExtractArchive_RestoresContents (AC8)
func TestExtractArchive_RestoresContents(t *testing.T) {
	home := t.TempDir()
	withJennyHome(t, home)
	dir := makeFullSessionLayout(t, home, "sess-1")

	originals := map[string][]byte{}
	for rel := range map[string]struct{}{
		"transcript.jsonl": {}, "config": {}, "memory.md": {},
		"scratchpad/notes.md": {}, "spills/spill-1.txt": {}, "mcp-resources/blob.bin": {},
	} {
		data, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil {
			t.Fatalf("ReadFile(%q): %v", rel, err)
		}
		originals[rel] = data
	}

	sessionsDir := filepath.Join(home, "sessions")
	var stdout, stderr bytes.Buffer
	if err := CompactOne(sessionsDir, "sess-1", false, &stdout, &stderr); err != nil {
		t.Fatalf("CompactOne: %v (stderr=%q)", err, stderr.String())
	}
	archive := filepath.Join(sessionsDir, "sess-1.tar.gz")

	// Extract into a fresh destination to simulate resume.
	destParent := filepath.Join(home, "resume")
	if err := os.MkdirAll(destParent, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := ExtractArchive(archive, destParent, "sess-1", false); err != nil {
		t.Fatalf("ExtractArchive: %v", err)
	}

	// Archive is gone after extraction (keepArchive=false).
	if _, err := os.Stat(archive); !os.IsNotExist(err) {
		t.Errorf("archive still present after extract+remove: err=%v", err)
	}
	// Restored contents byte-identical.
	for rel, orig := range originals {
		got, err := os.ReadFile(filepath.Join(destParent, "sess-1", rel))
		if err != nil {
			t.Fatalf("ReadFile(%q): %v", rel, err)
		}
		if !bytes.Equal(got, orig) {
			t.Errorf("%s after extract differs: got=%q want=%q", rel, got, orig)
		}
	}
}

// TestExtractArchive_KeepArchiveEnv (AC8 — keepArchive=true)
func TestExtractArchive_KeepArchiveEnv(t *testing.T) {
	home := t.TempDir()
	withJennyHome(t, home)
	makeFullSessionLayout(t, home, "sess-1")
	sessionsDir := filepath.Join(home, "sessions")
	if err := CompactOne(sessionsDir, "sess-1", false, io.Discard, io.Discard); err != nil {
		t.Fatalf("CompactOne: %v", err)
	}
	archive := filepath.Join(sessionsDir, "sess-1.tar.gz")

	destParent := filepath.Join(home, "resume")
	if err := os.MkdirAll(destParent, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := ExtractArchive(archive, destParent, "sess-1", true); err != nil {
		t.Fatalf("ExtractArchive: %v", err)
	}
	if _, err := os.Stat(archive); err != nil {
		t.Errorf("archive removed despite keepArchive=true: %v", err)
	}
}

// TestCompact_HelpOutput — usage mentions clean, --dry-run, --force.
func TestCompact_HelpOutput(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := RunCompactHelp(&stdout, &stderr)
	if rc != 0 {
		t.Errorf("RunCompactHelp rc = %d, want 0 (stderr=%q)", rc, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{"compact", "clean", "--dry-run", "--force"} {
		if !strings.Contains(out, want) {
			t.Errorf("compact --help output missing %q:\n%s", want, out)
		}
	}
}

// readArchiveEntry reads a single named entry from a tar.gz archive.
func readArchiveEntry(t *testing.T, archivePath, name string) ([]byte, error) {
	t.Helper()
	f, err := os.Open(archivePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if hdr.Name == name {
			return io.ReadAll(tr)
		}
	}
	return nil, os.ErrNotExist
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
