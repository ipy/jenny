package tool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestWriteTool_NoPriorRead_BlackBox validates that writing without a prior Read fails.
// Also validates the exact error message per spec.
func TestWriteTool_NoPriorRead_BlackBox(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewReadFileCache()
	wt := NewWriteTool(cache)

	// Write to a path never read
	result, err := wt.Execute(context.Background(), map[string]any{
		"file_path": filepath.Join(tmpDir, "never_read.txt"),
		"content":   "should fail",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error (should be in result.IsError): %v", err)
	}
	if !result.IsError {
		t.Fatal("FAIL: expected IsError=true when writing without prior Read")
	}
	if !strings.Contains(result.Content, "Cannot write without reading first") {
		t.Fatalf("FAIL: wrong error message. Got: %s", result.Content)
	}

	// Write with empty file_path
	result, err = wt.Execute(context.Background(), map[string]any{
		"file_path": "",
		"content":   "should fail",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("FAIL: expected error for empty file_path")
	}

	// Write with non-string file_path
	result, err = wt.Execute(context.Background(), map[string]any{
		"file_path": 42,
		"content":   "should fail",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("FAIL: expected error for non-string file_path")
	}
}

// TestWriteTool_StaleMtime_BlackBox validates staleness detection from
// multiple external modification methods.
func TestWriteTool_StaleMtime_BlackBox(t *testing.T) {
	tmpDir := t.TempDir()
	t.Run("external write changes mtime", func(t *testing.T) {
		cache := NewReadFileCache()
		rt := NewReadTool(PermissionEdit, cache)
		wt := NewWriteTool(cache)

		f := filepath.Join(tmpDir, "stale.txt")
		if err := os.WriteFile(f, []byte("original\n"), 0644); err != nil {
			t.Fatal(err)
		}

		// Read
		_, err := rt.Execute(context.Background(), map[string]any{"file_path": f}, tmpDir)
		if err != nil {
			t.Fatal(err)
		}

		// External modification
		time.Sleep(time.Millisecond * 10)
		if err := os.WriteFile(f, []byte("external change\n"), 0644); err != nil {
			t.Fatal(err)
		}

		// Write should fail
		result, err := wt.Execute(context.Background(), map[string]any{
			"file_path": f,
			"content":   "my content",
		}, tmpDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.IsError {
			t.Fatal("FAIL: expected staleness error after external write")
		}
		if !strings.Contains(result.Content, "File has changed since it was read") {
			t.Fatalf("FAIL: wrong error message. Got: %s", result.Content)
		}

		// Verify original content preserved (write was rejected)
		data, _ := os.ReadFile(f)
		if string(data) != "external change\n" {
			t.Fatalf("FAIL: file content should be unchanged after rejected write. Got: %s", string(data))
		}
	})

	t.Run("touch changes mtime", func(t *testing.T) {
		cache := NewReadFileCache()
		rt := NewReadTool(PermissionEdit, cache)
		wt := NewWriteTool(cache)

		f := filepath.Join(tmpDir, "stale2.txt")
		if err := os.WriteFile(f, []byte("original\n"), 0644); err != nil {
			t.Fatal(err)
		}

		_, err := rt.Execute(context.Background(), map[string]any{"file_path": f}, tmpDir)
		if err != nil {
			t.Fatal(err)
		}

		// touch the file (change mtime without changing content)
		time.Sleep(time.Millisecond * 10)
		newTime := time.Now()
		if err := os.Chtimes(f, newTime, newTime); err != nil {
			t.Fatal(err)
		}

		result, err := wt.Execute(context.Background(), map[string]any{
			"file_path": f,
			"content":   "my content",
		}, tmpDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.IsError {
			t.Fatal("FAIL: expected staleness error after touch (mtime change)")
		}
	})
}

// TestWriteTool_ParentDirs_BlackBox validates parent directory creation.
// Covers: deep path creation, already-existing parents, and single-level parents.
func TestWriteTool_ParentDirs_BlackBox(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewReadFileCache()
	rt := NewReadTool(PermissionEdit, cache)
	wt := NewWriteTool(cache)

	// Setup: create a deep file, read it, then remove both file and parents
	deepPath := filepath.Join(tmpDir, "a", "b", "c", "d", "file.txt")
	if err := os.MkdirAll(filepath.Dir(deepPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(deepPath, []byte("initial\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Read to satisfy prior-read requirement
	_, err := rt.Execute(context.Background(), map[string]any{"file_path": deepPath}, tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	// Remove file AND parent dirs
	if err := os.RemoveAll(filepath.Join(tmpDir, "a")); err != nil {
		t.Fatal(err)
	}

	// Write — should recreate parent dirs and file
	result, err := wt.Execute(context.Background(), map[string]any{
		"file_path": deepPath,
		"content":   "new content here",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("FAIL: write returned error: %s", result.Content)
	}

	// Verify file exists with correct content
	data, err := os.ReadFile(deepPath)
	if err != nil {
		t.Fatalf("FAIL: file not found after write: %v", err)
	}
	if string(data) != "new content here" {
		t.Fatalf("FAIL: unexpected content: %q", string(data))
	}

	// Verify intermediate directories were created
	if _, err := os.Stat(filepath.Join(tmpDir, "a", "b", "c", "d")); err != nil {
		t.Fatalf("FAIL: intermediate dirs not created: %v", err)
	}
}

// TestWriteTool_PatchDiff_BlackBox validates success message output format.
func TestWriteTool_PatchDiff_BlackBox(t *testing.T) {
	t.Run("modified file produces success message", func(t *testing.T) {
		tmpDir := t.TempDir()
		cache := NewReadFileCache()
		rt := NewReadTool(PermissionEdit, cache)
		wt := NewWriteTool(cache)

		f := filepath.Join(tmpDir, "diff_test.txt")
		if err := os.WriteFile(f, []byte("line one\nline two\nline three\n"), 0644); err != nil {
			t.Fatal(err)
		}

		_, err := rt.Execute(context.Background(), map[string]any{"file_path": f}, tmpDir)
		if err != nil {
			t.Fatal(err)
		}

		result, err := wt.Execute(context.Background(), map[string]any{
			"file_path": f,
			"content":   "line one\nline two modified\nline three\nline four\n",
		}, tmpDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.IsError {
			t.Fatalf("write failed: %s", result.Content)
		}

		// Verify success message format
		if !strings.Contains(result.Content, "Wrote ") {
			t.Fatal("FAIL: missing 'Wrote ' prefix")
		}
		if !strings.Contains(result.Content, f) {
			t.Fatal("FAIL: missing file path")
		}
		if !strings.Contains(result.Content, "bytes") {
			t.Fatal("FAIL: missing 'bytes'")
		}

		// Verify file content was actually written
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "line two modified") {
			t.Fatal("FAIL: file content was not updated")
		}
	})

	t.Run("new file (empty old content) shows success message", func(t *testing.T) {
		tmpDir := t.TempDir()
		cache := NewReadFileCache()
		rt := NewReadTool(PermissionEdit, cache)
		wt := NewWriteTool(cache)

		f := filepath.Join(tmpDir, "new_diff_test.txt")
		if err := os.WriteFile(f, []byte(""), 0644); err != nil {
			t.Fatal(err)
		}

		// Read empty file to satisfy prior-read requirement
		_, err := rt.Execute(context.Background(), map[string]any{"file_path": f}, tmpDir)
		if err != nil {
			t.Fatal(err)
		}

		result, err := wt.Execute(context.Background(), map[string]any{
			"file_path": f,
			"content":   "brand new\ncontent\n",
		}, tmpDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.IsError {
			t.Fatalf("write failed: %s", result.Content)
		}

		if !strings.Contains(result.Content, "Wrote ") {
			t.Fatal("FAIL: missing 'Wrote ' prefix")
		}
	})
}

// TestWriteTool_CacheUpdated_BlackBox validates that the readFileState is
// refreshed after write, enabling subsequent writes without re-read.
func TestWriteTool_CacheUpdated_BlackBox(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewReadFileCache()
	rt := NewReadTool(PermissionEdit, cache)
	wt := NewWriteTool(cache)

	f := filepath.Join(tmpDir, "cache_test.txt")
	if err := os.WriteFile(f, []byte("version 1\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Read
	_, err := rt.Execute(context.Background(), map[string]any{"file_path": f}, tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	// First write (updates cache)
	result, err := wt.Execute(context.Background(), map[string]any{
		"file_path": f,
		"content":   "version 2\n",
	}, tmpDir)
	if err != nil {
		t.Fatalf("first write error: %v", err)
	}
	if result.IsError {
		t.Fatalf("first write failed: %s", result.Content)
	}

	// External modification between writes (this should NOT cause staleness
	// because the cache was updated after first write with new mtime)
	time.Sleep(time.Millisecond * 10)

	// Second write — no intervening Read, should succeed
	result, err = wt.Execute(context.Background(), map[string]any{
		"file_path": f,
		"content":   "version 3\n",
	}, tmpDir)
	if err != nil {
		t.Fatalf("second write error: %v", err)
	}
	if result.IsError {
		t.Fatalf("FAIL: second write (no re-read) failed: %s", result.Content)
	}

	// Verify final content
	data, _ := os.ReadFile(f)
	if string(data) != "version 3\n" {
		t.Fatalf("FAIL: unexpected final content: %q", string(data))
	}
}

// TestWriteTool_PathTraversal_BlackBox validates that writes outside cwd are blocked.
func TestWriteTool_PathTraversal_BlackBox(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewReadFileCache()
	wt := NewWriteTool(cache)

	// Register a path inside tmpDir in cache to bypass prior-read requirement
	insidePath := filepath.Join(tmpDir, "inside.txt")
	cache.RecordRead(insidePath, "test", time.Now(), true, 0, 0)

	// Try to write outside cwd via ..
	result, err := wt.Execute(context.Background(), map[string]any{
		"file_path": filepath.Join(tmpDir, "..", "outside.txt"),
		"content":   "should not work",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("path traversal FAIL: expected error when writing outside cwd")
	}
	if !strings.Contains(result.Content, "not allowed") && !strings.Contains(result.Content, "outside working directory") {
		t.Fatalf("path traversal FAIL: wrong error: %s", result.Content)
	}
}

// TestWriteTool_FileDeletedAfterRead validates behavior when file is deleted
// between Read and Write.
func TestWriteTool_FileDeletedAfterRead(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewReadFileCache()
	rt := NewReadTool(PermissionEdit, cache)
	wt := NewWriteTool(cache)

	f := filepath.Join(tmpDir, "deleted.txt")
	if err := os.WriteFile(f, []byte("original\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Read to satisfy prior-read requirement
	_, err := rt.Execute(context.Background(), map[string]any{"file_path": f}, tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	// Delete the file (but keep parent dirs)
	if err := os.Remove(f); err != nil {
		t.Fatal(err)
	}

	// Write should succeed (file doesn't exist, but cache has entry)
	// os.Stat will fail, so staleness check is skipped, and we proceed
	result, err := wt.Execute(context.Background(), map[string]any{
		"file_path": f,
		"content":   "new content",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("write for deleted file failed: %s", result.Content)
	}

	// Verify file was recreated
	data, _ := os.ReadFile(f)
	if string(data) != "new content" {
		t.Fatalf("unexpected content after recreating deleted file: %q", string(data))
	}
}

// TestWriteTool_EmptyContent validates writing empty string content.
func TestWriteTool_EmptyContent(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewReadFileCache()
	rt := NewReadTool(PermissionEdit, cache)
	wt := NewWriteTool(cache)

	f := filepath.Join(tmpDir, "empty.txt")
	if err := os.WriteFile(f, []byte("original\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Read
	_, err := rt.Execute(context.Background(), map[string]any{"file_path": f}, tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	// Write empty content
	result, err := wt.Execute(context.Background(), map[string]any{
		"file_path": f,
		"content":   "",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("write empty content failed: %s", result.Content)
	}

	data, _ := os.ReadFile(f)
	if string(data) != "" {
		t.Fatalf("expected empty file, got: %q", string(data))
	}

	// Verify success message
	if !strings.Contains(result.Content, "Wrote ") {
		t.Fatalf("expected success message, got: %s", result.Content)
	}
}

// TestWriteTool_RegistryIntegration validates that Registry properly wires
// WriteTool when WithReadFileCache is used.
func TestWriteTool_RegistryIntegration(t *testing.T) {
	t.Run("WriteTool present when WithReadFileCache called", func(t *testing.T) {
		tools := NewRegistry().WithBaseTools().WithReadFileCache(nil).Build()
		wt := FindTool(tools, "write")
		if wt == nil {
			t.Fatal("REGISTRY FAIL: WriteTool not found when WithReadFileCache used")
		}
	})

	t.Run("WriteTool absent without WithReadFileCache", func(t *testing.T) {
		tools := NewRegistry().WithBaseTools().Build()
		wt := FindTool(tools, "write")
		if wt != nil {
			t.Fatal("REGISTRY FAIL: WriteTool should not be present without WithReadFileCache")
		}
	})

	t.Run("WriteTool in deny rules", func(t *testing.T) {
		tools := NewRegistry().WithBaseTools().WithReadFileCache(nil).WithDenyRules([]string{"write"}).Build()
		wt := FindTool(tools, "write")
		if wt != nil {
			t.Fatal("REGISTRY FAIL: WriteTool should be filtered by deny rules")
		}
	})

	t.Run("name and description", func(t *testing.T) {
		cache := NewReadFileCache()
		wt := NewWriteTool(cache)
		if wt.Name() != "write" {
			t.Fatalf("expected name 'write', got %q", wt.Name())
		}
		if !strings.Contains(wt.Description(), "Requires prior Read") {
			t.Fatalf("description missing 'Requires prior Read': %s", wt.Description())
		}
	})
}
