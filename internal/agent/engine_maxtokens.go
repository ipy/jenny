package agent

import (
	"context"
	"encoding/json"
	"strings"
	"sync"

	"github.com/ipy/jenny/internal/api"
	"github.com/ipy/jenny/internal/log"
)

// ComplexityResult holds the parsed result from a fork-based complexity evaluation.
type ComplexityResult struct {
	Complexity int  `json:"complexity"`
	IsCoding   bool `json:"is_coding"`
}

// MaxTokensController manages dynamic max_tokens computation.
type MaxTokensController struct {
	mu               sync.RWMutex
	complexityResult *ComplexityResult
	forkFired        bool
	modelMax         int
	progressLevel    ProgressLevel
}

// NewMaxTokensController creates a new controller for the given model.
func NewMaxTokensController(model string) *MaxTokensController {
	return &MaxTokensController{
		modelMax: api.ModelParams(model).MaxOutputTokens,
	}
}

// SetProgressLevel updates the progress monitor level for max_tokens capping.
func (c *MaxTokensController) SetProgressLevel(level ProgressLevel) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.progressLevel = level
}

// SetComplexityResult stores the fork evaluation result.
func (c *MaxTokensController) SetComplexityResult(cr *ComplexityResult) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.complexityResult = cr
}

// ComplexityResult returns the current complexity result (nil if not yet available).
func (c *MaxTokensController) GetComplexityResult() *ComplexityResult {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.complexityResult
}

// ShouldFork returns true if the fork hasn't been fired yet and sets forkFired=true atomically.
func (c *MaxTokensController) ShouldFork() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.forkFired {
		return false
	}
	c.forkFired = true
	return true
}

// ComputeEffective returns the effective max_tokens for the given turn.
func (c *MaxTokensController) ComputeEffective(turn int) int {
	c.mu.RLock()
	cr := c.complexityResult
	level := c.progressLevel
	c.mu.RUnlock()

	var base int
	if turn <= 3 {
		base = 2000
	} else if cr != nil {
		base = computeMaxTokens(cr.Complexity, cr.IsCoding, c.modelMax)
	} else {
		base = turnBasedMaxTokens(turn, c.modelMax)
	}

	switch level {
	case ProgressLevelL2:
		if base > 1000 {
			base = 1000
		}
	case ProgressLevelL1:
		if base > 2000 {
			base = 2000
		}
	}

	return base
}

// computeMaxTokens calculates max_tokens from complexity and coding flag.
func computeMaxTokens(complexity int, isCoding bool, modelMax int) int {
	if complexity < 1 {
		complexity = 1
	}
	if complexity > 5 {
		complexity = 5
	}
	base := modelMax * complexity / 100
	if base < 800 {
		base = 800
	}
	if isCoding {
		base *= 5
	}
	if base > modelMax {
		base = modelMax
	}
	return base
}

// turnBasedMaxTokens returns the schedule-based default for a given turn.
func turnBasedMaxTokens(turn int, modelMax int) int {
	switch {
	case turn <= 3:
		return 2000
	case turn <= 10:
		return min(8000, modelMax)
	default:
		v := 8000 - (turn-10)*200
		if v < 2000 {
			v = 2000
		}
		return min(v, modelMax)
	}
}

const complexityEvalPrompt = `Evaluate the task being worked on. Respond ONLY with a JSON object, no other text:
{"complexity": <1-5>, "is_coding": <true/false>}
Where complexity 1=trivial, 5=very complex, and is_coding=true if the task involves writing/modifying code.`

// forkComplexityEval sends a side request to evaluate task complexity.
// It reuses the cached system prompt prefix for cache hits.
func forkComplexityEval(ctx context.Context, client api.Requester, messages []api.Message, systemPrompt []string) (*ComplexityResult, error) {
	evalMsg := api.Message{
		Role:    api.RoleUser,
		Content: complexityEvalPrompt,
	}
	forkMessages := make([]api.Message, len(messages), len(messages)+1)
	copy(forkMessages, messages)
	forkMessages = append(forkMessages, evalMsg)

	resp, err := client.SendMessage(ctx, forkMessages, nil, nil, systemPrompt, "")
	if err != nil {
		return nil, err
	}

	var text string
	for _, block := range resp.Content {
		if block.Type == "text" || block.Text != "" {
			text += block.Text
		}
	}

	text = extractJSON(text)

	var cr ComplexityResult
	if err := json.Unmarshal([]byte(text), &cr); err != nil {
		return nil, err
	}

	if cr.Complexity < 1 || cr.Complexity > 5 {
		cr.Complexity = 3
	}

	log.Debug("Complexity eval result", "complexity", cr.Complexity, "is_coding", cr.IsCoding)
	return &cr, nil
}

// extractJSON finds the first {...} in the text.
func extractJSON(text string) string {
	start := strings.Index(text, "{")
	if start < 0 {
		return text
	}
	end := strings.LastIndex(text, "}")
	if end < start {
		return text
	}
	return text[start : end+1]
}
