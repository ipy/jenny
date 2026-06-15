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
)

// mapTerminalReason maps the API stop_reason to the terminal_reason field.
// terminal_reason is "completed" for end_turn/stop_sequence, "max_tokens" for max_tokens,
// and empty for other/unknown stop reasons.
func mapTerminalReason(stopReason string) string {
	switch stopReason {
	case "end_turn", "stop_sequence":
		return "completed"
	case "max_tokens":
		return "max_tokens"
	default:
		return ""
	}
}

// handleStopReason processes the model's stop_reason and returns the result
// string, error, and a boolean indicating whether the loop should continue.
// When shouldContinue is true, messages is modified in-place to include any
// tool_result user messages needed for the next iteration.
func (e *QueryEngine) handleStopReason(
	ctx context.Context,
	resp *api.Response,
	streamResult api.StreamResult,
	textOutput strings.Builder,
	sessionID string,
	toolResults []api.ToolResult,
	toolUseBlocks []api.ToolUseBlock,
	thinkingBlocks []thinkingBlock,
	assistantMsg *api.Message,
	messages *[]api.Message,
) (result string, err error, shouldContinue bool) {

	switch resp.StopReason {
	case api.StopReasonEndTurn:
		return e.handleStopEndTurn(ctx, resp, textOutput, sessionID, toolResults, assistantMsg, messages)

	case api.StopReasonToolUse:
		return e.handleStopToolUse(toolResults, messages)

	case api.StopReasonMaxTokens:
		return e.handleStopMaxTokens(resp, streamResult, textOutput, sessionID)

	case api.StopReasonStopSeq:
		return e.handleStopStopSeq(ctx, resp, textOutput, sessionID, assistantMsg, messages)

	default:
		// Empty or unrecognized stop_reason: treat as end_turn (terminal).
		// Defensive: if tool_use blocks are present, continue the loop to keep
		// the chain valid (the API requires tool_use to be answered with tool_result).
		if len(toolUseBlocks) > 0 {
			if len(toolResults) > 0 {
				userMsg := buildToolResultUserMsg(toolResults)
				*messages = append(*messages, userMsg)
			}
			return "", nil, true
		}
		result, err := e.finalizeAsEndTurn(ctx, resp, textOutput, sessionID, assistantMsg, *messages)
		return result, err, false
	}
}

// handleStopEndTurn processes the end_turn stop reason: validates structured
// output, determines final result, emits NDJSON success result, resets compaction
// counter, runs memory extraction, and returns the final result.
func (e *QueryEngine) handleStopEndTurn(
	ctx context.Context,
	resp *api.Response,
	textOutput strings.Builder,
	sessionID string,
	toolResults []api.ToolResult,
	assistantMsg *api.Message,
	messages *[]api.Message,
) (string, error, bool) {
	// AC3: Enforce structured output at end of turn
	if e.structuredOutputTool != nil && !e.structuredOutputTool.IsEmitted() {
		return "", fmt.Errorf("structured output not emitted"), false
	}
	// AC3: Determine final result - use structured output if available
	var finalResult string
	if e.secretRedactor != nil && e.secretRedactor.Enabled() {
		finalResult = e.secretRedactor.Recover(textOutput.String())
		if e.structuredOutputTool != nil && e.structuredOutputTool.IsEmitted() && e.structuredOutputResult != "" {
			finalResult = e.secretRedactor.Recover(e.structuredOutputResult)
		}
	} else {
		finalResult = textOutput.String()
		if e.structuredOutputTool != nil && e.structuredOutputTool.IsEmitted() && e.structuredOutputResult != "" {
			finalResult = e.structuredOutputResult
		}
	}
	if len(toolResults) > 0 {
		// Send tool results back to model before ending
		userMsg := buildToolResultUserMsg(toolResults)
		*messages = append(*messages, userMsg)
		// end_turn means the model is done - output final result
		if e.streamCfg.Enabled {
			e.emitSuccessResult(resp, finalResult, sessionID)
		}
		// AC2: Reset compaction failure counter on successful API response
		e.resetCompactFailCount()
		return finalResult, nil, false
	}
	// Output final result
	if e.streamCfg.Enabled {
		e.emitSuccessResult(resp, finalResult, sessionID)
	}
	// AC2: Reset compaction failure counter on successful API response
	e.resetCompactFailCount()

	// Check and run memory extraction before returning
	if e.memExtractor != nil && resp.StopReason != "" {
		e.memExtractor.CheckAndExtract(ctx, TurnContext{
			StopReason:       resp.StopReason,
			AssistantMessage: assistantMsg,
			TotalMessages:    len(*messages),
			RecentMessages:   *messages,
		})
	}

	return finalResult, nil, false
}

// handleStopToolUse processes the tool_use stop reason: appends tool results
// as a user message and signals the loop to continue.
func (e *QueryEngine) handleStopToolUse(toolResults []api.ToolResult, messages *[]api.Message) (string, error, bool) {
	// Continue the loop to let the model process tool results
	if len(toolResults) > 0 {
		userMsg := buildToolResultUserMsg(toolResults)
		*messages = append(*messages, userMsg)
	}
	return "", nil, true
}

