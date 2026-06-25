package lsp

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ServerManager manages multiple LSP server instances and routes requests
// based on file extensions.
type ServerManager struct {
	mu           sync.RWMutex
	servers      map[string]*RealClient // server name -> client
	extensionMap map[string][]string    // ".go" -> ["server-name"]
	openedFiles  map[string]string      // file URI -> server name
	initialized  bool
}

// NewServerManager creates a new ServerManager.
func NewServerManager() *ServerManager {
	return &ServerManager{
		servers:      make(map[string]*RealClient),
		extensionMap: make(map[string][]string),
		openedFiles:  make(map[string]string),
	}
}

// Initialize configures the manager with server configs.
// Safe to call multiple times; subsequent calls are no-ops.
func (m *ServerManager) Initialize(configs map[string]ServerConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.initialized {
		return nil
	}

	for name, config := range configs {
		if config.Command == "" {
			return fmt.Errorf("server %q missing required 'command' field", name)
		}
		if len(config.ExtensionToLanguage) == 0 {
			return fmt.Errorf("server %q missing required 'extensionToLanguage' field", name)
		}

		client := NewRealClient(config)
		m.servers[name] = client

		for ext := range config.ExtensionToLanguage {
			normalized := strings.ToLower(ext)
			m.extensionMap[normalized] = append(m.extensionMap[normalized], name)
		}
	}

	m.initialized = true
	return nil
}

// Connected returns true if at least one server is healthy.
func (m *ServerManager) Connected() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, server := range m.servers {
		if server.Connected() {
			return true
		}
	}
	return false
}

