package grepinproc

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRun_BasicMatch(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.txt", "hello world\nfoo bar\nhello again\n")
	writeFile(t, root, "b.txt", "no match here\n")
	writeFile(t, root, "sub/c.txt", "deep hello\n")

	r, err := Run(context.Background(), Options{
		Pattern: "hello",
		Path:    root,
		Cwd:     root,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(r) != 2 {
		t.Fatalf("expected 2 files, got %d", len(r))
	}
	for _, res := range r {
		if res.Target == filepath.Join(root, "a.txt") {
			if len(res.Matches) != 2 {
				t.Errorf("a.txt: expected 2 matches, got %d", len(res.Matches))
			}
		}
	}
}

func TestRun_CaseInsensitive(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.txt", "Hello\nHELLO\nhello\n")

	r, err := Run(context.Background(), Options{
		Pattern:    "hello",
		Path:       root,
		Cwd:        root,
		IgnoreCase: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(r) != 1 {
		t.Fatalf("expected 1 file, got %d", len(r))
	}
	if len(r[0].Matches) != 3 {
		t.Errorf("expected 3 matches, got %d", len(r[0].Matches))
	}
}

func TestRun_GlobFilter(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.txt", "hello\n")
	writeFile(t, root, "b.md", "hello\n")
	writeFile(t, root, "c.txt", "hello\n")

	r, err := Run(context.Background(), Options{
		Pattern: "hello",
		Path:    root,
		Cwd:     root,
		Glob:    "*.txt",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(r) != 2 {
		t.Errorf("expected 2 files (only .txt), got %d", len(r))
	}
	for _, res := range r {
		if filepath.Ext(res.Target) != ".txt" {
			t.Errorf("non-.txt file in results: %s", res.Target)
		}
	}
}

func TestRun_IgnoresGitAndSvn(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.txt", "hello\n")
	writeFile(t, root, ".git/HEAD", "hello\n")
	writeFile(t, root, ".svn/entries", "hello\n")

	r, err := Run(context.Background(), Options{
		Pattern: "hello",
		Path:    root,
		Cwd:     root,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(r) != 1 {
		t.Errorf("expected 1 file (a.txt only), got %d", len(r))
	}
}

func TestRun_ContextBeforeAfter(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.txt", "L1\nL2\nL3 hello\nL4\nL5\nL6\n")

	r, err := Run(context.Background(), Options{
		Pattern:       "hello",
		Path:          root,
		Cwd:           root,
		ContextBefore: 2,
		ContextAfter:  2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(r) != 1 {
		t.Fatalf("expected 1 file, got %d", len(r))
	}
	m := r[0].Matches[0]
	if m.Line != 3 {
		t.Errorf("expected line 3, got %d", m.Line)
	}
	if len(m.Before) != 2 || m.Before[0] != "L1" || m.Before[1] != "L2" {
		t.Errorf("unexpected Before: %v", m.Before)
	}
	if len(m.After) != 2 || m.After[0] != "L4" || m.After[1] != "L5" {
		t.Errorf("unexpected After: %v", m.After)
	}
}

func TestRun_Multiline(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.txt", "start\nfoo\nbar\nend\n")

	r, err := Run(context.Background(), Options{
		Pattern:   "start.*end",
		Path:      root,
		Cwd:       root,
		Multiline: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(r) != 1 {
		t.Fatalf("expected 1 file, got %d", len(r))
	}
	if len(r[0].Matches) != 1 {
		t.Errorf("expected 1 match, got %d", len(r[0].Matches))
	}
}

func TestRun_FileTypeFilter(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.go", "hello\n")
	writeFile(t, root, "b.txt", "hello\n")

	r, err := Run(context.Background(), Options{
		Pattern:  "hello",
		Path:     root,
		Cwd:      root,
		FileType: "go",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(r) != 1 {
		t.Fatalf("expected 1 file, got %d", len(r))
	}
	if filepath.Ext(r[0].Target) != ".go" {
		t.Errorf("expected .go file, got %s", r[0].Target)
	}
}

func TestRun_HiddenFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.txt", "hello\n")
	writeFile(t, root, ".hidden.txt", "hello\n")

	// Without Hidden: only a.txt
	r, err := Run(context.Background(), Options{
		Pattern: "hello",
		Path:    root,
		Cwd:     root,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(r) != 1 {
		t.Errorf("expected 1 file without Hidden, got %d", len(r))
	}

	// With Hidden: both
	r, err = Run(context.Background(), Options{
		Pattern: "hello",
		Path:    root,
		Cwd:     root,
		Hidden:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(r) != 2 {
		t.Errorf("expected 2 files with Hidden, got %d", len(r))
	}
}

func TestRun_EmptyPatternErrors(t *testing.T) {
	_, err := Run(context.Background(), Options{
		Pattern: "",
		Path:    t.TempDir(),
		Cwd:     t.TempDir(),
	})
	if err == nil {
		t.Error("expected error for empty pattern")
	}
}

func TestRun_InvalidPatternErrors(t *testing.T) {
	_, err := Run(context.Background(), Options{
		Pattern: "[unclosed",
		Path:    t.TempDir(),
		Cwd:     t.TempDir(),
	})
	if err == nil {
		t.Error("expected error for invalid pattern")
	}
}

func TestRun_ContextCancellation(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.txt", "hello\n")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled
	_, err := Run(ctx, Options{
		Pattern: "hello",
		Path:    root,
		Cwd:     root,
	})
	if err == nil {
		t.Error("expected context error")
	}
}

func TestRender_Content(t *testing.T) {
	// Test the rendering helpers (these will live in grep.go, but we
	// verify the format expected by the existing GrepTool tests).
	root := t.TempDir()
	writeFile(t, root, "a.txt", "L1 hello\nL2\nL3 hello again\n")

	r, err := Run(context.Background(), Options{
		Pattern: "hello",
		Path:    root,
		Cwd:     root,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(r) != 1 {
		t.Fatalf("expected 1 file, got %d", len(r))
	}
	// Simulate the format: "filename:lineno:content"
	for _, m := range r[0].Matches {
		if m.Line < 1 {
			t.Errorf("line number should be >= 1, got %d", m.Line)
		}
		if m.Content == "" {
			t.Error("content should be non-empty")
		}
	}
}
