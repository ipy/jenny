// Package agent provides the core agent loop and query engine.
package agent

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/ipy/jenny/internal/api"
	"github.com/ipy/jenny/internal/log"
)

const (
	// BLOCKING_BUFFER_TOKENS is the buffer subtracted from effective context window
	// to determine the blocking limit when auto-compact is disabled.
	BLOCKING_BUFFER_TOKENS = 3_000

	// WARNING_BUFFER_TOKENS is subtracted from autoCompactThreshold to determine
	// when to emit a warning event.
	WARNING_BUFFER_TOKENS = 20_000

	// MAX_CONSECUTIVE_AUTOCOMPACT_FAILURES is the number of consecutive failures
	// before the circuit breaker trips and skips all further auto-compact attempts.
	MAX_CONSECUTIVE_AUTOCOMPACT_FAILURES = 3

	// SUMMARY_MAX_TOKENS is the maximum output tokens allocated for summary
	// generation during compaction. Used both for budget calculation and as the
	// actual max_tokens override sent to the API. Set high enough for thinking
	// models where the output budget is shared between thinking blocks and text.
	SUMMARY_MAX_TOKENS = 20_000

	// minAutoCompactBuffer is the floor for the auto-compact buffer.
	minAutoCompactBuffer = 13_000

	// MIN_SAFETY_OVERHEAD is the minimum overhead reserved for system prompt,
	// tools definition, and compaction instruction when estimating if in-session
	// compaction would overflow the context window.
	MIN_SAFETY_OVERHEAD = 30_000

	// minTailMessages is the minimum number of tail messages kept after compaction.
	minTailMessages = 2

	// maxTailMessages caps tail selection to prevent degenerate cases.
	maxTailMessages = 30

	// tailPruneThresholdChars is the minimum tool result size subject to tail pruning.
	tailPruneThresholdChars = 4096

	// tailPruneHeadChars is the number of leading characters kept when pruning.
	tailPruneHeadChars = 1024

	// tailPruneTailChars is the number of trailing characters kept when pruning.
	tailPruneTailChars = 512
)

// CompactConfig holds configuration for compaction.
type CompactConfig struct {
	// Environment overrides
	DisableCompact     bool
	DisableAutoCompact bool
	AutoCompactWindow  int //0 means use modelContextWindow

	// Model parameters
	ModelContextWindow   int
	ModelMaxOutputTokens int

	// Session state
	CompactFailCount int

	// Feature flags
	SessionMemoryEnabled bool
}

// effectiveContextWindow returns the effective context window after accounting
// for model max output tokens reserve.
func (c CompactConfig) effectiveContextWindow() int {
	return max(c.ModelContextWindow-c.ModelMaxOutputTokens, 0)
}

// autoCompactBuffer returns the buffer for auto-compact threshold, scaled to
// the model's max output tokens: max(modelMaxOutputTokens + 5000, minAutoCompactBuffer).
// Capped at 50% of effective context window so threshold never goes negative
// for models with large output relative to context window.
func (c CompactConfig) autoCompactBuffer() int {
	raw := max(c.ModelMaxOutputTokens+5_000, minAutoCompactBuffer)
	effectiveWindow := c.effectiveContextWindow()
	if maxBuf := effectiveWindow / 2; maxBuf > 0 && raw > maxBuf {
		return maxBuf
	}
	return raw
}

// autoCompactThreshold returns the token count at which auto-compact triggers.
// Returns 0 if the effective context window is too small for a positive threshold.
//
// When AUTO_COMPACT_WINDOW is explicitly set, the original formula is always used
// regardless of context window size (user has taken manual control).
//
// Otherwise, strategy depends on context window size:
//   - ≤ 256K: original formula (effectiveContextWindow - autoCompactBuffer)
//   - > 256K and ≤ 512K: 50% of context window
//   - > 512K: 512K (fixed cap)
func (c CompactConfig) autoCompactThreshold() int {
	// User override: always use original formula
	if c.AutoCompactWindow > 0 {
		t := c.effectiveContextWindow() - c.autoCompactBuffer()
		if t < 0 {
			return 0
		}
		return t
	}

	if c.ModelContextWindow <= 256_000 {
		t := c.effectiveContextWindow() - c.autoCompactBuffer()
		if t < 0 {
			return 0
		}
		return t
	}
	if c.ModelContextWindow <= 512_000 {
		return c.ModelContextWindow / 2
	}
	return 512_000
}

