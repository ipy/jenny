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
	Name   string // Original server name
	cmd    string
	args   []string
	env    map[string]string
	proc   *proc
	mu     sync.Mutex
	icons  map[string]string // Icons from serverInfo (set after initialization)

	respChans map[string]chan *jsonRPCResponse
	muResp    sync.Mutex

	notifHandlers []func(Notification)
	muNotif       sync.Mutex

	done chan struct{}

	// transport is HTTP transport (nil for stdio)
	transport Transport

	// bgErr captures errors from BackgroundListen SSE connection
	bgErr error
	muBg  sync.Mutex
}

// Err returns the error from the background SSE connection, if any.
func (c *Client) Err() error {
	c.muBg.Lock()
	defer c.muBg.Unlock()
	return c.bgErr
}

// Notification represents a JSON-RPC notification.
type Notification struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
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
	ID      any            `json:"id,omitempty"`
	Method  string         `json:"method"`
	Params  map[string]any `json:"params,omitempty"`
	Meta    map[string]any `json:"_meta,omitempty"`
}

// ProgressParams represents the parameters for a notifications/progress notification.
type ProgressParams struct {
	ProgressToken any     `json:"progressToken"`
	Progress      float64 `json:"progress"`
	Total         float64 `json:"total,omitempty"`
}

// ResourceUpdatedParams represents the parameters for a notifications/resources/updated notification.
type ResourceUpdatedParams struct {
	URI string `json:"uri"`
}

// jsonRPCResponse represents a JSON-RPC response.
type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
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
		Name    string            `json:"name"`
		Version string            `json:"version"`
		Icons   map[string]string `json:"icons,omitempty"`
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

// promptInfo represents a prompt from the prompts/list response.
type promptInfo struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Arguments   []promptArgInfo `json:"arguments,omitempty"`
}

// promptArgInfo represents a prompt argument.
type promptArgInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// promptsListResult represents the result of a prompts/list call.
type promptsListResult struct {
	Prompts    []promptInfo `json:"prompts"`
	NextCursor string       `json:"nextCursor,omitempty"`
}

// promptGetResult represents the result of a prompts/get call.
type promptGetResult struct {
	Description string          `json:"description,omitempty"`
	Messages    []promptMessage `json:"messages"`
}

// promptMessage represents a message in a prompt.
type promptMessage struct {
	Role    string        `json:"role"`
	Content promptContent `json:"content"`
}

// promptContent represents the content of a prompt message.
type promptContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// resourceTemplateEntry represents a resource template from the resources/templates/list response.
type resourceTemplateEntry struct {
	URITemplate string `json:"uriTemplate"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

// resourcesTemplatesListResult represents the result of a resources/templates/list call.
type resourcesTemplatesListResult struct {
	ResourceTemplates []resourceTemplateEntry `json:"resourceTemplates"`
	NextCursor        string                  `json:"nextCursor,omitempty"`
}

// MCPPrompt represents a prompt exposed by an MCP server.
type MCPPrompt struct {
	Name        string
	Description string
	Arguments   []MCPPromptArg
	ServerName  string
}

// TaskTemplate represents a task template declared by the MCP server.
type TaskTemplate struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	InputSchema any    `json:"inputSchema,omitempty"`
}

// TaskInfo represents a task instance and its status.
type TaskInfo struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"` // pending, running, completed, failed, cancelled
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

// MCPPromptArg represents an argument for an MCP prompt.
type MCPPromptArg struct {
	Name        string
	Description string
	Required    bool
}

// MCPResourceTemplate represents a resource template exposed by an MCP server.
type MCPResourceTemplate struct {
	URITemplate string
	Name        string
	Description string
	MimeType    string
	ServerName  string
}

// MCPResource represents a resource exposed by an MCP server.
type MCPResource struct {
	URI         string
	Name        string
	MimeType    string
	Description string
}

// ListPrompts discovers prompts from the MCP server.
func (c *Client) ListPrompts(ctx context.Context) ([]MCPPrompt, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.proc == nil && c.transport == nil {
		return nil, fmt.Errorf("not connected")
	}

	var allPrompts []MCPPrompt
	var cursor string

	for {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}

		req := jsonRPCRequest{
			JSONRPC: "2.0",
			ID:      nextJSONID(),
			Method:  "prompts/list",
		}
		if len(params) > 0 {
			req.Params = params
		}

		resp, err := c.doRequest(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("prompts/list request failed: %w", err)
		}

		if resp.Error != nil {
			return nil, fmt.Errorf("prompts/list error: %s (code %d)", resp.Error.Message, resp.Error.Code)
		}

		var listResult promptsListResult
		if err := json.Unmarshal(resp.Result, &listResult); err != nil {
			return nil, fmt.Errorf("parsing prompts/list result: %w", err)
		}

		for _, p := range listResult.Prompts {
			args := make([]MCPPromptArg, 0, len(p.Arguments))
			for _, a := range p.Arguments {
				args = append(args, MCPPromptArg{
					Name:        a.Name,
					Description: a.Description,
					Required:    a.Required,
				})
			}
			allPrompts = append(allPrompts, MCPPrompt{
				Name:        p.Name,
				Description: p.Description,
				Arguments:   args,
				ServerName:  c.Name,
			})
			log.Debug("discovered MCP prompt", "server", c.Name, "prompt", p.Name)
		}

		if listResult.NextCursor == "" {
			break
		}
		cursor = listResult.NextCursor
	}

	return allPrompts, nil
}

