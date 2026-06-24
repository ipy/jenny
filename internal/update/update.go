// Package update implements self-update checks and CDN-based asset downloads.
package update

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/ipy/jenny/internal/log"
)

const (
	DefaultInfoURL  = "https://info.jenny.tokenplant.api.ltd"
	infoTimeout     = 10 * time.Second
	downloadTimeout = 60 * time.Second
)

type ResourceInfo struct {
	Version string `json:"version"`
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
}

type InfoResponse struct {
	Executable *ResourceInfo `json:"executable"`
	Models     *ResourceInfo `json:"models"`
	CDN        []string      `json:"cdn"`
}

// FetchInfo contacts the info endpoint and returns the update manifest.
func FetchInfo(ctx context.Context, baseURL, currentVersion, modelsVersion string) (*InfoResponse, error) {
	path := fmt.Sprintf("/v1/%s/%s/%s/%s", currentVersion, runtime.GOOS, runtime.GOARCH, modelsVersion)
	url := baseURL + path

	reqCtx, cancel := context.WithTimeout(ctx, infoTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("info endpoint returned HTTP %d", resp.StatusCode)
	}

	var info InfoResponse
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("decoding info response: %w", err)
	}
	return &info, nil
}

// DownloadFromCDN tries each CDN prefix in order, downloads the resource,
// verifies SHA-256 integrity, and writes to destPath atomically.
func DownloadFromCDN(ctx context.Context, cdnPrefixes []string, resourcePath, expectedSHA, destPath string) error {
	var lastErr error

	for _, prefix := range cdnPrefixes {
		url := prefix + resourcePath
		err := downloadAndVerify(ctx, url, expectedSHA, destPath)
		if err == nil {
			return nil
		}
		lastErr = err
		log.Debug("CDN download failed, trying next", "url", url, "error", err)
	}

	return fmt.Errorf("all CDN mirrors failed for %s: %w", resourcePath, lastErr)
}

func downloadAndVerify(ctx context.Context, url, expectedSHA, destPath string) error {
	reqCtx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	// Write to temp file while computing SHA
	tmpPath := destPath + ".tmp"
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return err
	}

	f, err := os.Create(tmpPath)
	if err != nil {
		return err
	}
	defer func() {
		f.Close()
		os.Remove(tmpPath)
	}()

	hasher := sha256.New()
	writer := io.MultiWriter(f, hasher)

	if _, err := io.Copy(writer, resp.Body); err != nil {
		return err
	}
	f.Close()

	// Verify SHA
	actualSHA := hex.EncodeToString(hasher.Sum(nil))
	if actualSHA != expectedSHA {
		return fmt.Errorf("SHA-256 mismatch: got %s, want %s", actualSHA, expectedSHA)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, destPath); err != nil {
		return err
	}
	return nil
}

// InstallBinary copies the downloaded binary (already UPX-compressed, not gzipped)
// to destPath and marks it executable.
func InstallBinary(srcPath, destPath string) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return err
	}

	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()

	tmpPath := destPath + ".tmp"
	dst, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}

	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		os.Remove(tmpPath)
		return err
	}
	dst.Close()

	return os.Rename(tmpPath, destPath)
}

// InstallModels decompresses a gzipped models.json and updates the meta file.
func InstallModels(gzPath, modelsPath, metaPath, version string) error {
	src, err := os.Open(gzPath)
	if err != nil {
		return err
	}
	defer src.Close()

	gr, err := gzip.NewReader(src)
	if err != nil {
		return err
	}
	defer gr.Close()

	tmpPath := modelsPath + ".cdn.tmp"
	dst, err := os.Create(tmpPath)
	if err != nil {
		return err
	}

	if _, err := io.Copy(dst, gr); err != nil {
		dst.Close()
		os.Remove(tmpPath)
		return err
	}
	dst.Close()

	if err := os.Rename(tmpPath, modelsPath); err != nil {
		os.Remove(tmpPath)
		return err
	}

	// Update meta.json with new models version
	meta := make(map[string]interface{})
	if data, err := os.ReadFile(metaPath); err == nil {
		json.Unmarshal(data, &meta)
	}
	meta["modelsVersion"] = version
	meta["fetchedAt"] = time.Now().Format(time.RFC3339)
	data, _ := json.Marshal(meta)
	return os.WriteFile(metaPath, data, 0644)
}

