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
	qaMaxRetries      = 2
	qaTurnThreshold   = 20
	qaVerifyTimeout   = 60 * time.Second
	qaVerifyMaxTokens = 4096
	qaVerifyMaxTurns  = 5
)

// qaVerdict holds the parsed evaluation result from a QA verification agent.
type qaVerdict struct {
	Verdict     string   `json:"verdict"`
	Reasoning   string   `json:"reasoning"`
	Gaps        []string `json:"gaps"`
	Suggestions []string `json:"suggestions"`
}

// shouldTriggerQA returns true when the QA gate should intercept end_turn.
// Skipped for: disabled flag, resumed sessions, fork children, short sessions (<20 turns),
// L3-terminated sessions, and retries exhausted.
func (e *QueryEngine) shouldTriggerQA() bool {
	if e.streamCfg.DisableQAGate {
		return false
	}
	if !e.streamCfg.Enabled {
		return false
	}
	if e.streamCfg.IsForkChild {
		return false
	}
	if e.streamCfg.IsResume {
		return false
	}
	if e.qaRetryCount >= qaMaxRetries {
		return false
	}
	// Skip QA for short sessions — the overhead is not worthwhile
	if e.turnCount < qaTurnThreshold {
		return false
	}
	// Skip QA if progress monitor reached L3 (session is being terminated)
	if e.progressMonitor != nil && e.progressMonitor.Level() >= ProgressLevelL3 {
		return false
	}
	return true
}

// runQAGate spawns a verification agent and returns the verdict.
// It runs synchronously within the SubmitMessage call. The verification agent
// receives the user's original request directly (no accept_criteria extraction)
// and is instructed to perform only critical targeted checks.
func (e *QueryEngine) runQAGate(ctx context.Context, messages []api.Message, sessionID string) (*qaVerdict, error) {
	userRequest := extractUserRequest(messages)
	if userRequest == "" {
		log.Debug("QA gate skipped: no user request found")
		return &qaVerdict{Verdict: "PASS", Reasoning: "No user request found"}, nil
	}

	fileSummary := buildFileSummary(messages)
	prompt := buildQAVerificationPrompt(userRequest, e.cwd, fileSummary)

	qaTools := filterQATools(e.tools)

	// Child config: disable all meta-evaluation mechanisms to avoid recursive overhead
	childCfg := &StreamConfig{
		Enabled:                false,
		IsForkChild:            true,
		MaxTurns:               qaVerifyMaxTurns,
		DisableComplexityEval:  true,
		DisableProgressMonitor: true,
		DisableQAGate:          true,
	}

	qaCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), qaVerifyTimeout)
	defer cancel()

	model := resolveSubagentModel("", e.streamCfg.SubagentModel, e.client)

	_, output, _, err := RunStream(qaCtx, prompt, qaTools, e.cwd, childCfg, model, WithClient(e.client))
	if err != nil {
		log.Warn("QA verification agent failed", "error", err)
		return &qaVerdict{Verdict: "FAIL", Reasoning: fmt.Sprintf("Verification failed: %v", err)}, nil
	}

	verdict, err := parseQAVerdict(output)
	if err != nil {
		log.Warn("QA verdict parse failed", "error", err, "output", output)
		return &qaVerdict{Verdict: "FAIL", Reasoning: fmt.Sprintf("Could not parse verdict: %v", err)}, nil
	}
	return verdict, nil
}

// extractUserRequest finds the first non-virtual user message in the conversation,
// which is the user's original task request.
func extractUserRequest(messages []api.Message) string {
	for _, msg := range messages {
		if msg.Role == api.RoleUser && !msg.IsVirtual {
			return msg.Content
		}
	}
	return ""
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
// It provides the user's original request directly and instructs the agent to
// perform only critical targeted checks with minimal turns.
func buildQAVerificationPrompt(userRequest, cwd, fileSummary string) string {
	var sb strings.Builder
	sb.WriteString("You are a QA verification agent. Verify that the deliverables satisfy the user's request.\n\n")

	sb.WriteString("## User's Original Request\n")
	sb.WriteString(userRequest)
	sb.WriteString("\n")

	fmt.Fprintf(&sb, "\n## Working Directory\n%s\n", cwd)

	if fileSummary != "" {
		fmt.Fprintf(&sb, "\n## Modifications Summary\n%s\n", fileSummary)
	}

	sb.WriteString("\n## Instructions\n")
	sb.WriteString("Complete verification in as few turns as possible. Be efficient:\n")
	sb.WriteString("- Run critical tests or check commands ONLY — do not read entire files.\n")
	sb.WriteString("- Use Bash for targeted checks (file existence, command exit codes, grep for key outputs).\n")
	sb.WriteString("- Use Glob to confirm expected files exist. Use Grep for specific content checks.\n")
	sb.WriteString("- NEVER modify files. Read-only verification only.\n")
	sb.WriteString("- After verification, respond with ONLY a JSON object:\n")
	sb.WriteString(`{"verdict": "PASS" or "FAIL", "reasoning": "brief explanation", "gaps": ["unmet requirement 1", ...], "suggestions": ["fix 1", ...]}`)
	sb.WriteString("\n")

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