// GetPrompt retrieves a prompt by name with arguments.
func (c *Client) GetPrompt(ctx context.Context, name string, arguments map[string]any) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.proc == nil && c.transport == nil {
		return "", fmt.Errorf("not connected to MCP server %s", c.Name)
	}

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      nextJSONID(),
		Method:  "prompts/get",
		Params: map[string]any{
			"name":      name,
			"arguments": arguments,
		},
	}

	resp, err := c.doRequest(ctx, req)
	if err != nil {
		return "", fmt.Errorf("prompts/get request failed: %w", err)
	}

	if resp.Error != nil {
		return "", fmt.Errorf("prompts/get error: %s (code %d)", resp.Error.Message, resp.Error.Code)
	}

	var getResult promptGetResult
	if err := json.Unmarshal(resp.Result, &getResult); err != nil {
		return "", fmt.Errorf("parsing prompts/get result: %w", err)
	}

	var sb strings.Builder
	for _, msg := range getResult.Messages {
		if msg.Content.Text != "" {
			if sb.Len() > 0 {
				sb.WriteString("\n\n")
			}
			fmt.Fprintf(&sb, "[%s]: %s", msg.Role, msg.Content.Text)
		}
	}

	return sb.String(), nil
}

// ListResourceTemplates discovers resource templates from the MCP server.
func (c *Client) ListResourceTemplates(ctx context.Context) ([]MCPResourceTemplate, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.proc == nil && c.transport == nil {
		return nil, fmt.Errorf("not connected")
	}

	var allTemplates []MCPResourceTemplate
	var cursor string

	for {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}

		req := jsonRPCRequest{
			JSONRPC: "2.0",
			ID:      nextJSONID(),
			Method:  "resources/templates/list",
		}
		if len(params) > 0 {
			req.Params = params
		}

		resp, err := c.doRequest(ctx, req)
		if err != nil {
			// Some servers might not support templates, gracefully handle as empty
			if strings.Contains(err.Error(), "Method not found") {
				return nil, nil
			}
			return nil, fmt.Errorf("resources/templates/list request failed: %w", err)
		}

		if resp.Error != nil {
			if resp.Error.Code == -32601 { // Method not found
				return nil, nil
			}
			return nil, fmt.Errorf("resources/templates/list error: %s (code %d)", resp.Error.Message, resp.Error.Code)
		}

		var listResult resourcesTemplatesListResult
		if err := json.Unmarshal(resp.Result, &listResult); err != nil {
			return nil, fmt.Errorf("parsing resources/templates/list result: %w", err)
		}

		for _, t := range listResult.ResourceTemplates {
			allTemplates = append(allTemplates, MCPResourceTemplate{
				URITemplate: t.URITemplate,
				Name:        t.Name,
				Description: t.Description,
				MimeType:    t.MimeType,
				ServerName:  c.Name,
			})
			log.Debug("discovered MCP resource template", "server", c.Name, "template", t.Name, "uriTemplate", t.URITemplate)
		}

		if listResult.NextCursor == "" {
			break
		}
		cursor = listResult.NextCursor
	}

	return allTemplates, nil
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

	result, err := client.CallTool(ctx, t.toolName, input)
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
		Name:      name,
		cmd:       cmd,
		args:      args,
		env:       env,
		respChans: make(map[string]chan *jsonRPCResponse),
		done:      make(chan struct{}),
	}
}

