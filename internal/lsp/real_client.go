package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultRequestTimeout = 30 * time.Second
)

// jsonrpcRequest is a JSON-RPC 2.0 request.
type jsonrpcRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int64       `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// jsonrpcResponse is a JSON-RPC 2.0 response.
type jsonrpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonrpcError   `json:"error,omitempty"`
}

// jsonrpcNotification is a JSON-RPC 2.0 notification (no id).
type jsonrpcNotification struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *jsonrpcError) Error() string {
	return fmt.Sprintf("LSP error %d: %s", e.Code, e.Message)
}

// pendingCall tracks an in-flight request.
type pendingCall struct {
	response chan *jsonrpcResponse
	ctx      context.Context
}

// RealClient is a JSON-RPC LSP client that communicates with a language server
// process over stdio.
type RealClient struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader

	mu        sync.Mutex
	nextID    int64
	pending   map[int64]*pendingCall
	connected atomic.Bool

	config ServerConfig
	done   chan struct{}
}

// NewRealClient creates a new LSP client for the given server config.
// The server process is not started until the first request.
func NewRealClient(config ServerConfig) *RealClient {
	return &RealClient{
		config:  config,
		pending: make(map[int64]*pendingCall),
	}
}

// Start launches the language server process and begins reading responses.
func (c *RealClient) Start() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connected.Load() {
		return nil
	}

	cmd := exec.Command(c.config.Command, c.config.Args...)
	cmd.Env = os.Environ()
	for k, v := range c.config.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("creating stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return fmt.Errorf("creating stdout pipe: %w", err)
	}

	// Capture stderr for debugging
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		return fmt.Errorf("starting LSP server %s: %w", c.config.Command, err)
	}

	c.cmd = cmd
	c.stdin = stdin
	c.stdout = bufio.NewReaderSize(stdout, 65536)
	c.done = make(chan struct{})
	c.connected.Store(true)

	// Initialize the LSP server with an initialize request
	rootURI := c.config.WorkspaceFolder
	if rootURI == "" {
		rootURI = "file:///"
	}

	initParams := map[string]interface{}{
		"processId": os.Getpid(),
		"rootUri":   rootURI,
		"capabilities": map[string]interface{}{
			"textDocument": map[string]interface{}{
				"definition": map[string]interface{}{
					"linkSupport": true,
				},
				"references": map[string]interface{}{
					"dynamicRegistration": false,
				},
				"hover": map[string]interface{}{
					"contentFormat": []string{"plaintext", "markdown"},
				},
				"documentSymbol": map[string]interface{}{
					"hierarchicalDocumentSymbolSupport": true,
				},
				"implementation": map[string]interface{}{
					"linkSupport": true,
				},
				"callHierarchy": map[string]interface{}{
					"dynamicRegistration": false,
				},
			},
			"workspace": map[string]interface{}{
				"symbol": map[string]interface{}{
					"dynamicRegistration": false,
				},
			},
		},
	}

	result, err := c.sendRequestLocked(context.Background(), "initialize", initParams)
	if err != nil {
		c.shutdownLocked()
		return fmt.Errorf("initializing LSP server %s: %w", c.config.Command, err)
	}

	// Check capabilities
	var initResult struct {
		Capabilities struct {
			TextDocumentSync        interface{} `json:"textDocumentSync"`
			DefinitionProvider      interface{} `json:"definitionProvider"`
			ReferencesProvider      interface{} `json:"referencesProvider"`
			HoverProvider           interface{} `json:"hoverProvider"`
			DocumentSymbolProvider  interface{} `json:"documentSymbolProvider"`
			WorkspaceSymbolProvider interface{} `json:"workspaceSymbolProvider"`
			ImplementationProvider  interface{} `json:"implementationProvider"`
			CallHierarchyProvider   interface{} `json:"callHierarchyProvider"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal(result, &initResult); err != nil {
		c.shutdownLocked()
		return fmt.Errorf("parsing server capabilities: %w", err)
	}

	// Send initialized notification
	c.sendNotificationLocked("initialized", map[string]interface{}{})

	go c.readLoop()

	return nil
}

// Connected returns true if the server process is running.
func (c *RealClient) Connected() bool {
	return c.connected.Load()
}

// Shutdown gracefully stops the language server.
func (c *RealClient) Shutdown() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.shutdownLocked()
}

