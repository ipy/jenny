package portal

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMarketplaceBrowse_CustomSource(t *testing.T) {
	// Mock marketplace server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"skills": [{"name": "test-skill", "description": "A test skill", "version": "1.0.0", "download_url": "http://example.com/skill.tar.gz"}],
			"plugins": [{"name": "test-plugin", "description": "A test plugin", "version": "1.1.0", "download_url": "http://example.com/plugin.tar.gz"}],
			"mcp": [{"name": "test-mcp", "description": "A test mcp", "version": "1.2.0", "download_url": "http://example.com/mcp.tar.gz"}]
		}`)
	}))
	defer ts.Close()

	p := &Portal{}

	// Test custom source
	req := httptest.NewRequest("GET", "/api/marketplace/browse?source="+ts.URL, nil)
	rr := httptest.NewRecorder()

	p.handleMarketplaceBrowse(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d. Body: %s", rr.Code, rr.Body.String())
	}

	var items []MarketplaceItem
	if err := json.Unmarshal(rr.Body.Bytes(), &items); err != nil {
		t.Fatal(err)
	}

	if len(items) != 3 {
		t.Errorf("expected 3 items, got %d", len(items))
	}
}

func TestMarketplaceBrowse_Error502(t *testing.T) {
	p := &Portal{}
	// Use an unreachable local address
	req := httptest.NewRequest("GET", "/api/marketplace/browse?source=http://127.0.0.1:1", nil)
	rr := httptest.NewRecorder()

	p.handleMarketplaceBrowse(rr, req)

	if rr.Code != http.StatusBadGateway {
		t.Errorf("expected 502 Bad Gateway, got %d", rr.Code)
	}
}

func TestMarketplaceInstall_Validation_Unit(t *testing.T) {
	p := &Portal{}

	t.Run("InvalidSuffix", func(t *testing.T) {
		installReq := MarketplaceInstallRequest{
			Type:        "skill",
			Name:        "test-skill",
			DownloadURL: "http://example.com/package.zip",
		}
		body, _ := json.Marshal(installReq)
		req := httptest.NewRequest("POST", "/api/marketplace/install", strings.NewReader(string(body)))
		rr := httptest.NewRecorder()

		p.handleMarketplaceInstall(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request, got %d", rr.Code)
		}
		if !strings.Contains(rr.Body.String(), ".tar.gz") {
			t.Errorf("expected error message to mention .tar.gz, got %s", rr.Body.String())
		}
	})

	t.Run("InvalidType", func(t *testing.T) {
		installReq := MarketplaceInstallRequest{
			Type:        "banana",
			Name:        "test-skill",
			DownloadURL: "http://example.com/package.tar.gz",
		}
		body, _ := json.Marshal(installReq)
		req := httptest.NewRequest("POST", "/api/marketplace/install", strings.NewReader(string(body)))
		rr := httptest.NewRecorder()

		p.handleMarketplaceInstall(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request, got %d", rr.Code)
		}
		if !strings.Contains(rr.Body.String(), "type must be skill, plugin, or mcp") {
			t.Errorf("expected error message to mention type, got %s", rr.Body.String())
		}
	})
}

func TestMarketplaceInstall_Skill_Unit(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "jenny-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Mock JENNY_HOME
	oldHome := os.Getenv("JENNY_HOME")
	os.Setenv("JENNY_HOME", tmpDir)
	defer os.Setenv("JENNY_HOME", oldHome)

	// Create a mock tar.gz
	tarData := createTarGz(t, map[string]string{
		"test-skill/SKILL.md": "Test skill content",
	})

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(tarData)
	}))
	defer ts.Close()

	p := &Portal{}
	installReq := MarketplaceInstallRequest{
		Type:        "skill",
		Name:        "test-skill",
		DownloadURL: ts.URL + "/test.tar.gz",
	}
	body, _ := json.Marshal(installReq)
	req := httptest.NewRequest("POST", "/api/marketplace/install", strings.NewReader(string(body)))
	rr := httptest.NewRecorder()

	p.handleMarketplaceInstall(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d. Body: %s", rr.Code, rr.Body.String())
	}

	// Verify installation
	skillDir := filepath.Join(tmpDir, "skills", "test-skill")
	if _, err := os.Stat(skillDir); err != nil {
		t.Errorf("expected skill directory to exist: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(skillDir, "test-skill/SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "Test skill content" {
		t.Errorf("expected 'Test skill content', got %q", string(content))
	}

	// Test conflict
	rr = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/api/marketplace/install", strings.NewReader(string(body)))
	p.handleMarketplaceInstall(rr, req)

	if rr.Code != http.StatusConflict {
		t.Errorf("expected 409 Conflict, got %d", rr.Code)
	}
}

func TestMarketplaceInstall_MCP_Unit(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "jenny-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Mock JENNY_HOME
	oldHome := os.Getenv("JENNY_HOME")
	os.Setenv("JENNY_HOME", tmpDir)
	defer os.Setenv("JENNY_HOME", oldHome)

	// Create a mock tar.gz with manifest.json
	tarData := createTarGz(t, map[string]string{
		"manifest.json": `{"command": "node", "args": ["server.js"]}`,
	})

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(tarData)
	}))
	defer ts.Close()

	p := &Portal{}
	installReq := MarketplaceInstallRequest{
		Type:        "mcp",
		Name:        "test-mcp",
		DownloadURL: ts.URL + "/test.tar.gz",
	}
	body, _ := json.Marshal(installReq)
	req := httptest.NewRequest("POST", "/api/marketplace/install", strings.NewReader(string(body)))
	rr := httptest.NewRecorder()

	p.handleMarketplaceInstall(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d. Body: %s", rr.Code, rr.Body.String())
	}

	// Verify mcp.json
	mcpPath := filepath.Join(tmpDir, "mcp.json")
	data, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatal(err)
	}

	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}

	if _, exists := config["test-mcp"]; !exists {
		t.Error("expected test-mcp to exist in mcp.json")
	}
}

func TestMarketplaceInstall_Plugin_Unit(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "jenny-test-plugin-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// In production, plugins are installed relative to git root or CWD.
	// For testing, we'll use tmpDir as our base.
	oldWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldWd)

	// Create a mock tar.gz
	tarData := createTarGz(t, map[string]string{
		"test-plugin/plugin.json": `{"name": "test-plugin", "version": "1.0.0", "description": "Test plugin"}`,
	})

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(tarData)
	}))
	defer ts.Close()

	p := &Portal{}
	installReq := MarketplaceInstallRequest{
		Type:        "plugin",
		Name:        "test-plugin",
		DownloadURL: ts.URL + "/test.tar.gz",
	}
	body, _ := json.Marshal(installReq)
	req := httptest.NewRequest("POST", "/api/marketplace/install", strings.NewReader(string(body)))
	rr := httptest.NewRecorder()

	p.handleMarketplaceInstall(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d. Body: %s", rr.Code, rr.Body.String())
	}

	// Verify installation
	pluginDir := filepath.Join(tmpDir, ".jenny-plugin", "test-plugin")
	if _, err := os.Stat(pluginDir); err != nil {
		t.Errorf("expected plugin directory to exist at %s: %v", pluginDir, err)
	}

	content, err := os.ReadFile(filepath.Join(pluginDir, "test-plugin/plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "test-plugin") {
		t.Errorf("expected plugin.json to contain 'test-plugin', got %q", string(content))
	}
}

func TestPathTraversalDefense(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "jenny-test-traversal-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a malicious tar.gz
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	// Try to write to a file outside destDir using ".."
	hdr := &tar.Header{
		Name: "../outside.txt",
		Mode: 0644,
		Size: 4,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	tw.Write([]byte("evil"))

	tw.Close()
	gw.Close()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(buf.Bytes())
	}))
	defer ts.Close()

	err = downloadAndExtract(ts.URL, tmpDir)
	if err == nil {
		t.Error("expected error for path traversal, got nil")
	} else if !strings.Contains(err.Error(), "path traversal detected") {
		t.Errorf("expected 'path traversal detected' error, got %v", err)
	}

	// Test defense-in-depth Abs check
	buf.Reset()
	gw = gzip.NewWriter(&buf)
	tw = tar.NewWriter(gw)

	// Some tar implementations might allow absolute paths or other tricks
	hdr = &tar.Header{
		Name: "/tmp/evil.txt",
		Mode: 0644,
		Size: 4,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	tw.Write([]byte("evil"))

	tw.Close()
	gw.Close()

	err = downloadAndExtract(ts.URL, tmpDir)
	if err == nil {
		t.Error("expected error for absolute path traversal, got nil")
	} else if !strings.Contains(err.Error(), "not allowed") && !strings.Contains(err.Error(), "escape") {
		t.Errorf("expected security error, got %v", err)
	}
}

func createTarGz(t *testing.T, files map[string]string) []byte {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	for name, content := range files {
		hdr := &tar.Header{
			Name: name,
			Mode: 0644,
			Size: int64(len(content)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}

	return buf.Bytes()
}