// NewHTTPClient creates a new MCP client for the given server (HTTP transport).
func NewHTTPClient(name string, url string, headers map[string]string) *Client {
	return &Client{
		Name:      name,
		transport: NewHTTPTransport(url, headers),
		respChans: make(map[string]chan *jsonRPCResponse),
		done:      make(chan struct{}),
	}
}

// NewHTTPClientWithOAuth creates a new MCP client for the given server (HTTP transport)
// with OAuth 2.1 token refresh configuration.
func NewHTTPClientWithOAuth(name string, url string, headers map[string]string, tokenEndpoint, clientID, clientSecret string) *Client {
	transport := NewHTTPTransport(url, headers)
	transport.SetOAuthConfig(tokenEndpoint, clientID, clientSecret)
	return &Client{
		Name:      name,
		transport: transport,
		respChans: make(map[string]chan *jsonRPCResponse),
		done:      make(chan struct{}),
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
		c.transport.SetNotificationHandler(c.handleNotification)
		// Start background listener for SSE notifications
		go func() {
			if err := c.transport.BackgroundListen(context.Background()); err != nil {
				c.muBg.Lock()
				c.bgErr = err
				c.muBg.Unlock()
				log.Debug("MCP HTTP background listener failed", "server", c.Name, "error", err)
			}
		}()
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

	// Start background reader loop for stdio
	go c.loop()

	// Perform initialization
	if err := c.initialize(ctx); err != nil {
		c.cleanup()
		return err
	}

	return nil
}

type progressTokenKey struct{}

// WithProgressToken returns a context with the given progress token.
func WithProgressToken(ctx context.Context, token any) context.Context {
	return context.WithValue(ctx, progressTokenKey{}, token)
}

// GetProgressToken retrieves the progress token from the context.
func GetProgressToken(ctx context.Context) any {
	return ctx.Value(progressTokenKey{})
}

// ProgressHandler is a function that receives progress updates.
type ProgressHandler func(token any, progress, total float64)

var (
	progressHandlers   = make(map[uint64]ProgressHandler)
	progressHandlersMu sync.Mutex
	progressHandlerID  uint64
)

// RegisterProgressHandler registers a global handler for progress notifications.
// Returns an unregister function that removes the handler when called.
func RegisterProgressHandler(h ProgressHandler) func() {
	progressHandlersMu.Lock()
	defer progressHandlersMu.Unlock()
	progressHandlerID++
	id := progressHandlerID
	progressHandlers[id] = h
	return func() {
		progressHandlersMu.Lock()
		delete(progressHandlers, id)
		progressHandlersMu.Unlock()
	}
}

// LogMessageParams represents the parameters for a notifications/message notification.
type LogMessageParams struct {
	Level  string `json:"level"`
	Logger string `json:"logger,omitempty"`
	Data   any    `json:"data,omitempty"`
}

// handleNotification processes an incoming MCP notification.
func (c *Client) handleNotification(notif Notification) {
	// Handle specific notifications for AC1/AC2
	switch notif.Method {
	case "notifications/progress":
		var params ProgressParams
		if err := json.Unmarshal(notif.Params, &params); err == nil {
			log.Debug("MCP progress notification", "server", c.Name, "token", params.ProgressToken, "progress", params.Progress, "total", params.Total)
			progressHandlersMu.Lock()
			for _, h := range progressHandlers {
				h(params.ProgressToken, params.Progress, params.Total)
			}
			progressHandlersMu.Unlock()
		}
	case "notifications/resources/updated":
		var params ResourceUpdatedParams
		if err := json.Unmarshal(notif.Params, &params); err == nil {
			log.Info("MCP resource updated", "server", c.Name, "uri", params.URI)
			bumpCacheGen() // Invalidate resource cache on update
		}
	case "notifications/resources/list_changed":
		// AC1: Invalidate resource cache when server signals list change
		log.Debug("MCP resource list changed", "server", c.Name)
		bumpCacheGen()
	case "notifications/message":
		// AC2: Route server logging notifications to internal log system
		var params LogMessageParams
		if err := json.Unmarshal(notif.Params, &params); err == nil {
			msg := "MCP server log"
			if params.Logger != "" {
				msg = fmt.Sprintf("MCP server log [%s]", params.Logger)
			}
			switch params.Level {
			case "debug":
				log.Debug(msg, "server", c.Name, "level", params.Level, "data", params.Data)
			case "info":
				log.Info(msg, "server", c.Name, "level", params.Level, "data", params.Data)
			case "warning":
				log.Warn(msg, "server", c.Name, "level", params.Level, "data", params.Data)
			case "error":
				log.Error(msg, "server", c.Name, "level", params.Level, "data", params.Data)
			default:
				log.Debug(msg, "server", c.Name, "level", params.Level, "data", params.Data)
			}
		} else {
			log.Debug("MCP server log notification dropped: failed to unmarshal params",
				"server", c.Name, "error", err)
		}
	}

	c.muNotif.Lock()
	for _, handler := range c.notifHandlers {
		go handler(notif)
	}
	c.muNotif.Unlock()
}

// loop reads messages from the stdout of the MCP server process.
func (c *Client) loop() {
	reader := c.proc.stdoutRd
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err != io.EOF && !strings.Contains(err.Error(), "file already closed") {
				log.Error("MCP background reader loop error", "server", c.Name, "error", err)
			}
			return
		}

		// Try to parse as response or notification
		var msg struct {
			ID     any             `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			Result json.RawMessage `json:"result"`
			Error  *jsonRPCError   `json:"error"`
		}

		if err := json.Unmarshal(line, &msg); err != nil {
			log.Warn("MCP background reader failed to parse message", "server", c.Name, "line", string(line), "error", err)
			continue
		}

		if msg.ID != nil {
			// It's a response
			resp := &jsonRPCResponse{
				JSONRPC: "2.0",
				ID:      msg.ID,
				Result:  msg.Result,
				Error:   msg.Error,
			}
			c.muResp.Lock()
			ch, ok := c.respChans[fmt.Sprintf("%v", msg.ID)]
			if ok {
				ch <- resp
			}
			c.muResp.Unlock()
		} else if msg.Method != "" {
			// It's a notification
			c.handleNotification(Notification{
				Method: msg.Method,
				Params: msg.Params,
			})
		}
	}
}

// RegisterNotificationHandler registers a callback for MCP notifications.
func (c *Client) RegisterNotificationHandler(handler func(Notification)) {
	c.muNotif.Lock()
	defer c.muNotif.Unlock()
	c.notifHandlers = append(c.notifHandlers, handler)
}

// initializeViaTransport performs the MCP handshake using the HTTP transport.
// Caller must hold c.mu.
func (c *Client) initializeViaTransport(ctx context.Context) error {
	// AC3: Advertise roots and sampling capabilities per MCP spec baseline
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      nextJSONID(),
		Method:  "initialize",
		Params: map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities": map[string]any{
				"roots": map[string]any{
					"listChanged": true,
				},
				"sampling": map[string]any{},
			},
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

	// Save icons from serverInfo
	c.icons = initResult.ServerInfo.Icons

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
	// AC3: Advertise roots and sampling capabilities per MCP spec baseline
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      nextJSONID(),
		Method:  "initialize",
		Params: map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities": map[string]any{
				"roots": map[string]any{
					"listChanged": true,
				},
				"sampling": map[string]any{},
			},
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

	// Save icons from serverInfo
	c.icons = initResult.ServerInfo.Icons

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

// SubscribeResource subscribes to changes for a specific resource URI.
// This allows the client to receive notifications when the resource is updated.
func (c *Client) SubscribeResource(ctx context.Context, uri string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.proc == nil && c.transport == nil {
		return fmt.Errorf("not connected to MCP server %s", c.Name)
	}

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      nextJSONID(),
		Method:  "resources/subscribe",
		Params:  map[string]any{"uri": uri},
	}

	resp, err := c.doRequest(ctx, req)
	if err != nil {
		return fmt.Errorf("resources/subscribe request failed: %w", err)
	}
	if resp.Error != nil {
		return fmt.Errorf("resources/subscribe error: %s (code %d)", resp.Error.Message, resp.Error.Code)
	}
	return nil
}

// UnsubscribeResource unsubscribes from changes for a specific resource URI.
func (c *Client) UnsubscribeResource(ctx context.Context, uri string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.proc == nil && c.transport == nil {
		return fmt.Errorf("not connected to MCP server %s", c.Name)
	}

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      nextJSONID(),
		Method:  "resources/unsubscribe",
		Params:  map[string]any{"uri": uri},
	}

	resp, err := c.doRequest(ctx, req)
	if err != nil {
		return fmt.Errorf("resources/unsubscribe request failed: %w", err)
	}
	if resp.Error != nil {
		return fmt.Errorf("resources/unsubscribe error: %s (code %d)", resp.Error.Message, resp.Error.Code)
	}
	return nil
}

// ListTasks discovers task templates from the MCP server.
func (c *Client) ListTasks(ctx context.Context) ([]TaskTemplate, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.proc == nil && c.transport == nil {
		return nil, fmt.Errorf("not connected")
	}

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      nextJSONID(),
		Method:  "tasks/list",
	}

	resp, err := c.doRequest(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("tasks/list request failed: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("tasks/list error: %s (code %d)", resp.Error.Message, resp.Error.Code)
	}

	var result struct {
		Tasks []TaskTemplate `json:"tasks"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("parsing tasks/list result: %w", err)
	}

	return result.Tasks, nil
}

