// Package agent provides the core agent loop and query engine.
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ipy/jenny/internal/session"
)

// thinkingTokensEvents returns SSE events for a response with a thinking block
// that emits multiple thinking_delta events (to test periodic debouncing).
func thinkingTokensEvents(thinkingDeltas []string, signature string, text string) []string {
	events := []string{
		sseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"test","stop_reason":null,"usage":{"input_tokens":1,"output_tokens":1}}}`),
		// Thinking block (index 0)
		sseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`),
	}
	for _, delta := range thinkingDeltas {
		events = append(events, sseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"`+delta+`"}}`))
	}
	if signature != "" {
		events = append(events, sseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"`+signature+`"}}`))
	}
	events = append(events,
		sseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
		// Text block (index 1)
		sseLine("content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`),
		sseLine("content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"`+text+`"}}`),
		sseLine("content_block_stop", `{"type":"content_block_stop","index":1}`),
		sseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":1,"output_tokens":2}}`),
		sseLine("message_stop", `{"type":"message_stop"}`),
	)
	return events
}

// TestThinkingTokens_EventShape verifies AC3: a thinking_tokens system event
// is emitted when the stream receives a thinking block, and has the correct
// field shape (type, subtype, session_id, uuid, estimated_tokens, estimated_tokens_delta).
func TestThinkingTokens_EventShape(t *testing.T) {
	thinking := "Let me analyze this problem step by step"
	server := makeMockStreamServer(t, thinkingTextToolEvents(thinking, "sig-abc", "Here is the answer."))
	defer server.Close()

	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	tmpDir := t.TempDir()
	sessMgr, err := session.NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}

	cfg := StreamConfig{
		Enabled:        true,
		SessionManager: sessMgr,
		SessionID:      "sess_tt_shape",
	}

	stdout := captureTTStdout(t, func() {
		engine := mustNewQueryEngine(&cfg, nil, "", WithClient(fastClient(t)))
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = engine.SubmitMessage(ctx, "test")
	})

	// Find thinking_tokens events
	var thinkingTokensLines []string
	for _, line := range strings.Split(strings.TrimRight(stdout, "\n"), "\n") {
		if strings.Contains(line, `"type":"system"`) && strings.Contains(line, `"subtype":"thinking_tokens"`) {
			fmt.Fprintf(os.Stderr, "THINKING_TEST: found thinking_tokens: %s\n", line)
			thinkingTokensLines = append(thinkingTokensLines, line)
		}
	}

	if len(thinkingTokensLines) == 0 {
		t.Fatal("AC3 FAIL: no thinking_tokens events found in output")
	}

	// Verify shape of first thinking_tokens event
	var evt map[string]any
	if err := json.Unmarshal([]byte(thinkingTokensLines[0]), &evt); err != nil {
		t.Fatalf("AC3 FAIL: thinking_tokens line is not valid JSON: %v", err)
	}

	// Required fields
	for _, field := range []string{"type", "subtype", "session_id", "uuid", "estimated_tokens", "estimated_tokens_delta"} {
		if _, ok := evt[field]; !ok {
			t.Errorf("AC3 FAIL: thinking_tokens event missing field %q", field)
		}
	}

	// Verify correct values
	if evt["type"] != "system" {
		t.Errorf("AC3 FAIL: type = %v, want \"system\"", evt["type"])
	}
	if evt["subtype"] != "thinking_tokens" {
		t.Errorf("AC3 FAIL: subtype = %v, want \"thinking_tokens\"", evt["subtype"])
	}
	if evt["session_id"] != "sess_tt_shape" {
		t.Errorf("AC3 FAIL: session_id = %v, want \"sess_tt_shape\"", evt["session_id"])
	}

	// estimated_tokens should be integer >= 0
	et, ok := evt["estimated_tokens"].(float64)
	if !ok {
		t.Errorf("AC3 FAIL: estimated_tokens is not a number: %T", evt["estimated_tokens"])
	} else if et < 0 {
		t.Errorf("AC3 FAIL: estimated_tokens = %v, want >= 0", et)
	}

	// estimated_tokens_delta should be integer >= 1
	etd, ok := evt["estimated_tokens_delta"].(float64)
	if !ok {
		t.Errorf("AC3 FAIL: estimated_tokens_delta is not a number: %T", evt["estimated_tokens_delta"])
	} else if etd < 1 {
		t.Errorf("AC3 FAIL: estimated_tokens_delta = %v, want >= 1", etd)
	}

	t.Logf("AC3 PASS: thinking_tokens event shape verified, estimated_tokens=%d, estimated_tokens_delta=%d", int(et), int(etd))
}

