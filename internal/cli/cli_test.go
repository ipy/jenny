package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestParseNoArgs(t *testing.T) {
	// Save original args
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"jenny"}

	flags, err := Parse()
	if err == nil {
		t.Error("expected error for no prompt")
	}
	if flags != nil {
		t.Error("expected nil flags on error")
	}
}

func TestParsePositionalArg(t *testing.T) {
	// Save original args
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"jenny", "hello world"}

	flags, err := Parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if flags.Prompt != "hello world" {
		t.Errorf("expected prompt 'hello world', got %q", flags.Prompt)
	}
}

func TestParsePFlag(t *testing.T) {
	// Save original args
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"jenny", "-p", "hello from -p"}

	flags, err := Parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if flags.Prompt != "hello from -p" {
		t.Errorf("expected prompt 'hello from -p', got %q", flags.Prompt)
	}
}

// AC8: -p may be specified multiple times; values are joined with newlines.
func TestParseMultiplePFlags(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"jenny", "-p", "first", "-p", "second", "-p", "third"}

	flags, err := Parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if flags.Prompt != "first\nsecond\nthird" {
		t.Errorf("expected 'first\\nsecond\\nthird', got %q", flags.Prompt)
	}
}

func TestParseModelFlag(t *testing.T) {
	// Save original args
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"jenny", "--model", "deepseek-v4-flash", "-p", "hello"}

	flags, err := Parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if flags.Model != "deepseek-v4-flash" {
		t.Errorf("expected model 'deepseek-v4-flash', got %q", flags.Model)
	}
}

func TestParseOutputFormatFlag(t *testing.T) {
	// Save original args
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"jenny", "--output-format", "stream-json", "-p", "hello"}

	flags, err := Parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if flags.OutputFormat != "stream-json" {
		t.Errorf("expected output-format 'stream-json', got %q", flags.OutputFormat)
	}
}

func TestParseMaxIterationsFlag(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"jenny", "--max-iterations", "50", "-p", "hello"}

	flags, err := Parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if flags.MaxIterations != 50 {
		t.Errorf("expected MaxIterations=50, got %d", flags.MaxIterations)
	}
}

func TestParseMaxIterationsDefault(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"jenny", "-p", "hello"}

	flags, err := Parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if flags.MaxIterations != 0 {
		t.Errorf("expected MaxIterations=0 (unlimited), got %d", flags.MaxIterations)
	}
}

func TestParseVerboseFlag(t *testing.T) {
	// Save original args
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"jenny", "--verbose", "-p", "hello"}

	flags, err := Parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !flags.Verbose {
		t.Error("expected verbose to be true")
	}
}

func TestParseIncludePartialMessagesFlag(t *testing.T) {
	// Save original args
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"jenny", "--include-partial-messages", "-p", "hello"}

	flags, err := Parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !flags.IncludePartialMessages {
		t.Error("expected include-partial-messages to be true")
	}
}

func TestParseSkipPermissionsFlag(t *testing.T) {
	// Save original args
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"jenny", "--dangerously-skip-permissions", "-p", "hello"}

	flags, err := Parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !flags.SkipPermissions {
		t.Error("expected skip-permissions to be true")
	}
}

func TestParseSessionResumeFlag(t *testing.T) {
	// Save original args
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"jenny", "-r", "sess_12345", "-p", "hello"}

	flags, err := Parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if flags.SessionResume != "sess_12345" {
		t.Errorf("expected session-resume 'sess_12345', got %q", flags.SessionResume)
	}
}

func TestParseMultipleFlags(t *testing.T) {
	// Save original args
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"jenny", "--model", "gpt-4", "--output-format", "stream-json", "--verbose", "-p", "hello"}

	flags, err := Parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if flags.Model != "gpt-4" {
		t.Errorf("expected model 'gpt-4', got %q", flags.Model)
	}
	if flags.OutputFormat != "stream-json" {
		t.Errorf("expected output-format 'stream-json', got %q", flags.OutputFormat)
	}
	if !flags.Verbose {
		t.Error("expected verbose to be true")
	}
	if flags.Prompt != "hello" {
		t.Errorf("expected prompt 'hello', got %q", flags.Prompt)
	}
}

func TestParsePositionalWithPFlag(t *testing.T) {
	// When both -p and positional arg are provided, -p takes precedence
	// Save original args
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"jenny", "-p", "from -p", "from positional"}

	flags, err := Parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if flags.Prompt != "from -p" {
		t.Errorf("expected prompt 'from -p' (p flag takes precedence), got %q", flags.Prompt)
	}
}

func TestParseDoubleDash(t *testing.T) {
	// Save original args
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"jenny", "--", "hello world"}

	flags, err := Parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if flags.Prompt != "hello world" {
		t.Errorf("expected prompt 'hello world', got %q", flags.Prompt)
	}
}

func TestParseContinueFlag(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"jenny", "--continue", "-p", "hello"}

	flags, err := Parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !flags.Continue {
		t.Error("expected continue to be true")
	}
}

func TestParseContinueMutuallyExclusiveWithResume(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"jenny", "--continue", "-r", "sess_12345", "-p", "hello"}

	_, err := Parse()
	if err == nil {
		t.Error("expected error for --continue with -r")
	}
}

func TestParseContinueMutuallyExclusiveWithNoSessionPersistence(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"jenny", "--continue", "--no-session-persistence", "-p", "hello"}

	_, err := Parse()
	if err == nil {
		t.Error("expected error for --continue with --no-session-persistence")
	}
}

func TestParseForkSessionRequiresResume(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"jenny", "--fork-session", "-p", "hello"}

	_, err := Parse()
	if err == nil {
		t.Error("expected error for --fork-session without -r")
	}
}

