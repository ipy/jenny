// Package agent provides the core agent loop and query engine.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

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

	// SUMMARY_MAX_TOKENS is the maximum output tokens allocated for summary.
	SUMMARY_MAX_TOKENS = 20_000

	// minAutoCompactBuffer is the floor for the auto-compact buffer.
	minAutoCompactBuffer = 13_000

	// MIN_SAFETY_OVERHEAD is the minimum overhead reserved for system prompt,
	// tools definition, and compaction instruction when estimating if in-session
	// compaction would overflow the context window.
	MIN_SAFETY_OVERHEAD = 30_000
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
	return c.ModelContextWindow - c.ModelMaxOutputTokens
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
	// AC1: Skip auto-compact when querySource is 'compact' or 'session_memory'
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

// estimateTokens estimates the token count for a message chain using a
// charset-aware heuristic. ASCII/Latin text uses ~4 chars/token; multi-byte
// characters (CJK, emoji) use ~1.5 chars/token.
func estimateTokens(messages []api.Message) int {
	total := 0
	for _, msg := range messages {
		total += estimateStringTokens(msg.Content)
		total += estimateStringTokens(msg.Thinking)
		total += estimateStringTokens(msg.Signature)
		for _, tu := range msg.ToolUse {
			total += len(tu.Name) + 50
			inputBytes, _ := json.Marshal(tu.Input)
			total += estimateStringTokens(string(inputBytes))
		}
		for _, tr := range msg.ToolResults {
			total += estimateStringTokens(tr.Content) + 50
		}
	}
	return total
}

// estimateStringTokens estimates the token count for a string using charset-aware
// heuristics. ASCII chars → ~4 chars/token, multi-byte runes → ~1.5 chars/token.
func estimateStringTokens(s string) int {
	if len(s) == 0 {
		return 0
	}
	asciiChars := 0
	multiByteRunes := 0
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r < 128 {
			asciiChars++
		} else {
			multiByteRunes++
		}
		i += size
	}
	asciiTokens := asciiChars * 10 / 40  // asciiChars / 4.0
	mbTokens := multiByteRunes * 10 / 15 // multiByteRunes / 1.5
	return asciiTokens + mbTokens
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
//  3. Fork summary agent (independent call, no cache sharing)
func (e *QueryEngine) compactMessages(ctx context.Context, messages []api.Message, cfg CompactConfig, systemPrompt []string) ([]api.Message, error) {
	log.Debug("Starting context compaction", "messageCount", len(messages))

	// AC1: Pre-flight check — skip in-session compaction if messages are too large.
	// Reserve MIN_SAFETY_OVERHEAD for system prompt + tools + compaction instruction,
	// plus SUMMARY_MAX_TOKENS for the summary output.
	maxMessagesTokens := cfg.effectiveContextWindow() - MIN_SAFETY_OVERHEAD - SUMMARY_MAX_TOKENS
	var estimated int
	if e.tokenEstimator != nil {
		estimated = e.tokenEstimator.EstimateMessages(messages, e.model)
	} else {
		estimated = estimateTokens(messages)
	}

	if estimated <= maxMessagesTokens {
		// Step 1: In-session compaction — reuses the cached system prompt + tools prefix
		compacted, err := e.inSessionCompact(ctx, messages, systemPrompt)
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
		log.Debug("Session memory compaction not available, falling back to summary agent")
	}

	// Step 3: Fork summary agent (independent call, no cache sharing)
	return e.forkSummaryAgent(ctx, messages, cfg, systemPrompt)
}

