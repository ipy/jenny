package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ipy/jenny/internal/api"
	"github.com/ipy/jenny/internal/log"
	"github.com/ipy/jenny/internal/tool"
)

const (
	qaMaxRetries          = 2
	qaComplexityThreshold = 3.0
	qaTurnThreshold       = 10
	qaVerifyTimeout       = 60 * time.Second
	qaVerifyMaxTokens     = 4096
	qaVerifyMaxTurns      = 5
)

// qaVerdict holds the parsed evaluation result from a QA verification agent.
type qaVerdict struct {
	Verdict     string   `json:"verdict"`
	Reasoning   string   `json:"reasoning"`
	Gaps        []string `json:"gaps"`
	Suggestions []string `json:"suggestions"`
}

// shouldTriggerQA returns true when the QA gate should intercept end_turn.
func (e *QueryEngine) shouldTriggerQA() bool {
	if !e.streamCfg.Enabled {
		return false
	}
	if e.streamCfg.IsForkChild {
		return false
	}
	if e.qaRetryCount >= qaMaxRetries {
		return false
	}

	// Check complexity signals
	cr := e.maxTokensCtrl.GetComplexityResult()
	if cr != nil && cr.OverallComplexity() >= qaComplexityThreshold {
		return true
	}
	if e.turnCount >= qaTurnThreshold {
		return true
	}
	if e.qaCompactSeen {
		return true
	}
	return false
}

// runQAGate spawns a verification agent and returns the verdict.
// It runs synchronously within the SubmitMessage call.
func (e *QueryEngine) runQAGate(ctx context.Context, messages []api.Message, sessionID string) (*qaVerdict, error) {
	ac, constraints := e.extractAcceptCriteria(messages)
	if len(ac) == 0 {
		log.Debug("QA gate skipped: no acceptance criteria extracted")
		return &qaVerdict{Verdict: "PASS", Reasoning: "No acceptance criteria to evaluate"}, nil
	}

	fileSummary := buildFileSummary(messages)
	prompt := buildQAVerificationPrompt(ac, constraints, e.cwd, fileSummary)

	// Filter tools to read-only subset
	qaTools := filterQATools(e.tools)

	// Build child StreamConfig
	childCfg := &StreamConfig{
		Enabled:     false,
		IsForkChild: true,
		MaxTurns:    qaVerifyMaxTurns,
	}

	qaCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), qaVerifyTimeout)
	defer cancel()

	model := resolveSubagentModel("", e.streamCfg.SubagentModel, e.client)

	_, output, _, err := RunStream(qaCtx, prompt, qaTools, e.cwd, childCfg, model, WithClient(e.client))
	if err != nil {
		// On timeout or error, default to PASS to avoid blocking delivery
		log.Warn("QA verification agent failed", "error", err)
		return &qaVerdict{Verdict: "PASS", Reasoning: fmt.Sprintf("Verification failed: %v", err)}, nil
	}

	verdict, err := parseQAVerdict(output)
	if err != nil {
		log.Warn("QA verdict parse failed", "error", err, "output", output)
		return &qaVerdict{Verdict: "PASS", Reasoning: fmt.Sprintf("Could not parse verdict: %v", err)}, nil
	}
	return verdict, nil
}