// compactionTarget returns the post-compaction token goal. It is always strictly
// less than autoCompactThreshold when threshold > 0, creating hysteresis that
// prevents immediate re-compaction on the next turn.
//
// Target strategy mirrors autoCompactThreshold tiers:
//   - AUTO_COMPACT_WINDOW set or ≤ 256K: 60% of effectiveContextWindow
//   - > 256K and ≤ 512K: 35% of context window
//   - > 512K: 360K (fixed)
func (c CompactConfig) compactionTarget() int {
	if c.autoCompactThreshold() <= 0 {
		return 0
	}

	if c.AutoCompactWindow > 0 || c.ModelContextWindow <= 256_000 {
		t := c.effectiveContextWindow() * 60 / 100
		if t < 0 {
			return 0
		}
		return t
	}
	if c.ModelContextWindow <= 512_000 {
		return c.ModelContextWindow * 35 / 100
	}
	return 360_000
}

// warningThreshold returns the token count at which to emit a warning event.
func (c CompactConfig) warningThreshold() int {
	return c.autoCompactThreshold() - WARNING_BUFFER_TOKENS
}

// blockingLimit returns the token count at which to block API calls when
// auto-compact is disabled.
func (c CompactConfig) blockingLimit() int {
	return c.effectiveContextWindow() - BLOCKING_BUFFER_TOKENS
}

// checkCompactThreshold returns true if estimated tokens exceed the auto-compact
// threshold and auto-compact should trigger.
// querySource is checked to skip auto-compact for 'compact' and 'session_memory' sources.
func (c CompactConfig) checkCompactThreshold(estimatedTokens int, querySource string) bool {
	// Skip auto-compact when querySource is 'compact' or 'session_memory'
	if querySource == "compact" || querySource == "session_memory" {
		return false
	}
	if c.DisableCompact || c.DisableAutoCompact {
		return false
	}
	return estimatedTokens >= c.autoCompactThreshold()
}

// checkWarningThreshold returns true if estimated tokens exceed the warning
// threshold.
func (c CompactConfig) checkWarningThreshold(estimatedTokens int) bool {
	return estimatedTokens >= c.warningThreshold()
}

// blockIfOverLimit returns an error if estimated tokens exceed the blocking
// limit and auto-compact is disabled.
func (c CompactConfig) blockIfOverLimit(estimatedTokens int, querySource string) error {
	// compact and session_memory sources never hard-block
	if querySource == "compact" || querySource == "session_memory" {
		return nil
	}

	if c.DisableAutoCompact || c.DisableCompact {
		if estimatedTokens >= c.blockingLimit() {
			return &PromptTooLongError{EstimatedTokens: estimatedTokens, Limit: c.blockingLimit()}
		}
	}
	return nil
}

// PromptTooLongError is returned when the estimated tokens exceed the blocking limit.
type PromptTooLongError struct {
	EstimatedTokens int
	Limit           int
}

func (e *PromptTooLongError) Error() string {
	return fmt.Sprintf("prompt too long: estimated %d tokens exceeds blocking limit %d", e.EstimatedTokens, e.Limit)
}

// newCompactConfigForModel creates a CompactConfig using model-specific parameters
// from api.ModelParams, with overrides sourced from streamCfg (which is itself
// populated by the koanf config layer — see docs/arch/koanf-config.md).
//
// streamCfg may be nil; in that case all override booleans default to false.
// AUTO_COMPACT_WINDOW remains a raw os.Getenv read because it is not
// JENNY_*-prefixed and therefore not in the koanf layer.
func newCompactConfigForModel(model string, streamCfg *StreamConfig) CompactConfig {
	params := api.ModelParams(model)
	cfg := CompactConfig{
		ModelContextWindow:   params.ContextWindow,
		ModelMaxOutputTokens: params.MaxOutputTokens,
	}
	if streamCfg != nil {
		cfg.DisableCompact = streamCfg.DisableCompact
		cfg.DisableAutoCompact = streamCfg.DisableAutoCompact
		cfg.SessionMemoryEnabled = streamCfg.EnableSessionMemory
	}

	if envWindow := readEnvInt("AUTO_COMPACT_WINDOW", 0); envWindow > 0 {
		cfg.AutoCompactWindow = envWindow
		cfg.ModelContextWindow = envWindow
	}

	return cfg
}

