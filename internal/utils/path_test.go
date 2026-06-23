package utils

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadDirSafe_UnderLimit(t *testing.T) {
	dir := t.TempDir()
	for i := range 10 {
		name := filepath.Join(dir, "file"+string(rune('0'+int32(i))))
		f, err := os.Create(name)
		if err != nil {
			t.Fatalf("create file %d: %v", i, err)
		}
		f.Close()
	}
	entries, err := ReadDirSafe(dir, 128)
	if err != nil {
		t.Fatalf("ReadDirSafe: %v", err)
	}
	if len(entries) != 10 {
		t.Errorf("got %d entries, want 10", len(entries))
	}
}

func TestReadDirSafe_OverLimit(t *testing.T) {
	dir := t.TempDir()
	for i := range 10 {
		name := filepath.Join(dir, "file"+string(rune('0'+int32(i))))
		f, err := os.Create(name)
		if err != nil {
			t.Fatalf("create file %d: %v", i, err)
		}
		f.Close()
	}
	entries, err := ReadDirSafe(dir, 3)
	if err != nil {
		t.Fatalf("ReadDirSafe: %v", err)
	}
	if len(entries) != 3 {
		t.Errorf("got %d entries, want 3 (truncated at max)", len(entries))
	}
}

func TestReadDirSafe_MaxZeroReturnsAll(t *testing.T) {
	dir := t.TempDir()
	for i := range 200 {
		name := filepath.Join(dir, "file"+string(rune('0'+(i%10)))+"_"+string(rune('0'+int32(i/100)))+string(rune('0'+int32((i/10)%10)))+string(rune('0'+int32(i%10))))
		f, err := os.Create(name)
		if err != nil {
			t.Fatalf("create file %d: %v", i, err)
		}
		f.Close()
	}
	entries, err := ReadDirSafe(dir, 0)
	if err != nil {
		t.Fatalf("ReadDirSafe: %v", err)
	}
	if len(entries) != 200 {
		t.Errorf("got %d entries, want 200 (max=0 should return all)", len(entries))
	}
}

func TestReadDirSafe_Empty(t *testing.T) {
	dir := t.TempDir()
	entries, err := ReadDirSafe(dir, 128)
	if err != nil {
		t.Fatalf("ReadDirSafe: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("got %d entries, want 0", len(entries))
	}
}

func TestReadDirSafe_NotExist(t *testing.T) {
	_, err := ReadDirSafe("/nonexistent/path/xyz", 128)
	if err == nil {
		t.Fatal("expected error for nonexistent path")
	}
	if !os.IsNotExist(err) {
		t.Errorf("expected os.IsNotExist, got %v", err)
	}
}

func TestReadFileSafe_Normal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "normal.txt")
	content := []byte("hello world")
	os.WriteFile(path, content, 0644)

	data, err := ReadFileSafe(path, 1<<20) // 1 MiB limit
	if err != nil {
		t.Fatalf("ReadFileSafe: %v", err)
	}
	if string(data) != string(content) {
		t.Errorf("got %q, want %q", data, content)
	}
}

func TestReadFileSafe_Truncated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.txt")
	// Create a file with 2000 bytes
	os.WriteFile(path, make([]byte, 2000), 0644)

	data, err := ReadFileSafe(path, 1000)
	if err != nil {
		t.Fatalf("ReadFileSafe: %v", err)
	}
	if len(data) != 1000 {
		t.Errorf("got %d bytes, want 1000 (truncated at max)", len(data))
	}
}

func TestReadFileSafe_NotExist(t *testing.T) {
	_, err := ReadFileSafe("/nonexistent/file.txt", 1024)
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}
