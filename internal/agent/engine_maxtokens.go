package agent

import (
	"encoding/json"
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

// MaxTokensController manages dynamic max_tokens computation with a FIXED
// thinking budget and variable completion budget.
//
// The thinking budget (budget_tokens for Anthropic) is determined once at
// session start and never changes. This is critical for prompt cache
// stability: Anthropic's cache key includes thinking parameters, so any
// change to budget_tokens invalidates the entire cached prefix.
//
// To control thinking intensity without changing the budget, use
// ThinkingGuidanceHint() to generate prompt-based guidance that can be
// injected as a <system-reminder> message.
type MaxTokensController struct {
	mu               sync.RWMutex
	complexityResult *ComplexityResult
	forkFired        bool
	modelMax         int
	progressLevel    ProgressLevel
	justEscalated    bool // true for exactly one ComputeBudget call after a level escalation

	// Fixed thinking budget — set once, never changes.
	fixedThinkingBudget int

	// Per-turn state for adaptive adjustments
	lastStopReason string // "max_tokens", "end_turn", "tool_use", etc.
	lastHadToolUse bool   // whether last turn contained tool calls
	lastHadWrite   bool   // whether last turn contained write/edit tool calls
}

// NewMaxTokensController creates a new controller for the given model.
// The thinking budget is computed once and remains constant for the session.
func NewMaxTokensController(model string) *MaxTokensController {
	modelMax := api.ModelParams(model).MaxOutputTokens
	return &MaxTokensController{
		modelMax:            modelMax,
		fixedThinkingBudget: computeFixedThinkingBudget(modelMax),
	}
}

// computeFixedThinkingBudget determines a generous, fixed thinking budget.
// The budget is large enough for deep reasoning on complex tasks; the model
// self-regulates actual usage. Using a fixed value prevents cache invalidation.
func computeFixedThinkingBudget(modelMax int) int {
	budget := modelMax * 60 / 100
	if budget < 1024 {
		budget = 1024
	}
	return budget
}

// FixedThinkingBudget returns the session's immutable thinking budget.
// Use this when creating side query clients that must share the cache prefix.
func (c *MaxTokensController) FixedThinkingBudget() int {
	return c.fixedThinkingBudget
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
// ThinkingBudget is ALWAYS the fixed value — only CompletionBudget varies.
func (c *MaxTokensController) ComputeBudget(turn int) TokenBudget {
	c.mu.Lock()
	cr := c.complexityResult
	level := c.progressLevel
	lastStop := c.lastStopReason
	lastWrite := c.lastHadWrite
	c.justEscalated = false
	c.mu.Unlock()

	thinking := c.fixedThinkingBudget

	var completion int
	if turn <= 3 {
		completion = c.earlyCompletionBudget()
	} else if cr != nil {
		completion = c.completionBudget(cr, turn, level, lastStop, lastWrite)
	} else {
		tb := turnBasedMaxTokens(turn, c.modelMax)
		completion = tb * 60 / 100
	}

	completion = c.applyCompletionCap(completion, level)

	// Ensure minimum completion room
	if completion < 1024 {
		completion = 1024
	}

	total := thinking + completion
	if total > c.modelMax {
		total = c.modelMax
		completion = total - thinking
		if completion < 1024 {
			completion = 1024
			thinking = total - completion
		}
	}

	log.Debug("TokenBudget computed",
		"turn", turn,
		"thinking", thinking,
		"completion", completion,
		"total", total,
		"level", level,
		"hasComplexity", cr != nil,
	)
	return TokenBudget{ThinkingBudget: thinking, CompletionBudget: completion, TotalBudget: total}
}

// ComputeEffective returns the total max_tokens for APIs that use a unified budget.
// This is the primary entry point used by the engine loop.
func (c *MaxTokensController) ComputeEffective(turn int) int {
	return c.ComputeBudget(turn).TotalBudget
}

// earlyCompletionBudget returns the completion budget for turns 1-3.
func (c *MaxTokensController) earlyCompletionBudget() int {
	if c.modelMax >= 100_000 {
		return min(4096, c.modelMax)
	}
	return min(2048, c.modelMax)
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

// applyCompletionCap applies progress-level-based caps to the completion budget.
// Only completion is capped — the thinking budget is immutable.
func (c *MaxTokensController) applyCompletionCap(completion int, level ProgressLevel) int {
	var cap int
	switch level {
	case ProgressLevelL3:
		cap = 2048
	case ProgressLevelL2:
		cap = 4096
	case ProgressLevelL1:
		cap = 8192
	default:
		return completion
	}
	if completion > cap {
		return cap
	}
	return completion
}

// ThinkingGuidanceHint returns a prompt-based hint for controlling the model's
// thinking intensity. This replaces the old approach of dynamically changing
// budget_tokens, which caused cache invalidation.
//
// The hint is injected as part of a <system-reminder> when the agent's behavior
// deviates from expectations (e.g., stuck at a progress level, or early turns
// where brief thinking suffices).
//
// When isResume is true, early-turn hints are suppressed because the agent is
// continuing prior work rather than starting a fresh exploration.
//
// The "straightforward task" hint is cross-checked against actual thinking token
// data from ThinkingMonitor: if the model is already thinking concisely, the hint
// is unnecessary; if the static complexity eval says "simple" but the model is
// actually thinking heavily, the hint would be misleading.
func (c *MaxTokensController) ThinkingGuidanceHint(turn int, isResume bool, tm *ThinkingMonitor) string {
	c.mu.RLock()
	cr := c.complexityResult
	level := c.progressLevel
	c.mu.RUnlock()

	switch {
	case turn <= 2 && !isResume:
		return "This is an early exploration turn. Think briefly — focus on understanding the task and gathering information before deep analysis."
	case level >= ProgressLevelL2:
		return "You appear to be stuck. Think very briefly about what is fundamentally different from what you have already tried, and consider taking a concrete, different action. Avoid repeating prior approaches."
	case level >= ProgressLevelL1:
		return "Progress appears limited. Reflect briefly on whether a substantively different strategy might be more effective."
	case cr != nil && cr.ThinkingIntensity() <= 2:
		// Cross-check static complexity assessment against actual thinking behavior.
		// Only emit "straightforward" hint if ThinkingMonitor confirms the model
		// is actually over-thinking (score >= 0.4). If score is low, the model
		// is already being concise and the hint would add noise.
		if tm != nil && tm.computeScore() < 0.4 {
			return ""
		}
		return "This is a straightforward task. Keep your thinking concise and move directly to action."
	default:
		return ""
	}
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
This is a separate meta-evaluation request. Prior conversation is from another agent, do not continue it. TOOL CALLS ARE FORBIDDEN.
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

// mapToStruct converts a map to a struct via JSON round-trip.
func mapToStruct(m map[string]any, dest any) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dest)
}