// readEnvInt reads an integer from an environment variable.
func readEnvInt(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if intVal, err := strconv.Atoi(val); err == nil {
			return intVal
		}
	}
	return defaultVal
}

// compactMessages performs context compaction on the message chain.
// It returns the compacted messages and any error encountered.
//
// Strategy order:
//  1. In-session compaction (shares system+tools+message cache with main session)
//  2. Session-memory compaction (when enabled)
//  3. Fallback summary compact (independent call, no cache sharing)
func (e *QueryEngine) compactMessages(ctx context.Context, messages []api.Message, cfg CompactConfig, systemPrompt []string) ([]api.Message, error) {
	log.Debug("Starting context compaction", "messageCount", len(messages))

	// Pre-flight check — skip in-session compaction if messages are too large.
	// Reserve wire overhead (system prompt + tools + fixed) for non-message payload,
	// plus SUMMARY_MAX_TOKENS for the summary output. Falls back to MIN_SAFETY_OVERHEAD
	// when precise overhead cannot be computed.
	wireOverhead := estimateSystemAndToolTokens(systemPrompt, e.toolParams)
	if e.tokenEstimator != nil {
		wireOverhead = e.tokenEstimator.estimateSystemAndToolTokensCalibrated(systemPrompt, e.toolParams, e.model)
	}
	wireOverhead = effectiveWireOverhead(wireOverhead)
	maxMessagesTokens := cfg.effectiveContextWindow() - wireOverhead - SUMMARY_MAX_TOKENS
	var estimated int
	if e.tokenEstimator != nil {
		estimated = e.tokenEstimator.EstimateMessages(messages, e.model)
	} else {
		estimated = estimateTokens(messages)
	}

	if estimated <= maxMessagesTokens {
		// Step 1: In-session compaction — reuses the cached system prompt + tools prefix
		targetTokens := cfg.compactionTarget()
		compacted, err := e.inSessionCompact(ctx, messages, systemPrompt, targetTokens)
		if err == nil {
			return compacted, nil
		}
		log.Warn("In-session compaction failed, trying fallback", "error", err)
	} else {
		log.Warn("In-session compaction skipped: messages too large for context window",
			"estimatedTokens", estimated, "maxMessagesTokens", maxMessagesTokens)
	}

	// Step 2: Session-memory compaction (when enabled)
	if cfg.SessionMemoryEnabled {
		compacted, err := e.trySessionMemoryCompact(ctx, messages, cfg, systemPrompt)
		if err == nil {
			return compacted, nil
		}
		log.Debug("Session memory compaction not available, falling back to summary compact")
	}

	// Step 3: Fallback summary compact (independent call, no cache sharing)
	return e.fallbackSummaryCompact(ctx, messages, cfg, systemPrompt)
}

// in-session compaction and fallback summary compaction. Both paths must produce
// identical output format so the compacted chain is consistent regardless of
// which path succeeded.
//
// The summary is the only preserved context from dropped messages. Each item
// ensures the model can continue correctly after compaction.
const compactSummaryRequirements = `Provide a comprehensive summary of the entire conversation so far. Output ONLY the summary text. TOOL CALLS ARE FORBIDDEN.

For every claim, tag it with a confidence level: [CONFIRMED] for observed facts, [SPECULATIVE] for reasoned but unverified judgments, [UNVERIFIED] for stated intent not yet acted on, [EXCLUDED] for approaches tried and rejected or ruled out.

1. Primary request and user intent — the explicit goal, plus any implicit or evolving intent (what the user really wants, not just what they said)
2. Key decisions and rationale — each decision point, why it was made, alternatives considered
3. Excluded paths — approaches that were tried and failed, or analyzed and ruled out. Include why each was rejected, so they are not re-attempted
4. Errors and fixes — specific error messages, root causes, and the fix that resolved each
5. Files and code sections — every file modified or created, with concrete identifiers (function names, type names, constants) and what changed
6. Key technical concepts — APIs, libraries, patterns, constraints, or domain knowledge relevant to the task
7. Current work — exactly what was being done when compaction triggered (the last few turns)
8. Next steps — actionable outstanding tasks, unresolved questions, or explicit next actions the user requested`

