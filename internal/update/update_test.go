package update

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestFetchInfo(t *testing.T) {
	resp := InfoResponse{
		Executable: &ResourceInfo{
			Version: "0.5.0",
			Path:    "jenny/0.5.0/jenny-darwin-arm64",
			SHA256:  "abc123",
		},
		Models: &ResourceInfo{
			Version: "20250625",
			Path:    "models/20250625/models.json.gz",
			SHA256:  "def456",
		},
		CDN: []string{"https://cdn1.example.com/", "https://cdn2.example.com/"},
	}

	var receivedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	ctx := context.Background()
	info, err := FetchInfo(ctx, srv.URL, "0.4.0", "20250624")
	if err != nil {
		t.Fatalf("FetchInfo failed: %v", err)
	}

	expectedPath := fmt.Sprintf("/v1/0.4.0/%s/%s/20250624", runtime.GOOS, runtime.GOARCH)
	if receivedPath != expectedPath {
		t.Errorf("request path = %q, want %q", receivedPath, expectedPath)
	}

	if info.Executable == nil {
		t.Fatal("expected non-nil Executable")
	}
	if info.Executable.Version != "0.5.0" {
		t.Errorf("Executable.Version = %q, want %q", info.Executable.Version, "0.5.0")
	}
	if len(info.CDN) != 2 {
		t.Errorf("CDN length = %d, want 2", len(info.CDN))
	}
}

func TestFetchInfoNoUpdate(t *testing.T) {
	resp := InfoResponse{CDN: []string{"https://cdn.example.com/"}}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	ctx := context.Background()
	info, err := FetchInfo(ctx, srv.URL, "0.4.0", "20250625")
	if err != nil {
		t.Fatalf("FetchInfo failed: %v", err)
	}
	if info.Executable != nil {
		t.Error("expected nil Executable when no update")
	}
	if info.Models != nil {
		t.Error("expected nil Models when no update")
	}
}

func TestFetchInfoTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {}
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := FetchInfo(ctx, srv.URL, "0.4.0", "20250624")
	if err == nil {
		t.Fatal("expected error on cancelled context")
	}
}

func TestDownloadFromCDN(t *testing.T) {
	content := []byte("hello world binary content")
	hash := sha256.Sum256(content)
	expectedSHA := hex.EncodeToString(hash[:])

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/good/path/file" {
			w.Write(content)
		} else {
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	destPath := filepath.Join(tmpDir, "downloaded")

	ctx := context.Background()
	cdns := []string{srv.URL + "/bad/", srv.URL + "/good/"}

	err := DownloadFromCDN(ctx, cdns, "path/file", expectedSHA, destPath)
	if err != nil {
		t.Fatalf("DownloadFromCDN failed: %v", err)
	}

	data, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("reading downloaded file: %v", err)
	}
	if string(data) != string(content) {
		t.Errorf("file content mismatch")
	}
}

func TestDownloadFromCDNBadSHA(t *testing.T) {
	content := []byte("some content")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(content)
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	destPath := filepath.Join(tmpDir, "downloaded")

	ctx := context.Background()
	cdns := []string{srv.URL + "/"}

	err := DownloadFromCDN(ctx, cdns, "file", "wronghash", destPath)
	if err == nil {
		t.Fatal("expected error on SHA mismatch")
	}

	if _, err := os.Stat(destPath); err == nil {
		t.Error("file should not exist after SHA mismatch")
	}
}

func TestDownloadFromCDNAllFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	destPath := filepath.Join(tmpDir, "downloaded")

	ctx := context.Background()
	cdns := []string{srv.URL + "/a/", srv.URL + "/b/"}

	err := DownloadFromCDN(ctx, cdns, "file", "sha", destPath)
	if err == nil {
		t.Fatal("expected error when all CDNs fail")
	}
}