func (c *RealClient) shutdownLocked() error {
	if !c.connected.Load() {
		return nil
	}

	c.connected.Store(false)

	// Try graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Fire-and-forget shutdown request
	_, _ = c.sendRequestLocked(ctx, "shutdown", nil)

	// Send exit notification
	c.sendNotificationLocked("exit", nil)

	// Close stdin to signal EOF
	if c.stdin != nil {
		c.stdin.Close()
	}

	// Wait for process
	if c.cmd != nil {
		_ = c.cmd.Wait()
	}

	// Clean up pending calls
	for id, pc := range c.pending {
		select {
		case pc.response <- &jsonrpcResponse{
			JSONRPC: "2.0",
			ID:      id,
			Error:   &jsonrpcError{Code: -32000, Message: "server shutting down"},
		}:
		default:
		}
	}
	c.pending = make(map[int64]*pendingCall)

	close(c.done)
	return nil
}

// sendRequest sends a JSON-RPC request and waits for the response.
func (c *RealClient) sendRequest(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected.Load() {
		return nil, errors.New("LSP server not connected")
	}

	return c.sendRequestLocked(ctx, method, params)
}

func (c *RealClient) sendRequestLocked(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	id := atomic.AddInt64(&c.nextID, 1)

	req := jsonrpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	pc := &pendingCall{
		response: make(chan *jsonrpcResponse, 1),
		ctx:      ctx,
	}
	c.pending[id] = pc

	// Write header + content
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
	if _, err := c.stdin.Write([]byte(header)); err != nil {
		delete(c.pending, id)
		return nil, fmt.Errorf("writing header: %w", err)
	}
	if _, err := c.stdin.Write(data); err != nil {
		delete(c.pending, id)
		return nil, fmt.Errorf("writing content: %w", err)
	}

	// Wait for response outside the lock, but we already hold it
	// Release and reacquire for the response wait
	c.mu.Unlock()
	defer c.mu.Lock()

	deadline := defaultRequestTimeout
	if dl, ok := ctx.Deadline(); ok {
		deadline = time.Until(dl)
	}

	select {
	case resp := <-pc.response:
		delete(c.pending, id)
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp.Result, nil
	case <-ctx.Done():
		delete(c.pending, id)
		return nil, ctx.Err()
	case <-time.After(deadline):
		delete(c.pending, id)
		return nil, fmt.Errorf("request %s timed out after %v", method, deadline)
	case <-c.done:
		delete(c.pending, id)
		return nil, errors.New("LSP server shut down")
	}
}

// sendNotification sends a JSON-RPC notification (no response expected).
func (c *RealClient) sendNotification(method string, params interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected.Load() {
		return errors.New("LSP server not connected")
	}

	c.sendNotificationLocked(method, params)
	return nil
}

func (c *RealClient) sendNotificationLocked(method string, params interface{}) {
	notif := jsonrpcNotification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}

	data, err := json.Marshal(notif)
	if err != nil {
		// Notifications are best-effort; marshal errors are silently dropped
		return
	}

	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
	c.stdin.Write([]byte(header))
	c.stdin.Write(data)
}

// readLoop reads JSON-RPC messages from the server's stdout.
func (c *RealClient) readLoop() {
	for c.connected.Load() {
		resp, err := c.readMessage()
		if err != nil {
			if c.connected.Load() {
				// Unexpected error — process likely crashed
				c.connected.Store(false)
			}
			return
		}
		if resp == nil {
			continue // notification, no client to dispatch to
		}

		c.mu.Lock()
		if pc, ok := c.pending[resp.ID]; ok {
			select {
			case pc.response <- resp:
			default:
			}
		}
		c.mu.Unlock()
	}
}