// inSessionCompactPrompt is the instruction appended to the message chain
// for in-session compaction. It reuses the session's system prompt and tools
// so the entire prefix is a cache hit. Wrapped in <system-reminder> to
// override the agent persona.
var inSessionCompactPrompt = "<system-reminder>\nThe context window is approaching its limit. This is a separate compaction request. Do not continue any prior conversation or call any tools.\n</system-reminder>\n" + compactSummaryRequirements

// fallbackCompactSystemPrompt is the system prompt for fallback summary
// compaction. It uses the same summary requirements as in-session compaction
// to ensure consistent output format.
var fallbackCompactSystemPrompt = []string{"You are a helpful assistant that summarizes conversations. Do not continue the conversation or call any tools. " + compactSummaryRequirements}

// fallbackSummarySystemPrompt returns the fallback summary system prompt, optionally
// annotated with pinned messages excluded from summarization.
func fallbackSummarySystemPrompt(originalMessages []api.Message) []string {
	pinnedNote := formatPinnedSummaryNote(pinnedMessagesForSummary(originalMessages))
	if pinnedNote == "" {
		return fallbackCompactSystemPrompt
	}
	return []string{fallbackCompactSystemPrompt[0] + "\n" + pinnedNote}
}

// inSessionCompact performs compaction by appending a summary instruction to the
// existing message chain, sharing the cached system prompt + tools + message
// prefix with the main session. Only the appended instruction is cold content.
func (e *QueryEngine) inSessionCompact(ctx context.Context, messages []api.Message, systemPrompt []string, targetTokens int) ([]api.Message, error) {
	pinnedNote := formatPinnedSummaryNote(pinnedMessagesForSummary(messages))
	compactMsg := api.Message{
		Role:    api.RoleUser,
		Content: inSessionCompactPrompt + "\n" + pinnedNote,
	}
	messagesWithInstruction := make([]api.Message, len(messages), len(messages)+1)
	copy(messagesWithInstruction, messages)
	messagesWithInstruction = append(messagesWithInstruction, compactMsg)

	e.client.SetMaxTokensOverride(SUMMARY_MAX_TOKENS)
	defer e.client.SetMaxTokensOverride(0) // restore to model default; main loop re-sets before each call
	resp, err := e.client.SendMessage(ctx, messagesWithInstruction, e.toolParams, nil, systemPrompt, "")
	if err != nil {
		return nil, fmt.Errorf("in-session compaction API call: %w", err)
	}

	e.mu.Lock()
	AccumulateUsage(e.costState, resolvedModelForCost(e.model, resp.Model), resp.Usage)
	e.mu.Unlock()

	// Reject if the model made tool calls instead of producing a summary
	for _, block := range resp.Content {
		if block.Type == api.BlockTypeToolUse {
			return nil, fmt.Errorf("model produced tool_use during in-session compaction")
		}
	}

	var summary strings.Builder
	for _, block := range resp.Content {
		if block.Type == api.BlockTypeText {
			summary.WriteString(block.Text)
		}
	}

	if summary.Len() == 0 {
		return nil, fmt.Errorf("empty summary from in-session compaction")
	}

	return buildCompactedChain(messages, summary.String(), targetTokens), nil
}

// trySessionMemoryCompact attempts compaction via session memory.
// Currently returns ErrNotImplemented.
func (e *QueryEngine) trySessionMemoryCompact(ctx context.Context, messages []api.Message, cfg CompactConfig, systemPrompt []string) ([]api.Message, error) {
	return nil, fmt.Errorf("session memory compaction not implemented")
}

