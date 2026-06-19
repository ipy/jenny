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

	flags, _, err := Parse()
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

	flags, _, err := Parse()
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

	os.Args = []string{"jenny", "--print", "hello from -p"}

	flags, _, err := Parse()
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

	os.Args = []string{"jenny", "--print", "first", "--print", "second", "--print", "third"}

	flags, _, err := Parse()
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

	os.Args = []string{"jenny", "--model", "deepseek-v4-flash", "--print", "hello"}

	flags, _, err := Parse()
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

	os.Args = []string{"jenny", "--output-format", "stream-json", "--print", "hello"}

	flags, _, err := Parse()
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

	os.Args = []string{"jenny", "--max-iterations", "50", "--print", "hello"}

	flags, _, err := Parse()
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

	os.Args = []string{"jenny", "--print", "hello"}

	flags, _, err := Parse()
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

	os.Args = []string{"jenny", "--verbose", "--print", "hello"}

	flags, _, err := Parse()
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

	os.Args = []string{"jenny", "--include-partial-messages", "--output-format", "stream-json", "--print", "hello"}

	flags, _, err := Parse()
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

	os.Args = []string{"jenny", "--dangerously-skip-permissions", "--print", "hello"}

	flags, _, err := Parse()
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

	os.Args = []string{"jenny", "-r", "sess_12345", "--print", "hello"}

	flags, _, err := Parse()
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

	os.Args = []string{"jenny", "--model", "gpt-4", "--output-format", "stream-json", "--verbose", "--print", "hello"}

	flags, _, err := Parse()
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

	os.Args = []string{"jenny", "--print", "from -p", "from positional"}

	flags, _, err := Parse()
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

	flags, _, err := Parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if flags.Prompt != "hello world" {
		t.Errorf("expected prompt 'hello world', got %q", flags.Prompt)
	}
}

// --prompt-file tests

func TestParsePromptFile(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	tmpFile, err := os.CreateTemp("", "prompt-*.txt")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.WriteString("hello from file")
	tmpFile.Close()

	os.Args = []string{"jenny", "--prompt-file", tmpFile.Name()}

	flags, _, err := Parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if flags.Prompt != "hello from file" {
		t.Errorf("expected prompt 'hello from file', got %q", flags.Prompt)
	}
}

func TestParsePromptFileMultiple(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	file1, err := os.CreateTemp("", "prompt1-*.txt")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer os.Remove(file1.Name())
	file1.WriteString("first part")
	file1.Close()

	file2, err := os.CreateTemp("", "prompt2-*.txt")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer os.Remove(file2.Name())
	file2.WriteString("second part")
	file2.Close()

	os.Args = []string{"jenny", "--prompt-file", file1.Name(), "--prompt-file", file2.Name()}

	flags, _, err := Parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if flags.Prompt != "first part\nsecond part" {
		t.Errorf("expected 'first part\\nsecond part', got %q", flags.Prompt)
	}
}

func TestParsePromptFileNotFound(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"jenny", "--prompt-file", "/nonexistent/path/prompt.txt"}

	_, _, err := Parse()
	if err == nil {
		t.Error("expected error for nonexistent prompt file, got nil")
	}
	if !strings.Contains(err.Error(), "prompt file not found") {
		t.Errorf("expected 'prompt file not found' error, got %v", err)
	}
}

func TestParsePromptFileIgnoredWhenPFlagSet(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	tmpFile, err := os.CreateTemp("", "prompt-*.txt")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.WriteString("from file")
	tmpFile.Close()

	os.Args = []string{"jenny", "--print", "from p flag", "--prompt-file", tmpFile.Name()}

	flags, _, err := Parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if flags.Prompt != "from p flag" {
		t.Errorf("expected prompt 'from p flag' (p flag takes precedence), got %q", flags.Prompt)
	}
}

func TestParsePromptFileOverridesPositional(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	tmpFile, err := os.CreateTemp("", "prompt-*.txt")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.WriteString("from file")
	tmpFile.Close()

	os.Args = []string{"jenny", "--prompt-file", tmpFile.Name(), "from positional"}

	flags, _, err := Parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if flags.Prompt != "from file" {
		t.Errorf("expected prompt 'from file' (prompt-file overrides positional), got %q", flags.Prompt)
	}
}

func TestParseContinueFlag(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"jenny", "--continue", "--print", "hello"}

	flags, _, err := Parse()
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

	os.Args = []string{"jenny", "--continue", "-r", "sess_12345", "--print", "hello"}

	_, _, err := Parse()
	if err == nil {
		t.Error("expected error for --continue with -r")
	}
}

func TestParseContinueMutuallyExclusiveWithNoSessionPersistence(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"jenny", "--continue", "--no-session-persistence", "--print", "hello"}

	_, _, err := Parse()
	if err == nil {
		t.Error("expected error for --continue with --no-session-persistence")
	}
}