// readMessage reads a single JSON-RPC message from stdout.
func (c *RealClient) readMessage() (*jsonrpcResponse, error) {
	// Read Content-Length header
	headerLine, err := c.stdout.ReadString('\n')
	if err != nil {
		return nil, err
	}
	headerLine = strings.TrimSpace(headerLine)
	if !strings.HasPrefix(headerLine, "Content-Length:") {
		return nil, fmt.Errorf("expected Content-Length header, got: %s", headerLine)
	}

	contentLen, err := strconv.Atoi(strings.TrimSpace(headerLine[len("Content-Length:"):]))
	if err != nil {
		return nil, fmt.Errorf("invalid Content-Length: %w", err)
	}

	// Skip the blank line separator
	_, err = c.stdout.ReadString('\n')
	if err != nil {
		return nil, err
	}

	// Read content
	content := make([]byte, contentLen)
	_, err = io.ReadFull(c.stdout, content)
	if err != nil {
		return nil, err
	}

	// Try to parse as response (has id) or notification (no id)
	var check struct {
		ID *int64 `json:"id"`
	}
	if err := json.Unmarshal(content, &check); err != nil {
		return nil, fmt.Errorf("parsing JSON-RPC message: %w", err)
	}

	if check.ID == nil {
		// Notification — no response needed
		return nil, nil
	}

	var resp jsonrpcResponse
	if err := json.Unmarshal(content, &resp); err != nil {
		return nil, fmt.Errorf("parsing JSON-RPC response: %w", err)
	}
	return &resp, nil
}

// openFile sends a textDocument/didOpen notification to the server.
func (c *RealClient) openFile(uri, languageID, content string) error {
	return c.sendNotification("textDocument/didOpen", map[string]interface{}{
		"textDocument": map[string]interface{}{
			"uri":        uri,
			"languageId": languageID,
			"version":    1,
			"text":       content,
		},
	})
}

// changeFile sends a textDocument/didChange notification to the server.
func (c *RealClient) changeFile(uri, content string) error {
	return c.sendNotification("textDocument/didChange", map[string]interface{}{
		"textDocument": map[string]interface{}{
			"uri":     uri,
			"version": 1,
		},
		"contentChanges": []map[string]interface{}{
			{"text": content},
		},
	})
}

// saveFile sends a textDocument/didSave notification to the server.
func (c *RealClient) saveFile(uri string) error {
	return c.sendNotification("textDocument/didSave", map[string]interface{}{
		"textDocument": map[string]interface{}{
			"uri": uri,
		},
	})
}

// closeFile sends a textDocument/didClose notification to the server.
func (c *RealClient) closeFile(uri string) error {
	return c.sendNotification("textDocument/didClose", map[string]interface{}{
		"textDocument": map[string]interface{}{
			"uri": uri,
		},
	})
}

// LSP operation implementations
// All coordinate parameters are 0-based (LSP wire protocol).

// GoToDefinition implements Client.
func (c *RealClient) GoToDefinition(ctx context.Context, uri string, line, character int) ([]Location, error) {
	result, err := c.textDocumentPositionRequest(ctx, "textDocument/definition", uri, line, character)
	if err != nil {
		return nil, err
	}
	return c.parseLocations(result)
}

// FindReferences implements Client.
func (c *RealClient) FindReferences(ctx context.Context, uri string, line, character int) ([]Reference, error) {
	result, err := c.sendRequest(ctx, "textDocument/references", map[string]interface{}{
		"textDocument": map[string]interface{}{"uri": uri},
		"position":     map[string]interface{}{"line": line, "character": character},
		"context":      map[string]interface{}{"includeDeclaration": true},
	})
	if err != nil {
		return nil, err
	}
	locations, err := c.parseLocations(result)
	if err != nil {
		return nil, err
	}
	refs := make([]Reference, len(locations))
	for i, loc := range locations {
		refs[i] = Reference{URI: loc.URI, Line: loc.Line, Start: loc.Start, End: loc.End}
	}
	return refs, nil
}

// Hover implements Client.
func (c *RealClient) Hover(ctx context.Context, uri string, line, character int) (*HoverResult, error) {
	result, err := c.textDocumentPositionRequest(ctx, "textDocument/hover", uri, line, character)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}

	var hover struct {
		Contents interface{} `json:"contents"`
		Range    interface{} `json:"range"`
	}
	if err := json.Unmarshal(result, &hover); err != nil {
		return nil, nil
	}

	contents := c.formatHoverContents(hover.Contents)
	if contents == "" {
		return nil, nil
	}

	return &HoverResult{Contents: contents}, nil
}

