package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ipy/jenny/internal/lsp"
)

// LoadPluginLSPServers loads LSP server configurations from a plugin.
// Reads the .lsp.json file referenced by the manifest's lspServers field.
// Returns nil, nil if no lspServers path is configured.
func LoadPluginLSPServers(p *LoadedPlugin) (map[string]lsp.ServerConfig, error) {
	if p.Manifest == nil || p.Manifest.LSPServers == "" {
		return nil, nil
	}

	configPath := filepath.Join(p.RootPath, p.Manifest.LSPServers)
	info, err := os.Stat(configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("plugin LSP config %q: %w", configPath, err)
	}

	if info.IsDir() {
		return loadLSPConfigDir(configPath)
	}

	return loadLSPConfigFile(configPath)
}

// loadLSPConfigFile loads LSP server configs from a single .lsp.json file.
func loadLSPConfigFile(path string) (map[string]lsp.ServerConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading LSP config %q: %w", path, err)
	}

	var configs map[string]lsp.ServerConfig
	if err := json.Unmarshal(data, &configs); err != nil {
		return nil, fmt.Errorf("parsing LSP config %q: %w", path, err)
	}

	return configs, nil
}

// loadLSPConfigDir loads LSP server configs from all .lsp.json files in a directory.
func loadLSPConfigDir(dir string) (map[string]lsp.ServerConfig, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading LSP config dir %q: %w", dir, err)
	}

	configs := make(map[string]lsp.ServerConfig)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		fileConfigs, err := loadLSPConfigFile(path)
		if err != nil {
			continue
		}
		for name, cfg := range fileConfigs {
			configs[name] = cfg
		}
	}

	return configs, nil
}