// fallbackSummaryCompact generates a summary of the conversation history as a
// last-resort compaction strategy. Delegates to SideQuery in summary mode
// (creates its own client, disables thinking, uses fallbackCompactSystemPrompt).
// On prompt-too-long errors, retries with proportional dropping: each retry
// drops 25% of remaining messages from the head.
func (e *QueryEngine) fallbackSummaryCompact(ctx context.Context, messages []api.Message, cfg CompactConfig, systemPrompt []string) ([]api.Message, error) {
	sqctx := e.sideQueryCtx
	if sqctx == nil {
		sqctx = &SideQueryContext{
			Model:     ResolveModel(e.model),
			CostState: e.costState,
			CostMu:    &e.mu,
		}
	}

	currentMessages := messages
	var lastErr error
	maxRetries := 5

	for attempt := range maxRetries {
		if attempt > 0 {
			log.Debug("Retrying fallback summary compact", "attempt", attempt+1, "messageCount", len(currentMessages))
			dropCount := len(currentMessages) / 4
			if dropCount < 1 {
				dropCount = 1
			}
			if len(currentMessages)-dropCount < 4 {
				return nil, fmt.Errorf("cannot retry summary: too few messages remaining (%d)", len(currentMessages))
			}
			currentMessages = currentMessages[dropCount:]
			for len(currentMessages) > 0 {
				m := currentMessages[0]
				if m.Role == api.RoleUser && len(m.ToolResults) > 0 {
					currentMessages = currentMessages[1:]
					continue
				}
				break
			}
			for len(currentMessages) > 0 && currentMessages[0].Role != api.RoleUser {
				currentMessages = currentMessages[1:]
			}
			if len(currentMessages) < 2 {
				return nil, fmt.Errorf("cannot retry summary: too few messages remaining after alignment (%d)", len(currentMessages))
			}
		}

		sq := &SideQuery{
			Name:                "compact_fallback",
			Messages:            currentMessages,
			MaxTokens:           SUMMARY_MAX_TOKENS,
			SummarySystemPrompt: fallbackSummarySystemPrompt(messages),
			DisableThinking:     true,
		}

		result, err := sq.ExecuteSummary(ctx, sqctx)
		if err != nil {
			lastErr = err
			if isPromptTooLongError(err) {
				continue
			}
			return nil, err
		}

		if result.Text == "" {
			lastErr = fmt.Errorf("empty summary response")
			continue
		}

		return buildCompactedChain(messages, result.Text, cfg.compactionTarget()), nil
	}

	return nil, fmt.Errorf("fallback summary compact failed after %d attempts: %w", maxRetries, lastErr)
}

// isPromptTooLongError checks if an error indicates a prompt-too-long condition.
func isPromptTooLongError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "prompt too long") ||
		strings.Contains(errStr, "too many tokens") ||
		strings.Contains(errStr, "context length") ||
		strings.Contains(errStr, "context window") ||
		strings.Contains(errStr, "413")
}

// prepareSummaryMessages prepares messages for the summary API call by stripping
// images, documents, thinking blocks, and signatures.
func prepareSummaryMessages(messages []api.Message) []api.Message {
	var result []api.Message
	for _, msg := range messages {
		processedMsg := msg
		processedMsg.Thinking = ""
		processedMsg.Signature = ""

		if msg.Role == "user" {
			processedMsg.Content = stripMediaMarkers(msg.Content)
		}

		result = append(result, processedMsg)
	}
	return result
}

// Pre-compiled regexps for stripMediaMarkers (avoid recompilation on every call).
var (
	base64ImageRe   = regexp.MustCompile(`data:image/[^;]+;base64,[A-Za-z0-9+/=]{100,}`)
	base64PdfRe     = regexp.MustCompile(`data:application/pdf[^,;]*,[A-Za-z0-9+/=]{100,}`)
	markdownImageRe = regexp.MustCompile(`!\[([^\]]*)\]\([^)]+\)`)
	imageURLRe      = regexp.MustCompile(`https?://[^)\s"']+\.(png|jpg|jpeg|gif|bmp|webp|svg)(\?[^)\s"']*)?`)
	pdfURLRe        = regexp.MustCompile(`https?://[^)\s"']+\.pdf(\?[^)\s"']*)?`)
)

