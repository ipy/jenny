// Package agent provides the core agent loop and query engine.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ipy/jenny/internal/api"
	"github.com/ipy/jenny/internal/tool"
)

// TurnCount returns the current turn count for diagnostics.
func (e *QueryEngine) TurnCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.turnCount
}

// Model returns the resolved model name (from flags or ANTHROPIC_MODEL env var).
func (e *QueryEngine) Model() string {
	return e.model
}

func (e *QueryEngine) buildModelUsage() any {
	if e.costState == nil || e.costState.ModelUsage == nil {
		return map[string]any{}
	}
	result := make(map[string]any)
	for model, usage := range e.costState.ModelUsage {
		params := api.ModelParams(model)
		result[model] = map[string]any{
			"inputTokens":              usage.InputTokens,
			"outputTokens":             usage.OutputTokens,
			"cacheReadInputTokens":     usage.CacheReadInputTokens,
			"cacheCreationInputTokens": usage.CacheCreationInputTokens,
			"webSearchRequests":        0,
			"costUSD":                  usage.CostUSD,
			"contextWindow":            params.ContextWindow,
			"maxOutputTokens":          params.MaxOutputTokens,
		}
	}
	return result
}

// Drain waits for any in-progress memory extraction to complete.
// Used during shutdown to ensure clean termination.
func (e *QueryEngine) Drain(ctx context.Context) {
	if e.memExtractor == nil {
		return
	}
	e.memExtractor.Drain(ctx)
}

// drainTaskCompletions drains pending task completions from the TaskManager.
// AC3: Completions are injected as synthetic tool_results in the message chain.
func (e *QueryEngine) drainTaskCompletions() []tool.TaskCompletion {
	tm := e.getTaskManager()
	if tm == nil {
		return nil
	}
	return tm.DrainCompletions()
}

// finalizeAsEndTurn handles finalization for the end_turn stop reason and for
// empty/unrecognized stop_reason values (treated as terminal). It returns the
// final text result and nil error on success.
func (e *QueryEngine) finalizeAsEndTurn(ctx context.Context, resp *api.Response, textOutput strings.Builder, sessionID string, assistantMsg *api.Message, messages []api.Message) (string, error) {
	// AC3: Enforce structured output at end of turn
	if e.structuredOutputTool != nil && !e.structuredOutputTool.IsEmitted() {
		return "", fmt.Errorf("structured output not emitted")
	}
	// AC3: Determine final result - use structured output if available
	finalResult := textOutput.String()
	if e.structuredOutputTool != nil && e.structuredOutputTool.IsEmitted() && e.structuredOutputResult != "" {
		finalResult = e.structuredOutputResult
	}
	// Output final result
	if e.streamCfg.Enabled {
		usage := &Usage{
			InputTokens:              resp.Usage.InputTokens,
			OutputTokens:             resp.Usage.OutputTokens,
			CacheReadInputTokens:     resp.Usage.CacheReadInputTokens,
			CacheCreationInputTokens: resp.Usage.CacheCreationInputTokens,
			ServerToolUse:            &ServerToolUse{},
			ServiceTier:              "standard",
			CacheCreation:            &CacheCreation{},
			InferenceGeo:             "",
			Iterations:               []any{},
			Speed:                    "standard",
		}
		// Calculate TTFT (time to first token) in milliseconds
		var ttftMs int64
		if !e.firstTokenTime.IsZero() && !e.lastAPIStartTime.IsZero() {
			ttftMs = e.firstTokenTime.Sub(e.lastAPIStartTime).Milliseconds()
			if ttftMs < 0 {
				ttftMs = 0
			}
		}
		// Calculate TTFT stream (time to first stream event) in milliseconds
		var ttftStreamMs int64
		if !e.firstStreamTime.IsZero() && !e.lastAPIStartTime.IsZero() {
			ttftStreamMs = e.firstStreamTime.Sub(e.lastAPIStartTime).Milliseconds()
			if ttftStreamMs < 0 {
				ttftStreamMs = 0
			}
		}
		// Calculate time to request (pre-API processing time) in milliseconds
		var timeToRequestMs int64
		if !e.turnStartTime.IsZero() && !e.lastAPIStartTime.IsZero() {
			timeToRequestMs = e.lastAPIStartTime.Sub(e.turnStartTime).Milliseconds()
			if timeToRequestMs < 0 {
				timeToRequestMs = 0
			}
		}

		msg := StreamMessage{
			Type:            "result",
			Subtype:         "success",
			Result:          finalResult,
			SessionID:       sessionID,
			ParentToolUseID: nil,
			Uuid:            GenerateUUID(),
			Usage:           usage,
			IsError:         false,
			StopReason:      string(resp.StopReason),
			TTFTMs:          ttftMs,
			TTFTStreamMs:    ttftStreamMs,
			TimeToRequestMs: timeToRequestMs,
			TerminalReason:  mapTerminalReason(string(resp.StopReason)),
			APIErrorStatus:  nil,
			DurationMs:      time.Since(e.startTime).Milliseconds(),
			DurationAPIMs:   e.totalAPIDurationMs,
			NumTurns:        e.turnCount,
			TotalCostUSD:    e.costState.TotalCostUSD,
			ModelUsage:      e.buildModelUsage(),
			FastModeState:   "off",
		}
		data, _ := json.Marshal(msg)
		fmt.Fprintln(os.Stdout, string(data))
	}
	// AC2: Reset compaction failure counter on successful API response
	e.resetCompactFailCount()

	// Check and run memory extraction before returning.
	// StopReason is passed through verbatim (may be "" for empty stop_reason).
	if e.memExtractor != nil {
		e.memExtractor.CheckAndExtract(ctx, TurnContext{
			StopReason: resp.StopReason,

			AssistantMessage: assistantMsg,
			TotalMessages:    len(messages),
			RecentMessages:   messages,
		})
	}

	return finalResult, nil
}

