package tool

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/ipy/jenny/internal/lsp"
)

// mockServerManager is a mock implementation for testing LSPTool.
type mockServerManager struct {
	connected       bool
	openFiles       map[string]bool
	goToDefResult   []lsp.Location
	findRefsResult  []lsp.Reference
	hoverResult     *lsp.HoverResult
	docSymResults   []lsp.DocumentSymbol
	workspaceSymRes []lsp.WorkspaceSymbol
	goToImplResult  []lsp.Location
	prepareCHRes    []lsp.CallHierarchyItem
	incomingRes     []lsp.CallHierarchyItem
	outgoingRes     []lsp.CallHierarchyItem
	mu              sync.RWMutex
}

func newMockServerManager() *mockServerManager {
	return &mockServerManager{
		connected: true,
		openFiles: make(map[string]bool),
	}
}

func (m *mockServerManager) Connected() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.connected
}

func (m *mockServerManager) IsFileOpen(path string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.openFiles[path]
}

func (m *mockServerManager) OpenFile(path, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.openFiles[path] = true
	return nil
}

func (m *mockServerManager) ChangeFile(_, _ string) error { return nil }
func (m *mockServerManager) SaveFile(_ string) error      { return nil }
func (m *mockServerManager) CloseFile(_ string) error     { return nil }

func (m *mockServerManager) GoToDefinition(_ context.Context, _ string, _, _ int) ([]lsp.Location, error) {
	return m.goToDefResult, nil
}
func (m *mockServerManager) FindReferences(_ context.Context, _ string, _, _ int) ([]lsp.Reference, error) {
	return m.findRefsResult, nil
}
func (m *mockServerManager) Hover(_ context.Context, _ string, _, _ int) (*lsp.HoverResult, error) {
	return m.hoverResult, nil
}
func (m *mockServerManager) DocumentSymbol(_ context.Context, _ string) ([]lsp.DocumentSymbol, error) {
	return m.docSymResults, nil
}
func (m *mockServerManager) WorkspaceSymbol(_ context.Context, _ string) ([]lsp.WorkspaceSymbol, error) {
	return m.workspaceSymRes, nil
}
func (m *mockServerManager) GoToImplementation(_ context.Context, _ string, _, _ int) ([]lsp.Location, error) {
	return m.goToImplResult, nil
}
func (m *mockServerManager) PrepareCallHierarchy(_ context.Context, _ string, _, _ int) ([]lsp.CallHierarchyItem, error) {
	return m.prepareCHRes, nil
}
func (m *mockServerManager) IncomingCalls(_ context.Context, _ string, _, _ int) ([]lsp.CallHierarchyItem, error) {
	return m.incomingRes, nil
}
func (m *mockServerManager) OutgoingCalls(_ context.Context, _ string, _, _ int) ([]lsp.CallHierarchyItem, error) {
	return m.outgoingRes, nil
}

func TestLSPTool_NameAndDescription(t *testing.T) {
	mgr := newMockServerManager()
	tool := newLSPToolForTesting(mgr)
	if tool.Name() != "lsp" {
		t.Errorf("expected Name() to be 'lsp', got %q", tool.Name())
	}
	if tool.Description() == "" {
		t.Error("Description() should not be empty")
	}

	schema := tool.InputSchema()
	if schema["type"] != "object" {
		t.Errorf("expected schema type 'object', got %v", schema["type"])
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("properties should be a map")
	}
	if _, ok := props["operation"]; !ok {
		t.Error("schema should have 'operation' property")
	}
}

func TestLSPTool_OneBasedCoordinates(t *testing.T) {
	mgr := newMockServerManager()
	mgr.goToDefResult = []lsp.Location{
		{URI: "file:///test.go", Line: 0, Start: 0, End: 0},
	}
	tool := newLSPToolForTesting(mgr)
	ctx := context.Background()

	// Input line=1, character=1 should be converted to LSP line=0, character=0
	result, err := tool.Execute(ctx, map[string]any{
		"operation": "goToDefinition",
		"uri":       "file:///test.go",
		"line":      1,
		"character": 1,
	}, t.TempDir())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("expected no error, got: %s", result.Content)
	}
}