// DocumentSymbol implements Client.
func (c *RealClient) DocumentSymbol(ctx context.Context, uri string) ([]DocumentSymbol, error) {
	result, err := c.sendRequest(ctx, "textDocument/documentSymbol", map[string]interface{}{
		"textDocument": map[string]interface{}{"uri": uri},
	})
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}

	// Try DocumentSymbol[] first (hierarchical), then SymbolInformation[]
	var docSymbols []struct {
		Name  string `json:"name"`
		Kind  int    `json:"kind"`
		Range struct {
			Start struct{ Line, Character int } `json:"start"`
		} `json:"range"`
		Children []interface{} `json:"children"`
	}
	if err := json.Unmarshal(result, &docSymbols); err == nil {
		var symbols []DocumentSymbol
		c.flattenDocumentSymbols(docSymbols, "", &symbols, uri)
		return symbols, nil
	}

	// Try SymbolInformation[]
	var symbolInfos []struct {
		Name     string `json:"name"`
		Kind     int    `json:"kind"`
		Location struct {
			URI   string `json:"uri"`
			Range struct {
				Start struct{ Line, Character int } `json:"start"`
			} `json:"range"`
		} `json:"location"`
	}
	if err := json.Unmarshal(result, &symbolInfos); err == nil {
		var symbols []DocumentSymbol
		for _, si := range symbolInfos {
			symbols = append(symbols, DocumentSymbol{
				Name:     si.Name,
				Kind:     symbolKindName(si.Kind),
				Line:     si.Location.Range.Start.Line,
				Start:    si.Location.Range.Start.Character,
				FilePath: uriToFilePath(si.Location.URI),
			})
		}
		return symbols, nil
	}

	return nil, fmt.Errorf("unrecognized documentSymbol response format")
}

func (c *RealClient) flattenDocumentSymbols(symbols []struct {
	Name  string `json:"name"`
	Kind  int    `json:"kind"`
	Range struct {
		Start struct{ Line, Character int } `json:"start"`
	} `json:"range"`
	Children []interface{} `json:"children"`
}, parent string, out *[]DocumentSymbol, uri string) {
	for _, s := range symbols {
		sym := DocumentSymbol{
			Name:   s.Name,
			Kind:   symbolKindName(s.Kind),
			Line:   s.Range.Start.Line,
			Start:  s.Range.Start.Character,
			Parent: parent,
		}
		*out = append(*out, sym)
		if len(s.Children) > 0 {
			// Re-marshal children to recurse
			childData, _ := json.Marshal(s.Children)
			var childSymbols []struct {
				Name  string `json:"name"`
				Kind  int    `json:"kind"`
				Range struct {
					Start struct{ Line, Character int } `json:"start"`
				} `json:"range"`
				Children []interface{} `json:"children"`
			}
			json.Unmarshal(childData, &childSymbols)
			c.flattenDocumentSymbols(childSymbols, s.Name, out, uri)
		}
	}
}

// WorkspaceSymbol implements Client.
func (c *RealClient) WorkspaceSymbol(ctx context.Context, query string) ([]WorkspaceSymbol, error) {
	result, err := c.sendRequest(ctx, "workspace/symbol", map[string]interface{}{
		"query": query,
	})
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}

	var rawSymbols []struct {
		Name     string `json:"name"`
		Kind     int    `json:"kind"`
		Location struct {
			URI   string `json:"uri"`
			Range struct {
				Start struct{ Line, Character int } `json:"start"`
				End   struct{ Character int }       `json:"end"`
			} `json:"range"`
		} `json:"location"`
	}
	if err := json.Unmarshal(result, &rawSymbols); err != nil {
		return nil, err
	}

	symbols := make([]WorkspaceSymbol, len(rawSymbols))
	for i, s := range rawSymbols {
		symbols[i] = WorkspaceSymbol{
			Name:     s.Name,
			Kind:     symbolKindName(s.Kind),
			URI:      s.Location.URI,
			Line:     s.Location.Range.Start.Line,
			Start:    s.Location.Range.Start.Character,
			End:      s.Location.Range.End.Character,
			FilePath: uriToFilePath(s.Location.URI),
		}
	}
	return symbols, nil
}

// GoToImplementation implements Client.
func (c *RealClient) GoToImplementation(ctx context.Context, uri string, line, character int) ([]Location, error) {
	result, err := c.textDocumentPositionRequest(ctx, "textDocument/implementation", uri, line, character)
	if err != nil {
		return nil, err
	}
	return c.parseLocations(result)
}

// PrepareCallHierarchy implements Client.
func (c *RealClient) PrepareCallHierarchy(ctx context.Context, uri string, line, character int) ([]CallHierarchyItem, error) {
	result, err := c.textDocumentPositionRequest(ctx, "textDocument/prepareCallHierarchy", uri, line, character)
	if err != nil {
		return nil, err
	}
	return c.parseCallHierarchyItems(result)
}

