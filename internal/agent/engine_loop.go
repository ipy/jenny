// Package agent provides the core agent loop and query engine.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ipy/jenny/internal/api"
	"github.com/ipy/jenny/internal/git"
	"github.com/ipy/jenny/internal/log"
	"github.com/ipy/jenny/internal/memdir"
	"github.com/ipy/jenny/internal/session"
	"github.com/ipy/jenny/internal/utils"
)

// forkMaxTokens is the max_tokens override for side-query forks (complexity eval,
// progress eval). Set high enough that thinking models don't starve the JSON output:
// the prompt + expected response is ~200 tokens, but thinking models share the output
// budget between thinking blocks and visible text, so a larger headroom is needed.
const forkMaxTokens = 1024

// SubmitMessage runs a single query turn: persist message, run agent loop,
// flush state on completion. Returns the text result and error.
func (e *QueryEngine) SubmitMessage(ctx context.Context, prompt string) (string, error) {
	e.mu.Lock()
	// Reset turn counter and start time for this submit
	e.turnCount = 0
	e.startTime = time.Now()
	sessionID := e.streamCfg.SessionID
	isResume := e.streamCfg.IsResume
	sessionManager := e.sessionManager
	historyMessages := e.streamCfg.HistoryMessages
	e.mu.Unlock()

	// Get working directory
	cwd, err := os.Getwd()
	if err != nil {
		cwd, _ = os.UserHomeDir()
	}
	e.mu.Lock()
	e.cwd = cwd
	e.mu.Unlock()

	// Persist user message to transcript BEFORE any API call
	if sessionManager != nil {
		// For resume sessions, check for duplicate user message
		skipUserPersist := false
		if isResume {
			exists, err := sessionManager.UserMessageExists(sessionID, prompt)
			if err != nil {
				return "", fmt.Errorf("checking for duplicate user message: %w", err)
			}
			skipUserPersist = exists
		}
		if !skipUserPersist {
			if err := sessionManager.AppendEntry(sessionID, session.TranscriptEntry{
				Type:    session.EntryTypeUser,
				Content: prompt,
				CWD:     cwd,
			}); err != nil {
				return "", fmt.Errorf("persisting user message to transcript: %w", err)
			}
		}
	}

	// Create memdir and inject memory content into system prompt
	if e.streamCfg.AutoMemoryEnabled {
		if gitRoot, err := git.GetRoot(cwd); err == nil {
			memdirCfg := memdir.Config{
				ProjectRoot:       gitRoot,
				AutoMemoryEnabled: true,
				DisableAutoMemory: e.streamCfg.DisableAutoMemory,
			}
			if m, err := memdir.New(memdirCfg); err == nil {
				_ = m.Create()
				// Read memory content to inject into system prompt
				if indexContent, err := m.ReadIndex(); err == nil && indexContent != "" {
					e.streamCfg.MemoryContent = indexContent
				}
			}
			// Initialize memory extractor with project root
			e.initMemoryExtractor(gitRoot)
		}
	}

	// Build messages slice - use history if resuming, otherwise start fresh
	var messages []api.Message
	if len(historyMessages) > 0 {
		messages = historyMessages

		// Inject system reminders for environment changes detected on resume.
		// All reminders are merged into a single <system-reminder> block and
		// sent as one virtual user message, preserving prompt cache integrity.
		if isResume {
			reminders := e.detectResumeChanges(cwd)
			if len(reminders) > 0 {
				// Store bare reminders joined by newlines (no <system-reminder> wrapper),
				// consistent with compact path and cross-turn skill-change path.
				// The restore code wraps with <system-reminder> so the model sees the full block.
				bareContent := strings.Join(reminders, "\n")
				messages = append(messages, api.Message{
					Role:      api.RoleUser,
					Content:   "<system-reminder>\n" + bareContent + "\n</system-reminder>",
					IsVirtual: true,
				})
				e.persistSystemReminder(sessionID, bareContent)
			}
		}

		messages = append(messages, api.Message{
			Role:    api.RoleUser,
			Content: prompt,
		})
	} else {
		messages = []api.Message{
			{
				Role:    api.RoleUser,
				Content: prompt,
			},
		}
	}

	// Run the agent loop
	result, err := e.runLoop(ctx, messages, cwd, sessionID, "user")

	// Flush cost state on completion (success, error, or limit exceeded)
	e.mu.Lock()
	e.costState.LastSessionID = sessionID
	e.mu.Unlock()
	_ = SaveCostState(e.costState)

	return result, err
}