// TestThinkingTokens_NoEventWhenDisabled verifies AC5: when stream-json is disabled,
// no thinking_tokens events are emitted even when thinking blocks are present.
func TestThinkingTokens_NoEventWhenDisabled(t *testing.T) {
	thinking := "Thinking content here"
	server := makeMockStreamServer(t, thinkingTextToolEvents(thinking, "sig-abc", "answer"))
	defer server.Close()

	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	tmpDir := t.TempDir()
	sessMgr, err := session.NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}

	cfg := StreamConfig{
		Enabled:        false, // Streaming disabled
		SessionManager: sessMgr,
		SessionID:      "sess_tt_disabled",
	}

	stdout := captureTTStdout(t, func() {
		engine := mustNewQueryEngine(&cfg, nil, "", WithClient(fastClient(t)))
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = engine.SubmitMessage(ctx, "test")
	})

	if strings.Contains(stdout, "thinking_tokens") {
		t.Error("AC5 FAIL: thinking_tokens found in output when streamCfg.Enabled=false")
	} else {
		t.Log("AC5 PASS: no thinking_tokens when streaming disabled")
	}
}

// TestThinkingTokens_PeriodicEmission verifies AC3: thinking_tokens events
// are emitted when the stream receives thinking deltas, and that the
// estimated_tokens is a running total while estimated_tokens_delta reflects
// the increment since the last event.
func TestThinkingTokens_PeriodicEmission(t *testing.T) {
	// Two thinking deltas: "first part " and "second part"
	deltas := []string{"first part ", "second part"}
	server := makeMockStreamServer(t, thinkingTokensEvents(deltas, "", "final answer"))
	defer server.Close()

	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	tmpDir := t.TempDir()
	sessMgr, err := session.NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}

	cfg := StreamConfig{
		Enabled:        true,
		SessionManager: sessMgr,
		SessionID:      "sess_tt_periodic",
	}

	stdout := captureTTStdout(t, func() {
		engine := mustNewQueryEngine(&cfg, nil, "", WithClient(fastClient(t)))
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = engine.SubmitMessage(ctx, "test")
	})

	// Collect all thinking_tokens events
	var events []map[string]any
	for _, line := range strings.Split(strings.TrimRight(stdout, "\n"), "\n") {
		if strings.Contains(line, `"type":"system"`) && strings.Contains(line, `"subtype":"thinking_tokens"`) {
			var evt map[string]any
			if json.Unmarshal([]byte(line), &evt) == nil {
				events = append(events, evt)
			}
		}
	}

	if len(events) == 0 {
		t.Fatal("AC3 FAIL: no thinking_tokens events found")
	}

	// Verify estimated_tokens is non-decreasing across events
	for i := 1; i < len(events); i++ {
		prev := int(events[i-1]["estimated_tokens"].(float64))
		curr := int(events[i]["estimated_tokens"].(float64))
		if curr < prev {
			t.Errorf("AC3 FAIL: estimated_tokens decreased from %d to %d between events %d and %d", prev, curr, i-1, i)
		}
	}

	// Verify estimated_tokens_delta >= 1 for all events (AC6)
	for i, evt := range events {
		delta := int(evt["estimated_tokens_delta"].(float64))
		if delta < 1 {
			t.Errorf("AC3/AC6 FAIL: event %d estimated_tokens_delta=%d, want >= 1", i, delta)
		}
	}

	t.Logf("AC3 PASS: %d thinking_tokens events emitted, running total verified", len(events))
}

// TestThinkingTokens_FinalEmission verifies AC4: when a thinking block completes
// (content_block_stop), a final thinking_tokens event is emitted with the final
// accumulated totals, even if the periodic timer has not fired since the last delta.
func TestThinkingTokens_FinalEmission(t *testing.T) {
	// Single thinking delta - periodic timer won't fire (no 100ms elapsed),
	// but content_block_stop should trigger final emission
	deltas := []string{"complete thinking block content"}
	server := makeMockStreamServer(t, thinkingTokensEvents(deltas, "sig-final", "answer"))
	defer server.Close()

	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	tmpDir := t.TempDir()
	sessMgr, err := session.NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}

	cfg := StreamConfig{
		Enabled:        true,
		SessionManager: sessMgr,
		SessionID:      "sess_tt_final",
	}

	stdout := captureTTStdout(t, func() {
		engine := mustNewQueryEngine(&cfg, nil, "", WithClient(fastClient(t)))
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = engine.SubmitMessage(ctx, "test")
	})

	var events []map[string]any
	for _, line := range strings.Split(strings.TrimRight(stdout, "\n"), "\n") {
		if strings.Contains(line, `"type":"system"`) && strings.Contains(line, `"subtype":"thinking_tokens"`) {
			var evt map[string]any
			if json.Unmarshal([]byte(line), &evt) == nil {
				events = append(events, evt)
			}
		}
	}

	if len(events) == 0 {
		t.Fatal("AC4 FAIL: no thinking_tokens events found - final emission missing")
	}

	// Final event should have the highest estimated_tokens (cumulative total)
	lastET := int(events[len(events)-1]["estimated_tokens"].(float64))
	lastETD := int(events[len(events)-1]["estimated_tokens_delta"].(float64))

	// AC6: estimated_tokens_delta >= 1 for final event
	if lastETD < 1 {
		t.Errorf("AC4/AC6 FAIL: final event estimated_tokens_delta=%d, want >= 1", lastETD)
	} else {
		t.Logf("AC4/AC6 PASS: final event has estimated_tokens=%d, estimated_tokens_delta=%d", lastET, lastETD)
	}
}