// inSessionCompactPrompt is the instruction appended to the message chain
// for in-session compaction. It reuses the session's system prompt and tools
// so the entire prefix is a cache hit.
//
// The summary is the only preserved context from dropped messages. Each item
// below ensures the model can continue correctly after compaction. For every
// claim in the summary, distinguish confidence level:
//
//	[CONFIRMED] — observed, measured, or explicitly stated
//	[SPECULATIVE] — reasoned but not yet verified
//	[UNVERIFIED] — stated intent or assumption, not yet acted on
//	[EXCLUDED] — tried and rejected, or ruled out by analysis
const inSessionCompactPrompt = `<system-reminder>
The context window is approaching its limit. This is a separate compaction request. Do not continue any prior conversation or call any tools.
</system-reminder>
Provide a comprehensive summary of the entire conversation so far. Output ONLY the summary text.

For every claim, tag it with a confidence level: [CONFIRMED] for observed facts, [SPECULATIVE] for reasoned but unverified judgments, [UNVERIFIED] for stated intent not yet acted on, [EXCLUDED] for approaches tried and rejected or ruled out.

1. Primary request and user intent — the explicit goal, plus any implicit or evolving intent (what the user really wants, not just what they said)
2. Key decisions and rationale — each decision point, why it was made, alternatives considered
3. Excluded paths — approaches that were tried and failed, or analyzed and ruled out. Include why each was rejected, so they are not re-attempted
4. Errors and fixes — specific error messages, root causes, and the fix that resolved each
5. Files and code sections — every file modified or created, with concrete identifiers (function names, type names, constants) and what changed
6. Key technical concepts — APIs, libraries, patterns, constraints, or domain knowledge relevant to the task
7. Current work — exactly what was being done when compaction triggered (the last few turns)
8. Next steps — actionable outstanding tasks, unresolved questions, or explicit next actions the user requested`

// inSessionCompact performs compaction by appending a summary instruction to the
// existing message chain, sharing the cached system prompt + tools + message
// prefix with the main session. Only the appended instruction is cold content.
func (e *QueryEngine) inSessionCompact(ctx context.Context, messages []api.Message, systemPrompt []string) ([]api.Message, error) {
	compactMsg := api.Message{
		Role:    api.RoleUser,
		Content: inSessionCompactPrompt,
	}
	messagesWithInstruction := make([]api.Message, len(messages), len(messages)+1)
	copy(messagesWithInstruction, messages)
	messagesWithInstruction = append(messagesWithInstruction, compactMsg)

	resp, err := e.client.SendMessage(ctx, messagesWithInstruction, e.toolParams, nil, systemPrompt, "")
	if err != nil {
		return nil, fmt.Errorf("in-session compaction API call: %w", err)
	}

	e.mu.Lock()
	AccumulateUsage(e.costState, e.model, resp.Usage)
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

	return buildCompactedChain(messages, summary.String()), nil
}

// trySessionMemoryCompact attempts compaction via session memory.
// Currently returns ErrNotImplemented.
func (e *QueryEngine) trySessionMemoryCompact(ctx context.Context, messages []api.Message, cfg CompactConfig, systemPrompt []string) ([]api.Message, error) {
	return nil, fmt.Errorf("session memory compaction not implemented")
}

// forkSummaryAgent forks a single-turn API call to generate a summary of the
// conversation history. Uses proportional dropping: on each retry, drops 25%
// of remaining messages from the head instead of a single API-round group.
func (e *QueryEngine) forkSummaryAgent(ctx context.Context, messages []api.Message, cfg CompactConfig, systemPrompt []string) ([]api.Message, error) {
	// Prepare messages for summary call (strip images/documents)
	summaryMessages := prepareSummaryMessages(messages)

	// Truncate large tool results before sending to summary agent
	summaryMessages = truncateLargeToolResults(summaryMessages, 2000)

	summarySystemPrompt := []string{"You are a helpful assistant that summarizes conversations concisely. For every claim, tag confidence: [CONFIRMED] observed fact, [SPECULATIVE] reasoned but unverified, [UNVERIFIED] stated intent not yet acted on, [EXCLUDED] tried and rejected or ruled out. Cover: (1) primary request and user intent — explicit and implicit, (2) key decisions and rationale, (3) excluded paths — approaches tried and rejected or ruled out, with reasons, (4) errors and fixes, (5) files modified with specific identifiers (function names, type names), (6) key technical concepts, (7) current work when compaction triggered, (8) actionable next steps."}

	var lastErr error
	maxRetries := 5

	for attempt := range maxRetries {
		if attempt > 0 {
			log.Debug("Retrying summary agent", "attempt", attempt+1, "messageCount", len(summaryMessages))
			dropCount := len(summaryMessages) / 4
			if dropCount < 1 {
				dropCount = 1
			}
			if len(summaryMessages)-dropCount < 4 {
				return nil, fmt.Errorf("cannot retry summary: too few messages remaining (%d)", len(summaryMessages))
			}
			summaryMessages = summaryMessages[dropCount:]
			// Advance past any orphaned tool_result message (user msg with
			// ToolResults but no preceding assistant tool_use) to maintain
			// the tool_use/tool_result pairing invariant.
			for len(summaryMessages) > 0 {
				m := summaryMessages[0]
				if m.Role == api.RoleUser && len(m.ToolResults) > 0 {
					summaryMessages = summaryMessages[1:]
					continue
				}
				break
			}
			// Ensure first remaining message is a user message for valid API request
			for len(summaryMessages) > 0 && summaryMessages[0].Role != api.RoleUser {
				summaryMessages = summaryMessages[1:]
			}
			if len(summaryMessages) < 2 {
				return nil, fmt.Errorf("cannot retry summary: too few messages remaining after alignment (%d)", len(summaryMessages))
			}
		}

		resp, err := e.client.SendMessage(ctx, summaryMessages, nil, nil, summarySystemPrompt, "")
		if err != nil {
			lastErr = err
			if isPromptTooLongError(err) {
				continue
			}
			return nil, err
		}

		e.mu.Lock()
		AccumulateUsage(e.costState, e.model, resp.Usage)
		e.mu.Unlock()

		var summaryText strings.Builder
		for _, block := range resp.Content {
			if block.Type == api.BlockTypeText {
				summaryText.WriteString(block.Text)
			}
		}

		if summaryText.String() == "" {
			lastErr = fmt.Errorf("empty summary response")
			continue
		}

		return buildCompactedChain(messages, summaryText.String()), nil
	}

	return nil, fmt.Errorf("summary agent failed after %d attempts: %w", maxRetries, lastErr)
}

