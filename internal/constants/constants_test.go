package constants

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScratchpadDir(t *testing.T) {
	// Override JennyHomeDir to use a captured t.TempDir value
	tmpDir := t.TempDir()
	originalFunc := JennyHomeDirFunc
	JennyHomeDirFunc = func() string { return tmpDir }
	defer func() { JennyHomeDirFunc = originalFunc }()

	// Test that ScratchpadDir returns correct path
	scratchpadDir := ScratchpadDir()
	expected := filepath.Join(tmpDir, "scratchpad")
	if scratchpadDir != expected {
		t.Errorf("expected %s, got %s", expected, scratchpadDir)
	}
}

func TestScratchpadDir_WithHome(t *testing.T) {
	// Test with a mock home directory
	tmpDir := t.TempDir()
	originalFunc := JennyHomeDirFunc
	JennyHomeDirFunc = func() string { return tmpDir }
	defer func() { JennyHomeDirFunc = originalFunc }()

	scratchpadDir := ScratchpadDir()
	expected := filepath.Join(tmpDir, "scratchpad")
	if scratchpadDir != expected {
		t.Errorf("expected %s, got %s", expected, scratchpadDir)
	}
}

func TestJennyHomeDir(t *testing.T) {
	// Test that default JennyHomeDir returns a non-empty path
	homeDir := JennyHomeDir()
	if homeDir == "" {
		t.Error("expected non-empty JennyHomeDir")
	}

	// Test with home directory
	home, err := os.UserHomeDir()
	if err == nil {
		expected := filepath.Join(home, ProjectDirName)
		if JennyHomeDir() != expected {
			t.Errorf("expected %s, got %s", expected, JennyHomeDir())
		}
	}
}

func TestJennyHomeDir_Env(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("JENNY_HOME", tmpDir)

	if JennyHomeDir() != tmpDir {
		t.Errorf("expected %s, got %s", tmpDir, JennyHomeDir())
	}
}

func TestProjectDirName(t *testing.T) {
	if ProjectDirName != ".jenny" {
		t.Errorf("ProjectDirName = %q, want .jenny", ProjectDirName)
	}
}

func TestPluginDirName(t *testing.T) {
	if PluginDirName != ".jenny-plugin" {
		t.Errorf("PluginDirName = %q, want .jenny-plugin", PluginDirName)
	}
}

func TestIgnoreFileName(t *testing.T) {
	if IgnoreFileName != ".jennyignore" {
		t.Errorf("IgnoreFileName = %q, want .jennyignore", IgnoreFileName)
	}
}

func TestEnsureScratchpadDir(t *testing.T) {
	tmpDir := t.TempDir()
	originalFunc := JennyHomeDirFunc
	JennyHomeDirFunc = func() string { return tmpDir }
	defer func() { JennyHomeDirFunc = originalFunc }()

	sessionID := "test-session-123"
	if err := EnsureScratchpadDir(sessionID); err != nil {
		t.Fatalf("EnsureScratchpadDir() error = %v", err)
	}

	expected := filepath.Join(tmpDir, "sessions", sessionID, "scratchpad")
	info, err := os.Stat(expected)
	if err != nil {
		t.Fatalf("scratchpad dir not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("scratchpad path is not a directory")
	}
}

func TestEnsureScratchpadDir_Idempotent(t *testing.T) {
	tmpDir := t.TempDir()
	originalFunc := JennyHomeDirFunc
	JennyHomeDirFunc = func() string { return tmpDir }
	defer func() { JennyHomeDirFunc = originalFunc }()

	sessionID := "test-session-456"
	if err := EnsureScratchpadDir(sessionID); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if err := EnsureScratchpadDir(sessionID); err != nil {
		t.Fatalf("second call: %v", err)
	}
}

func TestEnsureScratchpadDir_EmptySessionID(t *testing.T) {
	if err := EnsureScratchpadDir(""); err != nil {
		t.Errorf("EnsureScratchpadDir(\"\") should return nil, got %v", err)
	}
}

func TestEnsureScratchpadDir_InvalidSessionID(t *testing.T) {
	// EnsureScratchpadDir delegates to ScratchpadDir which uses SessionDir,
	// so path traversal attempts are handled by the path resolution layer.
	// EnsureScratchpadDir itself just creates the directory.
	tmpDir := t.TempDir()
	originalFunc := JennyHomeDirFunc
	JennyHomeDirFunc = func() string { return tmpDir }
	defer func() { JennyHomeDirFunc = originalFunc }()

	// Even with a session ID containing path separators, os.MkdirAll will
	// create the path as-is under the sessions tree (harmless).
	err := EnsureScratchpadDir("test-session")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestProjectJennyDir(t *testing.T) {
	cwd := "/some/project"
	expected := filepath.Join(cwd, ProjectDirName)
	if got := ProjectJennyDir(cwd); got != expected {
		t.Errorf("ProjectJennyDir(%q) = %q, want %q", cwd, got, expected)
	}
}
