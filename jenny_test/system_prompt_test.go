package e2e_test

import (
	"strings"
	"testing"

	"github.com/ipy/jenny/jenny_test/harness"
)

func runPrintSystemPrompt(t *testing.T) harness.RunResult {
	t.Helper()
	// No ANTHROPIC_BASE_URL or ANTHROPIC_AUTH_TOKEN — the flag must work
	// without any network credentials.
	return harness.RunJenny(t, nil, "--print-system-prompt")
}

// TestPrintSystemPromptFlag verifies AC1 and AC2.
func TestPrintSystemPromptFlag(t *testing.T) {
	res := runPrintSystemPrompt(t)
	if res.ExitCode != 0 {
		t.Fatalf("exit %d; stderr=%q", res.ExitCode, res.Stderr)
	}
	text := strings.Join(res.Lines, "\n")
	if len(text) == 0 {
		t.Fatal("stdout is empty")
	}
}

// TestSystemPromptToolList verifies AC3 and AC4.
func TestSystemPromptToolList(t *testing.T) {
	res := runPrintSystemPrompt(t)
	text := strings.Join(res.Lines, "\n")
	for _, want := range []string{"Available tools:", "Bash", "Read"} {
		if !strings.Contains(text, want) {
			t.Errorf("system prompt does not contain %q", want)
		}
	}
}

// TestSystemPromptCwd verifies AC5.
func TestSystemPromptCwd(t *testing.T) {
	res := runPrintSystemPrompt(t)
	text := strings.Join(res.Lines, "\n")
	if !strings.Contains(text, "Cwd:") {
		t.Error("system prompt does not contain 'Cwd:'")
	}
}

// TestSystemPromptSubstantial verifies AC6.
func TestSystemPromptSubstantial(t *testing.T) {
	res := runPrintSystemPrompt(t)
	text := strings.Join(res.Lines, "\n")
	if len(text) < 500 {
		t.Errorf("system prompt length %d < 500", len(text))
	}
}
