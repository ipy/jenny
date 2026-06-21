package agent

import (
	"encoding/json"
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
)

// TokenEstimator provides charset-aware token estimation with per-model EWMA
// calibration from actual API usage data. It is the single source of truth for
// all token estimation in Jenny.
type TokenEstimator struct {
	mu                     sync.Mutex
	asciiCharsPerToken     float64
	multiByteCharsPerToken float64
	modelCorrections       map[string]*modelCorrection
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
			// +50 for JSON envelope: {"type":"tool_use",...}
			total += len(tu.Name) + 50
			charCount += len(tu.Name) + 50
			inputBytes, _ := json.Marshal(tu.Input)
			total += e.EstimateString(string(inputBytes), model)
			charCount += len(inputBytes)
		}
		for _, tr := range msg.ToolResults {
			total += e.EstimateString(tr.Content, model) + 50
			charCount += len(tr.Content) + 50
		}
	}
	return total, charCount
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