// IncomingCalls implements Client.
func (c *RealClient) IncomingCalls(ctx context.Context, uri string, line, character int) ([]CallHierarchyItem, error) {
	items, err := c.PrepareCallHierarchy(ctx, uri, line, character)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, nil
	}

	// Use the first item to request incoming calls
	callParams := map[string]interface{}{
		"item": map[string]interface{}{
			"name": items[0].Name,
			"kind": symbolKindCode(items[0].Kind),
			"uri":  items[0].URI,
			"range": map[string]interface{}{
				"start": map[string]interface{}{"line": items[0].Line, "character": items[0].Start},
				"end":   map[string]interface{}{"line": items[0].Line, "character": items[0].End},
			},
		},
	}

	result, err := c.sendRequest(ctx, "callHierarchy/incomingCalls", callParams)
	if err != nil {
		return nil, err
	}
	return c.parseCallHierarchyFromResult(result, "from")
}

// OutgoingCalls implements Client.
func (c *RealClient) OutgoingCalls(ctx context.Context, uri string, line, character int) ([]CallHierarchyItem, error) {
	items, err := c.PrepareCallHierarchy(ctx, uri, line, character)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, nil
	}

	callParams := map[string]interface{}{
		"item": map[string]interface{}{
			"name": items[0].Name,
			"kind": symbolKindCode(items[0].Kind),
			"uri":  items[0].URI,
			"range": map[string]interface{}{
				"start": map[string]interface{}{"line": items[0].Line, "character": items[0].Start},
				"end":   map[string]interface{}{"line": items[0].Line, "character": items[0].End},
			},
		},
	}

	result, err := c.sendRequest(ctx, "callHierarchy/outgoingCalls", callParams)
	if err != nil {
		return nil, err
	}
	return c.parseCallHierarchyFromResult(result, "to")
}

// Helper methods

func (c *RealClient) textDocumentPositionRequest(ctx context.Context, method, uri string, line, character int) (json.RawMessage, error) {
	params := map[string]interface{}{
		"textDocument": map[string]interface{}{
			"uri": uri,
		},
		"position": map[string]interface{}{
			"line":      line,
			"character": character,
		},
	}
	return c.sendRequest(ctx, method, params)
}

func (c *RealClient) parseLocations(result json.RawMessage) ([]Location, error) {
	if result == nil {
		return nil, nil
	}

	// Single location
	var single struct {
		URI   string `json:"uri"`
		Range struct {
			Start struct{ Line, Character int } `json:"start"`
			End   struct{ Character int }       `json:"end"`
		} `json:"range"`
	}
	if err := json.Unmarshal(result, &single); err == nil && single.URI != "" {
		return []Location{{
			URI:   single.URI,
			Line:  single.Range.Start.Line,
			Start: single.Range.Start.Character,
			End:   single.Range.End.Character,
		}}, nil
	}

	// Array of locations
	var arr []struct {
		URI   string `json:"uri"`
		Range struct {
			Start struct{ Line, Character int } `json:"start"`
			End   struct{ Character int }       `json:"end"`
		} `json:"range"`
	}
	if err := json.Unmarshal(result, &arr); err != nil {
		return nil, nil
	}

	locations := make([]Location, len(arr))
	for i, a := range arr {
		locations[i] = Location{
			URI:   a.URI,
			Line:  a.Range.Start.Line,
			Start: a.Range.Start.Character,
			End:   a.Range.End.Character,
		}
	}
	return locations, nil
}