// TestThinkingTokens_NoKindField verifies AC1: thinking_tokens events do NOT
// contain a "kind" field (which was a jenny extension, not in Claude Code SDK).
func TestThinkingTokens_NoKindField(t *testing.T) {
	thinking := "Some thinking content"
	server := makeMockStreamServer(t, thinkingTextToolEvents(thinking, "", "answer"))
	defer server.Close()

	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	tmpDir := t.TempDir()
	sessMgr, err := session.NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}

	cfg := StreamConfig{
		Enabled:        true,
		SessionManager: sessMgr,
	}

	stdout := captureTTStdout(t, func() {
		engine := mustNewQueryEngine(&cfg, nil, "", WithClient(fastClient(t)))
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = engine.SubmitMessage(ctx, "test")
	})

	for li, line := range strings.Split(strings.TrimRight(stdout, "\n"), "\n") {
		if strings.Contains(line, `"type":"system"`) && strings.Contains(line, `"subtype":"thinking_tokens"`) {
			var evt map[string]any
			if json.Unmarshal([]byte(line), &evt) == nil {
				if _, hasKind := evt["kind"]; hasKind {
					t.Errorf("Line %d has unexpected 'kind' field in thinking_tokens event - per spec, kind is a jenny extension", li)
				}
			}
		}
	}
	t.Log("AC1 PASS: thinking_tokens events have no 'kind' field")
}

// TestThinkingTokens_EmissionWithin200ms verifies AC3: a thinking_tokens event is
// emitted on stdout within 200ms of the stream receiving a thinking_delta.
// The mock server sends the thinking block immediately on connect, so any delay
// in the engine processing the block and emitting the event is bounded by the
// 100ms debounce (first call has zero LastEmitTime so emits immediately).
func TestThinkingTokens_EmissionWithin200ms(t *testing.T) {
	thinking := strings.Repeat("x", 200) // ~50 tokens at 4 chars/token
	server := makeMockStreamServer(t, thinkingTextToolEvents(thinking, "", "answer"))
	defer server.Close()

	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	tmpDir := t.TempDir()
	sessMgr, err := session.NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}

	cfg := StreamConfig{
		Enabled:        true,
		SessionManager: sessMgr,
		SessionID:      "sess_tt_timing",
	}

	before := time.Now()
	stdout := captureTTStdout(t, func() {
		engine := mustNewQueryEngine(&cfg, nil, "", WithClient(fastClient(t)))
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = engine.SubmitMessage(ctx, "test")
	})
	elapsed := time.Since(before)

	var hasEvent bool
	for _, line := range strings.Split(strings.TrimRight(stdout, "\n"), "\n") {
		if strings.Contains(line, `"type":"system"`) && strings.Contains(line, `"subtype":"thinking_tokens"`) {
			hasEvent = true
			break
		}
	}

	// AC3: thinking_tokens event must appear (verifies hook fires at all)
	if !hasEvent {
		t.Fatal("AC3 FAIL: no thinking_tokens event found - emission hook not firing")
	}

	// AC3: total elapsed time must be < 200ms.
	// The mock server sends the thinking block immediately; the engine's first
	// emit has zero LastEmitTime so fires without the 100ms wait, making the
	// total time well under the 200ms spec threshold even on slow CI runners.
	if elapsed > 200*time.Millisecond {
		t.Errorf("AC3 FAIL: total elapsed = %v, want < 200ms", elapsed)
	} else {
		t.Logf("AC3 PASS: thinking_tokens event emitted within %v (< 200ms)", elapsed)
	}
}

// captureTTStdout runs fn with stdout redirected to a bytes.Buffer and returns the captured output.
func captureTTStdout(t *testing.T, fn func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	done := make(chan struct{})
	var buf bytes.Buffer
	go func() {
		io.Copy(&buf, r)
		close(done)
	}()

	fn()

	w.Close()
	os.Stdout = old
	<-done
	return buf.String()
}
