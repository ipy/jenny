package clean

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ipy/jenny/internal/constants"
)

// withJennyHome temporarily overrides constants.JennyHomeDirFunc so the clean
// subcommand operates inside t.TempDir(). The original is restored via t.Cleanup.
func withJennyHome(t *testing.T, dir string) {
	t.Helper()
	orig := constants.JennyHomeDirFunc
	constants.JennyHomeDirFunc = func() string { return dir }
	t.Cleanup(func() { constants.JennyHomeDirFunc = orig })
}

// makeSessionDir creates a session directory at <home>/sessions/<id> with a
// transcript.jsonl inside, simulating a real session layout.
func makeSessionDir(t *testing.T, home, id string) string {
	t.Helper()
	dir := filepath.Join(home, "sessions", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "transcript.jsonl"), []byte(`{"type":"user","content":"hi"}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return dir
}

// TestCleanDryRun_NoMutation (AC1)
func TestCleanDryRun_NoMutation(t *testing.T) {
	home := t.TempDir()
	withJennyHome(t, home)

	sess1 := makeSessionDir(t, home, "sess-A")
	sess2 := makeSessionDir(t, home, "sess-B")

	var stdout, stderr bytes.Buffer
	rc := RunClean(true, &stdout, &stderr)
	if rc != 0 {
		t.Errorf("RunClean(dryRun=true) rc = %d, want 0 (stderr=%q)", rc, stderr.String())
	}

	out := stdout.String()
	if !strings.Contains(out, "would remove: "+sess1) {
		t.Errorf("stdout missing line for %q: %s", sess1, out)
	}
	if !strings.Contains(out, "would remove: "+sess2) {
		t.Errorf("stdout missing line for %q: %s", sess2, out)
	}

	// No files removed.
	if _, err := os.Stat(sess1); err != nil {
		t.Errorf("session dir %q removed during dry-run: %v", sess1, err)
	}
	if _, err := os.Stat(sess2); err != nil {
		t.Errorf("session dir %q removed during dry-run: %v", sess2, err)
	}
}

// TestCleanDryRun_IgnoresNonSessionFiles (AC1)
// Files outside sessions/ must not be reported.
func TestCleanDryRun_IgnoresNonSessionFiles(t *testing.T) {
	home := t.TempDir()
	withJennyHome(t, home)

	// Create a top-level config file (outside sessions/).
	if err := os.WriteFile(filepath.Join(home, ".env"), []byte("X=1"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	sess := makeSessionDir(t, home, "sess-1")

	var stdout bytes.Buffer
	rc := RunClean(true, &stdout, io.Discard)
	if rc != 0 {
		t.Errorf("RunClean rc = %d, want 0", rc)
	}

	out := stdout.String()
	if strings.Contains(out, ".env") {
		t.Errorf("stdout reported a file outside sessions/: %s", out)
	}
	if !strings.Contains(out, "would remove: "+sess) {
		t.Errorf("stdout missing session line: %s", out)
	}
}

// TestClean_RemovesAllSessions (AC2)
// Without flags, sessions/ is wiped but top-level files survive byte-identical.
func TestClean_RemovesAllSessions(t *testing.T) {
	home := t.TempDir()
	withJennyHome(t, home)

	sess1 := makeSessionDir(t, home, "sess-A")
	sess2 := makeSessionDir(t, home, "sess-B")

	// Top-level files that must be preserved byte-identical.
	topEnv := filepath.Join(home, ".env")
	topRoutes := filepath.Join(home, "routes.yaml")
	envBytes := []byte("FOO=bar\n")
	routesBytes := []byte("providers:\n  - name: anthropic\n")
	if err := os.WriteFile(topEnv, envBytes, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(topRoutes, routesBytes, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	// Top-level skills dir.
	skillsDir := filepath.Join(home, "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillsDir, "README.md"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var stdout, stderr bytes.Buffer
	rc := RunClean(false, &stdout, &stderr)
	if rc != 0 {
		t.Errorf("RunClean rc = %d, want 0 (stderr=%q)", rc, stderr.String())
	}

	// Sessions should be gone.
	if _, err := os.Stat(sess1); !os.IsNotExist(err) {
		t.Errorf("session dir %q still exists after clean: err=%v", sess1, err)
	}
	if _, err := os.Stat(sess2); !os.IsNotExist(err) {
		t.Errorf("session dir %q still exists after clean: err=%v", sess2, err)
	}

	// sessions/ must exist again (empty) so future runs can append.
	entries, err := os.ReadDir(filepath.Join(home, "sessions"))
	if err != nil {
		t.Fatalf("ReadDir(sessions): %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("sessions/ exists but is non-empty: %d entries", len(entries))
	}

	// Top-level files must be byte-identical.
	gotEnv, err := os.ReadFile(topEnv)
	if err != nil {
		t.Fatalf("ReadFile(.env): %v", err)
	}
	if !bytes.Equal(gotEnv, envBytes) {
		t.Errorf(".env contents changed: got=%q want=%q", gotEnv, envBytes)
	}
	gotRoutes, err := os.ReadFile(topRoutes)
	if err != nil {
		t.Fatalf("ReadFile(routes.yaml): %v", err)
	}
	if !bytes.Equal(gotRoutes, routesBytes) {
		t.Errorf("routes.yaml contents changed: got=%q want=%q", gotRoutes, routesBytes)
	}
	if _, err := os.Stat(filepath.Join(skillsDir, "README.md")); err != nil {
		t.Errorf("skills/README.md vanished: %v", err)
	}
}

// TestClean_NothingToClean_EmptySessionsDir (AC3)
func TestClean_NothingToClean_EmptySessionsDir(t *testing.T) {
	home := t.TempDir()
	withJennyHome(t, home)

	// Create the empty sessions dir.
	if err := os.MkdirAll(filepath.Join(home, "sessions"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	var stdout, stderr bytes.Buffer
	rc := RunClean(false, &stdout, &stderr)
	if rc != 0 {
		t.Errorf("RunClean rc = %d, want 0 (stderr=%q)", rc, stderr.String())
	}
	if !strings.Contains(stdout.String(), "nothing to clean") {
		t.Errorf("stdout = %q, want it to contain %q", stdout.String(), "nothing to clean")
	}
}

// TestClean_NothingToClean_MissingSessionsDir (AC3)
func TestClean_NothingToClean_MissingSessionsDir(t *testing.T) {
	home := t.TempDir()
	withJennyHome(t, home)
	// No sessions/ directory exists.

	var stdout, stderr bytes.Buffer
	rc := RunClean(false, &stdout, &stderr)
	if rc != 0 {
		t.Errorf("RunClean rc = %d, want 0 (stderr=%q)", rc, stderr.String())
	}
	if !strings.Contains(stdout.String(), "nothing to clean") {
		t.Errorf("stdout = %q, want it to contain %q", stdout.String(), "nothing to clean")
	}
}

// TestClean_HelpOutput (AC3) — usage must mention `clean`, `compact`, and `--dry-run`.
func TestClean_HelpOutput(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := RunCleanHelp(&stdout, &stderr)
	if rc != 0 {
		t.Errorf("RunCleanHelp rc = %d, want 0 (stderr=%q)", rc, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{"clean", "compact", "--dry-run"} {
		if !strings.Contains(out, want) {
			t.Errorf("clean --help output missing %q:\n%s", want, out)
		}
	}
}

// TestClean_HonorsJennyHome (AC4) — only JENNY_HOME sessions are touched;
// the real ~/.jenny must remain intact.
func TestClean_HonorsJennyHome(t *testing.T) {
	// Use two temp dirs: real (never touched) and alt (the JENNY_HOME target).
	realHome := t.TempDir()
	altHome := t.TempDir()
	withJennyHome(t, altHome)

	// Put a session in the alt home.
	makeSessionDir(t, altHome, "alpha")

	// Plant a sentinel in the real home that must NOT be removed.
	realSessions := filepath.Join(realHome, "sessions")
	if err := os.MkdirAll(realSessions, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	realSentinel := filepath.Join(realSessions, "realSession")
	if err := os.MkdirAll(realSentinel, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(realSentinel, "transcript.jsonl"), []byte(`{}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var stdout, stderr bytes.Buffer
	rc := RunClean(false, &stdout, &stderr)
	if rc != 0 {
		t.Errorf("RunClean rc = %d, want 0 (stderr=%q)", rc, stderr.String())
	}

	// alt home: alpha must be gone.
	if _, err := os.Stat(filepath.Join(altHome, "sessions", "alpha")); !os.IsNotExist(err) {
		t.Errorf("alpha session in alt home survived: err=%v", err)
	}
	// real home: sentinel must survive.
	if _, err := os.Stat(filepath.Join(realSentinel, "transcript.jsonl")); err != nil {
		t.Errorf("real home sentinel was removed: %v", err)
	}
}
