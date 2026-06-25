package agent

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/ipy/jenny/internal/api"
	"github.com/ipy/jenny/internal/tool"
)

type countingMockTool struct {
	name  string
	calls atomic.Int32
}

func (m *countingMockTool) Name() string                        { return m.name }
func (m *countingMockTool) Description() string                 { return "counting mock" }
func (m *countingMockTool) InputSchema() map[string]any         { return map[string]any{} }
func (m *countingMockTool) ConcurrencySafe(map[string]any) bool { return true }

func (m *countingMockTool) Execute(ctx context.Context, input map[string]any, cwd string) (*tool.ToolResult, error) {
	m.calls.Add(1)
	return &tool.ToolResult{Content: "ok"}, nil
}

func TestExecuteAndProcessTools_LoopGuardBlockSkipsExecution(t *testing.T) {
	mock := &countingMockTool{name: "Read"}
	e := mustNewQueryEngine(&StreamConfig{}, []tool.Tool{mock}, "test-model", WithClient(fastClient(t)))
	e.loopGuard = NewLoopGuard()

	block := api.ToolUseBlock{
		ID:    "tool-1",
		Name:  "Read",
		Input: map[string]any{"file_path": "/tmp/x.go"},
	}
	for i := 0; i < toolHardThreshold-1; i++ {
		e.loopGuard.EvaluateToolCalls([]api.ToolUseBlock{block})
	}
	decisions := e.loopGuard.EvaluateToolCalls([]api.ToolUseBlock{block})
	if decisions[0].Action != ToolActionBlock {
		t.Fatalf("expected block decision, got %v", decisions[0].Action)
	}

	results, _, err := e.executeAndProcessTools(context.Background(), []api.ToolUseBlock{block}, "sess", t.TempDir(), decisions)
	if err != nil {
		t.Fatalf("executeAndProcessTools: %v", err)
	}
	if mock.calls.Load() != 0 {
		t.Fatalf("blocked tool should not execute, calls=%d", mock.calls.Load())
	}
	if len(results) != 1 || !results[0].IsError {
		t.Fatalf("expected synthetic error result, got %+v", results)
	}
	if results[0].Content != decisions[0].BlockMsg {
		t.Fatalf("expected block message in result, got %q", results[0].Content)
	}
}

func TestExecuteAndProcessTools_NoBatchDedup(t *testing.T) {
	mock := &countingMockTool{name: "Read"}
	e := mustNewQueryEngine(&StreamConfig{}, []tool.Tool{mock}, "test-model", WithClient(fastClient(t)))
	e.loopGuard = NewLoopGuard()

	blocks := []api.ToolUseBlock{
		{ID: "tool-1", Name: "Read", Input: map[string]any{"file_path": "/tmp/a.go"}},
		{ID: "tool-2", Name: "Read", Input: map[string]any{"file_path": "/tmp/a.go"}},
		{ID: "tool-3", Name: "Read", Input: map[string]any{"file_path": "/tmp/a.go"}},
	}
	decisions := e.loopGuard.EvaluateToolCalls(blocks)
	for i, d := range decisions {
		if d.Action != ToolActionExecute {
			t.Fatalf("decision[%d] should execute (no batch dedup), got %v", i, d.Action)
		}
	}

	results, _, err := e.executeAndProcessTools(context.Background(), blocks, "sess", t.TempDir(), decisions)
	if err != nil {
		t.Fatalf("executeAndProcessTools: %v", err)
	}
	if mock.calls.Load() != 3 {
		t.Fatalf("expected 3 executions (no batch dedup), calls=%d", mock.calls.Load())
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 tool results, got %d", len(results))
	}
}
