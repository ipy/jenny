package portal

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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

func TestMarketplaceInstall_SuffixValidation(t *testing.T) {
	p := &Portal{}
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