func TestLSPTool_DisconnectedError(t *testing.T) {
	mgr := newMockServerManager()
	mgr.connected = false
	tool := newLSPToolForTesting(mgr)
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{
		"operation": "hover",
		"uri":       "file:///test.go",
		"line":      1,
	}, t.TempDir())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError when disconnected")
	}
	if !strings.Contains(result.Content, "not connected") {
		t.Errorf("expected error mentioning 'not connected', got: %s", result.Content)
	}
}

func TestLSPTool_FileSizeLimit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping large file test on Windows (file path handling differs)")
	}
	mgr := newMockServerManager()
	tool := newLSPToolForTesting(mgr)
	ctx := context.Background()

	tmpDir := t.TempDir()
	largeFile := filepath.Join(tmpDir, "large.go")
	f, err := os.Create(largeFile)
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	data := make([]byte, 11*1024*1024) // 11 MB
	for i := range data {
		data[i] = 'x'
	}
	f.Write(data)
	f.Close()

	result, err := tool.Execute(ctx, map[string]any{
		"operation": "hover",
		"uri":       "file://" + largeFile,
		"line":      1,
	}, tmpDir)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError for file >10MB")
	}
	if !strings.Contains(result.Content, "too large") {
		t.Errorf("expected error mentioning 'too large', got: %s", result.Content)
	}
}

func TestLSPTool_ConcurrencySafety(t *testing.T) {
	mgr := newMockServerManager()
	mgr.hoverResult = &lsp.HoverResult{
		Contents: "example documentation",
		Line:     5,
		Start:    10,
	}
	mgr.goToDefResult = []lsp.Location{
		{URI: "file:///test.go", Line: 5, Start: 10, End: 20},
	}
	tool := newLSPToolForTesting(mgr)
	ctx := context.Background()

	var wg sync.WaitGroup
	errors := make(chan error, 20)

	for i := range 10 {
		wg.Add(2)

		go func(id int) {
			defer wg.Done()
			result, err := tool.Execute(ctx, map[string]any{
				"operation": "hover",
				"uri":       "file:///test.go",
				"line":      6,
				"character": 11,
			}, t.TempDir())
			if err != nil {
				errors <- err
				return
			}
			if result == nil {
				errors <- nil
			}
		}(i)

		go func(id int) {
			defer wg.Done()
			result, err := tool.Execute(ctx, map[string]any{
				"operation": "goToDefinition",
				"uri":       "file:///test.go",
				"line":      6,
				"character": 11,
			}, t.TempDir())
			if err != nil {
				errors <- err
				return
			}
			if result == nil {
				errors <- nil
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		if err != nil {
			t.Fatalf("concurrent call failed: %v", err)
		}
	}
}

func TestLSPTool_GitignoreFiltering_WorkspaceSymbol(t *testing.T) {
	tmpDir := t.TempDir()
	gitignorePath := filepath.Join(tmpDir, ".gitignore")
	os.WriteFile(gitignorePath, []byte("ignored.go\n*.log\n"), 0644)
	os.MkdirAll(filepath.Join(tmpDir, ".git"), 0755)

	mgr := newMockServerManager()
	tool := newLSPToolForTesting(mgr)
	ctx := context.Background()

	mgr.workspaceSymRes = []lsp.WorkspaceSymbol{
		{Name: "GoodSymbol", Kind: "Function", URI: "file:///good.go", Line: 1, Start: 0, FilePath: filepath.Join(tmpDir, "good.go")},
		{Name: "BadSymbol", Kind: "Function", URI: "file:///ignored.go", Line: 1, Start: 0, FilePath: filepath.Join(tmpDir, "ignored.go")},
		{Name: "AlsoBad", Kind: "Function", URI: "file:///debug.log", Line: 1, Start: 0, FilePath: filepath.Join(tmpDir, "debug.log")},
	}

	result, err := tool.Execute(ctx, map[string]any{
		"operation": "workspaceSymbol",
		"query":     "test",
	}, tmpDir)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("expected no error, got: %s", result.Content)
	}

	if strings.Contains(result.Content, "ignored.go") {
		t.Error("gitignored path 'ignored.go' should be filtered from results")
	}
	if strings.Contains(result.Content, "debug.log") {
		t.Error("gitignored path 'debug.log' should be filtered from results")
	}
	if !strings.Contains(result.Content, "GoodSymbol") {
		t.Error("non-gitignored path should be in results")
	}
}

func TestLSPTool_GitignoreFiltering_FindReferences(t *testing.T) {
	tmpDir := t.TempDir()
	gitignorePath := filepath.Join(tmpDir, ".gitignore")
	os.WriteFile(gitignorePath, []byte("secret.go\n"), 0644)
	os.MkdirAll(filepath.Join(tmpDir, ".git"), 0755)

	mgr := newMockServerManager()
	tool := newLSPToolForTesting(mgr)
	ctx := context.Background()

	mgr.findRefsResult = []lsp.Reference{
		{URI: "file://" + filepath.Join(tmpDir, "visible.go"), Line: 5, Start: 10},
		{URI: "file://" + filepath.Join(tmpDir, "secret.go"), Line: 5, Start: 10},
	}

	result, err := tool.Execute(ctx, map[string]any{
		"operation": "findReferences",
		"uri":       "file:///test.go",
		"line":      6,
		"character": 11,
	}, tmpDir)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("expected no error, got: %s", result.Content)
	}

	if strings.Contains(result.Content, "secret.go") {
		t.Error("gitignored path 'secret.go' should be filtered from findReferences results")
	}
	if !strings.Contains(result.Content, "visible.go") {
		t.Error("non-gitignored path should be in findReferences results")
	}
}