// SendRequest sends an LSP request to the appropriate server for the given file.
// Returns undefined if no server handles this file type.
func (m *ServerManager) SendRequest(ctx context.Context, filePath, method string, params interface{}) (interface{}, error) {
	server, err := m.ensureServerStarted(filePath)
	if err != nil {
		return nil, err
	}
	if server == nil {
		return nil, nil
	}

	result, err := server.sendRequest(ctx, method, params)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// OpenFile sends a textDocument/didOpen notification to the appropriate server.
func (m *ServerManager) OpenFile(filePath, content string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	server, err := m.ensureServerStartedLocked(filePath)
	if err != nil {
		return err
	}
	if server == nil {
		return nil
	}

	fileURI := pathToFileURL(filePath)
	ext := strings.ToLower(filepath.Ext(filePath))
	serverName := m.getServerNameForExt(ext)
	if serverName == "" {
		return nil
	}

	langID := "plaintext"
	if cfg, ok := m.servers[serverName]; ok {
		if lid, ok := cfg.config.ExtensionToLanguage[ext]; ok {
			langID = lid
		}
	}

	// Skip if already opened
	if _, ok := m.openedFiles[fileURI]; ok {
		return nil
	}

	if err := server.openFile(fileURI, langID, content); err != nil {
		return err
	}

	m.openedFiles[fileURI] = serverName
	return nil
}

// ChangeFile sends a textDocument/didChange notification to the appropriate server.
// If the file hasn't been opened yet, it opens it first.
func (m *ServerManager) ChangeFile(filePath, content string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	fileURI := pathToFileURL(filePath)

	// If not yet opened, open first then change
	if _, ok := m.openedFiles[fileURI]; !ok {
		server, err := m.ensureServerStartedLocked(filePath)
		if err != nil {
			return err
		}
		if server == nil {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(filePath))
		langID := "plaintext"
		if serverName := m.getServerNameForExt(ext); serverName != "" {
			if cfg, ok := m.servers[serverName]; ok {
				if lid, ok := cfg.config.ExtensionToLanguage[ext]; ok {
					langID = lid
				}
			}
		}

		if err := server.openFile(fileURI, langID, content); err != nil {
			return err
		}
		serverName := m.getServerNameForExt(ext)
		m.openedFiles[fileURI] = serverName
		return nil
	}

	server, err := m.ensureServerStartedLocked(filePath)
	if err != nil {
		return err
	}
	if server == nil {
		return nil
	}

	return server.changeFile(fileURI, content)
}

// SaveFile sends a textDocument/didSave notification to the appropriate server.
func (m *ServerManager) SaveFile(filePath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	server, err := m.ensureServerStartedLocked(filePath)
	if err != nil {
		return err
	}
	if server == nil {
		return nil
	}

	fileURI := pathToFileURL(filePath)
	return server.saveFile(fileURI)
}

// CloseFile sends a textDocument/didClose notification to the appropriate server.
func (m *ServerManager) CloseFile(filePath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	fileURI := pathToFileURL(filePath)
	delete(m.openedFiles, fileURI)

	server, err := m.ensureServerStartedLocked(filePath)
	if err != nil {
		return err
	}
	if server == nil {
		return nil
	}

	return server.closeFile(fileURI)
}

// IsFileOpen returns true if the file has been opened on an LSP server.
func (m *ServerManager) IsFileOpen(filePath string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	fileURI := pathToFileURL(filePath)
	_, ok := m.openedFiles[fileURI]
	return ok
}

// GoToDefinition routes to the appropriate server.
func (m *ServerManager) GoToDefinition(ctx context.Context, uri string, line, character int) ([]Location, error) {
	filePath := uriToFilePath(uri)
	server, err := m.ensureServerStarted(filePath)
	if err != nil {
		return nil, err
	}
	if server == nil {
		return nil, nil
	}
	return server.GoToDefinition(ctx, uri, line, character)
}

// FindReferences routes to the appropriate server.
func (m *ServerManager) FindReferences(ctx context.Context, uri string, line, character int) ([]Reference, error) {
	filePath := uriToFilePath(uri)
	server, err := m.ensureServerStarted(filePath)
	if err != nil {
		return nil, err
	}
	if server == nil {
		return nil, nil
	}
	return server.FindReferences(ctx, uri, line, character)
}

// Hover routes to the appropriate server.
func (m *ServerManager) Hover(ctx context.Context, uri string, line, character int) (*HoverResult, error) {
	filePath := uriToFilePath(uri)
	server, err := m.ensureServerStarted(filePath)
	if err != nil {
		return nil, err
	}
	if server == nil {
		return nil, nil
	}
	return server.Hover(ctx, uri, line, character)
}

// DocumentSymbol routes to the appropriate server.
func (m *ServerManager) DocumentSymbol(ctx context.Context, uri string) ([]DocumentSymbol, error) {
	filePath := uriToFilePath(uri)
	server, err := m.ensureServerStarted(filePath)
	if err != nil {
		return nil, err
	}
	if server == nil {
		return nil, nil
	}
	return server.DocumentSymbol(ctx, uri)
}

// WorkspaceSymbol searches across all running servers, returning results from the first that responds.
func (m *ServerManager) WorkspaceSymbol(ctx context.Context, query string) ([]WorkspaceSymbol, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, server := range m.servers {
		if server.Connected() {
			symbols, err := server.WorkspaceSymbol(ctx, query)
			if err == nil && len(symbols) > 0 {
				return symbols, nil
			}
		}
	}
	return nil, nil
}

// GoToImplementation routes to the appropriate server.
func (m *ServerManager) GoToImplementation(ctx context.Context, uri string, line, character int) ([]Location, error) {
	filePath := uriToFilePath(uri)
	server, err := m.ensureServerStarted(filePath)
	if err != nil {
		return nil, err
	}
	if server == nil {
		return nil, nil
	}
	return server.GoToImplementation(ctx, uri, line, character)
}

// PrepareCallHierarchy routes to the appropriate server.
func (m *ServerManager) PrepareCallHierarchy(ctx context.Context, uri string, line, character int) ([]CallHierarchyItem, error) {
	filePath := uriToFilePath(uri)
	server, err := m.ensureServerStarted(filePath)
	if err != nil {
		return nil, err
	}
	if server == nil {
		return nil, nil
	}
	return server.PrepareCallHierarchy(ctx, uri, line, character)
}

// IncomingCalls routes to the appropriate server.
func (m *ServerManager) IncomingCalls(ctx context.Context, uri string, line, character int) ([]CallHierarchyItem, error) {
	filePath := uriToFilePath(uri)
	server, err := m.ensureServerStarted(filePath)
	if err != nil {
		return nil, err
	}
	if server == nil {
		return nil, nil
	}
	return server.IncomingCalls(ctx, uri, line, character)
}

// OutgoingCalls routes to the appropriate server.
func (m *ServerManager) OutgoingCalls(ctx context.Context, uri string, line, character int) ([]CallHierarchyItem, error) {
	filePath := uriToFilePath(uri)
	server, err := m.ensureServerStarted(filePath)
	if err != nil {
		return nil, err
	}
	if server == nil {
		return nil, nil
	}
	return server.OutgoingCalls(ctx, uri, line, character)
}

// Shutdown stops all running servers.
func (m *ServerManager) Shutdown() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, server := range m.servers {
		_ = server.Shutdown()
	}

	m.servers = make(map[string]*RealClient)
	m.extensionMap = make(map[string][]string)
	m.openedFiles = make(map[string]string)
	m.initialized = false
}

func (m *ServerManager) ensureServerStarted(filePath string) (*RealClient, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.ensureServerStartedLocked(filePath)
}

func (m *ServerManager) ensureServerStartedLocked(filePath string) (*RealClient, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	serverName := m.getServerNameForExt(ext)
	if serverName == "" {
		return nil, nil
	}

	server, ok := m.servers[serverName]
	if !ok {
		return nil, nil
	}

	if !server.Connected() {
		if err := server.Start(); err != nil {
			return nil, fmt.Errorf("starting LSP server %s: %w", serverName, err)
		}
	}

	return server, nil
}

func (m *ServerManager) getServerNameForExt(ext string) string {
	names := m.extensionMap[ext]
	if len(names) == 0 {
		return ""
	}
	return names[0]
}

// pathToFileURL converts a file path to a file:// URI.
func pathToFileURL(path string) string {
	absPath, err := filepath.Abs(path)
	if err != nil {
		absPath = path
	}

	// On Windows, convert backslashes to forward slashes
	absPath = filepath.ToSlash(absPath)

	u := url.URL{
		Scheme: "file",
		Path:   absPath,
	}
	return u.String()
}

// Ensure pathToFileURL matches os.CreateTemp expectation
var _ = os.CreateTemp
