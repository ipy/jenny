package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/ipy/jenny/internal/api"
)

const (
	// Default ASCII/Latin charset ratio: ~4 characters per token.
	defaultASCIICharsPerToken = 4.0
	// Default multi-byte rune ratio: ~1.5 characters per token.
	defaultMultiByteCharsPerToken = 1.5
	// EWMA alpha — weight given to the most recent observation.
	ewmaAlpha = 0.2
	// wireFixedOverhead accounts for message structure, cache control markers,
	// thinking parameters, and other per-request overhead not in message content.
	wireFixedOverhead = 200
	// toolMessageEnvelopeOverhead accounts for the JSON envelope wrapping each
	// tool_use and tool_result block: {"type":"tool_use",...} and similar.
	toolMessageEnvelopeOverhead = 50
)

// TokenEstimator provides charset-aware token estimation with per-model EWMA
// calibration from actual API usage data. It is the single source of truth for
// all token estimation in Jenny.
type TokenEstimator struct {
	mu                     sync.Mutex
	asciiCharsPerToken     float64
	multiByteCharsPerToken float64
	modelCorrections       map[string]*modelCorrection
	toolTokenCache         cachedToolTokens
}

// cachedToolTokens holds the cached token estimate for tool definitions.
type cachedToolTokens struct {
	fingerprint string // hash of sorted tool names
	tokens      int
}

// modelCorrection tracks EWMA correction factors for a single model.
// A factor of 1.0 means the base ratio is correct.
// A factor < 1.0 means the model packs MORE chars per token than the base ratio predicts.
type modelCorrection struct {
	inputFactor  float64
	outputFactor float64
	sampleCount  int
}

// NewTokenEstimator returns a new TokenEstimator with default base ratios.
func NewTokenEstimator() *TokenEstimator {
	return &TokenEstimator{
		asciiCharsPerToken:     defaultASCIICharsPerToken,
		multiByteCharsPerToken: defaultMultiByteCharsPerToken,
		modelCorrections:       make(map[string]*modelCorrection),
	}
}

// estimateCharset counts ASCII vs multi-byte runes in s, returning (asciiCount, mbCount).
func estimateCharset(s string) (asciiCount, mbCount int) {
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r < 128 {
			asciiCount++
		} else {
			mbCount++
		}
		i += size
	}
	return
}

// EstimateString estimates token count for a string using charset-aware ratios
// with per-model EWMA correction applied. When no calibration data exists for the
// model, raw base ratios are used.
func (e *TokenEstimator) EstimateString(s string, model string) int {
	if len(s) == 0 {
		return 0
	}
	asciiCount, mbCount := estimateCharset(s)

	// Raw estimate from base ratios
	asciiTokens := float64(asciiCount) / e.asciiCharsPerToken
	mbTokens := float64(mbCount) / e.multiByteCharsPerToken
	rawEstimate := asciiTokens + mbTokens

	// Apply per-model correction if available
	correction := e.getInputCorrection(model)
	corrected := rawEstimate * correction

	tokens := int(corrected)
	if tokens < 1 && len(s) > 0 {
		tokens = 1
	}
	return tokens
}

// EstimateStringOutput is like EstimateString but applies the output correction factor,
// for estimating token counts of model-generated text (thinking, response content).
func (e *TokenEstimator) EstimateStringOutput(s string, model string) int {
	if len(s) == 0 {
		return 0
	}
	asciiCount, mbCount := estimateCharset(s)

	asciiTokens := float64(asciiCount) / e.asciiCharsPerToken
	mbTokens := float64(mbCount) / e.multiByteCharsPerToken
	rawEstimate := asciiTokens + mbTokens

	correction := e.getOutputCorrection(model)
	corrected := rawEstimate * correction

	tokens := int(corrected)
	if tokens < 1 && len(s) > 0 {
		tokens = 1
	}
	return tokens
}

// EstimateMessages estimates the token count for a message chain.
// Equivalent to the old estimateTokens but model-aware.
func (e *TokenEstimator) EstimateMessages(messages []api.Message, model string) int {
	total, _ := e.EstimateMessagesWithChars(messages, model)
	return total
}