func TestLSPTool_InvalidOperation(t *testing.T) {
	mgr := newMockServerManager()
	tool := newLSPToolForTesting(mgr)
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{
		"operation": "invalidOperation",
	}, t.TempDir())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError for invalid operation")
	}
	if !strings.Contains(result.Content, "unknown operation") {
		t.Errorf("expected error mentioning 'unknown operation', got: %s", result.Content)
	}
}

func TestLSPTool_MissingOperation(t *testing.T) {
	mgr := newMockServerManager()
	tool := newLSPToolForTesting(mgr)
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{
		"uri": "file:///test.go",
	}, t.TempDir())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError for missing operation")
	}
}

func TestLSPTool_AllOperations(t *testing.T) {
	mgr := newMockServerManager()
	tool := newLSPToolForTesting(mgr)
	ctx := context.Background()

	tmpDir := t.TempDir()
	os.MkdirAll(filepath.Join(tmpDir, ".git"), 0755)

	mgr.goToDefResult = []lsp.Location{{URI: "file:///test.go", Line: 5, Start: 10}}
	mgr.findRefsResult = []lsp.Reference{{URI: "file:///test.go", Line: 5, Start: 10}}
	mgr.hoverResult = &lsp.HoverResult{Contents: "test", Line: 5, Start: 10}
	mgr.docSymResults = []lsp.DocumentSymbol{{Name: "Test", Kind: "Function", Line: 5, Start: 10, FilePath: filepath.Join(tmpDir, "test.go")}}
	mgr.workspaceSymRes = []lsp.WorkspaceSymbol{{Name: "Test", Kind: "Function", URI: "file:///test.go", Line: 5, Start: 10, FilePath: filepath.Join(tmpDir, "test.go")}}
	mgr.goToImplResult = []lsp.Location{{URI: "file:///test.go", Line: 5, Start: 10}}
	mgr.prepareCHRes = []lsp.CallHierarchyItem{{Name: "Test", Kind: "Function", URI: "file:///test.go", Line: 5, Start: 10}}
	mgr.incomingRes = []lsp.CallHierarchyItem{{Name: "Test", Kind: "Function", URI: "file:///test.go", Line: 5, Start: 10}}
	mgr.outgoingRes = []lsp.CallHierarchyItem{{Name: "Test", Kind: "Function", URI: "file:///test.go", Line: 5, Start: 10}}

	operations := []string{
		"goToDefinition",
		"findReferences",
		"hover",
		"documentSymbol",
		"workspaceSymbol",
		"goToImplementation",
		"prepareCallHierarchy",
		"incomingCalls",
		"outgoingCalls",
	}

	for _, op := range operations {
		t.Run(op, func(t *testing.T) {
			input := map[string]any{
				"operation": op,
				"uri":       "file:///test.go",
				"line":      6,
				"character": 11,
			}
			if op == "workspaceSymbol" {
				input = map[string]any{
					"operation": op,
					"query":     "test",
				}
			} else if op == "documentSymbol" {
				input = map[string]any{
					"operation": op,
					"uri":       "file:///test.go",
				}
			}

			result, err := tool.Execute(ctx, input, tmpDir)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.IsError && strings.Contains(result.Content, "not connected") {
				t.Errorf("operation %s failed with connection error", op)
			}
		})
	}
}