// handleStopMaxTokens processes the max_tokens stop reason: emits structured
// error_max_tokens result and returns the error.
func (e *QueryEngine) handleStopMaxTokens(resp *api.Response, streamResult api.StreamResult, textOutput strings.Builder, sessionID string) (string, error, bool) {
	// AC1: Emit structured error_max_tokens result event
	if e.streamCfg.Enabled && streamResult.MaxTokensErr != nil {
		mte := streamResult.MaxTokensErr
		threshold := e.compactConfig.autoCompactThreshold()
		errMsg := fmt.Sprintf("max tokens reached: %s", mte.Category)
		msg := StreamMessage{
			Type:            "result",
			Subtype:         "error_max_tokens",
			Result:          errMsg,
			SessionID:       sessionID,
			ParentToolUseID: nil,
			Uuid:            GenerateUUID(),
			Model:           mte.Model,
			IsError:         true,
			Usage: &Usage{
				InputTokens:              resp.Usage.InputTokens,
				OutputTokens:             mte.OutputTokens,
				CacheReadInputTokens:     resp.Usage.CacheReadInputTokens,
				CacheCreationInputTokens: resp.Usage.CacheCreationInputTokens,
			},
			StopReason:    string(resp.StopReason),
			DurationMs:    time.Since(e.startTime).Milliseconds(),
			DurationAPIMs: e.totalAPIDurationMs,
			TotalCostUSD:  e.costState.TotalCostUSD,
			ModelUsage:    e.buildModelUsage(),
			ErrorMaxTokens: &ErrorMaxTokensDetail{
				Category:        string(mte.Category),
				OutputTokens:    mte.OutputTokens,
				MaxOutputTokens: mte.MaxOutputTokens,
				InputTokens:     resp.Usage.InputTokens,
				Threshold:       threshold,
			},
		}
		data, _ := json.Marshal(msg)
		fmt.Fprintln(os.Stdout, string(data))
	}
	category := api.MaxTokensCategory("unknown")
	if streamResult.MaxTokensErr != nil {
		category = streamResult.MaxTokensErr.Category
	}
	return textOutput.String(), fmt.Errorf("max tokens reached: %s", category), false
}

// handleStopStopSeq processes the stop_sequence stop reason: emits success
// result, resets compaction counter, runs memory extraction, returns result.
func (e *QueryEngine) handleStopStopSeq(
	ctx context.Context,
	resp *api.Response,
	textOutput strings.Builder,
	sessionID string,
	assistantMsg *api.Message,
	messages *[]api.Message,
) (string, error, bool) {
	if e.streamCfg.Enabled {
		e.emitSuccessResult(resp, textOutput.String(), sessionID)
	}
	// AC2: Reset compaction failure counter on successful API response
	e.resetCompactFailCount()

	// Check and run memory extraction before returning
	if e.memExtractor != nil {
		e.memExtractor.CheckAndExtract(ctx, TurnContext{
			StopReason:       resp.StopReason,
			AssistantMessage: assistantMsg,
			TotalMessages:    len(*messages),
			RecentMessages:   *messages,
		})
	}

	result := textOutput.String()
	if e.secretRedactor != nil && e.secretRedactor.Enabled() {
		result = e.secretRedactor.Recover(textOutput.String())
	}
	return result, nil, false
}

// emitSuccessResult emits a stream-json success result event with the given
// final result and model response metadata.
func (e *QueryEngine) emitSuccessResult(resp *api.Response, finalResult, sessionID string) {
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
	// firstTokenTime is set when the first content block arrives
	// lastAPIStartTime is set at the start of the API call
	var ttftMs int64
	if !e.firstTokenTime.IsZero() && !e.lastAPIStartTime.IsZero() {
		ttftMs = e.firstTokenTime.Sub(e.lastAPIStartTime).Milliseconds()
		if ttftMs < 0 {
			ttftMs = 0
		}
	}

	// Calculate TTFT stream (time to first stream event) in milliseconds
	// firstStreamTime is set when stream_request_start is emitted (R2 fix)
	var ttftStreamMs int64
	if !e.firstStreamTime.IsZero() && !e.lastAPIStartTime.IsZero() {
		ttftStreamMs = e.firstStreamTime.Sub(e.lastAPIStartTime).Milliseconds()
		if ttftStreamMs < 0 {
			ttftStreamMs = 0
		}
	}

	// Calculate time to request (pre-API processing time) in milliseconds
	// turnStartTime is set at the start of each turn iteration
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
		Result:         finalResult,
		SessionID:       sessionID,
		Uuid:            GenerateUUID(),
		Usage:           usage,
		IsError:         false,
		StopReason:      string(resp.StopReason),
		TTFTMs:          ttftMs,
		TTFTStreamMs:    ttftStreamMs,
		TimeToRequestMs: timeToRequestMs,
		TerminalReason:  mapTerminalReason(string(resp.StopReason)),
		APIErrorStatus:  nil, // null on success
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