// EstimateMessagesWithChars estimates tokens and also returns the total character
// count, useful for feeding observations back into the estimator.
func (e *TokenEstimator) EstimateMessagesWithChars(messages []api.Message, model string) (int, int) {
	total := 0
	charCount := 0
	for _, msg := range messages {
		total += e.EstimateString(msg.Content, model)
		charCount += len(msg.Content)
		total += e.EstimateString(msg.Thinking, model)
		charCount += len(msg.Thinking)
		total += e.EstimateString(msg.Signature, model)
		charCount += len(msg.Signature)
		for _, tu := range msg.ToolUse {
			total += len(tu.Name) + toolMessageEnvelopeOverhead
			charCount += len(tu.Name) + toolMessageEnvelopeOverhead
			inputBytes, _ := json.Marshal(tu.Input)
			total += e.EstimateString(string(inputBytes), model)
			charCount += len(inputBytes)
		}
		for _, tr := range msg.ToolResults {
			total += e.EstimateString(tr.Content, model) + toolMessageEnvelopeOverhead
			charCount += len(tr.Content) + toolMessageEnvelopeOverhead
		}
	}
	return total, charCount
}

// EstimateWireTokens estimates the total token count for a complete API request,
// including messages, system prompt, tool definitions, and fixed overhead.
func (e *TokenEstimator) EstimateWireTokens(
	messages []api.Message,
	systemPrompt []string,
	tools []api.ToolParam,
	model string,
) int {
	total := e.EstimateMessages(messages, model)
	for _, block := range systemPrompt {
		total += e.EstimateString(block, model)
	}
	total += e.estimateToolTokensCached(tools, model)
	total += wireFixedOverhead
	return total
}

// estimateSystemAndToolTokensCalibrated is the model-aware version of
// estimateSystemAndToolTokens for use when a TokenEstimator is available.
func (e *TokenEstimator) estimateSystemAndToolTokensCalibrated(systemPrompt []string, tools []api.ToolParam, model string) int {
	total := wireFixedOverhead
	for _, block := range systemPrompt {
		total += e.EstimateString(block, model)
	}
	total += e.estimateToolTokensCached(tools, model)
	return total
}

// estimateToolTokensCached returns the token estimate for tool definitions,
// reusing a per-session cache keyed by tool name fingerprint.
func (e *TokenEstimator) estimateToolTokensCached(tools []api.ToolParam, model string) int {
	fp := toolsFingerprint(tools)
	e.mu.Lock()
	if e.toolTokenCache.fingerprint == fp {
		tokens := e.toolTokenCache.tokens
		e.mu.Unlock()
		return tokens
	}
	e.mu.Unlock()

	tokens := 0
	if jsonBytes, err := json.Marshal(tools); err == nil && len(jsonBytes) > 0 {
		tokens = e.EstimateString(string(jsonBytes), model)
	}

	e.mu.Lock()
	e.toolTokenCache = cachedToolTokens{fingerprint: fp, tokens: tokens}
	e.mu.Unlock()
	return tokens
}

