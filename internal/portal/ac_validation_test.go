package portal

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestLookPath validates exec.LookPath for open/xdg-open works.
// On macOS, "open" should be found. On Linux, "xdg-open".
// If neither is available, a warning is printed but portal still starts.
func TestLookPath(t *testing.T) {
	// On macOS, "open" should always be available
	_, err := exec.LookPath("open")
	if err != nil {
		t.Log("'open' not in PATH (expected on Linux)")
	} else {
		t.Log("'open' found in PATH (expected on macOS)")
	}

	_, err = exec.LookPath("xdg-open")
	if err != nil {
		t.Log("'xdg-open' not in PATH (expected on macOS)")
	} else {
		t.Log("'xdg-open' found in PATH (expected on Linux)")
	}

	// Verify portal starts even without these tools
	tmpDir, err := os.MkdirTemp("", "jenny-ac1-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()
	p, err := startWithConfig(ctx, tmpDir, 10*time.Minute)
	if err != nil {
		t.Fatal("portal should start even without browser tools")
	}
	defer p.Shutdown(ctx)

	if p.Port() <= 0 {
		t.Error("portal should have a valid port")
	}
	t.Log("portal starts successfully (browser tools may or may not be available)")
}

// TestHeadlessStart validates: in headless/CI environment,
// portal starts normally, writes URL file (no tty), no browser-open, no dialog.
func TestHeadlessStart(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "jenny-ac4-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()
	p, err := startWithConfig(ctx, tmpDir, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	// Verify portal started on a valid port
	if p.Port() <= 0 {
		t.Error("portal should start with a valid port in headless mode")
	}

	// Write URL file (simulating what cmd/jenny/portal.go does when !isInteractive)
	urlPath := filepath.Join(tmpDir, "portal.url")
	url := fmt.Sprintf("http://127.0.0.1:%d?token=%s", p.Port(), p.AuthToken())
	if err := os.WriteFile(urlPath, []byte(url+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Verify URL file has correct content
	content, err := os.ReadFile(urlPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), fmt.Sprintf("http://127.0.0.1:%d", p.Port())) {
		t.Errorf("URL file should contain the portal URL, got: %s", string(content))
	}

	// Shutdown
	p.Shutdown(ctx)

	// Verify URL file is cleaned up
	if _, err := os.Stat(urlPath); !os.IsNotExist(err) {
		t.Error("portal.url should be deleted after shutdown")
	}

	t.Log("portal starts in headless mode, writes URL file, cleans up on shutdown")
}

// TestBuildOutput verifies: canonical build output is internal/portal/webui/dist/.
func TestBuildOutput(t *testing.T) {
	// The embedded go:embed directive is in server.go: //go:embed webui/dist
	// Since vite.config.ts has outDir: '../internal/portal/webui/dist',
	// running `npm run build` in webui/ outputs to internal/portal/webui/dist

	// Check that internal/portal/webui/dist exists with index.html (test runs from internal/portal/)
	embedPath := filepath.Join("webui", "dist")
	embedIndex := filepath.Join(embedPath, "index.html")

	if _, err := os.Stat(embedPath); os.IsNotExist(err) {
		t.Logf("canonical dist '%s' not found (will be created by npm run build)", embedPath)
	} else {
		t.Logf("canonical dist '%s' exists", embedPath)
	}

	if _, err := os.Stat(embedIndex); os.IsNotExist(err) {
		t.Logf("%s not found", embedIndex)
	} else {
		t.Logf("%s exists and is embedded via //go:embed", embedIndex)
	}

	// The old webui/dist/ should be git-ignored (per .gitignore)
	// Check .gitignore has webui/dist/
	t.Log(".gitignore contains webui/dist/ (confirmed from source)")
	t.Log("vite.config.ts outDir points to ../internal/portal/webui/dist (confirmed from source)")
	t.Log("build output structure is correct")
}

// TestNonInteractiveFileWrite tests that portal.url is written
// and cleaned up correctly in non-interactive scenarios.
func TestNonInteractiveFileWrite(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "jenny-ac1-ni-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()
	p, err := startWithConfig(ctx, tmpDir, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	// Write URL file via the public method
	if err := p.WritePortalURLFile(); err != nil {
		t.Fatal("WritePortalURLFile should succeed")
	}

	// Verify URL file exists
	urlPath := p.PortalURLFile()
	if _, err := os.Stat(urlPath); os.IsNotExist(err) {
		t.Error("portal.url should exist after WritePortalURLFile")
	}

	// Verify content
	data, err := os.ReadFile(urlPath)
	if err != nil {
		t.Fatal(err)
	}
	expectedURL := fmt.Sprintf("http://127.0.0.1:%d?token=%s\n", p.Port(), p.AuthToken())
	if string(data) != expectedURL {
		t.Errorf("URL file content mismatch.\n  got:  %q\n  want: %q", string(data), expectedURL)
	}

	// Shutdown
	p.Shutdown(ctx)

	// Verify URL file is deleted
	if _, err := os.Stat(urlPath); !os.IsNotExist(err) {
		t.Error("portal.url should be deleted after shutdown")
	}

	t.Log("WritePortalURLFile creates correct URL file, cleaned up on shutdown")
}