// CreateTask creates a task instance on the MCP server.
func (c *Client) CreateTask(ctx context.Context, name string, arguments map[string]any) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.proc == nil && c.transport == nil {
		return "", fmt.Errorf("not connected to MCP server %s", c.Name)
	}

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      nextJSONID(),
		Method:  "tasks/create",
		Params:  map[string]any{"name": name, "arguments": arguments},
	}

	resp, err := c.doRequest(ctx, req)
	if err != nil {
		return "", fmt.Errorf("tasks/create request failed: %w", err)
	}

	if resp.Error != nil {
		return "", fmt.Errorf("tasks/create error: %s (code %d)", resp.Error.Message, resp.Error.Code)
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return "", fmt.Errorf("parsing tasks/create result: %w", err)
	}

	return result.ID, nil
}

// GetTask queries the status of a task on the MCP server.
func (c *Client) GetTask(ctx context.Context, taskID string) (*TaskInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.proc == nil && c.transport == nil {
		return nil, fmt.Errorf("not connected to MCP server %s", c.Name)
	}

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      nextJSONID(),
		Method:  "tasks/get",
		Params:  map[string]any{"id": taskID},
	}

	resp, err := c.doRequest(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("tasks/get request failed: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("tasks/get error: %s (code %d)", resp.Error.Message, resp.Error.Code)
	}

	var task TaskInfo
	if err := json.Unmarshal(resp.Result, &task); err != nil {
		return nil, fmt.Errorf("parsing tasks/get result: %w", err)
	}

	return &task, nil
}

