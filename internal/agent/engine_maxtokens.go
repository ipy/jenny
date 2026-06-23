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
// Four orthogonal dimensions are evaluated independently and combined internally.
type ComplexityResult struct {
	ReasoningDepth       int  `json:"reasoning_depth"`
	CodeVolume           int  `json:"code_volume"`
	DomainSpecialization int  `json:"domain_specialization"`
	IntegrationBreadth   int  `json:"integration_breadth"`
	NeedWrite            bool `json:"need_write"`
}

// OverallComplexity computes a weighted aggregate from the four dimensions.
// The weights are internal and not exposed to the evaluation prompt.
func (cr *ComplexityResult) OverallComplexity() float64 {
	return float64(cr.ReasoningDepth)*0.30 +
		float64(cr.CodeVolume)*0.25 +
		float64(cr.DomainSpecialization)*0.20 +
		float64(cr.IntegrationBreadth)*0.25
}

// ThinkingIntensity returns a 1-5 score biased toward reasoning needs.
func (cr *ComplexityResult) ThinkingIntensity() int {
	v := float64(cr.ReasoningDepth)*0.60 + float64(cr.DomainSpecialization)*0.40
	return clampDim(int(v + 0.5))
}

// OutputIntensity returns a 1-5 score biased toward output volume needs.
func (cr *ComplexityResult) OutputIntensity() int {
	v := float64(cr.CodeVolume)*0.50 + float64(cr.IntegrationBreadth)*0.30 + float64(cr.ReasoningDepth)*0.20
	return clampDim(int(v + 0.5))
}

func clampDim(v int) int {
	if v < 1 {
		return 1
	}
	if v > 5 {
		return 5
	}
	return v
}

// TokenBudget holds the separate thinking and completion budgets.
type TokenBudget struct {
	ThinkingBudget   int // tokens allocated for reasoning (for providers that support it)
	CompletionBudget int // tokens allocated for visible output + tool calls
	TotalBudget      int // ThinkingBudget + CompletionBudget (what gets sent to unified-budget APIs)
}

// MaxTokensController manages dynamic max_tokens computation with separate
// thinking and completion budgets.
type MaxTokensController struct {
	mu               sync.RWMutex
	complexityResult *ComplexityResult
	forkFired        bool
	modelMax         int
	progressLevel    ProgressLevel
	justEscalated    bool // true for exactly one ComputeBudget call after a level escalation

	// Per-turn state for adaptive adjustments
	lastStopReason string // "max_tokens", "end_turn", "tool_use", etc.
	lastHadToolUse bool   // whether last turn contained tool calls
	lastHadWrite   bool   // whether last turn contained write/edit tool calls
}

// NewMaxTokensController creates a new controller for the given model.
func NewMaxTokensController(model string) *MaxTokensController {
	return &MaxTokensController{
		modelMax: api.ModelParams(model).MaxOutputTokens,
	}
}

// SetProgressLevel updates the progress monitor level.
// When the level escalates (e.g. L0→L1, L1→L2), the next ComputeBudget call
// gets a one-shot thinking boost for reflection before tightening kicks in.
func (c *MaxTokensController) SetProgressLevel(level ProgressLevel) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if level > c.progressLevel {
		c.justEscalated = true
	}
	c.progressLevel = level
}

// SetComplexityResult stores the fork evaluation result.
func (c *MaxTokensController) SetComplexityResult(cr *ComplexityResult) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.complexityResult = cr
}

// GetComplexityResult returns the current complexity result (nil if not yet available).
func (c *MaxTokensController) GetComplexityResult() *ComplexityResult {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.complexityResult
}

