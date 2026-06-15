// Package mcp provides MCP server configuration loading and management.
package mcp

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ipy/jenny/internal/constants"
	"github.com/ipy/jenny/internal/log"
	"github.com/ipy/jenny/internal/toolresult"
)

// Client represents an MCP client connection to a server.
type Client struct {
	Name      string // Original server name
	cmd       string
	args      []string
	env       map[string]string
	proc      *proc
	transport Transport // HTTP transport (nil for stdio)
	mu        sync.Mutex
}

// proc holds the process handles for a subprocess transport.
type proc struct {
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	stdout   io.ReadCloser
	stdoutRd *bufio.Reader
	cleanFn  func()
}

var (
	// clients is the registry of active MCP clients keyed by normalized server name.
	clients   = make(map[string]*Client)
	clientsMu sync.RWMutex

	// jsonID is used to generate unique JSON-RPC IDs.
	jsonID   int64
	jsonIDMu sync.Mutex
)

func nextJSONID() int64 {
	jsonIDMu.Lock()
	jsonID++
	id := jsonID
	jsonIDMu.Unlock()
	return id
}

// jsonRPCRequest represents a JSON-RPC request.
type jsonRPCRequest struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      any            `json:"id"`
	Method  string         `json:"method"`
	Params  map[string]any `json:"params,omitempty"`
}

// jsonRPCResponse represents a JSON-RPC response.
type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

