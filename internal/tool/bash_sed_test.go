package tool

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBashSedOOM(t *testing.T) {
	tool := NewBashTool(PermissionEdit)
	cwd := t.TempDir()

	// Create a sparse file > 1 GiB using Truncate (does not actually write 1 GiB of data)
	largeFile := filepath.Join(cwd, "large.bin")
	f, err := os.Create(largeFile)
	if err != nil {
		t.Fatalf("failed to create sparse file: %v", err)
	}
	if err := f.Truncate(1<<30 + 1); err != nil {
		f.Close()
		t.Fatalf("failed to truncate file: %v", err)
	}
	f.Close()

	// Call executeSed on the oversized file
	result, err := tool.executeSed("sed -i 's/foo/bar/g' "+largeFile, cwd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.IsError {
		t.Error("expected IsError=true for oversized file, got false")
	}
	if !strings.Contains(result.Content, "too large") {
		t.Errorf("expected error message to contain 'too large', got: %s", result.Content)
	}
}