// CancelTask cancels a task on the MCP server.
func (c *Client) CancelTask(ctx context.Context, taskID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.proc == nil && c.transport == nil {
		return fmt.Errorf("not connected to MCP server %s", c.Name)
	}

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      nextJSONID(),
		Method:  "tasks/cancel",
		Params:  map[string]any{"id": taskID},
	}

	resp, err := c.doRequest(ctx, req)
	if err != nil {
		return fmt.Errorf("tasks/cancel request failed: %w", err)
	}

	if resp.Error != nil {
		return fmt.Errorf("tasks/cancel error: %s (code %d)", resp.Error.Message, resp.Error.Code)
	}

	return nil
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
func (c *Client) CallTool(ctx context.Context, name string, arguments map[string]any) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.proc == nil && c.transport == nil {
		return "", fmt.Errorf("not connected to MCP server %s", c.Name)
	}

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      nextJSONID(),
		Method:  "tools/call",
		Params: map[string]any{
			"name":      name,
			"arguments": arguments,
		},
	}

	// AC2: If a progress token is provided in the context, include it in _meta
	if progressToken := GetProgressToken(ctx); progressToken != nil {
		req.Meta = map[string]any{
			"progressToken": progressToken,
		}
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
func (c *Client) sendRequestStdio(ctx context.Context, req jsonRPCRequest) (*jsonRPCResponse, error) {
	if c.proc == nil || c.proc.stdin == nil || c.proc.stdout == nil {
		return nil, fmt.Errorf("not connected")
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	// Register response channel before sending request
	ch := make(chan *jsonRPCResponse, 1)
	c.muResp.Lock()
	c.respChans[fmt.Sprintf("%v", req.ID)] = ch
	c.muResp.Unlock()

	defer func() {
		c.muResp.Lock()
		delete(c.respChans, fmt.Sprintf("%v", req.ID))
		c.muResp.Unlock()
	}()

	if _, err := c.proc.stdin.Write(append(data, '\n')); err != nil {
		return nil, fmt.Errorf("writing request: %w", err)
	}

	// Wait for response or timeout/context cancellation
	select {
	case resp := <-ch:
		return resp, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
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

// GetServerIcons returns the icons metadata for a given server name.
// Returns nil if the server is not found or does not provide icons.
func GetServerIcons(serverName string) map[string]string {
	client := GetClient(serverName)
	if client == nil {
		return nil
	}
	return client.Icons()
}

// Icons returns the icons metadata from the server's initialize response.
func (c *Client) Icons() map[string]string {
	return c.icons
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
			if def.TokenEndpoint != "" {
				client = NewHTTPClientWithOAuth(name, def.URL, def.Headers, def.TokenEndpoint, def.ClientID, def.ClientSecret)
			} else {
				client = NewHTTPClient(name, def.URL, def.Headers)
			}
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

// GetPrompts returns all discovered MCP prompts from all connected servers.
func GetPrompts() []MCPPrompt {
	clientsMu.RLock()
	defer clientsMu.RUnlock()

	var allPrompts []MCPPrompt
	for _, client := range clients {
		ctx := context.Background()
		prompts, err := client.ListPrompts(ctx)
		if err != nil {
			log.Warn("failed to list prompts", "server", client.Name, "error", err)
			continue
		}
		allPrompts = append(allPrompts, prompts...)
	}

	return allPrompts
}

// GetResourceTemplates returns all discovered MCP resource templates from all connected servers.
func GetResourceTemplates() []MCPResourceTemplate {
	clientsMu.RLock()
	defer clientsMu.RUnlock()

	var allTemplates []MCPResourceTemplate
	for _, client := range clients {
		ctx := context.Background()
		templates, err := client.ListResourceTemplates(ctx)
		if err != nil {
			log.Warn("failed to list resource templates", "server", client.Name, "error", err)
			continue
		}
		allTemplates = append(allTemplates, templates...)
	}

	return allTemplates
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