// extractAcceptCriteria retrieves acceptance criteria from the complexity result.
// If the async complexity eval hasn't fired yet, runs it synchronously using the
// same complexityEvalPrompt — one query yields both complexity dimensions and AC,
// feeding maxTokensCtrl as a side-effect. No separate AC-only query is ever sent.
func (e *QueryEngine) extractAcceptCriteria(messages []api.Message) (acceptCriteria []string, constraints []string) {
	cr := e.maxTokensCtrl.GetComplexityResult()
	if cr != nil && len(cr.AcceptCriteria) > 0 {
		return cr.AcceptCriteria, cr.Constraints
	}

	// Complexity eval never fired — run it synchronously with the same prompt
	// so one API call produces both complexity dimensions and acceptance criteria.
	if e.sideQueryCtx == nil || len(messages) == 0 {
		return nil, nil
	}

	sq := &SideQuery{
		Name:      "complexity_eval",
		Messages:  messages,
		Prompt:    complexityEvalPrompt,
		MaxTokens: forkMaxTokens,
		Timeout:   30 * time.Second,
	}

	ctx := context.Background()
	result, err := sq.Execute(ctx, e.sideQueryCtx)
	if err != nil {
		log.Debug("Synchronous complexity eval for QA gate failed", "error", err)
		return nil, nil
	}

	text := extractJSON(result.Text)
	var parsed ComplexityResult
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		log.Debug("Synchronous complexity eval parse failed", "error", err)
		return nil, nil
	}
	parsed.ReasoningDepth = clampDim(parsed.ReasoningDepth)
	parsed.CodeVolume = clampDim(parsed.CodeVolume)
	parsed.DomainSpecialization = clampDim(parsed.DomainSpecialization)
	parsed.IntegrationBreadth = clampDim(parsed.IntegrationBreadth)

	// Store result so maxTokensCtrl also benefits from the late eval
	e.maxTokensCtrl.SetComplexityResult(&parsed)
	log.Info("Synchronous complexity eval for QA gate",
		"overall", parsed.OverallComplexity(),
		"accept_criteria", len(parsed.AcceptCriteria),
	)

	return parsed.AcceptCriteria, parsed.Constraints
}

// parseQAVerdict extracts a structured verdict from the verification agent's output.
func parseQAVerdict(output string) (*qaVerdict, error) {
	text := extractJSON(output)
	var v qaVerdict
	if err := json.Unmarshal([]byte(text), &v); err != nil {
		return nil, fmt.Errorf("invalid verdict JSON: %w", err)
	}
	v.Verdict = strings.ToUpper(strings.TrimSpace(v.Verdict))
	if v.Verdict != "PASS" && v.Verdict != "FAIL" {
		return nil, fmt.Errorf("invalid verdict value: %q (must be PASS or FAIL)", v.Verdict)
	}
	return &v, nil
}

// buildQAFeedbackMessage creates a virtual user message containing QA failure feedback.
func buildQAFeedbackMessage(v *qaVerdict, retryCount, maxRetries int) api.Message {
	var sb strings.Builder
	sb.WriteString("<system-reminder>\n")
	fmt.Fprintf(&sb, "QA verification FAILED (attempt %d/%d).\n\n", retryCount, maxRetries)

	if len(v.Gaps) > 0 {
		sb.WriteString("Unmet acceptance criteria:\n")
		for _, gap := range v.Gaps {
			fmt.Fprintf(&sb, "- %s\n", gap)
		}
		sb.WriteString("\n")
	}

	if v.Reasoning != "" {
		fmt.Fprintf(&sb, "Reasoning: %s\n\n", v.Reasoning)
	}

	if len(v.Suggestions) > 0 {
		sb.WriteString("Suggested fixes:\n")
		for _, s := range v.Suggestions {
			fmt.Fprintf(&sb, "- %s\n", s)
		}
		sb.WriteString("\n")
	}

	sb.WriteString("Address the gaps above and complete the task. Do NOT explain what you will do — just do it.\n")
	sb.WriteString("</system-reminder>")

	return api.Message{
		Role:      api.RoleUser,
		Content:   sb.String(),
		IsVirtual: true,
	}
}

