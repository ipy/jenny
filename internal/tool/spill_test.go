package tool

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ipy/jenny/internal/constants"
)

func withJennyHome(t *testing.T, dir string) {
	t.Helper()
	orig := constants.JennyHomeDirFunc
	constants.JennyHomeDirFunc = func() string { return dir }
	t.Cleanup(func() { constants.JennyHomeDirFunc = orig })
}

func TestCheckAndSpill_UnderLimit(t *testing.T) {
	content := strings.Repeat("a", 100)
	result := CheckAndSpill(content, SpillConfig{MaxInlineSize: DefaultMaxInlineSize})

	if result.Spilled {
		t.Fatal("expected no spill for small content")
	}
	if result.Truncated {
		t.Fatal("expected truncated=false for small content")
	}
	if result.Content != content {
		t.Fatalf("expected original content, got len=%d", len(result.Content))
	}
}

func TestCheckAndSpill_SpillsWithPreview(t *testing.T) {
	home := t.TempDir()
	withJennyHome(t, home)

	head := strings.Repeat("H", 1024)
	middle := strings.Repeat("M", DefaultMaxInlineSize)
	tail := strings.Repeat("T", 512)
	content := head + middle + tail

	result := CheckAndSpill(content, SpillConfig{
		MaxInlineSize: DefaultMaxInlineSize,
		SessionID:     "sess-spill-1",
	})

	if !result.Spilled {
		t.Fatal("expected spill for oversized content")
	}
	if !result.Truncated {
		t.Fatal("expected truncated=true")
	}
	if result.SpillPath == "" {
		t.Fatal("expected spill path")
	}

	if !strings.Contains(result.Content, "Output too large for inline display") {
		t.Errorf("missing size notice: %s", result.Content[:min(200, len(result.Content))])
	}
	if !strings.Contains(result.Content, result.SpillPath) {
		t.Errorf("missing spill path in preview")
	}
	if !strings.Contains(result.Content, "First 1024 chars:") {
		t.Errorf("missing first-chars header")
	}
	if !strings.Contains(result.Content, "Last 512 chars:") {
		t.Errorf("missing last-chars header")
	}
	if !strings.Contains(result.Content, "Use the Read tool to view the full output if needed.") {
		t.Errorf("missing Read hint")
	}
	if !strings.Contains(result.Content, head) {
		t.Errorf("preview missing head segment")
	}
	if !strings.Contains(result.Content, tail) {
		t.Errorf("preview missing tail segment")
	}

	onDisk, err := os.ReadFile(result.SpillPath)
	if err != nil {
		t.Fatalf("reading spill file: %v", err)
	}
	if string(onDisk) != content {
		t.Fatalf("spill file content mismatch: got %d bytes, want %d", len(onDisk), len(content))
	}

	wantDir := filepath.Join(home, "sessions", "sess-spill-1", "spills")
	if !strings.HasPrefix(result.SpillPath, wantDir) {
		t.Errorf("spill path %q not under %q", result.SpillPath, wantDir)
	}
}

func TestCheckAndSpill_SeparateFilesPerSpill(t *testing.T) {
	home := t.TempDir()
	withJennyHome(t, home)

	cfg := SpillConfig{
		MaxInlineSize: DefaultMaxInlineSize,
		SessionID:     "sess-spill-2",
	}
	content := strings.Repeat("x", DefaultMaxInlineSize+1)

	r1 := CheckAndSpill(content, cfg)
	r2 := CheckAndSpill(content, cfg)

	if r1.SpillPath == "" || r2.SpillPath == "" {
		t.Fatal("expected both spills to produce paths")
	}
	if r1.SpillPath == r2.SpillPath {
		t.Errorf("expected distinct spill files, both %q", r1.SpillPath)
	}
}

func TestCheckAndSpill_ProjectLocalFallback(t *testing.T) {
	home := t.TempDir()
	withJennyHome(t, home)
	// Make home spills dir unusable by using a file at the spills path.
	blockPath := filepath.Join(home, "sessions", "sess-proj", "spills")
	if err := os.MkdirAll(filepath.Dir(blockPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(blockPath, []byte("not a dir"), 0644); err != nil {
		t.Fatal(err)
	}

	projectRoot := t.TempDir()
	content := strings.Repeat("p", DefaultMaxInlineSize+100)

	result := CheckAndSpill(content, SpillConfig{
		MaxInlineSize: DefaultMaxInlineSize,
		SessionID:     "sess-proj",
		ProjectRoot:   projectRoot,
	})

	if !result.Spilled {
		t.Fatal("expected spill")
	}
	wantPrefix := filepath.Join(constants.ProjectJennyDir(projectRoot), "sessions", "sess-proj", "spills")
	if !strings.HasPrefix(result.SpillPath, wantPrefix) {
		t.Errorf("spill path %q not under project spills %q", result.SpillPath, wantPrefix)
	}
}

func TestApplySpillToToolResult(t *testing.T) {
	home := t.TempDir()
	withJennyHome(t, home)

	content := strings.Repeat("z", DefaultMaxInlineSize+50)
	tr := ApplySpillToToolResult(content, SpillConfig{
		MaxInlineSize: DefaultMaxInlineSize,
		SessionID:     "sess-apply",
	}, false)

	if tr.IsError {
		t.Fatal("expected non-error result")
	}
	if !tr.Truncated {
		t.Fatal("expected truncated=true")
	}
	if !strings.Contains(tr.Content, "Full output saved to:") {
		t.Errorf("expected spill preview, got: %s", tr.Content[:min(120, len(tr.Content))])
	}
}

func TestApplySpillToToolResult_PreservesError(t *testing.T) {
	tr := ApplySpillToToolResult(strings.Repeat("e", DefaultMaxInlineSize+1), SpillConfig{}, true)
	if !tr.IsError {
		t.Fatal("expected error result unchanged")
	}
	if tr.Truncated {
		t.Fatal("expected no truncation flag on errors")
	}
}
