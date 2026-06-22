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
		base = c.earlyTurnCap()
	} else if cr != nil {
		base = computeMaxTokens(cr.Complexity, cr.IsCoding, c.modelMax)
	} else {
		base = turnBasedMaxTokens(turn, c.modelMax)
	}

	switch level {
	case ProgressLevelL3:
		cap := c.progressCap(2048)
		if base > cap {
			base = cap
		}
	case ProgressLevelL2:
		cap := c.progressCap(4096)
		if base > cap {
			base = cap
		}
	case ProgressLevelL1:
		cap := c.progressCap(8192)
		if base > cap {
			base = cap
		}
	}

	log.Debug("MaxTokens computed", "turn", turn, "base", base, "level", level, "hasComplexity", cr != nil)
	return base
}

// earlyTurnCap returns the max_tokens cap for the first 3 discovery turns.
// For models with large output capacity (thinking models where thinking tokens
// share the output budget), a higher cap prevents thinking from starving tool calls.
func (c *MaxTokensController) earlyTurnCap() int {
	if c.modelMax >= 100_000 {
		return min(16384, c.modelMax)
	}
	return min(4096, c.modelMax)
}

// progressCap returns the max_tokens cap for a given progress level,
// scaled to the model's capacity. Never returns below 2048.
func (c *MaxTokensController) progressCap(desired int) int {
	if desired > c.modelMax {
		return c.modelMax
	}
	if desired < 2048 {
		return 2048
	}
	return desired
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
// Uses a generous initial budget that tapers gradually, with a floor that
// accommodates thinking tokens on models where they share the output budget.
func turnBasedMaxTokens(turn int, modelMax int) int {
	switch {
	case turn <= 3:
		if modelMax >= 100_000 {
			return min(16384, modelMax)
		}
		return min(4096, modelMax)
	case turn <= 10:
		return min(16384, modelMax)
	default:
		v := 16384 - (turn-10)*400
		if v < 4096 {
			v = 4096
		}
		return min(v, modelMax)
	}
}

const complexityEvalPrompt = `<system-reminder>
This is a separate meta-evaluation request. Do not continue any prior conversation or call any tools.
</system-reminder>
Evaluate the task being worked on. Respond ONLY with a JSON object, no other text, comments or fences:
{"complexity": <1-5>, "is_coding": <true/false>}

Complexity rubric (pick the LOWEST level that fits):
1 = Trivial: one obvious action. e.g., "rename variable x to count", "add a doc comment to calculateTotal".
2 = Simple: one component, a few steps. e.g., "add a /health endpoint returning 200", "install Postgres and run the migration scripts".
3 = Moderate: several components or moderate domain knowledge. e.g., "swap session auth for JWT, update callers, add token refresh", "build a CI pipeline that lints, tests on 3 Python versions, and builds a Docker image".
4 = Complex: many interacting parts or non-trivial algorithms. e.g., "implement a distributed lock with leader election and fencing tokens across 5 nodes", "diagnose a thread-pool race condition that only reproduces under load".
5 = Very complex: research-level work, novel algorithms, or deep obscure-domain expertise. e.g., "implement a cost-based query optimizer with histograms and join enumeration", "write a JIT backend for a subset of WASM targeting a custom RISC-V accelerator".

is_coding=true if the task involves writing/modifying code OR operating developer tooling (compilers, build systems, servers, git, vms, debuggers). is_coding=false ONLY for pure reasoning tasks (math proofs, analysis, Q&A) with no code or tools.`

// forkComplexityEval sends a side request to evaluate task complexity.
// It reuses the cached system prompt prefix for cache hits.
func forkComplexityEval(ctx context.Context, client api.Requester, messages []api.Message, systemPrompt []string, tools []api.ToolParam) (*ComplexityResult, string, api.Usage, error) {
	evalMsg := api.Message{
		Role:    api.RoleUser,
		Content: complexityEvalPrompt,
	}
	forkMessages := make([]api.Message, len(messages), len(messages)+1)
	copy(forkMessages, messages)
	forkMessages = append(forkMessages, evalMsg)

	resp, err := client.SendMessage(ctx, forkMessages, tools, nil, systemPrompt, "")
	if err != nil {
		return nil, "", api.Usage{}, err
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
		return nil, "", api.Usage{}, err
	}

	if cr.Complexity < 1 || cr.Complexity > 5 {
		cr.Complexity = 3
	}

	log.Debug("Complexity eval result", "complexity", cr.Complexity, "is_coding", cr.IsCoding)
	return &cr, resp.Model, resp.Usage, nil
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