func TestInstallBinary(t *testing.T) {
	tmpDir := t.TempDir()
	binDir := filepath.Join(tmpDir, "bin")
	destPath := filepath.Join(binDir, "jenny")

	// Binary is raw (UPX-compressed, no gzip)
	content := []byte("#!/bin/sh\necho hello\n")
	srcPath := filepath.Join(tmpDir, "jenny-downloaded")
	if err := os.WriteFile(srcPath, content, 0644); err != nil {
		t.Fatal(err)
	}

	err := InstallBinary(srcPath, destPath)
	if err != nil {
		t.Fatalf("InstallBinary failed: %v", err)
	}

	info, err := os.Stat(destPath)
	if err != nil {
		t.Fatalf("binary not found: %v", err)
	}
	if info.Mode()&0111 == 0 {
		t.Error("binary is not executable")
	}

	data, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(content) {
		t.Errorf("binary content mismatch")
	}
}

func TestInstallModels(t *testing.T) {
	tmpDir := t.TempDir()
	modelsPath := filepath.Join(tmpDir, "models.json")
	metaPath := filepath.Join(tmpDir, "meta.json")

	modelsContent := []byte(`{"schemaVersion":1,"models":{}}`)
	gzPath := filepath.Join(tmpDir, "models.json.gz")
	f, err := os.Create(gzPath)
	if err != nil {
		t.Fatal(err)
	}
	gw := gzip.NewWriter(f)
	gw.Write(modelsContent)
	gw.Close()
	f.Close()

	err = InstallModels(gzPath, modelsPath, metaPath, "20250625")
	if err != nil {
		t.Fatalf("InstallModels failed: %v", err)
	}

	data, err := os.ReadFile(modelsPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(modelsContent) {
		t.Error("models.json content mismatch")
	}

	metaData, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatal(err)
	}
	var meta map[string]interface{}
	json.Unmarshal(metaData, &meta)
	if meta["modelsVersion"] != "20250625" {
		t.Errorf("meta modelsVersion = %v, want 20250625", meta["modelsVersion"])
	}
}

func TestShouldLaunchManaged(t *testing.T) {
	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "bin", "jenny")

	if ShouldLaunchManaged(binPath) {
		t.Error("should not launch when binary doesn't exist")
	}

	os.MkdirAll(filepath.Dir(binPath), 0755)
	os.WriteFile(binPath, []byte("binary"), 0644)
	if ShouldLaunchManaged(binPath) {
		t.Error("should not launch when binary is not executable")
	}

	os.Chmod(binPath, 0755)
	if !ShouldLaunchManaged(binPath) {
		t.Error("should launch when binary exists and is executable")
	}
}

func TestManagedBinaryPath(t *testing.T) {
	path := ManagedBinaryPath("/home/user/.jenny")
	expected := filepath.Join("/home/user/.jenny", "bin", "jenny")
	if path != expected {
		t.Errorf("ManagedBinaryPath = %q, want %q", path, expected)
	}
}

func TestGetModelsVersion(t *testing.T) {
	tmpDir := t.TempDir()
	metaPath := filepath.Join(tmpDir, "meta.json")

	v := GetModelsVersion(metaPath)
	if v != "none" {
		t.Errorf("expected 'none' without meta file, got %q", v)
	}

	meta := map[string]interface{}{"modelsVersion": "20250625", "fetchedAt": "2025-06-25T00:00:00Z"}
	data, _ := json.Marshal(meta)
	os.WriteFile(metaPath, data, 0644)

	v = GetModelsVersion(metaPath)
	if v != "20250625" {
		t.Errorf("expected '20250625', got %q", v)
	}

	meta2 := map[string]interface{}{"fetchedAt": "2025-06-25T00:00:00Z"}
	data2, _ := json.Marshal(meta2)
	os.WriteFile(metaPath, data2, 0644)

	v = GetModelsVersion(metaPath)
	if v != "none" {
		t.Errorf("expected 'none' without modelsVersion, got %q", v)
	}
}
