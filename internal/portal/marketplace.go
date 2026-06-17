package portal

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/ipy/jenny/internal/constants"
	"github.com/ipy/jenny/internal/git"
)

// MarketplaceItem represents a marketplace item for the API response.
type MarketplaceItem struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Version     string `json:"version"`
	DownloadURL string `json:"download_url"`
}

// MarketplaceInstallRequest represents the JSON body for POST /api/marketplace/install.
type MarketplaceInstallRequest struct {
	Type        string `json:"type"`
	Name        string `json:"name"`
	DownloadURL string `json:"download_url"`
}

// MarketplaceInstallResponse represents the JSON response for POST /api/marketplace/install.
type MarketplaceInstallResponse struct {
	Status string `json:"status"`
	Path   string `json:"path"`
}

// Default marketplace URL
const defaultMarketplaceURL = "https://raw.githubusercontent.com/ipy/jenny-marketplace/main/index.json"

// handleMarketplaceBrowse handles GET /api/marketplace/browse.
// Fetches marketplace index from URL and returns parsed items.
func (p *Portal) handleMarketplaceBrowse(w http.ResponseWriter, r *http.Request) {
	source := r.URL.Query().Get("source")
	if source == "" {
		source = defaultMarketplaceURL
	}

	// Validate URL scheme
	if !strings.HasPrefix(source, "http://") && !strings.HasPrefix(source, "https://") {
		http.Error(w, `{"error":"invalid URL scheme: must be http or https"}`, http.StatusBadRequest)
		return
	}

	// Fetch marketplace index
	resp, err := http.Get(source)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		http.Error(w, fmt.Sprintf(`{"error":"failed to fetch marketplace: status %d"}`, resp.StatusCode), http.StatusBadGateway)
		return
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadGateway)
		return
	}

	// Parse marketplace index
	var index struct {
		Skills  []MarketplaceItem `json:"skills"`
		Plugins []MarketplaceItem `json:"plugins"`
		MCP     []MarketplaceItem `json:"mcp"`
	}
	if err := json.Unmarshal(body, &index); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadRequest)
		return
	}

	// Combine all items and set type labels
	var items []MarketplaceItem
	for _, item := range index.Skills {
		item.Type = "skill"
		items = append(items, item)
	}
	for _, item := range index.Plugins {
		item.Type = "plugin"
		items = append(items, item)
	}
	for _, item := range index.MCP {
		item.Type = "mcp"
		items = append(items, item)
	}

	if items == nil {
		items = []MarketplaceItem{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(items)
}

// handleMarketplaceInstall handles POST /api/marketplace/install.
// Downloads and extracts a package to the correct directory.
func (p *Portal) handleMarketplaceInstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req MarketplaceInstallRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	// Validate request
	if req.Type == "" || req.Name == "" || req.DownloadURL == "" {
		http.Error(w, `{"error":"type, name, and download_url are required"}`, http.StatusBadRequest)
		return
	}

	if req.Type != "skill" && req.Type != "plugin" && req.Type != "mcp" {
		http.Error(w, `{"error":"type must be skill, plugin, or mcp"}`, http.StatusBadRequest)
		return
	}

	// AC 7: validate .tar.gz suffix
	if !strings.HasSuffix(req.DownloadURL, ".tar.gz") {
		http.Error(w, `{"error":"download_url must end with .tar.gz"}`, http.StatusBadRequest)
		return
	}

	homeDir := constants.JennyHomeDir()
	var installPath string

	switch req.Type {
	case "skill":
		installPath = filepath.Join(homeDir, "skills", req.Name)
		// Check if already installed
		if _, err := os.Stat(installPath); err == nil {
			http.Error(w, `{"error":"already installed"}`, http.StatusConflict)
			return
		}
		if err := downloadAndExtract(req.DownloadURL, installPath); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
			return
		}

	case "plugin":
		cwd, err := os.Getwd()
		if err != nil {
			cwd = "."
		}
		gitRoot, err := git.GetRoot(cwd)
		if err != nil {
			gitRoot = cwd
		}
		installPath = filepath.Join(gitRoot, constants.PluginDirName, req.Name)
		// Check if already installed
		if _, err := os.Stat(installPath); err == nil {
			http.Error(w, `{"error":"already installed"}`, http.StatusConflict)
			return
		}
		if err := downloadAndExtract(req.DownloadURL, installPath); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
			return
		}

	case "mcp":
		// For MCP, we download and parse the manifest to extract command/args,
		// then update mcp.json with the real config.
		mcpPath := filepath.Join(homeDir, "mcp.json")
		var config map[string]struct {
			Command  string   `json:"command"`
			Args     []string `json:"args"`
			Disabled bool     `json:"disabled,omitempty"`
		}

		// Read existing config if present
		if data, err := os.ReadFile(mcpPath); err == nil {
			if err := json.Unmarshal(data, &config); err != nil {
				http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadRequest)
				return
			}
		}

		// Initialize config map if nil (mcp.json doesn't exist)
		if config == nil {
			config = make(map[string]struct {
				Command  string   `json:"command"`
				Args     []string `json:"args"`
				Disabled bool     `json:"disabled,omitempty"`
			})
		}

		// Check if already installed
		if _, exists := config[req.Name]; exists {
			http.Error(w, `{"error":"already installed"}`, http.StatusConflict)
			return
		}

		// Download and extract to temp dir to get manifest
		tmpDir, err := os.MkdirTemp("", "jenny-mcp-*")
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
			return
		}
		defer os.RemoveAll(tmpDir)

		if err := downloadAndExtract(req.DownloadURL, tmpDir); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
			return
		}

		// Look for manifest.json in the extracted package
		manifestPath := filepath.Join(tmpDir, "manifest.json")
		var manifest struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		}
		if data, err := os.ReadFile(manifestPath); err == nil {
			if err := json.Unmarshal(data, &manifest); err != nil {
				http.Error(w, fmt.Sprintf(`{"error":"invalid manifest.json: %s"}`, err.Error()), http.StatusBadRequest)
				return
			}
		} else {
			http.Error(w, `{"error":"MCP package must contain manifest.json with command and args fields"}`, http.StatusBadRequest)
			return
		}

		if manifest.Command == "" {
			http.Error(w, `{"error":"manifest.json must specify a non-empty command field"}`, http.StatusBadRequest)
			return
		}

		// Add to config with real command from manifest
		config[req.Name] = struct {
			Command  string   `json:"command"`
			Args     []string `json:"args"`
			Disabled bool     `json:"disabled,omitempty"`
		}{
			Command:  manifest.Command,
			Args:     manifest.Args,
			Disabled: false,
		}

		// Write updated config
		configData, err := json.MarshalIndent(config, "", "  ")
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
			return
		}
		if err := os.WriteFile(mcpPath, configData, 0644); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
			return
		}

		// Return stable path (mcp.json path, not the temp dir that was cleaned up)
		installPath = mcpPath
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(MarketplaceInstallResponse{
		Status: "installed",
		Path:   installPath,
	})
}