// ManagedBinaryPath returns the path to the managed (updated) jenny binary.
func ManagedBinaryPath(jennyHome string) string {
	return filepath.Join(jennyHome, "bin", "jenny")
}

// ShouldLaunchManaged returns true if the managed binary exists and is executable.
func ShouldLaunchManaged(binPath string) bool {
	info, err := os.Stat(binPath)
	if err != nil {
		return false
	}
	return info.Mode()&0111 != 0
}

// GetModelsVersion reads the modelsVersion from meta.json.
// Returns "none" if the meta file doesn't exist or lacks the field.
func GetModelsVersion(metaPath string) string {
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return "none"
	}
	var meta map[string]interface{}
	if err := json.Unmarshal(data, &meta); err != nil {
		return "none"
	}
	v, ok := meta["modelsVersion"].(string)
	if !ok || v == "" {
		return "none"
	}
	return v
}

// GetInfoURL returns the info endpoint URL from env or default.
func GetInfoURL() string {
	if u := os.Getenv("JENNY_INFO_URL"); u != "" {
		return u
	}
	return DefaultInfoURL
}

// IsDisabled returns true if self-update is disabled via environment.
func IsDisabled() bool {
	return os.Getenv("JENNY_NO_UPDATE") == "1"
}

// RegistryReloader is implemented by config.ModelRegistry to allow reload after update.
type RegistryReloader interface {
	Reload() error
	MetaPath() string
}

// RunBackgroundUpdate performs the full update check and download in the background.
// It fetches the info endpoint, downloads any available updates from CDN,
// and installs them locally. If registry is non-nil and models are updated,
// the registry is reloaded from the refreshed cache file.
func RunBackgroundUpdate(ctx context.Context, jennyHome, currentVersion string, registry RegistryReloader) {
	if IsDisabled() {
		return
	}

	infoURL := GetInfoURL()

	// Determine models version from registry's meta or fallback
	var metaPath string
	if registry != nil {
		metaPath = registry.MetaPath()
	} else {
		metaPath = filepath.Join(jennyHome, "meta.json")
	}
	modelsVersion := GetModelsVersion(metaPath)

	info, err := FetchInfo(ctx, infoURL, currentVersion, modelsVersion)
	if err != nil {
		log.Debug("update check failed", "error", err)
		return
	}

	if len(info.CDN) == 0 {
		log.Debug("update: no CDN prefixes in response")
		return
	}

	// Download and install executable update.
	// Binary is served raw (UPX-compressed at build time, no gzip wrapper).
	if info.Executable != nil && info.Executable.SHA256 != "" {
		dlPath := filepath.Join(jennyHome, "bin", "jenny.dl.tmp")
		if err := os.MkdirAll(filepath.Dir(dlPath), 0755); err == nil {
			if err := DownloadFromCDN(ctx, info.CDN, info.Executable.Path, info.Executable.SHA256, dlPath); err == nil {
				destPath := ManagedBinaryPath(jennyHome)
				if err := InstallBinary(dlPath, destPath); err != nil {
					log.Warn("update: failed to install binary", "error", err)
				} else {
					log.Info("update: new version installed", "version", info.Executable.Version)
				}
				os.Remove(dlPath)
			} else {
				log.Debug("update: binary download failed", "error", err)
			}
		}
	}

	// Download and install models update (gzipped on CDN).
	if info.Models != nil && info.Models.SHA256 != "" {
		modelsGzPath := filepath.Join(jennyHome, "models-cdn.gz.tmp")
		if err := DownloadFromCDN(ctx, info.CDN, info.Models.Path, info.Models.SHA256, modelsGzPath); err == nil {
			modelsPath := filepath.Join(jennyHome, "models.json")
			if err := InstallModels(modelsGzPath, modelsPath, metaPath, info.Models.Version); err != nil {
				log.Warn("update: failed to install models", "error", err)
			} else {
				log.Debug("update: models updated", "version", info.Models.Version)
				if registry != nil {
					if err := registry.Reload(); err != nil {
						log.Warn("update: failed to reload registry after models update", "error", err)
					}
				}
			}
			os.Remove(modelsGzPath)
		} else {
			log.Debug("update: models download failed", "error", err)
		}
	}
}