func (c *RealClient) parseCallHierarchyItems(result json.RawMessage) ([]CallHierarchyItem, error) {
	if result == nil {
		return nil, nil
	}

	// Single item
	var single struct {
		Name  string `json:"name"`
		Kind  int    `json:"kind"`
		URI   string `json:"uri"`
		Range struct {
			Start struct{ Line, Character int } `json:"start"`
			End   struct{ Character int }       `json:"end"`
		} `json:"range"`
	}
	if err := json.Unmarshal(result, &single); err == nil && single.Name != "" {
		return []CallHierarchyItem{{
			Name:     single.Name,
			Kind:     symbolKindName(single.Kind),
			URI:      single.URI,
			Line:     single.Range.Start.Line,
			Start:    single.Range.Start.Character,
			End:      single.Range.End.Character,
			FilePath: uriToFilePath(single.URI),
		}}, nil
	}

	// Array
	var arr []struct {
		Name  string `json:"name"`
		Kind  int    `json:"kind"`
		URI   string `json:"uri"`
		Range struct {
			Start struct{ Line, Character int } `json:"start"`
			End   struct{ Character int }       `json:"end"`
		} `json:"range"`
	}
	if err := json.Unmarshal(result, &arr); err != nil {
		return nil, err
	}

	items := make([]CallHierarchyItem, len(arr))
	for i, a := range arr {
		items[i] = CallHierarchyItem{
			Name:     a.Name,
			Kind:     symbolKindName(a.Kind),
			URI:      a.URI,
			Line:     a.Range.Start.Line,
			Start:    a.Range.Start.Character,
			End:      a.Range.End.Character,
			FilePath: uriToFilePath(a.URI),
		}
	}
	return items, nil
}

func (c *RealClient) parseCallHierarchyFromResult(result json.RawMessage, direction string) ([]CallHierarchyItem, error) {
	if result == nil {
		return nil, nil
	}

	var calls []struct {
		From       interface{} `json:"from,omitempty"`
		To         interface{} `json:"to,omitempty"`
		FromRanges interface{} `json:"fromRanges"`
	}
	if err := json.Unmarshal(result, &calls); err != nil {
		return nil, err
	}

	var items []CallHierarchyItem
	for _, call := range calls {
		var node interface{}
		if direction == "from" {
			node = call.From
		} else {
			node = call.To
		}
		if node == nil {
			continue
		}
		data, _ := json.Marshal(node)
		parsed, err := c.parseCallHierarchyItems(data)
		if err != nil {
			continue
		}
		items = append(items, parsed...)
	}
	return items, nil
}

func (c *RealClient) formatHoverContents(contents interface{}) string {
	switch v := contents.(type) {
	case string:
		return v
	case map[string]interface{}:
		// MarkupContent: {kind, value}
		if value, ok := v["value"].(string); ok {
			return value
		}
		// Older format: {language, value}
		if value, ok := v["value"].(string); ok {
			return value
		}
	case []interface{}:
		// Array of MarkedString
		var parts []string
		for _, item := range v {
			switch s := item.(type) {
			case string:
				parts = append(parts, s)
			case map[string]interface{}:
				if value, ok := s["value"].(string); ok {
					parts = append(parts, value)
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

// symbolKindName converts an LSP SymbolKind integer to a human-readable name.
func symbolKindName(kind int) string {
	names := map[int]string{
		1:  "File",
		2:  "Module",
		3:  "Namespace",
		4:  "Package",
		5:  "Class",
		6:  "Method",
		7:  "Property",
		8:  "Field",
		9:  "Constructor",
		10: "Enum",
		11: "Interface",
		12: "Function",
		13: "Variable",
		14: "Constant",
		15: "String",
		16: "Number",
		17: "Boolean",
		18: "Array",
		19: "Object",
		20: "Key",
		21: "Null",
		22: "EnumMember",
		23: "Struct",
		24: "Event",
		25: "Operator",
		26: "TypeParameter",
	}
	if name, ok := names[kind]; ok {
		return name
	}
	return fmt.Sprintf("Kind(%d)", kind)
}

// symbolKindCode converts a human-readable kind name back to an LSP integer code.
func symbolKindCode(name string) int {
	codes := map[string]int{
		"File": 1, "Module": 2, "Namespace": 3, "Package": 4,
		"Class": 5, "Method": 6, "Property": 7, "Field": 8,
		"Constructor": 9, "Enum": 10, "Interface": 11, "Function": 12,
		"Variable": 13, "Constant": 14, "String": 15, "Number": 16,
		"Boolean": 17, "Array": 18, "Object": 19, "Key": 20,
		"Null": 21, "EnumMember": 22, "Struct": 23, "Event": 24,
		"Operator": 25, "TypeParameter": 26,
	}
	if code, ok := codes[name]; ok {
		return code
	}
	return 0
}

// uriToFilePath converts a file:// URI to a file path.
func uriToFilePath(uri string) string {
	path := strings.TrimPrefix(uri, "file://")
	// Handle Windows: file:///C:/... -> C:/...
	if len(path) > 2 && path[0] == '/' && path[2] == ':' {
		path = path[1:]
	}
	return path
}