// stripMediaMarkers replaces image/document content with markers.
// This prevents large media from being sent to the summary agent.
func stripMediaMarkers(content string) string {
	if content == "" {
		return content
	}

	content = base64ImageRe.ReplaceAllString(content, "[image]")
	content = base64PdfRe.ReplaceAllString(content, "[document]")
	content = markdownImageRe.ReplaceAllString(content, "[image]")
	content = imageURLRe.ReplaceAllString(content, "[image]")
	content = pdfURLRe.ReplaceAllString(content, "[document]")

	return content
}

// dropOldestAPIRoundGroup drops the oldest API-round group from the messages.
// An API-round group consists of: user message, assistant message (with tool_use),
// and tool_result messages.
func dropOldestAPIRoundGroup(messages []api.Message) []api.Message {
	if len(messages) == 0 {
		return messages
	}

	// Find the first user message (start of an API round)
	startIdx := -1
	for i, msg := range messages {
		if msg.Role == "user" {
			startIdx = i
			break
		}
	}

	if startIdx == -1 || startIdx >= len(messages)-1 {
		return messages
	}

	// Find the end of this API round (next user message or end of array)
	endIdx := startIdx + 1
	for endIdx < len(messages) {
		if messages[endIdx].Role == "user" {
			break
		}
		endIdx++
	}

	// Drop from startIdx to endIdx (exclusive)
	result := make([]api.Message, 0, len(messages)-(endIdx-startIdx))
	result = append(result, messages[:startIdx]...)
	result = append(result, messages[endIdx:]...)
	return result
}

// buildCompactedChain builds the new message chain after compaction.
// Order: boundaryMarker → olderPinnedMessages → tailMessages
//
// targetTokens is the compactionTarget used to derive tailTokenBudget. The cut
// point never splits a tool_use/tool_result pair. Pinned messages from the
// original chain always survive; their token cost reduces the tail budget.
func buildCompactedChain(originalMessages []api.Message, summary string, targetTokens int) []api.Message {
	var olderPinned []api.Message
	var nonPinned []api.Message
	for _, msg := range originalMessages {
		if msg.Pinned {
			olderPinned = append(olderPinned, msg)
		} else {
			nonPinned = append(nonPinned, msg)
		}
	}

	boundaryContent := "<system-reminder>\n" + compactSummaryPrefix + summary + "\n</system-reminder>"
	boundaryMsg := api.Message{
		Role:    api.RoleUser,
		Content: boundaryContent,
		Pinned:  true,
	}

	pinnedTokens := estimateTokens(olderPinned)
	tailTokenBudget := targetTokens - estimateMessageTokens(boundaryMsg) - pinnedTokens
	if tailTokenBudget < 0 {
		tailTokenBudget = 0
	}

	var tail []api.Message
	if tailTokenBudget > 0 {
		tail = selectTailMessages(nonPinned, tailTokenBudget)
	}
	tail = pruneTailToolResults(tail)

	var result []api.Message
	result = append(result, boundaryMsg)
	result = append(result, olderPinned...)
	result = append(result, tail...)
	return enforcePinnedLimit(result)
}

// tailMessageCount returns the number of tail messages preserved after compaction,
// excluding the boundary marker and any older pinned messages.
func tailMessageCount(compacted []api.Message) int {
	if len(compacted) <= 1 {
		return 0
	}
	i := 1
	for i < len(compacted) && compacted[i].Pinned {
		i++
	}
	return len(compacted) - i
}

// estimateMessageTokens estimates tokens for a single message using the
// uncalibrated charset-aware heuristic.
func estimateMessageTokens(msg api.Message) int {
	return estimateTokens([]api.Message{msg})
}