// downloadAndExtract downloads a tar.gz file from the given URL and extracts it to destDir.
func downloadAndExtract(url, destDir string) error {
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	gr, err := gzip.NewReader(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gr.Close()

	absDest, err := filepath.Abs(destDir)
	if err != nil {
		return fmt.Errorf("failed to get absolute path of destDir: %w", err)
	}

	tr := tar.NewReader(gr)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar: %w", err)
		}

		// Security: prevent path traversal by sanitizing the entry name.
		cleanName := filepath.ToSlash(h.Name)
		if cleanName == "" || strings.HasPrefix(cleanName, "/") {
			return fmt.Errorf("tar entry %q is not allowed: must be a safe relative path", h.Name)
		}
		for _, part := range strings.Split(cleanName, "/") {
			if part == ".." {
				return fmt.Errorf("tar entry %q is not allowed: path traversal detected", h.Name)
			}
		}

		target := filepath.Join(destDir, cleanName)

		// Security: defense-in-depth check using filepath.Abs.
		absTarget, err := filepath.Abs(target)
		if err != nil {
			return fmt.Errorf("failed to get absolute path of target: %w", err)
		}
		if !strings.HasPrefix(absTarget, absDest) {
			return fmt.Errorf("tar entry %q attempted to escape destination directory", h.Name)
		}

		// Security: mask file permissions to safe values.
		switch h.Typeflag {
		case tar.TypeDir:
			mode := os.FileMode(h.Mode) & 0o755
			if mode == 0 {
				mode = 0o755
			}
			if err := os.MkdirAll(target, mode); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}
		case tar.TypeReg:
			mode := os.FileMode(h.Mode) & 0o644
			if mode == 0 {
				mode = 0o644
			}
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return fmt.Errorf("failed to create parent directory: %w", err)
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR|os.O_TRUNC, mode)
			if err != nil {
				return fmt.Errorf("failed to create file: %w", err)
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return fmt.Errorf("failed to write file: %w", err)
			}
			f.Close()
		}
	}

	return nil
}