func TestParseForkSessionRequiresResume(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"jenny", "--fork-session", "--print", "hello"}

	_, _, err := Parse()
	if err == nil {
		t.Error("expected error for --fork-session without -r")
	}
}

func TestParseForkSessionWithResume(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"jenny", "--fork-session", "-r", "sess_12345", "--print", "hello"}

	flags, _, err := Parse()
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

	os.Args = []string{"jenny", "--mcp-config", "/path/to/config.json", "--print", "hello"}

	flags, _, err := Parse()
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

	os.Args = []string{"jenny", "--mcp-config", "/path/a.json", "--mcp-config", "/path/b.json", "--print", "hello"}

	flags, _, err := Parse()
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

	os.Args = []string{"jenny", "--print", "hello"}

	flags, _, err := Parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(flags.MCPConfig) > 0 {
		t.Errorf("expected nil or empty MCPConfig, got %v", flags.MCPConfig)
	}
}

func TestParseFeatureFlagsRemoved(t *testing.T) {
	// AC: --ff / --feature-flags no longer exist. Verify they are rejected.
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"jenny", "--ff", "redact=disabled", "--print", "hello"}

	_, _, err := Parse()
	if err == nil {
		t.Error("expected error for unknown --ff flag (feature flags were removed)")
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

// TestStreamMessageHasNoKindField verifies AC1: stream-json events do NOT include
// the 'kind' field. Per spec: 'kind' is a jenny extension not present in Claude
// Code's SDK format, so it should be absent from all event types.
func TestStreamMessageHasNoKindField(t *testing.T) {
	tests := []struct {
		eventType string
	}{
		{"assistant"},
		{"user"},
		{"result"},
		{"system"},
		{"tool_call"},
		{"other"},
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

			// AC1: 'kind' field should NOT be present
			if _, hasKind := parsed["kind"]; hasKind {
				t.Errorf("AC1 FAIL: 'kind' key found in JSON output for event type %q, got: %s", tt.eventType, string(data))
			} else {
				t.Logf("AC1 PASS: event type %q does not have 'kind' field", tt.eventType)
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
		_, _, _ = Parse()
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

func TestParsePermissionLevelFlag(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"jenny", "--permission-level", "read", "--print", "hello"}

	flags, _, err := Parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if flags.PermissionLevel != "read" {
		t.Errorf("expected permission-level 'read', got %q", flags.PermissionLevel)
	}
}

func TestParsePermissionLevelFlagAllValues(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	for _, level := range []string{"read", "analyze", "edit", "execute", "unrestricted"} {
		os.Args = []string{"jenny", "--permission-level", level, "--print", "hello"}

		flags, _, err := Parse()
		if err != nil {
			t.Errorf("unexpected error for level %q: %v", level, err)
		}
		if flags.PermissionLevel != level {
			t.Errorf("expected permission-level %q, got %q", level, flags.PermissionLevel)
		}
	}
}

func TestParsePermissionLevelInvalid(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"jenny", "--permission-level", "invalid", "--print", "hello"}

	_, _, err := Parse()
	if err == nil {
		t.Error("expected error for invalid permission level, got nil")
	}
	if !strings.Contains(err.Error(), "invalid --permission-level") {
		t.Errorf("expected invalid permission level error, got %v", err)
	}
}

func TestParsePermissionLevelEmpty(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	// No --permission-level flag: should be empty string (default)
	os.Args = []string{"jenny", "--print", "hello"}

	flags, _, err := Parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if flags.PermissionLevel != "" {
		t.Errorf("expected empty permission-level, got %q", flags.PermissionLevel)
	}
}

func TestParsePermissionLevelWithSkipPermissions(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	// Both flags set: Parse should succeed; resolution happens in main.go
	os.Args = []string{"jenny", "--dangerously-skip-permissions", "--permission-level", "read", "--print", "hello"}

	flags, _, err := Parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !flags.SkipPermissions {
		t.Error("expected skip-permissions to be true")
	}
	if flags.PermissionLevel != "read" {
		t.Errorf("expected permission-level 'read', got %q", flags.PermissionLevel)
	}
}

func TestParsePermissionLevelEnvVar(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Setenv("JENNY_PERMISSION_LEVEL", "analyze")
	defer os.Unsetenv("JENNY_PERMISSION_LEVEL")

	os.Args = []string{"jenny", "--print", "hello"}

	flags, _, err := Parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if flags.PermissionLevel != "analyze" {
		t.Errorf("expected permission-level 'analyze' from env, got %q", flags.PermissionLevel)
	}
}

// TestParseRedactModeDefault verifies that with no env var and no flag,
// RedactMode is empty (the default is applied later by redact.ParseRedactMode).
func TestParseRedactModeDefault(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Unsetenv("JENNY_REDACT")
	os.Args = []string{"jenny", "--print", "hello"}

	flags, _, err := Parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if flags.RedactMode != "" {
		t.Errorf("expected empty RedactMode when no env/flag set, got %q", flags.RedactMode)
	}
}

// TestParseRedactModeEnvVar verifies that JENNY_REDACT is read through koanf.
func TestParseRedactModeEnvVar(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	t.Setenv("JENNY_REDACT", "recover")

	os.Args = []string{"jenny", "--print", "hello"}

	flags, _, err := Parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if flags.RedactMode != "recover" {
		t.Errorf("expected redact-mode 'recover' from env, got %q", flags.RedactMode)
	}
}

// TestParseRedactModeFlagOverridesEnv verifies --redact wins over JENNY_REDACT.
func TestParseRedactModeFlagOverridesEnv(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	t.Setenv("JENNY_REDACT", "recover")

	os.Args = []string{"jenny", "--redact", "disabled", "--print", "hello"}

	flags, _, err := Parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if flags.RedactMode != "disabled" {
		t.Errorf("expected --redact 'disabled' to win over env, got %q", flags.RedactMode)
	}
}

// TestParseRedactModeInvalid rejects unknown values.
func TestParseRedactModeInvalid(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"jenny", "--redact", "banana", "--print", "hello"}

	_, _, err := Parse()
	if err == nil {
		t.Error("expected error for invalid --redact value")
	}
}

// TestParseTranscriptDirEnvVar verifies JENNY_TRANSCRIPT_DIR populates the field.
func TestParseTranscriptDirEnvVar(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	t.Setenv("JENNY_TRANSCRIPT_DIR", "/tmp/jenny-transcripts")

	os.Args = []string{"jenny", "--print", "hello"}

	flags, _, err := Parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if flags.TranscriptDir != "/tmp/jenny-transcripts" {
		t.Errorf("expected transcript-dir from env, got %q", flags.TranscriptDir)
	}
}

// TestParseTranscriptDirFlagOverridesEnv verifies --transcript-dir wins over JENNY_TRANSCRIPT_DIR.
func TestParseTranscriptDirFlagOverridesEnv(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	t.Setenv("JENNY_TRANSCRIPT_DIR", "/tmp/from-env")

	os.Args = []string{"jenny", "--transcript-dir", "/tmp/from-flag", "--print", "hello"}

	flags, _, err := Parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if flags.TranscriptDir != "/tmp/from-flag" {
		t.Errorf("expected --transcript-dir to win over env, got %q", flags.TranscriptDir)
	}
}

// TestParseMaxToolConcurrencyEnvVar verifies JENNY_MAX_TOOL_CONCURRENCY populates the field.
func TestParseMaxToolConcurrencyEnvVar(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	t.Setenv("JENNY_MAX_TOOL_CONCURRENCY", "4")

	os.Args = []string{"jenny", "--print", "hello"}

	flags, _, err := Parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if flags.MaxToolConcurrency != 4 {
		t.Errorf("expected max-tool-concurrency=4 from env, got %d", flags.MaxToolConcurrency)
	}
}

// TestParseMaxToolConcurrencyFlagOverridesEnv verifies --max-tool-concurrency wins over JENNY_MAX_TOOL_CONCURRENCY.
func TestParseMaxToolConcurrencyFlagOverridesEnv(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	t.Setenv("JENNY_MAX_TOOL_CONCURRENCY", "4")

	os.Args = []string{"jenny", "--max-tool-concurrency", "8", "--print", "hello"}

	flags, _, err := Parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if flags.MaxToolConcurrency != 8 {
		t.Errorf("expected --max-tool-concurrency=8 to win over env, got %d", flags.MaxToolConcurrency)
	}
}

// TestParseCompactKeepArchiveEnvVar verifies JENNY_COMPACT_KEEP_ARCHIVE populates the field.
func TestParseCompactKeepArchiveEnvVar(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	t.Setenv("JENNY_COMPACT_KEEP_ARCHIVE", "1")

	os.Args = []string{"jenny", "--print", "hello"}

	flags, _, err := Parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !flags.CompactKeepArchive {
		t.Error("expected compact-keep-archive=true from env")
	}
}

// TestParseCompactKeepArchiveFlagOverridesEnv verifies --compact-keep-archive wins.
func TestParseCompactKeepArchiveFlagOverridesEnv(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Unsetenv("JENNY_COMPACT_KEEP_ARCHIVE")

	os.Args = []string{"jenny", "--compact-keep-archive", "--print", "hello"}

	flags, _, err := Parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !flags.CompactKeepArchive {
		t.Error("expected --compact-keep-archive to set the field to true")
	}
}

// TestParseDisableCompactEnvVar verifies JENNY_DISABLE_COMPACT populates the field.
func TestParseDisableCompactEnvVar(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	t.Setenv("JENNY_DISABLE_COMPACT", "1")

	os.Args = []string{"jenny", "--print", "hello"}

	flags, _, err := Parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !flags.DisableCompact {
		t.Error("expected disable-compact=true from env")
	}
}

// TestParseDisableCompactFlagOverridesEnv verifies --disable-compact wins.
func TestParseDisableCompactFlagOverridesEnv(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Unsetenv("JENNY_DISABLE_COMPACT")

	os.Args = []string{"jenny", "--disable-compact", "--print", "hello"}

	flags, _, err := Parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !flags.DisableCompact {
		t.Error("expected --disable-compact to set the field to true")
	}
}

// TestParseDisableAutoCompactEnvVar verifies JENNY_DISABLE_AUTO_COMPACT populates the field.
func TestParseDisableAutoCompactEnvVar(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	t.Setenv("JENNY_DISABLE_AUTO_COMPACT", "1")

	os.Args = []string{"jenny", "--print", "hello"}

	flags, _, err := Parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !flags.DisableAutoCompact {
		t.Error("expected disable-auto-compact=true from env")
	}
}

// TestParseEnableSessionMemoryEnvVar verifies JENNY_ENABLE_SESSION_MEMORY populates the field.
func TestParseEnableSessionMemoryEnvVar(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	t.Setenv("JENNY_ENABLE_SESSION_MEMORY", "1")

	os.Args = []string{"jenny", "--print", "hello"}

	flags, _, err := Parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !flags.EnableSessionMemory {
		t.Error("expected enable-session-memory=true from env")
	}
}

// TestParseDisableAutoMemoryEnvVar verifies JENNY_DISABLE_AUTO_MEMORY populates the field.
func TestParseDisableAutoMemoryEnvVar(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	t.Setenv("JENNY_DISABLE_AUTO_MEMORY", "1")

	os.Args = []string{"jenny", "--print", "hello"}

	flags, _, err := Parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !flags.DisableAutoMemory {
		t.Error("expected disable-auto-memory=true from env")
	}
}

// TestParseDisableAutoMemoryFlagOverridesEnv verifies --disable-auto-memory wins.
func TestParseDisableAutoMemoryFlagOverridesEnv(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Unsetenv("JENNY_DISABLE_AUTO_MEMORY")

	os.Args = []string{"jenny", "--disable-auto-memory", "--print", "hello"}

	flags, _, err := Parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !flags.DisableAutoMemory {
		t.Error("expected --disable-auto-memory to set the field to true")
	}
}

// TestParseBoolFlagExplicitFalseOverridesConfig is a regression test for the
// pflag + koanf chain. pflag's BoolVar accepts `--flag=false` as an explicit
// negation, and posflag.Provider writes that value into koanf, which then
// overrides the lower-precedence config.json. This test pins that behaviour
// so a future refactor doesn't accidentally drop it.
func TestParseBoolFlagExplicitFalseOverridesConfig(t *testing.T) {
	origArgs := os.Args
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() {
		os.Args = origArgs
		os.Chdir(origDir)
	}()

	// Stage a project-local .jenny/config.json with the flag set to true.
	tmpDir := t.TempDir()
	jennyDir := tmpDir + "/.jenny"
	if err := os.MkdirAll(jennyDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(jennyDir+"/config.json",
		[]byte(`{"disable-compact": true, "disable-auto-memory": true}`),
		0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	// Pass --disable-compact=false and --disable-auto-memory=false explicitly.
	// The CLI flag must win over the config.json values.
	os.Args = []string{"jenny", "--disable-compact=false", "--disable-auto-memory=false", "--print", "hello"}

	flags, _, err := Parse()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if flags.DisableCompact {
		t.Errorf("expected --disable-compact=false to override config.json true, got true")
	}
	if flags.DisableAutoMemory {
		t.Errorf("expected --disable-auto-memory=false to override config.json true, got true")
	}
}

// TestParseBoolFlagAbsentUsesConfig verifies that without an explicit CLI flag,
// the value from .jenny/config.json flows through into the parsed Flags.
func TestParseBoolFlagAbsentUsesConfig(t *testing.T) {
	origArgs := os.Args
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() {
		os.Args = origArgs
		os.Chdir(origDir)
	}()

	tmpDir := t.TempDir()
	jennyDir := tmpDir + "/.jenny"
	if err := os.MkdirAll(jennyDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(jennyDir+"/config.json",
		[]byte(`{"disable-compact": true}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	os.Args = []string{"jenny", "--print", "hello"}

	flags, _, err := Parse()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !flags.DisableCompact {
		t.Errorf("expected config.json disable-compact=true to flow through, got false")
	}
}