// jsonRPCError represents a JSON-RPC error.
type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// initializeResult represents the result of an initialize call.
type initializeResult struct {
	ProtocolVersion string `json:"protocolVersion"`
	Capabilities    struct {
		Roots struct {
			Listen bool `json:"listen"`
		} `json:"roots"`
		Tools     any `json:"tools"`
		Resources any `json:"resources"`
	} `json:"capabilities"`
	ServerInfo struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"serverInfo"`
}

// toolInfo represents a tool from the tools/list response.
type toolInfo struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// toolsListResult represents the result of a tools/list call.
type toolsListResult struct {
	Tools      []toolInfo `json:"tools"`
	NextCursor string     `json:"nextCursor,omitempty"`
}

// resourceEntry represents a resource entry from the resources/list response.
type resourceEntry struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	MimeType    string `json:"mimeType,omitempty"`
	Description string `json:"description,omitempty"`
}

// resourcesListResult represents the result of a resources/list call.
type resourcesListResult struct {
	Resources  []resourceEntry `json:"resources"`
	NextCursor string          `json:"nextCursor,omitempty"`
}

// MCPResource represents a resource exposed by an MCP server.
type MCPResource struct {
	URI         string
	Name        string
	MimeType    string
	Description string
}

// toolsCallResult represents the result of a tools/call call.
type toolsCallResult struct {
	Content []contentPart `json:"content"`
}

// contentPart represents a content part in a tool result.
type contentPart struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	Mime string `json:"mimeType,omitempty"`
	Data string `json:"data,omitempty"`
}

// resourceContent represents a content item in a resources/read response.
type resourceContent struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	Blob     string `json:"blob,omitempty"`
}

// resourcesReadResult represents the result of a resources/read call.
type resourcesReadResult struct {
	Contents []resourceContent `json:"contents"`
}

// ResourceContent represents a resource's content returned by ReadResource.
type ResourceContent struct {
	Type     string
	Text     string
	MimeType string
	Blob     []byte
}

// MCPTool implements tool.Tool for an MCP tool.
type MCPTool struct {
	serverName  string
	toolName    string
	inputSchema map[string]any
}

// Name returns the tool name with MCP prefix.
func (t *MCPTool) Name() string {
	return fmt.Sprintf("mcp__%s__%s", NormalizeName(t.serverName), NormalizeName(t.toolName))
}

// Description returns a description of the tool.
func (t *MCPTool) Description() string {
	return fmt.Sprintf("MCP tool %s from server %s", t.toolName, t.serverName)
}

// InputSchema returns the JSON schema for tool input.
func (t *MCPTool) InputSchema() map[string]any {
	return t.inputSchema
}

// Execute runs the tool with the given input and returns the result.
func (t *MCPTool) Execute(ctx context.Context, input map[string]any, cwd string) (*toolresult.ToolResult, error) {
	client := GetClient(t.serverName)
	if client == nil {
		return &toolresult.ToolResult{
			Content: fmt.Sprintf("Error: MCP server '%s' not found", t.serverName),
			IsError: true,
		}, nil
	}

	result, err := client.CallTool(t.toolName, input)
	if err != nil {
		return &toolresult.ToolResult{
			Content: fmt.Sprintf("Error calling MCP tool: %v", err),
			IsError: true,
		}, nil
	}

	return &toolresult.ToolResult{
		Content: result,
		IsError: false,
	}, nil
}

// NormalizeName normalizes a name for use in tool naming.
// Lowercase, non-alphanumeric characters become underscores, repeats collapsed.
// If the input produces an empty result, returns "unnamed" as a safe fallback.
func NormalizeName(name string) string {
	result := strings.ToLower(name)

	var sb strings.Builder
	prevUnderscore := false
	for _, r := range result {
		if ('a' <= r && r <= 'z') || ('0' <= r && r <= '9') || r == '_' {
			if r == '_' {
				if !prevUnderscore {
					sb.WriteRune(r)
					prevUnderscore = true
				}
			} else {
				sb.WriteRune(r)
				prevUnderscore = false
			}
		} else {
			if !prevUnderscore {
				sb.WriteRune('_')
				prevUnderscore = true
			}
		}
	}

	normalized := strings.Trim(sb.String(), "_")
	if normalized == "" {
		return "unnamed"
	}
	return normalized
}

// NewClient creates a new MCP client for the given server (stdio transport).
func NewClient(name string, cmd string, args []string, env map[string]string) *Client {
	return &Client{
		Name: name,
		cmd:  cmd,
		args: args,
		env:  env,
	}
}

// NewHTTPClient creates a new MCP client for the given server (HTTP transport).
func NewHTTPClient(name string, url string, headers map[string]string) *Client {
	return &Client{
		Name:      name,
		transport: NewHTTPTransport(url, headers),
	}
}

// Connect establishes a connection to the MCP server.
// For HTTP transport, performs the initialization handshake over HTTP.
// For stdio transport, spawns the subprocess and initializes.
func (c *Client) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// HTTP transport path
	if c.transport != nil {
		return c.initializeViaTransport(ctx)
	}

	// Stdio transport path
	if c.proc != nil {
		return nil // Already connected
	}

	cmd := exec.CommandContext(ctx, c.cmd, c.args...)
	cmd.Stderr = os.Stderr

	// Set environment
	if c.env != nil {
		cmd.Env = os.Environ()
		for k, v := range c.env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}

	// Set up pipes
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("creating stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return fmt.Errorf("creating stdout pipe: %w", err)
	}

	// Start the process
	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		return fmt.Errorf("starting MCP server process: %w", err)
	}

	c.proc = &proc{
		cmd:      cmd,
		stdin:    stdin,
		stdout:   stdout,
		stdoutRd: bufio.NewReader(stdout),
		cleanFn: func() {
			cmd.Process.Kill()
			cmd.Wait()
		},
	}

	// Perform initialization
	if err := c.initialize(ctx); err != nil {
		c.cleanup()
		return err
	}

	return nil
}

// initializeViaTransport performs the MCP handshake using the HTTP transport.
// Caller must hold c.mu.
func (c *Client) initializeViaTransport(ctx context.Context) error {
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      nextJSONID(),
		Method:  "initialize",
		Params: map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    "jenny",
				"version": "0.1.0",
			},
		},
	}

	resp, err := c.transport.SendRequest(ctx, req)
	if err != nil {
		return fmt.Errorf("initialize request failed: %w", err)
	}

	if resp.Error != nil {
		return fmt.Errorf("initialize error: %s (code %d)", resp.Error.Message, resp.Error.Code)
	}

	var initResult initializeResult
	if err := json.Unmarshal(resp.Result, &initResult); err != nil {
		return fmt.Errorf("parsing initialize result: %w", err)
	}

	log.Debug("MCP server initialized (HTTP)",
		"server", c.Name,
		"protocolVersion", initResult.ProtocolVersion,
		"serverInfo", initResult.ServerInfo.Name+"@"+initResult.ServerInfo.Version,
	)

	// Send notifications/initialized
	notif := jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	}
	_ = c.transport.SendNotification(ctx, notif)

	return nil
}

// initialize performs the MCP handshake.
func (c *Client) initialize(ctx context.Context) error {
	// Send initialize request
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      nextJSONID(),
		Method:  "initialize",
		Params: map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    "jenny",
				"version": "0.1.0",
			},
		},
	}

	resp, err := c.sendRequest(ctx, req)
	if err != nil {
		return fmt.Errorf("initialize request failed: %w", err)
	}

	if resp.Error != nil {
		return fmt.Errorf("initialize error: %s (code %d)", resp.Error.Message, resp.Error.Code)
	}

	// Parse the result to get server capabilities
	var initResult initializeResult
	if err := json.Unmarshal(resp.Result, &initResult); err != nil {
		return fmt.Errorf("parsing initialize result: %w", err)
	}

	log.Debug("MCP server initialized",
		"server", c.Name,
		"protocolVersion", initResult.ProtocolVersion,
		"serverInfo", initResult.ServerInfo.Name+"@"+initResult.ServerInfo.Version,
	)

	// Send notifications/initialized (notification, no response expected)
	notif := jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	}
	_ = c.sendNotification(notif)

	return nil
}

// ListTools discovers tools from the MCP server.
// Handles cursor-based pagination to collect all tools.
func (c *Client) ListTools(ctx context.Context) ([]MCPTool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.proc == nil && c.transport == nil {
		return nil, fmt.Errorf("not connected")
	}

	var allTools []MCPTool
	var cursor string

	for {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}

		req := jsonRPCRequest{
			JSONRPC: "2.0",
			ID:      nextJSONID(),
			Method:  "tools/list",
		}
		if len(params) > 0 {
			req.Params = params
		}

		resp, err := c.doRequest(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("tools/list request failed: %w", err)
		}

		if resp.Error != nil {
			return nil, fmt.Errorf("tools/list error: %s (code %d)", resp.Error.Message, resp.Error.Code)
		}

		var listResult toolsListResult
		if err := json.Unmarshal(resp.Result, &listResult); err != nil {
			return nil, fmt.Errorf("parsing tools/list result: %w", err)
		}

		for _, t := range listResult.Tools {
			var inputSchema map[string]any
			if err := json.Unmarshal(t.InputSchema, &inputSchema); err != nil {
				log.Warn("failed to parse tool input schema", "tool", t.Name, "error", err)
				inputSchema = map[string]any{"type": "object"}
			}
			allTools = append(allTools, MCPTool{
				serverName:  c.Name,
				toolName:    t.Name,
				inputSchema: inputSchema,
			})
			log.Debug("discovered MCP tool", "server", c.Name, "tool", t.Name)
		}

		if listResult.NextCursor == "" {
			break
		}
		cursor = listResult.NextCursor
	}

	return allTools, nil
}

// ListResources discovers resources from the MCP server.
// Handles cursor-based pagination to collect all resources.
func (c *Client) ListResources(ctx context.Context) ([]MCPResource, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.proc == nil && c.transport == nil {
		return nil, fmt.Errorf("not connected")
	}

	var allResources []MCPResource
	var cursor string

	for {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}

		req := jsonRPCRequest{
			JSONRPC: "2.0",
			ID:      nextJSONID(),
			Method:  "resources/list",
		}
		if len(params) > 0 {
			req.Params = params
		}

		resp, err := c.doRequest(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("resources/list request failed: %w", err)
		}

		if resp.Error != nil {
			return nil, fmt.Errorf("resources/list error: %s (code %d)", resp.Error.Message, resp.Error.Code)
		}

		var listResult resourcesListResult
		if err := json.Unmarshal(resp.Result, &listResult); err != nil {
			return nil, fmt.Errorf("parsing resources/list result: %w", err)
		}

		for _, r := range listResult.Resources {
			allResources = append(allResources, MCPResource{
				URI:         r.URI,
				Name:        r.Name,
				MimeType:    r.MimeType,
				Description: r.Description,
			})
			log.Debug("discovered MCP resource", "server", c.Name, "resource", r.Name, "uri", r.URI)
		}

		if listResult.NextCursor == "" {
			break
		}
		cursor = listResult.NextCursor
	}

	return allResources, nil
}

// ReadResource reads a single resource content by URI.
func (c *Client) ReadResource(ctx context.Context, uri string) ([]ResourceContent, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.proc == nil && c.transport == nil {
		return nil, fmt.Errorf("not connected to MCP server %s", c.Name)
	}

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      nextJSONID(),
		Method:  "resources/read",
		Params: map[string]any{
			"uri": uri,
		},
	}

	resp, err := c.doRequest(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("resources/read request failed: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("resources/read error: %s (code %d)", resp.Error.Message, resp.Error.Code)
	}

	var readResult resourcesReadResult
	if err := json.Unmarshal(resp.Result, &readResult); err != nil {
		return nil, fmt.Errorf("parsing resources/read result: %w", err)
	}

	contents := make([]ResourceContent, 0, len(readResult.Contents))
	for _, r := range readResult.Contents {
		var blobData []byte
		if r.Blob != "" {
			var err error
			blobData, err = base64.StdEncoding.DecodeString(r.Blob)
			if err != nil {
				return nil, fmt.Errorf("decoding blob data: %w", err)
			}
		}
		contents = append(contents, ResourceContent{
			Type:     r.Type,
			Text:     r.Text,
			MimeType: r.MimeType,
			Blob:     blobData,
		})
	}

	return contents, nil
}

// maxMCPOutputChars is the maximum character count for MCP tool results.
// Approximate token count: 1 token ≈ 4 chars, so 25000 tokens ≈ 100000 chars.
// Configurable via MCP_MAX_OUTPUT_CHARS env var.
const defaultMaxMCPOutputChars = 100000

func maxMCPOutputChars() int {
	if v := os.Getenv("MCP_MAX_OUTPUT_CHARS"); v != "" {
		if n, err := fmt.Sscanf(v, "%d", new(int)); n == 1 && err == nil {
			var val int
			fmt.Sscanf(v, "%d", &val)
			if val > 0 {
				return val
			}
		}
	}
	return defaultMaxMCPOutputChars
}

// CallTool calls a tool on the MCP server.
func (c *Client) CallTool(name string, arguments map[string]any) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.proc == nil && c.transport == nil {
		return "", fmt.Errorf("not connected to MCP server %s", c.Name)
	}

	ctx := context.Background()
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      nextJSONID(),
		Method:  "tools/call",
		Params: map[string]any{
			"name":      name,
			"arguments": arguments,
		},
	}

	resp, err := c.doRequest(ctx, req)
	if err != nil {
		return "", fmt.Errorf("tools/call request failed: %w", err)
	}

	if resp.Error != nil {
		return "", fmt.Errorf("tools/call error: %s (code %d)", resp.Error.Message, resp.Error.Code)
	}

	var callResult toolsCallResult
	if err := json.Unmarshal(resp.Result, &callResult); err != nil {
		return "", fmt.Errorf("parsing tools/call result: %w", err)
	}

	var result strings.Builder
	for _, part := range callResult.Content {
		switch part.Type {
		case "text":
			result.WriteString(part.Text)
		case "image", "blob":
			if part.Data != "" {
				savedPath, err := persistBinaryToolResult(part.Data, part.Mime)
				if err != nil {
					fmt.Fprintf(&result, "[binary %s: failed to save: %v]", part.Type, err)
				} else {
					fmt.Fprintf(&result, "[%s saved to: %s]", part.Type, savedPath)
				}
			} else {
				fmt.Fprintf(&result, "[%s: %s]", part.Type, part.Text)
			}
		default:
			if part.Text != "" {
				fmt.Fprintf(&result, "[%s: %s]", part.Type, part.Text)
			}
		}
	}

	output := result.String()
	maxChars := maxMCPOutputChars()
	if len(output) > maxChars {
		truncated := output[:maxChars]
		output = truncated + fmt.Sprintf("\n\n[Content truncated: original %d chars, showing first %d chars]", len(output), maxChars)
	}

	return output, nil
}

// persistBinaryToolResult decodes base64 data and writes to disk.
func persistBinaryToolResult(data string, mimeType string) (string, error) {
	decoded, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return "", fmt.Errorf("decoding base64: %w", err)
	}

	ext := ".bin"
	switch {
	case strings.HasPrefix(mimeType, "image/png"):
		ext = ".png"
	case strings.HasPrefix(mimeType, "image/jpeg"):
		ext = ".jpg"
	case strings.HasPrefix(mimeType, "image/gif"):
		ext = ".gif"
	case strings.HasPrefix(mimeType, "image/webp"):
		ext = ".webp"
	case strings.HasPrefix(mimeType, "application/pdf"):
		ext = ".pdf"
	}

	var b [8]byte
	rand.Read(b[:])
	filename := fmt.Sprintf("%d-%x%s", time.Now().UnixNano(), b, ext)

	dir := filepath.Join(constants.JennyHomeDir(), "mcp-tool-output")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("creating output directory: %w", err)
	}

	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, decoded, 0644); err != nil {
		return "", fmt.Errorf("writing file: %w", err)
	}

	return path, nil
}

// doRequest routes the request to the appropriate transport.
// For HTTP transport, handles session expiration by re-initializing once.
// Caller must hold c.mu.
func (c *Client) doRequest(ctx context.Context, req jsonRPCRequest) (*jsonRPCResponse, error) {
	if c.transport != nil {
		resp, err := c.transport.SendRequest(ctx, req)
		if err != nil && IsSessionExpired(err) {
			// AC6: re-initialize on session expiry, then retry once
			if reinitErr := c.initializeViaTransport(ctx); reinitErr != nil {
				return nil, fmt.Errorf("re-initialization after session expiry failed: %w", reinitErr)
			}
			return c.transport.SendRequest(ctx, req)
		}
		return resp, err
	}
	return c.sendRequestStdio(ctx, req)
}

// sendRequestStdio sends a JSON-RPC request via stdio and waits for a response.
// Caller must hold c.mu.
func (c *Client) sendRequestStdio(_ context.Context, req jsonRPCRequest) (*jsonRPCResponse, error) {
	if c.proc == nil || c.proc.stdin == nil || c.proc.stdout == nil {
		return nil, fmt.Errorf("not connected")
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	if _, err := c.proc.stdin.Write(append(data, '\n')); err != nil {
		return nil, fmt.Errorf("writing request: %w", err)
	}

	line, err := c.proc.stdoutRd.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	var resp jsonRPCResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	return &resp, nil
}

// sendRequest is kept for backward compatibility with the initialize method (stdio path).
// Caller must hold c.mu.
func (c *Client) sendRequest(ctx context.Context, req jsonRPCRequest) (*jsonRPCResponse, error) {
	return c.sendRequestStdio(ctx, req)
}

// sendNotification sends a JSON-RPC notification (no response expected) via stdio.
// Caller must hold c.mu.
func (c *Client) sendNotification(notif jsonRPCRequest) error {
	if c.proc == nil || c.proc.stdin == nil {
		return fmt.Errorf("not connected")
	}

	data, err := json.Marshal(notif)
	if err != nil {
		return fmt.Errorf("marshaling notification: %w", err)
	}

	_, err = c.proc.stdin.Write(append(data, '\n'))
	return err
}

// cleanup shuts down the transport and/or process.
func (c *Client) cleanup() {
	if c.transport != nil {
		c.transport.Close()
		c.transport = nil
	}

	if c.proc == nil {
		return
	}

	if c.proc.stdin != nil {
		notif := jsonRPCRequest{
			JSONRPC: "2.0",
			Method:  "notifications/shutdown",
		}
		data, _ := json.Marshal(notif)
		c.proc.stdin.Write(append(data, '\n'))
		c.proc.stdin.Close()
	}

	if c.proc.cleanFn != nil {
		c.proc.cleanFn()
	}

	c.proc = nil
}

// Disconnect disconnects from the MCP server.
func (c *Client) Disconnect() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cleanup()
	bumpCacheGen()
}

// GetClient returns the client for a given normalized server name.
func GetClient(serverName string) *Client {
	clientsMu.RLock()
	defer clientsMu.RUnlock()
	return clients[NormalizeName(serverName)]
}

// ConnectAll connects to all MCP servers in the configuration.
// Servers with a `command` field use stdio transport.
// Servers with a `url` field (and no command) use HTTP transport.
// Servers with neither are skipped.
func ConnectAll(cfg map[string]MCPServerDef) error {
	clientsMu.Lock()
	defer clientsMu.Unlock()

	for name, def := range cfg {
		var client *Client

		switch {
		case def.Command != "":
			client = NewClient(name, def.Command, def.Args, def.Env)
		case def.URL != "":
			client = NewHTTPClient(name, def.URL, def.Headers)
		default:
			continue
		}

		clients[NormalizeName(name)] = client

		ctx := context.Background()
		if err := client.Connect(ctx); err != nil {
			return fmt.Errorf("connecting to MCP server %q: %w", name, err)
		}
	}

	// Invalidate resource cache since client set has changed
	bumpCacheGen()

	return nil
}

// ShutdownAll disconnects all MCP clients.
func ShutdownAll() {
	clientsMu.Lock()
	defer clientsMu.Unlock()

	for _, client := range clients {
		client.Disconnect()
	}
	clients = make(map[string]*Client)
	bumpCacheGen()
}

// GetTools returns all discovered MCP tools from all connected servers.
func GetTools() []any {
	clientsMu.RLock()
	defer clientsMu.RUnlock()

	var allTools []any
	for _, client := range clients {
		ctx := context.Background()
		tools, err := client.ListTools(ctx)
		if err != nil {
			log.Warn("failed to list tools", "server", client.Name, "error", err)
			continue
		}
		for i := range tools {
			allTools = append(allTools, &tools[i])
		}
	}

	return allTools
}

// GetMCPClients returns a copy of the map of normalized server names to clients.
func GetMCPClients() map[string]*Client {
	clientsMu.RLock()
	defer clientsMu.RUnlock()

	result := make(map[string]*Client, len(clients))
	maps.Copy(result, clients)
	return result
}

// SetTestClient registers a client for testing (only for testing).
func SetTestClient(name string, client *Client) {
	clientsMu.Lock()
	defer clientsMu.Unlock()
	clients[NormalizeName(name)] = client
}

// ResetTestClients clears all test clients.
func ResetTestClients() {
	clientsMu.Lock()
	defer clientsMu.Unlock()
	clients = make(map[string]*Client)
}