// toLoopUsage converts api.Usage to *Usage (loop.go Usage type).
func toLoopUsage(src api.Usage) *Usage {
	return &Usage{
		InputTokens:              src.InputTokens,
		OutputTokens:             src.OutputTokens,
		CacheReadInputTokens:     src.CacheReadInputTokens,
		CacheCreationInputTokens: src.CacheCreationInputTokens,
	}
}

// thinkingBlock holds the text and optional signature of a thinking block
// collected during streaming or fallback processing. Used as the unit of
// emission by emitConsolidatedAssistant.
type thinkingBlock struct {
	Text      string
	Signature string
}

// emitConsolidatedAssistant writes ONE `type: "assistant"` envelope to stdout
// containing every collected block for the current API turn, in spec order:
// thinking blocks first (with omitempty signature), then the text block
// (omitted when empty), then tool_use blocks. The 17-line emission logic was
// previously duplicated at the streaming-path and fallback-path call sites;
// this helper consolidates them so envelope-shape changes only happen once.
func (e *QueryEngine) emitConsolidatedAssistant(
	sessionID string,
	thinkingBlocks []thinkingBlock,
	textOutput *strings.Builder,
	toolUseBlocks []api.ToolUseBlock,
	messageID string,
	stopReason string,
	stopSequence string,
	usage *Usage,
	model string,
) {
	if !e.streamCfg.Enabled {
		return
	}
	if len(thinkingBlocks) == 0 && textOutput.Len() == 0 && len(toolUseBlocks) == 0 {
		return
	}

	// Build content array with ordered fields per reference format
	// Reference order: thinking → text → tool_use
	// Each block needs ordered fields (type first) - use string construction to avoid map key ordering
	var contentFields []string
	for _, tb := range thinkingBlocks {
		// Reference order: type, thinking, signature
		blockFields := []string{
			`"type":"thinking"`,
			`"thinking":` + encodeString(tb.Text),
		}
		if tb.Signature != "" {
			blockFields = append(blockFields, `"signature":`+encodeString(tb.Signature))
		}
		contentFields = append(contentFields, "{"+strings.Join(blockFields, ",")+"}")
	}
	if textOutput.Len() > 0 {
		// Reference order: type, text
		contentFields = append(contentFields, `{"type":"text","text":`+encodeString(textOutput.String())+`}`)
	}
	for _, tb := range toolUseBlocks {
		// Reference order: type, id, name, input
		inputBytes, _ := json.Marshal(tb.Input)
		blockFields := []string{
			`"type":"tool_use"`,
			`"id":` + encodeString(tb.ID),
			`"name":` + encodeString(tb.Name),
			`"input":` + string(inputBytes),
		}
		contentFields = append(contentFields, "{"+strings.Join(blockFields, ",")+"}")
	}
	contentJSON := "[" + strings.Join(contentFields, ",") + "]"

	// Build full message structure per spec: id, type, role, model, content, stop_reason, stop_sequence, usage
	// Using ordered field construction to match reference format
	messageFields := []string{
		`"id":` + encodeString(messageID),
		`"type":"message"`,
		`"role":"assistant"`,
		`"model":` + encodeString(model),
		`"content":` + contentJSON,
	}

	// Always include stop_reason and stop_sequence (possibly null)
	if stopReason != "" {
		messageFields = append(messageFields, `"stop_reason":`+encodeString(stopReason))
	} else {
		messageFields = append(messageFields, `"stop_reason":null`)
	}
	if stopSequence != "" {
		messageFields = append(messageFields, `"stop_sequence":`+encodeString(stopSequence))
	} else {
		messageFields = append(messageFields, `"stop_sequence":null`)
	}

	// Include usage if present - reference order: input_tokens, cache_creation_input_tokens, cache_read_input_tokens, output_tokens, service_tier
	if usage != nil {
		usageJSON := fmt.Sprintf(`{"input_tokens":%d,"cache_creation_input_tokens":%d,"cache_read_input_tokens":%d,"output_tokens":%d,"service_tier":"standard"}`,
			usage.InputTokens, usage.CacheCreationInputTokens, usage.CacheReadInputTokens, usage.OutputTokens)
		messageFields = append(messageFields, `"usage":`+usageJSON)
	}

	messageJSON := "{" + strings.Join(messageFields, ",") + "}"
	messageObj := json.RawMessage(messageJSON)

	// Reference order for assistant: type, message, parent_tool_use_id, session_id, uuid
	msg := StreamMessage{
		Type:            "assistant",
		Message:         messageObj,
		ParentToolUseID: nil,
		SessionID:       sessionID,
		Uuid:            GenerateUUID(),
	}
	data, _ := json.Marshal(msg)
	fmt.Fprintln(os.Stdout, string(data))
}