// truncateLargeToolResults truncates tool result content that exceeds maxLen
// characters, replacing the middle with a truncation marker.
// Allocates new slices only when truncation is actually needed.
func truncateLargeToolResults(messages []api.Message, maxLen int) []api.Message {
	var result []api.Message // nil until first mutation
	for i, msg := range messages {
		if len(msg.ToolResults) == 0 {
			continue
		}
		mutatedIdx := -1
		for j, tr := range msg.ToolResults {
			if len(tr.Content) > maxLen {
				if result == nil {
					result = make([]api.Message, len(messages))
					copy(result, messages)
				}
				if mutatedIdx != i {
					newResults := make([]api.ToolResultBlock, len(msg.ToolResults))
					copy(newResults, msg.ToolResults)
					result[i].ToolResults = newResults
					mutatedIdx = i
				}
				half := maxLen / 2
				result[i].ToolResults[j].Content = tr.Content[:half] +
					"\n... [truncated for compaction] ...\n" +
					tr.Content[len(tr.Content)-half:]
			}
		}
	}
	if result == nil {
		return messages
	}
	return result
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

// truncateContent truncates content to a maximum number of runes.
func truncateContent(content string, maxLen int) string {
	runes := []rune(content)
	if len(runes) <= maxLen {
		return content
	}
	return string(runes[:maxLen]) + "..."
}

// prepareSummaryMessages prepares messages for the summary API call by stripping
// images and documents and replacing them with markers.
func prepareSummaryMessages(messages []api.Message) []api.Message {
	var result []api.Message
	for _, msg := range messages {
		// Make a copy to avoid modifying original
		processedMsg := msg

		// Strip image/document content from user messages
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
// Order: boundaryMarker → summaryMessages → messagesToKeep → attachments → hookResults
//
// AC7: The cut point never splits a tool_use/tool_result pair. If the boundary
// falls between an assistant with tool_use and its user with tool_results,
// the boundary is moved earlier to include the full pair.
func buildCompactedChain(originalMessages []api.Message, summary string) []api.Message {
	messagesToKeep := 10
	if len(originalMessages) <= messagesToKeep {
		// Nothing to trim — keep everything
	} else {
		cutIdx := len(originalMessages) - messagesToKeep
		// If the message at cutIdx is a user with tool_results, the preceding
		// assistant (cutIdx-1) likely has the matching tool_use. Include both.
		if cutIdx > 0 && originalMessages[cutIdx].Role == "user" && len(originalMessages[cutIdx].ToolResults) > 0 {
			cutIdx--
		}
		originalMessages = originalMessages[cutIdx:]
	}

	var result []api.Message
	result = append(result, api.Message{
		Role:    api.RoleUser,
		Content: "<system-reminder>\n" + compactSummaryPrefix + summary + "\n</system-reminder>",
	})
	result = append(result, originalMessages...)
	return result
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
		AccumulateUsage(e.costState, e.model, resp.Usage)
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

	result := normalizeCompactedChain(buildCompactedChain(truncated, summaryText))
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