// selectTailMessages chooses tail messages by walking backwards from the end,
// accumulating token estimates until the budget is exceeded. Always keeps at
// least minTailMessages and at most maxTailMessages.
func selectTailMessages(messages []api.Message, tailTokenBudget int) []api.Message {
	n := len(messages)
	if n <= minTailMessages {
		return messages
	}

	startIdx := n - minTailMessages
	accumulated := estimateTokens(messages[startIdx:])

	for startIdx > 0 && (n-startIdx) < maxTailMessages {
		prevTokens := estimateMessageTokens(messages[startIdx-1])
		if tailTokenBudget > 0 && accumulated+prevTokens > tailTokenBudget {
			break
		}
		startIdx--
		accumulated += prevTokens
	}

	// If cut falls between assistant tool_use and user tool_results, include both.
	if startIdx > 0 && messages[startIdx].Role == api.RoleUser && len(messages[startIdx].ToolResults) > 0 {
		startIdx--
	}

	return messages[startIdx:]
}

// totalUserRounds counts user messages as API round markers.
func totalUserRounds(messages []api.Message) int {
	count := 0
	for _, msg := range messages {
		if msg.Role == api.RoleUser {
			count++
		}
	}
	return count
}

// userRoundNumber returns the 1-based API round number for the message at idx.
func userRoundNumber(messages []api.Message, idx int) int {
	round := 0
	for i := 0; i <= idx; i++ {
		if messages[i].Role == api.RoleUser {
			round++
		}
	}
	return round
}

// pruneTailToolResults trims oversized tool results in tail messages belonging
// to API rounds older than the most recent 2. Error tool results are preserved.
// Operates on a copy; the original slice is returned unchanged when no pruning
// is needed.
func pruneTailToolResults(messages []api.Message) []api.Message {
	totalRounds := totalUserRounds(messages)
	if totalRounds <= 2 {
		return messages
	}

	var result []api.Message
	for i, msg := range messages {
		if len(msg.ToolResults) == 0 {
			continue
		}
		roundNum := userRoundNumber(messages, i)
		if roundNum > totalRounds-2 {
			continue
		}

		for j, tr := range msg.ToolResults {
			if tr.IsError || len(tr.Content) <= tailPruneThresholdChars {
				continue
			}
			if result == nil {
				result = make([]api.Message, len(messages))
				copy(result, messages)
			}
			newResults := make([]api.ToolResultBlock, len(msg.ToolResults))
			copy(newResults, msg.ToolResults)
			newResults[j].Content = formatTrimmedToolResult(tr.Content)
			result[i].ToolResults = newResults
		}
	}
	if result == nil {
		return messages
	}
	return result
}

// formatTrimmedToolResult produces the elided tool result content for tail pruning.
func formatTrimmedToolResult(content string) string {
	head := content
	if len(head) > tailPruneHeadChars {
		head = head[:tailPruneHeadChars]
	}
	tail := content
	if len(tail) > tailPruneTailChars {
		tail = tail[len(tail)-tailPruneTailChars:]
	}
	return fmt.Sprintf(
		"[Trimmed: original %d chars. First 1024 chars shown, last 512 chars shown. Re-run the tool if the full data is needed.]\n%s\n...\n%s",
		len(content), head, tail,
	)
}

// compactSummaryPrefix is the prefix added by buildCompactedChain to the boundary marker.
const compactSummaryPrefix = "[Context boundary: earlier conversation summarized]\n\nPrevious summary:\n"

// extractCompactSummary extracts the summary text from the compacted chain's
// boundary marker (first message). Returns empty string if no summary found.
func extractCompactSummary(compacted []api.Message) string {
	if len(compacted) == 0 {
		return ""
	}
	content := compacted[0].Content
	// Strip <system-reminder> wrapper if present.
	const (
		wrapperOpen  = "<system-reminder>\n"
		wrapperClose = "\n</system-reminder>"
	)
	if strings.HasPrefix(content, wrapperOpen) && strings.HasSuffix(content, wrapperClose) {
		content = content[len(wrapperOpen) : len(content)-len(wrapperClose)]
	}
	if strings.HasPrefix(content, compactSummaryPrefix) {
		return content[len(compactSummaryPrefix):]
	}
	return content
}