// emitAssistantEvent emits a single assistant event for a finished content block.
// AC1: Every content block stop emits a separate assistant event sharing the same message.id.
func (e *QueryEngine) emitAssistantEvent(sessionID string, block api.ContentBlock, messageID string, model string) {
	if !e.streamCfg.Enabled {
		return
	}

	var blockFields []string
	switch block.Type {
	case api.BlockTypeThinking:
		blockFields = append(blockFields, `"type":"thinking"`)
		blockFields = append(blockFields, `"thinking":`+encodeString(block.Thinking))
		if block.Signature != "" {
			blockFields = append(blockFields, `"signature":`+encodeString(block.Signature))
		}
	case api.BlockTypeText:
		blockFields = append(blockFields, `"type":"text"`)
		blockFields = append(blockFields, `"text":`+encodeString(block.Text))
	case api.BlockTypeToolUse:
		// Ensure ToolInput is never nil in stream-json output —
		// some models return arguments=null which can leave ToolInput nil.
		if block.ToolInput == nil {
			block.ToolInput = make(map[string]any)
		}
		inputBytes, _ := json.Marshal(block.ToolInput)
		blockFields = append(blockFields, `"type":"tool_use"`)
		blockFields = append(blockFields, `"id":`+encodeString(block.ToolID))
		blockFields = append(blockFields, `"name":`+encodeString(block.ToolName))
		blockFields = append(blockFields, `"input":`+string(inputBytes))
	default:
		return
	}
	contentJSON := "[{" + strings.Join(blockFields, ",") + "}]"

	messageFields := []string{
		`"id":` + encodeString(messageID),
		`"type":"message"`,
		`"role":"assistant"`,
		`"model":` + encodeString(model),
		`"content":` + contentJSON,
		`"stop_reason":null`,
		`"stop_sequence":null`,
	}

	// Include usage if available (input tokens are usually known at start)
	// AC1: Shared message.id and consistent metadata
	usage := toLoopUsage(e.currentUsage)
	usageJSON := fmt.Sprintf(`{"input_tokens":%d,"cache_creation_input_tokens":%d,"cache_read_input_tokens":%d,"output_tokens":%d,"service_tier":"standard"}`,
		usage.InputTokens, usage.CacheCreationInputTokens, usage.CacheReadInputTokens, usage.OutputTokens)
	messageFields = append(messageFields, `"usage":`+usageJSON)

	messageJSON := "{" + strings.Join(messageFields, ",") + "}"
	msg := StreamMessage{
		Type:            "assistant",
		Message:         json.RawMessage(messageJSON),
		ParentToolUseID: nil,
		SessionID:       sessionID,
		Uuid:            GenerateUUID(),
	}
	data, _ := json.Marshal(msg)
	fmt.Fprintln(os.Stdout, string(data))
}