// runLoop implements the core agent loop. It iterates with the API,
// executing tools and accumulating cost, until the model signals
// end_turn or stop_sequence, or a limit is reached.
// querySource indicates the origin of the request ("user", "compact", "session_memory").
func (e *QueryEngine) runLoop(ctx context.Context, messages []api.Message, cwd, sessionID, querySource string) (string, error) {
	systemPromptBlocks := AssembleSystemPrompt(e.streamCfg, e.tools, cwd)
	// Freeze the system prompt after first assembly so that subsequent turns
	// within the same session receive an identical string, protecting prompt caching.
	if len(e.streamCfg.CachedSystemPrompt) == 0 {
		e.streamCfg.CachedSystemPrompt = systemPromptBlocks
		// Persist frozen system prompt and tool names to transcript for cross-process resume
		if e.sessionManager != nil && sessionID != "" {
			fullPrompt := strings.Join(systemPromptBlocks, "\n\n")
			_ = e.sessionManager.AppendSystemPrompt(sessionID, fullPrompt)
			_ = e.sessionManager.AppendToolNames(sessionID, e.frozenToolNames())
		}
	}

	// For providers that don't support multi-block system prompt, we join them
	// but currently providers handle the slice.
	systemPrompt := e.streamCfg.CachedSystemPrompt

	// When stream-json mode is active, redirect debug logs to stderr
	// to prevent interleaving with NDJSON output on stdout
	if e.streamCfg.Enabled {
		log.SetOutput(os.Stderr)
	}

	maxIterations := e.streamCfg.MaxIterations

	for i := 0; maxIterations <= 0 || i < maxIterations; i++ {
		// Record turn start time for time_to_request_ms calculation
		e.turnStartTime = time.Now()
		// Reset firstStreamTime and firstTokenTime for this turn
		e.firstStreamTime = time.Time{}
		e.firstTokenTime = time.Time{}

		// Check if context is already cancelled/timed out before attempting API call
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		e.mu.Lock()
		// maxTurns enforcement - check before each API call
		if e.maxTurns > 0 && e.turnCount >= e.maxTurns {
			e.mu.Unlock()
			if e.streamCfg.Enabled {
				errStr := fmt.Sprintf("Maximum number of turns (%d) reached. stopping.", e.maxTurns)
				msg := StreamMessage{
					Type:            "result",
					Subtype:         "error_max_turns",
					Result:          errStr,
					SessionID:       sessionID,
					ParentToolUseID: nil,
					Uuid:            GenerateUUID(),
					Model:           e.model,
					IsError:         true,
					NumTurns:        e.turnCount,
					StopReason:      "max_turns",
					TTFTMs:          0,
					TerminalReason:  "",
					APIErrorStatus:  &errStr,
					DurationMs:      time.Since(e.startTime).Milliseconds(),
					DurationAPIMs:   e.totalAPIDurationMs,
					TotalCostUSD:    e.costState.TotalCostUSD,
					ModelUsage:      e.buildModelUsage(),
				}
				data, _ := json.Marshal(msg)
				fmt.Fprintln(os.Stdout, string(data))
			}
			return "", fmt.Errorf("error_max_turns: limit reached at turn %d", e.turnCount)
		}
		// Increment turn counter at start of each API iteration
		e.turnCount++
		budgetUSD := e.streamCfg.MaxBudgetUSD
		e.mu.Unlock()

		// Reset structured output tool at start of each turn
		if e.structuredOutputTool != nil {
			e.structuredOutputTool.Reset()
		}

		// Budget enforcement - check before each API call
		if budgetUSD > 0 {
			if exceeded, _ := CheckBudgetExceeded(e.costState, budgetUSD); exceeded {
				if e.streamCfg.Enabled {
					errStr := fmt.Sprintf("budget exceeded: %.4f USD > %.4f USD limit", e.costState.TotalCostUSD, budgetUSD)
					msg := StreamMessage{
						Type:            "result",
						Subtype:         "error",
						Result:          errStr,
						SessionID:       sessionID,
						ParentToolUseID: nil,
						Uuid:            GenerateUUID(),
						Model:           e.model,
						IsError:         true,
						StopReason:      "budget_exceeded",
						TTFTMs:          0,
						TerminalReason:  "",
						APIErrorStatus:  &errStr,
						DurationMs:      time.Since(e.startTime).Milliseconds(),
						DurationAPIMs:   e.totalAPIDurationMs,
						TotalCostUSD:    e.costState.TotalCostUSD,
						ModelUsage:      e.buildModelUsage(),
					}
					data, _ := json.Marshal(msg)
					fmt.Fprintln(os.Stdout, string(data))
				}
				return "", fmt.Errorf("error_budget_exceeded: %.4f USD > %.4f USD limit", e.costState.TotalCostUSD, budgetUSD)
			}
		}

		// Inject pending task completions as a virtual user message
		// with Content (not ToolResults) to avoid violating API protocol:
		// tool_result blocks require their tool_use_id to reference an
		// actual tool_use in a preceding assistant message.
		completions := e.drainTaskCompletions()
		if len(completions) > 0 {
			var sb strings.Builder
			for _, c := range completions {
				if sb.Len() > 0 {
					sb.WriteString("\n")
				}
				sb.WriteString(fmt.Sprintf(
					`<task_completed task_id="%s" duration_seconds="%.1f" exit_code="%d"/>`,
					c.TaskID, c.DurationSeconds, c.ExitCode,
				))
			}
			messages = append(messages, api.Message{
				Role:      api.RoleUser,
				Content:   sb.String(),
				IsVirtual: true,
			})
		}

		// Progress monitor: dispatch L1-second fork-judge if needed, then drain results
		if e.progressMonitor.ShouldForkProgressEval() {
			e.progressMonitor.MarkForkDispatched()
			forkMessages := make([]api.Message, len(messages))
			copy(forkMessages, messages)
			forkModel := e.model
			if e.requestedModel != "" {
				forkModel = e.requestedModel
			}
			forkTools := e.toolParams
			parentCtx := ctx
			go func() {
				forkCtx, forkCancel := context.WithTimeout(context.WithoutCancel(parentCtx), 30*time.Second)
				defer forkCancel()
				forkClient, err := api.NewClientWithModel(forkModel)
				if err != nil {
					log.Debug("Progress eval fork client creation failed", "error", err)
					return
				}
				forkClient.SetMaxTokensOverride(forkMaxTokens)
				forkClient.SetThinkingConfig(api.ThinkingConfig{
					Effort:       e.streamCfg.Effort,
					BudgetTokens: e.streamCfg.ThinkingBudget,
				})
				result, model, usage, err := forkProgressEval(forkCtx, forkClient, forkMessages, systemPrompt, forkTools)
				if err != nil {
					log.Debug("Progress eval fork failed", "error", err)
					return
				}
				if parentCtx.Err() != nil {
					log.Debug("Progress eval completed but session already ended, discarding result")
					return
				}
				// Accumulate cost for the side request
				e.mu.Lock()
				if model != "" {
					AccumulateUsage(e.costState, model, usage)
				}
				totalCost := e.costState.TotalCostUSD
				modelUsage := e.buildModelUsage()
				e.mu.Unlock()
				// Send result to main loop via channel (non-blocking, drop if full)
				select {
				case e.progressMonitor.forkResultCh <- result:
				default:
				}
				log.Info("Progress eval result",
					"overall", result.Overall,
					"meaningful_progress", result.MeaningfulProgress,
					"reasoning", result.Reasoning,
				)
				if e.streamCfg.Enabled {
					msg := StreamMessage{
						Type:         "system",
						Subtype:      "progress_eval",
						Result:       progressEvalResultJSON(result),
						Uuid:         GenerateUUID(),
						TotalCostUSD: totalCost,
						ModelUsage:   modelUsage,
					}
					data, _ := json.Marshal(msg)
					fmt.Fprintln(os.Stdout, string(data))
				}
			}()
		}

		// L3 fork-judge: dispatch before hard termination to give agent a last chance
		if e.progressMonitor.ShouldForkL3ProgressEval() {
			e.progressMonitor.MarkL3ForkDispatched()
			forkMessages := make([]api.Message, len(messages))
			copy(forkMessages, messages)
			forkModel := e.model
			if e.requestedModel != "" {
				forkModel = e.requestedModel
			}
			forkTools := e.toolParams
			parentCtx := ctx
			go func() {
				forkCtx, forkCancel := context.WithTimeout(context.WithoutCancel(parentCtx), 30*time.Second)
				defer forkCancel()
				forkClient, err := api.NewClientWithModel(forkModel)
				if err != nil {
					log.Debug("L3 progress eval fork client creation failed", "error", err)
					return
				}
				forkClient.SetMaxTokensOverride(forkMaxTokens)
				forkClient.SetThinkingConfig(api.ThinkingConfig{
					Effort:       e.streamCfg.Effort,
					BudgetTokens: e.streamCfg.ThinkingBudget,
				})
				result, model, usage, err := forkProgressEval(forkCtx, forkClient, forkMessages, systemPrompt, forkTools)
				if err != nil {
					log.Debug("L3 progress eval fork failed", "error", err)
					return
				}
				if parentCtx.Err() != nil {
					log.Debug("L3 progress eval completed but session already ended, discarding result")
					return
				}
				e.mu.Lock()
				if model != "" {
					AccumulateUsage(e.costState, model, usage)
				}
				totalCost := e.costState.TotalCostUSD
				modelUsage := e.buildModelUsage()
				e.mu.Unlock()
				select {
				case e.progressMonitor.forkResultCh <- result:
				default:
				}
				log.Info("L3 progress eval result",
					"overall", result.Overall,
					"meaningful_progress", result.MeaningfulProgress,
					"reasoning", result.Reasoning,
				)
				if e.streamCfg.Enabled {
					msg := StreamMessage{
						Type:         "system",
						Subtype:      "progress_eval",
						Result:       progressEvalResultJSON(result),
						Uuid:         GenerateUUID(),
						TotalCostUSD: totalCost,
						ModelUsage:   modelUsage,
					}
					data, _ := json.Marshal(msg)
					fmt.Fprintln(os.Stdout, string(data))
				}
			}()
		}

		// Drain any pending fork result before checking escalation
		e.progressMonitor.DrainForkResult()

		if msg, terminate := e.progressMonitor.CheckAndInject(); terminate {
			return e.terminateProgressMonitor(sessionID)
		} else if msg != nil {
			messages = append(messages, *msg)
			e.persistSystemReminder(sessionID, msg.Content)
		}

		// Check compaction threshold before API request
		// Estimate tokens and check if auto-compact should trigger
		var estimatedTokens int
		var preAPICharCount int // cached for EWMA Observe after API response
		if e.tokenEstimator != nil {
			estimatedTokens, preAPICharCount = e.tokenEstimator.EstimateMessagesWithChars(messages, e.requestedModel)
		} else {
			estimatedTokens = estimateTokens(messages)
		}

		// Emit warning if approaching threshold
		if e.compactConfig.checkWarningThreshold(estimatedTokens) {
			EmitCompactWarning(estimatedTokens, e.compactConfig.warningThreshold())
		}

		// Check blocking limit when auto-compact is disabled
		// compact/session_memory sources skip this check
		if err := e.compactConfig.blockIfOverLimit(estimatedTokens, querySource); err != nil {
			return "", err
		}

		// Auto-compact if threshold exceeded and circuit breaker not tripped
		if e.compactConfig.checkCompactThreshold(estimatedTokens, querySource) {
			e.mu.Lock()
			circuitBreakerTripped := e.compactFailCount >= MAX_CONSECUTIVE_AUTOCOMPACT_FAILURES
			e.mu.Unlock()

			if !circuitBreakerTripped {
				// Capture pre-compaction tokens for boundary persistence
				preCompactTokens := estimatedTokens

				// Attempt compaction
				compacted, err := e.compactMessages(ctx, messages, e.compactConfig, systemPrompt)
				if err == nil {
					summaryText := extractCompactSummary(compacted)
					messages = normalizeCompactedChain(compacted)
					preservedCount := len(compacted) - 1
					if err := e.persistCompactBoundary(preCompactTokens, preservedCount, "auto", summaryText); err != nil {
						log.Error("Failed to persist compaction boundary", "error", err)
					}
					// Re-inject active skills after compaction since the activation
					// tool results may have been summarized away.
					if reminder := activeSkillsSection(e.streamCfg.ActiveSkills); reminder != "" {
						messages = append(messages, api.Message{
							Role:      api.RoleUser,
							Content:   "<system-reminder>\n" + reminder + "\n</system-reminder>",
							IsVirtual: true,
						})
						e.persistSystemReminder(sessionID, reminder)
					}
					log.Debug("Context compaction succeeded", "newMessageCount", len(messages))
				} else {
					// Compaction failed - increment failure counter with persistence
					e.incrementCompactFailCount()
					e.mu.Lock()
					log.Warn("Context compaction failed", "error", err, "consecutiveFailures", e.compactFailCount)
					e.mu.Unlock()
				}
			} else {
				log.Debug("Auto-compact skipped: circuit breaker tripped")
			}
		}

		// Normalize messages before API request (content-level only, no structural transforms).
		// NormalizeMessagesAPI (which includes mergeConsecutiveSameRole) is intentionally
		// NOT called here — structural normalization would destroy cache continuity.
		// It is only used by the compaction path via normalizeCompactedChain.
		for i := range messages {
			messages[i] = normalizeNewMessage(messages[i])
		}

		// Create fallback function for streaming failures
		fallbackFn := func(fallbackCtx context.Context) (*api.Response, error) {
			return e.client.SendMessage(fallbackCtx, messages, e.toolParams, nil, systemPrompt, "")
		}

		// Dynamic token budget: compute and set thinking + completion budgets
		turnBudget := e.maxTokensCtrl.ComputeBudget(e.turnCount)
		e.client.SetMaxTokensOverride(turnBudget.TotalBudget)
		e.client.SetDynamicThinkingBudget(turnBudget.ThinkingBudget)

		// Use streaming API
		// Track API call duration
		apiStartTime := time.Now()
		// Reset timing fields for TTFT calculation per API call
		e.firstTokenTime = time.Time{}
		e.lastAPIStartTime = apiStartTime
		// Defense-in-depth: wrap the API call with an overall timeout so the
		// engine loop never blocks indefinitely on a single streaming request.
		apiCtx, apiCancel := context.WithTimeout(ctx, api.DefaultFallbackTimeout+30*time.Second)
		dynamicSuffix := DynamicSystemSuffix(e.streamCfg, cwd)
		blocksChan, streamResult := e.client.SendMessageStream(
			apiCtx,
			messages,
			e.toolParams,
			nil,
			systemPrompt,
			dynamicSuffix,
			api.DefaultIdleTimeout,
			api.DefaultFallbackTimeout,
			fallbackFn,
		)

		// Process streaming blocks
		var textOutput strings.Builder
		var toolResults []api.ToolResult
		var toolUseBlocks []api.ToolUseBlock
		var thinkingBlocks []thinkingBlock

		// Reset thinking block state at the start of each streaming turn
		e.resetThinkingBlockState()

		// Process blocks as they arrive
		for block := range blocksChan {
			// Capture message ID, usage, and stop reason from raw events for assistant events
			if block.Type == "stream_event" {
				if ev, ok := block.RawEvent.(api.AnthropicStreamEvent); ok {
					switch ev.Type {
					case api.EventMessageStart:
						if ev.Message != nil {
							e.currentMessageID = ev.Message.ID
							e.currentUsage = api.Usage{
								InputTokens:              ev.Message.Usage.InputTokens,
								CacheReadInputTokens:     ev.Message.Usage.CacheReadInputTokens,
								CacheCreationInputTokens: ev.Message.Usage.CacheCreationInputTokens,
							}
						}
					case api.EventMessageDelta:
						if ev.Delta != nil {
							e.currentStopReason = ev.Delta.StopReason
							e.currentStopSequence = ev.Delta.StopSequence
						}
						if ev.Usage != nil && ev.Usage.OutputTokens > 0 {
							e.currentUsage.OutputTokens = ev.Usage.OutputTokens
						}
					}
				}
			}

			// Handle raw stream_event passthrough
			if block.Type == "stream_event" && e.streamCfg.Enabled && e.streamCfg.IncludePartial {
				// Transform SDK event to minimal representation
				transformedEvent, err := TransformStreamEvent(block.RawEvent)
				if err != nil {
					continue
				}
				// Emit spec-compliant stream_event wire shape
				msg := StreamMessage{
					Type:            "stream_event",
					SessionID:       sessionID,
					ParentToolUseID: nil,
					Uuid:            GenerateUUID(),
					Event:           transformedEvent,
				}
				data, err := json.Marshal(msg)
				if err == nil {
					fmt.Fprintln(os.Stdout, string(data))
				}
				continue
			}

			switch block.Block.Type {
			case api.BlockTypeText:
				// Track TTFT: record time when first content block arrives
				if e.firstTokenTime.IsZero() {
					e.firstTokenTime = time.Now()
				}
				// Redact secrets from text output before writing
				var text string
				if e.secretRedactor != nil && e.secretRedactor.Enabled() {
					text = e.secretRedactor.Redact(block.Block.Text)
				} else {
					text = block.Block.Text
				}
				textOutput.WriteString(text)
				// Emit assistant event for text block stop
				if e.streamCfg.Enabled && text != "" {
					e.emitAssistantEvent(sessionID, api.ContentBlock{
						Type: api.BlockTypeText,
						Text: text,
					}, e.currentMessageID, e.model)
				}
			case api.BlockTypeThinking:
				// Track TTFT: record time when first content block arrives
				if e.firstTokenTime.IsZero() {
					e.firstTokenTime = time.Now()
				}
				// Track first stream event time (for ttft_stream_ms calculation)
				if e.firstStreamTime.IsZero() {
					e.firstStreamTime = time.Now()
				}
				// Emit thinking_tokens event when thinking block arrives (streaming delta)
				e.emitThinkingTokens(sessionID, block.Index, block.Block.Thinking)
				// When IncludePartial is true, thinking blocks are already emitted via thinking_tokens events,
				// so don't accumulate them for emitConsolidatedAssistant to avoid duplication.
				if !e.streamCfg.IncludePartial {
					thinkingBlocks = append(thinkingBlocks, thinkingBlock{Text: block.Block.Thinking, Signature: block.Block.Signature})
				}
				// Emit assistant event for thinking block stop
				if e.streamCfg.Enabled {
					e.emitAssistantEvent(sessionID, block.Block, e.currentMessageID, e.model)
				}
			case api.BlockTypeToolUse:
				// Track TTFT: record time when first content block arrives
				if e.firstTokenTime.IsZero() {
					e.firstTokenTime = time.Now()
				}
				// Track first stream event time (for ttft_stream_ms calculation)
				if e.firstStreamTime.IsZero() {
					e.firstStreamTime = time.Now()
				}
				// Collect tool_use blocks for the assistant message
				toolUseBlocks = append(toolUseBlocks, api.ToolUseBlock{
					ID:        block.Block.ToolID,
					Name:      block.Block.ToolName,
					Input:     block.Block.ToolInput,
					Signature: block.Block.Signature,
				})
				// Emit assistant event for tool_use block stop
				if e.streamCfg.Enabled {
					e.emitAssistantEvent(sessionID, block.Block, e.currentMessageID, e.model)
				}
			case api.BlockTypeWebSearchResult:
				// Process web search results and surface error codes
				if block.Block.WebSearchResult != nil && block.Block.WebSearchResult.IsError {
					// Surface server error code as a tool result
					toolResults = append(toolResults, api.ToolResult{
						ToolUseID: block.Block.WebSearchResult.ToolUseID,
						Content:   fmt.Sprintf("web search error: %s", block.Block.WebSearchResult.ErrorCode),
						IsError:   true,
					})
				}
			}
		}
		e.totalAPIDurationMs += time.Since(apiStartTime).Milliseconds()
		apiCancel()

		// Emit final thinking_tokens events for all active thinking blocks
		// before the consolidated assistant message. This ensures final totals are
		// emitted even if the periodic timer hasn't fired since last delta.
		e.emitAllFinalThinkingTokens(sessionID)

		// Emit final assistant event with usage and stop_reason in stream-json mode
		if e.streamCfg.Enabled {
			e.emitAssistantFinalEvent(sessionID, e.currentMessageID, e.currentStopReason, e.currentStopSequence, toLoopUsage(e.currentUsage), e.model)
		}

		var resp *api.Response

		// Check if streaming completed with error
		if streamResult.Error != "" && len(streamResult.Blocks) == 0 {
			category := streamResult.ErrorCategory
			if streamResult.ErrorInfo != nil {
				category = streamResult.ErrorInfo.Category
			}

			// ModelNotFound re-entry via non-streaming fallback
			if category == api.CategoryModelNotFound {
				e.mu.Lock()
				initialModel := e.model
				e.mu.Unlock()

				var err error
				resp, err = e.client.SendMessage(ctx, messages, e.toolParams, nil, systemPrompt, "")
				if err == nil {
					// Fallback success: continue turn with this response
					e.mu.Lock()
					e.model = resp.Model
					e.mu.Unlock()

					// Sync streamResult for handleStopReason downstream
					streamResult.Blocks = resp.Content
					streamResult.StopReason = resp.StopReason
					streamResult.Usage = resp.Usage
					streamResult.Model = resp.Model
					streamResult.Error = ""
					streamResult.ErrorCategory = ""
					streamResult.ErrorInfo = nil

					goto handle_resp
				}

				// Fallback failed: list all attempted models if it's still ModelNotFound
				var httpErr *api.HTTPError
				if errors.As(err, &httpErr) && httpErr.ErrorCategory == api.CategoryModelNotFound {
					return "", &ModelNotFoundError{Attempted: []string{initialModel}}
				}
				return "", err
			}

			// Quota/Payment fast-fail
			if category == api.CategoryQuotaExhausted || category == api.CategoryPaymentRequired {
				errMsg := "Quota exceeded. Check your billing settings."
				if e.streamCfg.Enabled {
					errSubtype := "error_quota_exhausted"
					if category == api.CategoryPaymentRequired {
						errSubtype = "error_payment_required"
					}
					msg := StreamMessage{
						Type:            "result",
						Subtype:         errSubtype,
						Result:          errMsg,
						SessionID:       sessionID,
						ParentToolUseID: nil,
						Uuid:            GenerateUUID(),
						Model:           e.model,
						IsError:         true,
						StopReason:      "error",
						APIErrorStatus:  &errMsg,
						DurationMs:      time.Since(e.startTime).Milliseconds(),
						DurationAPIMs:   e.totalAPIDurationMs,
						TotalCostUSD:    e.costState.TotalCostUSD,
						ModelUsage:      e.buildModelUsage(),
					}
					data, _ := json.Marshal(msg)
					fmt.Fprintln(os.Stdout, string(data))
				}
				return "", fmt.Errorf("%s", errMsg)
			}

			// Content filter fast-fail
			if category == api.CategoryContentFilter {
				errMsg := "Content blocked by provider policy."
				if e.streamCfg.Enabled {
					msg := StreamMessage{
						Type:            "result",
						Subtype:         "error_content_filter",
						Result:          errMsg,
						SessionID:       sessionID,
						ParentToolUseID: nil,
						Uuid:            GenerateUUID(),
						Model:           e.model,
						IsError:         true,
						StopReason:      "error",
						APIErrorStatus:  &errMsg,
						DurationMs:      time.Since(e.startTime).Milliseconds(),
						DurationAPIMs:   e.totalAPIDurationMs,
						TotalCostUSD:    e.costState.TotalCostUSD,
						ModelUsage:      e.buildModelUsage(),
					}
					data, _ := json.Marshal(msg)
					fmt.Fprintln(os.Stdout, string(data))
				}
				return "", fmt.Errorf("%s", errMsg)
			}

			// Check if this is a context_exhausted error from HTTP 400 rejection
			if streamResult.MaxTokensErr != nil && streamResult.MaxTokensErr.Category == api.CategoryContextExhausted {
				// Attempt emergency compaction before giving up
				if !e.compactConfig.DisableCompact {
					log.Warn("Context exhausted (pre-stream), attempting emergency compaction",
						"messageCount", len(messages),
					)
					compacted, err := e.emergencyCompact(ctx, messages, systemPrompt)
					if err == nil {
						messages = compacted
						log.Info("Emergency compaction succeeded on pre-stream error, retrying")
						continue
					}
					log.Error("Emergency compaction failed on pre-stream error", "error", err)
				}

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
						InputTokens:              streamResult.Usage.InputTokens,
						OutputTokens:             mte.OutputTokens,
						CacheReadInputTokens:     streamResult.Usage.CacheReadInputTokens,
						CacheCreationInputTokens: streamResult.Usage.CacheCreationInputTokens,
					},
					StopReason:    "max_tokens",
					DurationMs:    time.Since(e.startTime).Milliseconds(),
					DurationAPIMs: e.totalAPIDurationMs,
					TotalCostUSD:  e.costState.TotalCostUSD,
					ModelUsage:    e.buildModelUsage(),
					ErrorMaxTokens: &ErrorMaxTokensDetail{
						Category:        string(mte.Category),
						OutputTokens:    mte.OutputTokens,
						MaxOutputTokens: mte.MaxOutputTokens,
						InputTokens:     streamResult.Usage.InputTokens,
						Threshold:       threshold,
					},
				}
				data, _ := json.Marshal(msg)
				fmt.Fprintln(os.Stdout, string(data))
				e.incrementCompactFailCount()
				return "", fmt.Errorf("max tokens reached: %s", mte.Category)
			}

			// Emit standard error result for other streaming errors
			if e.streamCfg.Enabled {
				errStr := fmt.Sprintf("streaming error: %v", streamResult.Error)
				msg := StreamMessage{
					Type:            "result",
					Subtype:         "error",
					Result:          errStr,
					SessionID:       sessionID,
					ParentToolUseID: nil,
					Uuid:            GenerateUUID(),
					Model:           e.model,
					IsError:         true,
					StopReason:      "error",
					TTFTMs:          0,
					TerminalReason:  "",
					APIErrorStatus:  &errStr,
					DurationMs:      time.Since(e.startTime).Milliseconds(),
					DurationAPIMs:   e.totalAPIDurationMs,
					TotalCostUSD:    e.costState.TotalCostUSD,
					ModelUsage:      e.buildModelUsage(),
				}
				data, _ := json.Marshal(msg)
				fmt.Fprintln(os.Stdout, string(data))
			}

			// Check if this is a media error - if so, strip the offending tool_result and retry
			var wasMediaError bool
			messages, wasMediaError = HandleMediaErrorOnRetry(messages, streamResult.Error)
			if wasMediaError {
				continue // Retry with modified messages
			}
			// Non-user-abort error - increment compaction failure counter
			// Skip increment for user-initiated aborts (context cancellation, Esc, SIGINT, etc.)
			if !utils.IsUserAbortError(streamResult.Error) {
				e.incrementCompactFailCount()
			}
			return "", fmt.Errorf("streaming error: %v", streamResult.Error)
		}

	handle_resp:
		// Use results from streaming (or fallback)
		if resp == nil {
			resp = &api.Response{
				Content:    streamResult.Blocks,
				StopReason: streamResult.StopReason,
				Usage:      streamResult.Usage,
				Model:      streamResult.Model,
			}
		}

		// Fallback block processing: if blocksChan was empty but resp.Content has content
		if textOutput.Len() == 0 && len(toolUseBlocks) == 0 && len(resp.Content) > 0 {
			// Collect pending web search results to emit user wrappers after assistant
			var pendingWebSearchResults []api.ContentBlock

			for _, block := range resp.Content {
				switch block.Type {
				case api.BlockTypeText:
					// Redact secrets from text output in fallback path
					if e.secretRedactor != nil && e.secretRedactor.Enabled() {
						textOutput.WriteString(e.secretRedactor.Redact(block.Text))
					} else {
						textOutput.WriteString(block.Text)
					}
				case api.BlockTypeThinking:
					thinkingBlocks = append(thinkingBlocks, thinkingBlock{Text: block.Thinking, Signature: block.Signature})
				case api.BlockTypeToolUse:
					// Collect tool_use blocks for the assistant message
					toolUseBlocks = append(toolUseBlocks, api.ToolUseBlock{
						ID:        block.ToolID,
						Name:      block.ToolName,
						Input:     block.ToolInput,
						Signature: block.Signature,
					})
				case api.BlockTypeWebSearchResult:
					// Process web search results and surface error codes in fallback
					if block.WebSearchResult != nil && block.WebSearchResult.IsError {
						toolResults = append(toolResults, api.ToolResult{
							ToolUseID: block.WebSearchResult.ToolUseID,
							Content:   fmt.Sprintf("web search error: %s", block.WebSearchResult.ErrorCode),
							IsError:   true,
						})
					}
					pendingWebSearchResults = append(pendingWebSearchResults, block)
				}
			}

			// Emit ONE consolidated assistant message for all collected content
			e.emitConsolidatedAssistant(sessionID, thinkingBlocks, &textOutput, toolUseBlocks,
				e.currentMessageID, e.currentStopReason, e.currentStopSequence, toLoopUsage(e.currentUsage), e.model)

			// Emit user message wrappers for web search tool results
			for _, result := range pendingWebSearchResults {
				if result.WebSearchResult == nil {
					continue
				}
				var toolResultContent any
				if result.WebSearchResult.IsError {
					toolResultContent = fmt.Sprintf("web search error: %s", result.WebSearchResult.ErrorCode)
				} else {
					// Non-error case: pass through the block's text content if present
					toolResultContent = result.Text
				}
				userContent := []map[string]any{
					{"type": api.BlockTypeToolResult, "tool_use_id": result.WebSearchResult.ToolUseID, "content": toolResultContent},
				}
				// Format tool_use_result for user event
				var toolUseResult any
				if result.WebSearchResult.IsError {
					toolUseResult = fmt.Sprintf("web search error: %s", result.WebSearchResult.ErrorCode)
				} else {
					toolUseResult = map[string]any{"stdout": toolResultContent, "stderr": "", "interrupted": false, "isImage": false, "noOutputExpected": false}
				}
				userMsg := StreamMessage{
					Type:            "user",
					SessionID:       sessionID,
					ParentToolUseID: nil,
					Uuid:            GenerateUUID(),
					Message:         map[string]any{"role": "user", "content": userContent},
					Timestamp:       time.Now().UTC().Format(time.RFC3339Nano),
					ToolUseResult:   toolUseResult,
				}
				data, _ := json.Marshal(userMsg)
				fmt.Fprintln(os.Stdout, string(data))
			}
		}

		// Accumulate cost for this turn
		if resp.Model != "" {
			AccumulateUsage(e.costState, resp.Model, resp.Usage)
			// Feed actual token counts back into the estimator for EWMA calibration
			if e.tokenEstimator != nil {
				if resp.Usage.OutputTokens > 0 {
					outputChars := len(textOutput.String()) + totalThinkingChars(thinkingBlocks)
					e.tokenEstimator.ObserveOutput(resp.Model, outputChars, resp.Usage.OutputTokens)
					if resp.Model != e.model && e.model != "" {
						e.tokenEstimator.ObserveOutput(e.model, outputChars, resp.Usage.OutputTokens)
					}
				}
				// Feed input token observations for EWMA calibration of the
				// token estimator. Uses the pre-API char count to avoid
				// redundant re-marshaling of the entire message chain.
				actualInputTokens := resp.Usage.InputTokens + resp.Usage.CacheReadInputTokens + resp.Usage.CacheCreationInputTokens
				if actualInputTokens > 0 && preAPICharCount > 0 {
					e.tokenEstimator.Observe(e.model, preAPICharCount, actualInputTokens, false)
				}
			}
		}

		// Build and append assistant message with text, tool_use, and thinking blocks
		assistantMsg := api.Message{
			Role:    api.RoleAssistant,
			Content: textOutput.String(),
		}
		if len(thinkingBlocks) > 0 {
			var thinkingText strings.Builder
			for _, tb := range thinkingBlocks {
				thinkingText.WriteString(tb.Text)
			}
			assistantMsg.Thinking = thinkingText.String()
			// Use signature from last thinking block if present
			if thinkingBlocks[len(thinkingBlocks)-1].Signature != "" {
				assistantMsg.Signature = thinkingBlocks[len(thinkingBlocks)-1].Signature
			}
		}
		if len(toolUseBlocks) > 0 {
			assistantMsg.ToolUse = toolUseBlocks
		}
		if textOutput.String() != "" || len(toolUseBlocks) > 0 || len(thinkingBlocks) > 0 {
			messages = append(messages, assistantMsg)
		}

		// Persist assistant message to transcript BEFORE tool execution
		if e.sessionManager != nil && (textOutput.String() != "" || len(toolUseBlocks) > 0 || len(thinkingBlocks) > 0) {
			entry := session.TranscriptEntry{
				Type:    session.EntryTypeAssistant,
				Content: textOutput.String(),
				CWD:     cwd,
			}
			// Persist thinking blocks (concatenated text with signature from last block)
			if len(thinkingBlocks) > 0 {
				var thinkingText strings.Builder
				for _, tb := range thinkingBlocks {
					thinkingText.WriteString(tb.Text)
				}
				entry.Thinking = thinkingText.String()
				// Use signature from last thinking block if present
				if thinkingBlocks[len(thinkingBlocks)-1].Signature != "" {
					entry.Signature = thinkingBlocks[len(thinkingBlocks)-1].Signature
				}
			}
			for _, tu := range toolUseBlocks {
				entry.ToolUse = append(entry.ToolUse, session.ToolUse{
					ID:    tu.ID,
					Name:  tu.Name,
					Input: tu.Input,
				})
			}
			if err := e.sessionManager.AppendEntry(sessionID, entry); err != nil {
				return "", fmt.Errorf("persisting assistant message to transcript: %w", err)
			}
		}

		// Capture skill count BEFORE tool execution to detect changes
		prevSkillCount := len(e.streamCfg.ActiveSkills)

		// Execute all tools using the parallel executor with cross-turn state support
		execToolResults, hasSynthetic, execErr := e.executeAndProcessTools(ctx, toolUseBlocks, sessionID, cwd)
		if execErr != nil {
			return "", fmt.Errorf("executing tools: %w", execErr)
		}
		toolResults = append(toolResults, execToolResults...)

		// Progress monitor: record turn and update max_tokens level
		e.progressMonitor.RecordTurn(toolUseBlocks, execToolResults)
		e.maxTokensCtrl.SetProgressLevel(e.progressMonitor.Level())
		// Record turn outcome for next-turn adaptive max_tokens
		hadToolUse := len(toolUseBlocks) > 0
		hadWrite := false
		for _, tb := range toolUseBlocks {
			if tb.Name == "write" || tb.Name == "edit" || tb.Name == "notebook_edit" {
				hadWrite = true
				break
			}
		}
		e.maxTokensCtrl.RecordTurnOutcome(e.currentStopReason, hadToolUse, hadWrite)

		// Thinking monitor: record this turn's thinking usage and check for reminder
		turnThinkingTokens := e.sumThinkingTokens()
		thinkingComplexity := 3.0
		if cr := e.maxTokensCtrl.GetComplexityResult(); cr != nil {
			thinkingComplexity = cr.OverallComplexity()
		}
		e.thinkingMonitor.RecordTurn(e.turnCount, turnThinkingTokens, len(toolUseBlocks) > 0, thinkingComplexity)
		if reminder := e.thinkingMonitor.ShouldRemind(); reminder != "" {
			messages = append(messages, api.Message{
				Role:      api.RoleUser,
				Content:   reminder,
				IsVirtual: true,
			})
			log.Info("Thinking monitor injected reminder", "turn", e.turnCount, "thinking_tokens", turnThinkingTokens)
		}

		// Sync active skills and inject a reminder message if they changed.
		// (The executeAndProcessTools function also calls syncActiveSkills, but this
		// is the additional cross-turn reminder injection that must happen at loop level)
		e.syncActiveSkills()
		if len(e.streamCfg.ActiveSkills) != prevSkillCount {
			if reminder := activeSkillsSection(e.streamCfg.ActiveSkills); reminder != "" {
				reminderMsg := api.Message{
					Role:      api.RoleUser,
					Content:   "<system-reminder>\n" + reminder + "\n</system-reminder>",
					IsVirtual: true,
				}
				messages = append(messages, reminderMsg)
				e.persistSystemReminder(sessionID, reminder)
			}
		}

		// When synthetic results were generated, allow exactly one more
		// iteration to deliver them to the model. Detach the context from
		// cancellation for that single iteration so the API call can
		// proceed. After that iteration (or if already in-flight), the
		// flag is cleared and normal cancellation resumes.
		if hasSynthetic && e.syntheticInFlight <= 0 {
			e.originalCtx = ctx
			ctx = context.WithoutCancel(ctx)
			e.syntheticInFlight = 2
		}
		if e.syntheticInFlight > 0 {
			e.syntheticInFlight--
		}
		// Check session memory threshold after each turn
		if e.sessionMemory != nil {
			turnTokens := resp.Usage.InputTokens + resp.Usage.OutputTokens +
				resp.Usage.CacheReadInputTokens + resp.Usage.CacheCreationInputTokens
			toolCallCount := len(toolUseBlocks)
			shouldAct, action := e.sessionMemory.CheckThreshold(turnTokens, toolCallCount)
			if shouldAct {
				if action == "init" {
					if err := e.sessionMemory.Init(); err != nil {
						log.Warn("Session memory init failed", "error", err)
					}
				} else if action == "update" {
					if err := e.sessionMemory.Update(ctx); err != nil {
						log.Warn("Session memory update failed", "error", err)
					}
				}
			}
		}

		// Delegate to extracted handler
		result, err, shouldContinue := e.handleStopReason(ctx, resp, *streamResult, textOutput, sessionID, toolResults, toolUseBlocks, thinkingBlocks, &assistantMsg, &messages)
		if shouldContinue {
			// Complexity fork: fire after 3rd tool result, AFTER tool_results
			// are appended to messages by handleStopReason so the fork sees
			// a valid tool_use/tool_result sequence.
			e.toolResultCount += len(execToolResults)
			if e.toolResultCount >= 3 && e.maxTokensCtrl.ShouldFork() {
				forkMessages := make([]api.Message, len(messages))
				copy(forkMessages, messages)
				forkModel := e.model
				if e.requestedModel != "" {
					forkModel = e.requestedModel
				}
				forkTools := e.toolParams
				parentCtx := ctx
				go func() {
					forkCtx, forkCancel := context.WithTimeout(context.WithoutCancel(parentCtx), 30*time.Second)
					defer forkCancel()
					forkClient, err := api.NewClientWithModel(forkModel)
					if err != nil {
						log.Debug("Complexity eval fork client creation failed", "error", err)
						return
					}
					forkClient.SetMaxTokensOverride(forkMaxTokens)
					forkClient.SetThinkingConfig(api.ThinkingConfig{
						Effort:       e.streamCfg.Effort,
						BudgetTokens: e.streamCfg.ThinkingBudget,
					})
					cr, model, usage, err := forkComplexityEval(forkCtx, forkClient, forkMessages, systemPrompt, forkTools)
					if err != nil {
						log.Debug("Complexity eval fork failed", "error", err)
						return
					}
					// Check if parent session was cancelled while we were evaluating
					if parentCtx.Err() != nil {
						log.Debug("Complexity eval completed but session already ended, discarding result")
						return
					}
					// Accumulate cost for the side request
					e.mu.Lock()
					if model != "" {
						AccumulateUsage(e.costState, model, usage)
					}
					totalCost := e.costState.TotalCostUSD
					modelUsage := e.buildModelUsage()
					e.mu.Unlock()
					e.maxTokensCtrl.SetComplexityResult(cr)
					log.Info("Complexity eval result",
						"reasoning_depth", cr.ReasoningDepth,
						"code_volume", cr.CodeVolume,
						"domain_specialization", cr.DomainSpecialization,
						"integration_breadth", cr.IntegrationBreadth,
						"need_write", cr.NeedWrite,
					)
					if e.streamCfg.Enabled {
						crJSON, _ := json.Marshal(cr)
						msg := StreamMessage{
							Type:         "system",
							Subtype:      "complexity_eval",
							Result:       string(crJSON),
							Uuid:         GenerateUUID(),
							TotalCostUSD: totalCost,
							ModelUsage:   modelUsage,
						}
						data, _ := json.Marshal(msg)
						fmt.Fprintln(os.Stdout, string(data))
					}
				}()
			}
			continue
		}
		if err != nil {
			return result, err
		}
		return result, nil
	}

	return "", fmt.Errorf("max iterations (%d) exceeded", maxIterations)
}