// emergencyCompact is a last-resort compaction triggered when the API rejects
// a request with context_exhausted. It aggressively truncates the message chain
// before attempting a summary, dropping up to 75% of older messages to bring
// the context within bounds.
func (e *QueryEngine) emergencyCompact(ctx context.Context, messages []api.Message, systemPrompt []string) ([]api.Message, error) {
	log.Warn("Emergency compaction: aggressively truncating message chain",
		"originalMessageCount", len(messages),
	)

	// Strategy: keep the first message (user prompt) and the last 10 messages,
	// discard everything in between. Then summarize what was discarded.
	if len(messages) <= 12 {
		return nil, fmt.Errorf("emergency compact: too few messages (%d) to compact", len(messages))
	}

	// Keep first message + last 10
	kept := 10
	head := messages[:1]
	tail := messages[len(messages)-kept:]

	// Ensure tail doesn't start mid-tool-use/result pair
	if tail[0].Role == api.RoleUser && len(tail[0].ToolResults) > 0 && len(messages) > kept+1 {
		// Include the preceding assistant message
		tail = messages[len(messages)-kept-1:]
	}

	// Build a minimal summary of the dropped middle section
	droppedCount := len(messages) - len(head) - len(tail)
	summaryText := fmt.Sprintf(
		"[Emergency context recovery: %d messages were dropped to fit within the context window. "+
			"The conversation started with the initial task prompt. Recent context has been preserved. "+
			"Continue working on the task based on the remaining context.]",
		droppedCount,
	)

	// Attempt in-session compact with the truncated chain for a better summary
	truncated := make([]api.Message, 0, len(head)+len(tail))
	truncated = append(truncated, head...)
	truncated = append(truncated, tail...)

	compactMsg := api.Message{
		Role:    api.RoleUser,
		Content: inSessionCompactPrompt,
	}
	messagesForSummary := make([]api.Message, len(truncated), len(truncated)+1)
	copy(messagesForSummary, truncated)
	messagesForSummary = append(messagesForSummary, compactMsg)

	resp, err := e.client.SendMessage(ctx, messagesForSummary, e.toolParams, nil, systemPrompt, "")
	if err == nil {
		e.mu.Lock()
		AccumulateUsage(e.costState, resolvedModelForCost(e.model, resp.Model), resp.Usage)
		e.mu.Unlock()

		var summary strings.Builder
		for _, block := range resp.Content {
			if block.Type == api.BlockTypeText {
				summary.WriteString(block.Text)
			}
		}
		if summary.Len() > 0 {
			summaryText = summary.String()
		}
	} else {
		log.Warn("Emergency in-session summary failed, using mechanical summary", "error", err)
	}

	result := normalizeCompactedChain(buildCompactedChain(truncated, summaryText, e.compactConfig.compactionTarget()))
	log.Info("Emergency compaction completed", "newMessageCount", len(result))
	return result, nil
}

// normalizeCompactedChain applies post-compact normalization to ensure
// the compacted chain passes tool/thinking pairing rules.
func normalizeCompactedChain(messages []api.Message) []api.Message {
	if len(messages) == 0 {
		return messages
	}

	// Step 1: Filter orphaned thinking-only messages
	messages = filterOrphanedThinking(messages)

	// Step 2: Strip trailing thinking from last assistant
	messages = stripTrailingThinking(messages)

	// Step 3: Ensure non-empty assistant (insert placeholder if stripped content left empty)
	// (must run BEFORE filterWhitespaceOnly so placeholder is inserted before filtering)
	messages = ensureNonEmptyAssistant(messages)

	// Step 4: Filter whitespace-only assistant messages
	messages = filterWhitespaceOnly(messages)

	// Step 5: Ensure tool result pairing
	messages = ensureToolResultPairing(messages)

	return messages
}

// EmitCompactWarning emits a warning event when estimated tokens approach
// the compact threshold.
func EmitCompactWarning(estimatedTokens int, threshold int) {
	log.Warn("Context compaction warning: token count approaching threshold",
		"estimatedTokens", estimatedTokens,
		"threshold", threshold,
		"buffer", threshold-estimatedTokens)
}