// ObserveWireInput records actual API input tokens against a wire-equivalent
// prediction (uncorrected) and updates the input EWMA correction factor.
func (e *TokenEstimator) ObserveWireInput(model string, predictedWireTokens int, actualTokens int) {
	if predictedWireTokens <= 0 || actualTokens <= 0 || model == "" {
		return
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	corr, ok := e.modelCorrections[model]
	if !ok {
		corr = &modelCorrection{
			inputFactor:  1.0,
			outputFactor: 1.0,
		}
		e.modelCorrections[model] = corr
	}

	observedCorrection := float64(actualTokens) / float64(predictedWireTokens)
	corr.inputFactor = ewmaAlpha*observedCorrection + (1-ewmaAlpha)*corr.inputFactor
	corr.sampleCount++
}

// Observe records an actual token count from API usage data and updates the EWMA
// correction factor for the model. charCount should be the total character count
// (as counted by estimateCharset) of the text that produced actualTokens.
// isOutput should be true for output/completion tokens, false for input/prompt tokens.
func (e *TokenEstimator) Observe(model string, charCount int, actualTokens int, isOutput bool) {
	if charCount <= 0 || actualTokens <= 0 || model == "" {
		return
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	corr, ok := e.modelCorrections[model]
	if !ok {
		corr = &modelCorrection{
			inputFactor:  1.0,
			outputFactor: 1.0,
		}
		e.modelCorrections[model] = corr
	}

	// Compute what the base ratio predicted
	asciiCount, mbCount := estimateCharsetFromCount(charCount)
	rawPredicted := float64(asciiCount)/e.asciiCharsPerToken + float64(mbCount)/e.multiByteCharsPerToken
	if rawPredicted <= 0 {
		return
	}
	// observed correction: how much the base prediction was off
	observedCorrection := float64(actualTokens) / rawPredicted

	if isOutput {
		corr.outputFactor = ewmaAlpha*observedCorrection + (1-ewmaAlpha)*corr.outputFactor
	} else {
		corr.inputFactor = ewmaAlpha*observedCorrection + (1-ewmaAlpha)*corr.inputFactor
	}
	corr.sampleCount++
}

// ObserveInput observes an input token observation using actual character counts
// from messages (the same charset breakdown used by EstimateMessages).
func (e *TokenEstimator) ObserveInput(model string, asciiCount, mbCount int, actualTokens int) {
	if asciiCount <= 0 && mbCount <= 0 || actualTokens <= 0 || model == "" {
		return
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	corr, ok := e.modelCorrections[model]
	if !ok {
		corr = &modelCorrection{
			inputFactor:  1.0,
			outputFactor: 1.0,
		}
		e.modelCorrections[model] = corr
	}

	rawPredicted := float64(asciiCount)/e.asciiCharsPerToken + float64(mbCount)/e.multiByteCharsPerToken
	if rawPredicted <= 0 {
		return
	}
	observedCorrection := float64(actualTokens) / rawPredicted
	corr.inputFactor = ewmaAlpha*observedCorrection + (1-ewmaAlpha)*corr.inputFactor
	corr.sampleCount++
}

// ObserveOutput observes an output token observation using a simple character count.
func (e *TokenEstimator) ObserveOutput(model string, charCount int, actualTokens int) {
	e.Observe(model, charCount, actualTokens, true)
}

// getInputCorrection returns the input correction factor for the model, or 1.0 if no data.
func (e *TokenEstimator) getInputCorrection(model string) float64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	if corr, ok := e.modelCorrections[model]; ok && corr.sampleCount > 0 {
		return corr.inputFactor
	}
	return 1.0
}

// getOutputCorrection returns the output correction factor for the model, or 1.0 if no data.
func (e *TokenEstimator) getOutputCorrection(model string) float64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	if corr, ok := e.modelCorrections[model]; ok && corr.sampleCount > 0 {
		return corr.outputFactor
	}
	return 1.0
}

// estimateCharsetFromCount returns an approximate ASCII/multi-byte split from a
// total character count, assuming all chars are ASCII. This is used when only a
// character count is available (not the actual string). This is a fallback — the
// correction will still converge because the same approximation is used for both
// prediction and observation.
func estimateCharsetFromCount(totalChars int) (asciiCount, mbCount int) {
	return totalChars, 0
}

// ---------------------------------------------------------------------------
// Uncalibrated estimation helpers — used by SideQuery cost estimation,
// compact fallback (when TokenEstimator is nil), and engine_loop token checks.
// These are the simpler, non-EWMA versions with fixed ratios.
// ---------------------------------------------------------------------------

// estimateWireTokens is the uncalibrated version using fixed ratios.
func estimateWireTokens(
	messages []api.Message,
	systemPrompt []string,
	tools []api.ToolParam,
) int {
	return estimateTokens(messages) + estimateSystemAndToolTokens(systemPrompt, tools)
}

// estimateSystemAndToolTokens estimates non-message wire overhead: system prompt,
// tool definitions, and fixed per-request overhead.
func estimateSystemAndToolTokens(systemPrompt []string, tools []api.ToolParam) int {
	total := wireFixedOverhead
	for _, block := range systemPrompt {
		total += estimateStringTokens(block)
	}
	total += estimateToolDefinitionTokens(tools)
	return total
}

// effectiveWireOverhead returns the computed wire overhead, falling back to
// MIN_SAFETY_OVERHEAD when the precise estimate is unavailable (zero).
func effectiveWireOverhead(computed int) int {
	if computed <= 0 {
		return MIN_SAFETY_OVERHEAD
	}
	return computed
}

// estimateToolDefinitionTokens estimates tokens for serialized tool definitions.
func estimateToolDefinitionTokens(tools []api.ToolParam) int {
	if len(tools) == 0 {
		return 0
	}
	jsonBytes, err := json.Marshal(tools)
	if err != nil || len(jsonBytes) == 0 {
		return 0
	}
	return estimateStringTokens(string(jsonBytes))
}

// toolsFingerprint returns a stable hash of sorted tool names for cache lookup.
func toolsFingerprint(tools []api.ToolParam) string {
	if len(tools) == 0 {
		return ""
	}
	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = t.Name
	}
	sort.Strings(names)
	h := sha256.Sum256([]byte(strings.Join(names, "\x00")))
	return hex.EncodeToString(h[:])
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
			total += len(tu.Name) + toolMessageEnvelopeOverhead
			inputBytes, _ := json.Marshal(tu.Input)
			total += estimateStringTokens(string(inputBytes))
		}
		for _, tr := range msg.ToolResults {
			total += estimateStringTokens(tr.Content) + toolMessageEnvelopeOverhead
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
	asciiChars, multiByteRunes := estimateCharset(s)
	asciiTokens := asciiChars * 10 / 40  // asciiChars / 4.0
	mbTokens := multiByteRunes * 10 / 15 // multiByteRunes / 1.5
	return asciiTokens + mbTokens
}