// RecordTurnOutcome records signals from the completed turn for next-turn adaptation.
func (c *MaxTokensController) RecordTurnOutcome(stopReason string, hadToolUse, hadWrite bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastStopReason = stopReason
	c.lastHadToolUse = hadToolUse
	c.lastHadWrite = hadWrite
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

// ComputeBudget returns the token budget for the given turn.
func (c *MaxTokensController) ComputeBudget(turn int) TokenBudget {
	c.mu.Lock()
	cr := c.complexityResult
	level := c.progressLevel
	lastStop := c.lastStopReason
	lastWrite := c.lastHadWrite
	reflectionTurn := c.justEscalated
	c.justEscalated = false // consume: boost applies to exactly one turn
	c.mu.Unlock()

	if turn <= 3 {
		return c.earlyTurnBudget()
	}

	var thinking, completion int
	if cr != nil {
		thinking = c.thinkingBudget(cr, turn, level, reflectionTurn)
		completion = c.completionBudget(cr, turn, level, lastStop, lastWrite)
	} else {
		tb := turnBasedMaxTokens(turn, c.modelMax)
		thinking = tb * 40 / 100
		completion = tb - thinking
	}

	total := thinking + completion
	total = c.applyProgressCap(total, level)

	if total > c.modelMax {
		total = c.modelMax
	}

	// Re-proportion thinking/completion after capping
	if thinking+completion > 0 && total < thinking+completion {
		ratio := float64(thinking) / float64(thinking+completion)
		thinking = int(float64(total) * ratio)
		completion = total - thinking
	}

	log.Debug("TokenBudget computed",
		"turn", turn,
		"thinking", thinking,
		"completion", completion,
		"total", total,
		"level", level,
		"reflection", reflectionTurn,
		"hasComplexity", cr != nil,
	)
	return TokenBudget{ThinkingBudget: thinking, CompletionBudget: completion, TotalBudget: total}
}

// ComputeEffective returns the total max_tokens for APIs that use a unified budget.
// This is the primary entry point used by the engine loop.
func (c *MaxTokensController) ComputeEffective(turn int) int {
	return c.ComputeBudget(turn).TotalBudget
}

func (c *MaxTokensController) earlyTurnBudget() TokenBudget {
	cap := 8192
	if c.modelMax < 100_000 {
		cap = min(4096, c.modelMax)
	} else {
		cap = min(8192, c.modelMax)
	}
	thinking := cap * 30 / 100
	completion := cap - thinking
	return TokenBudget{ThinkingBudget: thinking, CompletionBudget: completion, TotalBudget: cap}
}

// thinkingBudget calculates the thinking token allocation based on complexity and turn state.
// reflectionTurn is true for exactly one turn after a progress level escalation,
// granting a one-shot thinking boost so the agent can reflect on why it's stuck.
// All subsequent turns at the same level get tightened budgets to force action.
func (c *MaxTokensController) thinkingBudget(cr *ComplexityResult, turn int, level ProgressLevel, reflectionTurn bool) int {
	intensity := cr.ThinkingIntensity() // 1-5
	// Base: per intensity level, with model-proportional scaling
	base := intensity * c.modelMax / 100

	if reflectionTurn && (level == ProgressLevelL1 || level == ProgressLevelL2) {
		// One-shot reflection boost: the agent just got flagged as stuck,
		// give it extra thinking capacity to reconsider its approach.
		base = base * 160 / 100
	} else {
		// Steady-state: tighten thinking to push toward action, not rumination.
		switch level {
		case ProgressLevelL1:
			base = base * 70 / 100
		case ProgressLevelL2, ProgressLevelL3:
			base = base * 40 / 100
		}
	}

	// Cap thinking at 60% of model max to leave room for output
	maxThinking := c.modelMax * 60 / 100
	if base > maxThinking {
		base = maxThinking
	}

	// Anthropic enforces a minimum of 1024 for budget_tokens.
	if base < 1024 {
		base = 1024
	}

	return base
}

// completionBudget calculates the completion token allocation.
func (c *MaxTokensController) completionBudget(cr *ComplexityResult, turn int, level ProgressLevel, lastStop string, lastWrite bool) int {
	intensity := cr.OutputIntensity() // 1-5
	base := intensity * c.modelMax / 100
	if base < 2048 {
		base = 2048
	}

	// Boost for code-writing tasks
	if cr.NeedWrite {
		base = base * 3
	}

	// Spike protection: if previous turn hit max_tokens, boost completion
	if lastStop == "max_tokens" {
		base = base * 150 / 100
	}

	// Sustained write phase: if last turn had writes, maintain high completion
	if lastWrite {
		minWrite := 8192
		if base < minWrite {
			base = minWrite
		}
	}

	if base > c.modelMax {
		base = c.modelMax
	}
	return base
}

// applyProgressCap applies progress-level-based caps to the total budget.
func (c *MaxTokensController) applyProgressCap(total int, level ProgressLevel) int {
	var cap int
	switch level {
	case ProgressLevelL3:
		cap = 2048
	case ProgressLevelL2:
		cap = 4096
	case ProgressLevelL1:
		cap = 8192
	default:
		return total
	}
	if c.modelMax < cap {
		cap = c.modelMax
	}
	if cap < 2048 {
		cap = 2048
	}
	if total > cap {
		return cap
	}
	return total
}

// turnBasedMaxTokens returns the schedule-based default for a given turn.
// Uses a generous initial budget that tapers gradually, with a floor that
// accommodates thinking tokens on models where they share the output budget.
func turnBasedMaxTokens(turn int, modelMax int) int {
	switch {
	case turn <= 3:
		if modelMax >= 100_000 {
			return min(8192, modelMax)
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
Evaluate the task being worked on across these dimensions. Respond ONLY with a JSON object, no other text, comments or fences:
{"reasoning_depth": <1-5>, "code_volume": <1-5>, "domain_specialization": <1-5>, "integration_breadth": <1-5>, "need_write": <true/false>}

Dimension rubrics (pick the LOWEST level that fits for each):

REASONING DEPTH — how much deliberation is needed before acting:
  1 = Obvious next step. e.g., rename a variable, fix a typo.
  2 = Straightforward logic. e.g., add a simple endpoint, write a unit test.
  3 = Moderate analysis. e.g., trace a bug across 2-3 call sites, choose between two design options.
  4 = Deep reasoning. e.g., design a concurrency strategy, analyze a subtle race condition.
  5 = Research-level. e.g., prove correctness of a distributed protocol, devise a novel algorithm.

CODE VOLUME — how much code/text needs to be produced:
  1 = A few lines. e.g., one-line fix, add a config entry.
  2 = A function or small block. e.g., add a helper, write a short test.
  3 = Multiple functions or a module. e.g., implement a REST handler with validation.
  4 = Several files or a subsystem. e.g., add JWT auth across middleware, routes, and tests.
  5 = Large-scale generation. e.g., scaffold an entire service, generate a full test suite.

DOMAIN SPECIALIZATION — how niche the required knowledge is:
  1 = General programming. e.g., string manipulation, basic I/O.
  2 = Common framework usage. e.g., Express.js routing, React components.
  3 = Specialized library or protocol. e.g., gRPC interceptors, OpenGL shaders.
  4 = Deep domain expertise. e.g., cryptographic implementations, kernel module development.
  5 = Research-level niche. e.g., LLVM IR optimization passes, custom FPGA synthesis.

INTEGRATION BREADTH — how many components or files are touched:
  1 = Single file. e.g., edit one function in one file.
  2 = 2-3 files. e.g., update a function and its test.
  3 = A module or package. e.g., refactor a package's public API.
  4 = Cross-module. e.g., change a shared interface used by 5+ consumers.
  5 = System-wide. e.g., migrate a database schema affecting all layers.

need_write=true if the task involves writing/modifying code, articles, or other complex artifacts.`

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

	cr.ReasoningDepth = clampDim(cr.ReasoningDepth)
	cr.CodeVolume = clampDim(cr.CodeVolume)
	cr.DomainSpecialization = clampDim(cr.DomainSpecialization)
	cr.IntegrationBreadth = clampDim(cr.IntegrationBreadth)

	log.Debug("Complexity eval result",
		"reasoning_depth", cr.ReasoningDepth,
		"code_volume", cr.CodeVolume,
		"domain_specialization", cr.DomainSpecialization,
		"integration_breadth", cr.IntegrationBreadth,
		"need_write", cr.NeedWrite,
	)
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