// buildQAVerificationPrompt assembles the prompt for the QA verification agent.
func buildQAVerificationPrompt(acceptCriteria, constraints []string, cwd, fileSummary string) string {
	var sb strings.Builder
	sb.WriteString("You are a QA verification agent. Evaluate whether the deliverables satisfy the acceptance criteria.\n\n")

	sb.WriteString("## Acceptance Criteria\n")
	for i, ac := range acceptCriteria {
		fmt.Fprintf(&sb, "%d. %s\n", i+1, ac)
	}

	if len(constraints) > 0 {
		sb.WriteString("\n## Constraints\n")
		for i, c := range constraints {
			fmt.Fprintf(&sb, "%d. %s\n", i+1, c)
		}
	}

	fmt.Fprintf(&sb, "\n## Working Directory\n%s\n", cwd)

	if fileSummary != "" {
		fmt.Fprintf(&sb, "\n## Modifications Summary\n%s\n", fileSummary)
	}

	sb.WriteString("\n## Instructions\n")
	sb.WriteString("1. Use Read, Glob, and Grep to inspect the filesystem and verify each criterion.\n")
	sb.WriteString("2. Use Bash ONLY for read-only commands (listing, checking, running tests). NEVER modify files.\n")
	sb.WriteString("3. Check each acceptance criterion independently.\n")
	sb.WriteString("4. After verification, respond with ONLY a JSON object:\n")
	sb.WriteString(`{"verdict": "PASS" or "FAIL", "reasoning": "brief explanation", "gaps": ["unmet criterion 1", ...], "suggestions": ["fix 1", ...]}`)
	sb.WriteString("\n\nRespond with the JSON verdict and nothing else after it.\n")

	return sb.String()
}

// buildFileSummary extracts a summary of file modifications and commands from messages.
func buildFileSummary(messages []api.Message) string {
	var written, edited, commands []string
	seen := make(map[string]bool)

	for _, msg := range messages {
		if msg.Role != api.RoleAssistant {
			continue
		}
		for _, tu := range msg.ToolUse {
			switch tu.Name {
			case "Write", "write":
				if path := extractPathFromInput(tu.Input); path != "" && !seen["w:"+path] {
					written = append(written, path)
					seen["w:"+path] = true
				}
			case "Edit", "edit", "NotebookEdit", "notebook_edit":
				if path := extractPathFromInput(tu.Input); path != "" && !seen["e:"+path] {
					edited = append(edited, path)
					seen["e:"+path] = true
				}
			case "Bash", "bash":
				if cmd := extractCommandFromInput(tu.Input); cmd != "" {
					if len(cmd) > 120 {
						cmd = cmd[:120] + "..."
					}
					commands = append(commands, cmd)
				}
			}
		}
	}

	if len(written) == 0 && len(edited) == 0 && len(commands) == 0 {
		return ""
	}

	var sb strings.Builder
	if len(written) > 0 {
		sb.WriteString("Files written:\n")
		for _, f := range written {
			fmt.Fprintf(&sb, "  - %s\n", f)
		}
	}
	if len(edited) > 0 {
		sb.WriteString("Files edited:\n")
		for _, f := range edited {
			fmt.Fprintf(&sb, "  - %s\n", f)
		}
	}
	if len(commands) > 0 {
		sb.WriteString("Commands executed:\n")
		for _, c := range commands {
			fmt.Fprintf(&sb, "  - %s\n", c)
		}
	}
	return sb.String()
}

func extractPathFromInput(input map[string]any) string {
	for _, key := range []string{"file_path", "path", "file", "target_file"} {
		if v, ok := input[key].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

func extractCommandFromInput(input map[string]any) string {
	if v, ok := input["command"].(string); ok {
		return v
	}
	return ""
}

// filterQATools returns tools restricted to read-only operations.
func filterQATools(allTools []tool.Tool) []tool.Tool {
	allowed := map[string]bool{
		"Read": true, "Glob": true, "Grep": true, "Bash": true,
	}
	var result []tool.Tool
	for _, t := range allTools {
		if allowed[t.Name()] {
			result = append(result, t)
		}
	}
	return result
}

// qaEvalResultJSON builds the JSON string for a system/qa_eval stream event.
func qaEvalResultJSON(v *qaVerdict, retryCount, maxRetries int) string {
	payload := map[string]any{
		"verdict":     v.Verdict,
		"reasoning":   v.Reasoning,
		"gaps":        v.Gaps,
		"suggestions": v.Suggestions,
		"retry_count": retryCount,
		"max_retries": maxRetries,
	}
	data, _ := json.Marshal(payload)
	return string(data)
}