func TestParseForkSessionWithResume(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"jenny", "--fork-session", "-r", "sess_12345", "-p", "hello"}

	flags, err := Parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !flags.ForkSession {
		t.Error("expected fork-session to be true")
	}
	if flags.SessionResume != "sess_12345" {
		t.Errorf("expected session-resume 'sess_12345', got %q", flags.SessionResume)
	}
}

func TestParseMCPConfigSingleFlag(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"jenny", "--mcp-config", "/path/to/config.json", "-p", "hello"}

	flags, err := Parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(flags.MCPConfig) != 1 {
		t.Errorf("expected 1 MCPConfig path, got %d", len(flags.MCPConfig))
	}
	if flags.MCPConfig[0] != "/path/to/config.json" {
		t.Errorf("expected MCPConfig '/path/to/config.json', got %q", flags.MCPConfig[0])
	}
}

func TestParseMCPConfigMultipleFlags(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"jenny", "--mcp-config", "/path/a.json", "--mcp-config", "/path/b.json", "-p", "hello"}

	flags, err := Parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(flags.MCPConfig) != 2 {
		t.Errorf("expected 2 MCPConfig paths, got %d", len(flags.MCPConfig))
	}
	if flags.MCPConfig[0] != "/path/a.json" {
		t.Errorf("expected MCPConfig[0] '/path/a.json', got %q", flags.MCPConfig[0])
	}
	if flags.MCPConfig[1] != "/path/b.json" {
		t.Errorf("expected MCPConfig[1] '/path/b.json', got %q", flags.MCPConfig[1])
	}
}

func TestParseMCPConfigNoFlag(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"jenny", "-p", "hello"}

	flags, err := Parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(flags.MCPConfig) > 0 {
		t.Errorf("expected nil or empty MCPConfig, got %v", flags.MCPConfig)
	}
}

func TestParseFeatureFlags(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"jenny", "-ff", "redact=disabled", "--feature-flags", "other=true", "-p", "hello"}

	flags, err := Parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(flags.FeatureFlags) != 2 {
		t.Errorf("expected 2 feature flags, got %d", len(flags.FeatureFlags))
	}
	if flags.FeatureFlags["redact"] != "disabled" {
		t.Errorf("expected redact=disabled, got %q", flags.FeatureFlags["redact"])
	}
	if flags.FeatureFlags["other"] != "true" {
		t.Errorf("expected other=true, got %q", flags.FeatureFlags["other"])
	}
}

func TestParseFeatureFlagsInvalid(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"jenny", "-ff", "invalid", "-p", "hello"}

	_, err := Parse()
	if err == nil {
		t.Error("expected error for invalid feature flag format")
	}
}

func TestStreamMessageToolInputUsesInputKey(t *testing.T) {
	msg := StreamMessage{
		Type:     "tool_use",
		ToolName: "Read",
		ToolInput: map[string]any{
			"file_path": "foo.go",
		},
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if _, ok := parsed["input"]; !ok {
		t.Errorf("expected 'input' key in JSON output, got: %s", string(data))
	}
	if _, ok := parsed["parameters"]; ok {
		t.Errorf("unexpected 'parameters' key found in JSON output: %s", string(data))
	}

	if !strings.Contains(string(data), `"input"`) {
		t.Errorf("JSON output does not contain 'input' key: %s", string(data))
	}
}

func TestStreamMessageHasKindField(t *testing.T) {
	tests := []struct {
		eventType string
		wantKind  string
	}{
		{"assistant", "message"},
		{"user", "message"},
		{"result", "message"},
		{"system", "message"},
		{"tool_call", "tool_call"},
		{"other", "other"},
	}

	for _, tt := range tests {
		t.Run(tt.eventType, func(t *testing.T) {
			msg := StreamMessage{
				Type: tt.eventType,
			}

			data, err := json.Marshal(msg)
			if err != nil {
				t.Fatalf("json.Marshal failed: %v", err)
			}

			var parsed map[string]any
			if err := json.Unmarshal(data, &parsed); err != nil {
				t.Fatalf("json.Unmarshal failed: %v", err)
			}

			kind, ok := parsed["kind"].(string)
			if !ok {
				t.Errorf("expected 'kind' key in JSON output, got: %s", string(data))
			}
			if kind != tt.wantKind {
				t.Errorf("expected kind %q, got %q", tt.wantKind, kind)
			}
		})
	}
}

// AC6: --help must print the usage block exactly once.
// Go's flag package invokes flags.Usage() itself when -h/--help is seen and
// the flag is undefined; our cli.Parse used to call it again on flag.ErrHelp,
// doubling the output. This subprocess test runs the test binary with -h
// (matching the actual CLI surface) and counts the number of times the
// "Usage:" line appears in stderr.
func TestHelpPrintedOnce(t *testing.T) {
	if os.Getenv("JENNY_HELP_CHILD") == "1" {
		// Child: run cli.Parse with -h; it will os.Exit(0) after printing usage.
		os.Args = []string{"jenny", "-h"}
		_, _ = Parse()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestHelpPrintedOnce")
	cmd.Env = append(os.Environ(), "JENNY_HELP_CHILD=1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		// exit 0 is the success case; the child exits via os.Exit(0).
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 0 {
			// ok
		} else {
			t.Fatalf("child process failed: %v\nstderr: %s", err, stderr.String())
		}
	}

	count := strings.Count(stderr.String(), "Usage:")
	if count != 1 {
		t.Errorf("expected 'Usage:' line to appear exactly once in --help output, got %d.\nstderr:\n%s", count, stderr.String())
	}
}