// emitAssistantFinalEvent emits a final assistant event with usage and stop reason.
// Used at the end of a streaming turn to provide usage metadata (AC1).
func (e *QueryEngine) emitAssistantFinalEvent(sessionID string, messageID string, stopReason string, stopSequence string, usage *Usage, model string) {
	if !e.streamCfg.Enabled {
		return
	}

	messageFields := []string{
		`"id":` + encodeString(messageID),
		`"type":"message"`,
		`"role":"assistant"`,
		`"model":` + encodeString(model),
		`"content":[]`, // Final event has empty content
	}

	// Include stop_reason and stop_sequence
	if stopReason != "" {
		messageFields = append(messageFields, `"stop_reason":`+encodeString(stopReason))
	} else {
		messageFields = append(messageFields, `"stop_reason":null`)
	}
	if stopSequence != "" {
		messageFields = append(messageFields, `"stop_sequence":`+encodeString(stopSequence))
	} else {
		messageFields = append(messageFields, `"stop_sequence":null`)
	}

	// Include usage if present
	if usage != nil {
		usageJSON := fmt.Sprintf(`{"input_tokens":%d,"cache_creation_input_tokens":%d,"cache_read_input_tokens":%d,"output_tokens":%d,"service_tier":"standard"}`,
			usage.InputTokens, usage.CacheCreationInputTokens, usage.CacheReadInputTokens, usage.OutputTokens)
		messageFields = append(messageFields, `"usage":`+usageJSON)
	}

	messageJSON := "{" + strings.Join(messageFields, ",") + "}"
	msg := StreamMessage{
		Type:            "assistant",
		Message:         json.RawMessage(messageJSON),
		ParentToolUseID: nil,
		SessionID:       sessionID,
		Uuid:            GenerateUUID(),
	}
	data, _ := json.Marshal(msg)
	fmt.Fprintln(os.Stdout, string(data))
}

// emitThinkingTokens emits a thinking_tokens system event to stdout.
// It is guarded by e.streamCfg.Enabled (AC5).
// AC3: Emits on first thinking delta arrival (no 100ms delay); subsequent emissions
// are debounced by ≥100ms. AC4: final emission on thinking block stop via emitThinkingTokensFinal.
func (e *QueryEngine) emitThinkingTokens(sessionID string, blockIndex int, newThinkingText string) {
	if !e.streamCfg.Enabled {
		return
	}

	now := time.Now()

	// Initialize state for this block index if not seen before
	if e.thinkingBlockState == nil {
		e.thinkingBlockState = make(map[int]*thinkingBlockState)
	}
	state, exists := e.thinkingBlockState[blockIndex]
	if !exists {
		state = &thinkingBlockState{
			EstimatedTokens:      0,
			AccumulatedThisCycle: 0,
			TotalTextLen:         0,
			PrevTotalTextLen:     0,
			LastEmitTime:         time.Time{}, // Zero value: first call always satisfies the OR condition
		}
		e.thinkingBlockState[blockIndex] = state
	}

	// Calculate delta tokens since last call using PrevTotalTextLen.
	// block.Thinking at each call contains the accumulated thinking text, so the
	// actual new content is the increment over the previous total.
	// Heuristic: 1 token ≈ 4 characters (conservative for thinking content).
	newTextLen := len(newThinkingText)
	deltaChars := newTextLen - state.PrevTotalTextLen
	if deltaChars < 0 {
		deltaChars = 0
	}
	deltaTokens := deltaChars / 4
	if deltaTokens < 1 && deltaChars > 0 {
		deltaTokens = 1
	}

	// Update accumulated state
	state.TotalTextLen = newTextLen
	state.PrevTotalTextLen = newTextLen
	state.EstimatedTokens += deltaTokens
	state.AccumulatedThisCycle += deltaTokens

	// AC3: Emit if first call (LastEmitTime==zero) OR ≥100ms have elapsed since last emit
	shouldEmit := state.LastEmitTime.IsZero() || now.Sub(state.LastEmitTime) >= 100*time.Millisecond
	if shouldEmit && state.AccumulatedThisCycle > 0 {
		msg := StreamMessage{
			Type:                 "system",
			Subtype:              "thinking_tokens",
			SessionID:            sessionID,
			Uuid:                 GenerateUUID(),
			EstimatedTokens:      state.EstimatedTokens,
			EstimatedTokensDelta: state.AccumulatedThisCycle,
		}
		data, _ := json.Marshal(msg)
		fmt.Fprintln(os.Stdout, string(data))

		// Reset cycle accumulator and update last emit time
		state.AccumulatedThisCycle = 0
		state.LastEmitTime = now
	}
}

// emitThinkingTokensFinal emits a final thinking_tokens event when a thinking block stops.
// AC4: Emits final totals even if the periodic timer has not fired.
// AC6: estimated_tokens_delta is always ≥ 1 when content was present.
func (e *QueryEngine) emitThinkingTokensFinal(sessionID string, blockIndex int, _ string) {
	if !e.streamCfg.Enabled {
		return
	}

	if e.thinkingBlockState == nil {
		return
	}
	state, exists := e.thinkingBlockState[blockIndex]
	if !exists {
		return
	}

	// deltaTokens = tokens accumulated since last emission (this cycle's content)
	deltaTokens := state.AccumulatedThisCycle

	// AC6: If AccumulatedThisCycle == 0 but EstimatedTokens > 0 from prior cycles,
	// we still have prior content — emit delta = 1 per spec contract (delta ≥ 1)
	if deltaTokens == 0 && state.EstimatedTokens > 0 {
		deltaTokens = 1
	}

	// AC4: EstimatedTokens already contains all content including this cycle.
	// Emit it as-is; deltaTokens carries only the last-cycle increment for the
	// delta field contract (delta ≥ 1 when content was present).
	if state.EstimatedTokens > 0 || deltaTokens > 0 {
		msg := StreamMessage{
			Type:                 "system",
			Subtype:              "thinking_tokens",
			SessionID:            sessionID,
			Uuid:                 GenerateUUID(),
			EstimatedTokens:      state.EstimatedTokens,
			EstimatedTokensDelta: deltaTokens,
		}
		data, _ := json.Marshal(msg)
		fmt.Fprintln(os.Stdout, string(data))
	}

	// Clean up state for this block
	delete(e.thinkingBlockState, blockIndex)
}

// resetThinkingBlockState clears all thinking block state.
// Called at the start of each streaming turn to avoid cross-turn pollution.
func (e *QueryEngine) resetThinkingBlockState() {
	e.thinkingBlockState = nil
}

// emitAllFinalThinkingTokens emits final thinking_tokens events for all active thinking blocks.
// AC4: Called at end of streaming turn to ensure final totals are emitted even if periodic timer hasn't fired.
func (e *QueryEngine) emitAllFinalThinkingTokens(sessionID string) {
	if !e.streamCfg.Enabled || e.thinkingBlockState == nil {
		return
	}

	// Iterate over a copy of the keys to avoid mutation during iteration
	for blockIndex := range e.thinkingBlockState {
		// Use empty string for final text - the state already has accumulated tokens
		e.emitThinkingTokensFinal(sessionID, blockIndex, "")
	}
}
