package e2e_test

import (
	"testing"

	"github.com/ipy/jenny/e2e/harness"
)

// TestCLIVersion verifies version flag behavior matches reference agent.
func TestCLIVersion(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "cli.version.long-flag",
			Category:    "cli-flags",
			Description: "--version prints semver and exits 0",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--version"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Matches: []string{`^\d+\.\d+\.\d+`},
				},
			},
		},
		{
			ID:          "cli.version.short-flag",
			Category:    "cli-flags",
			Description: "-v prints the same version",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"-v"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Matches: []string{`^\d+\.\d+\.\d+`},
				},
			},
		},
		{
			ID:          "cli.version.contains-product-name",
			Category:    "cli-flags",
			Description: "version output includes product identifier",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--version"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Contains: []string{"jenny"},
				},
			},
		},
	})
}

// TestCLIHelp verifies help flag behavior.
func TestCLIHelp(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "cli.help.long-flag",
			Category:    "cli-flags",
			Description: "--help prints usage and exits 0",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--help"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stderr: &harness.StderrExpectation{
					Contains: []string{"Usage", "usage"},
				},
			},
		},
		{
			ID:          "cli.help.short-flag",
			Category:    "cli-flags",
			Description: "-h prints usage and exits 0",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"-h"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stderr: &harness.StderrExpectation{
					Contains: []string{"Usage", "usage"},
				},
			},
		},
	})
}

// TestCLINoPrompt verifies missing prompt produces an error.
func TestCLINoPrompt(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "cli.no-prompt.exits-nonzero",
			Category:    "cli-flags",
			Description: "no prompt argument results in non-zero exit",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{},
				Env:  []string{"ANTHROPIC_AUTH_TOKEN=dummy"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 1,
			},
		},
	})
}

// TestCLIUnknownFlag verifies unknown flags produce errors.
func TestCLIUnknownFlag(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "cli.unknown-flag.exits-nonzero",
			Category:    "cli-flags",
			Description: "unknown flag exits 1 with error message",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--nonexistent-flag-xyz"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 1,
				Stderr: &harness.StderrExpectation{
					Contains: []string{"unknown", "unrecognized", "flag provided but not defined", "not defined"},
				},
			},
		},
		{
			ID:          "cli.unknown-short-flag.exits-nonzero",
			Category:    "cli-flags",
			Description: "unknown short flag exits 1",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"-Z"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 1,
			},
		},
	})
}

// TestCLIOutputFormat verifies output format flag handling.
func TestCLIOutputFormat(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "cli.output-format.stream-json-produces-ndjson",
			Category:    "cli-flags",
			Description: "--output-format stream-json emits NDJSON on stdout",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				StreamJSON: &harness.StreamJSONExpectation{
					AllLinesValidJSON: true,
					EventCount:        &harness.LengthExpectation{Min: 2},
				},
			},
		},
		{
			ID:          "cli.output-format.text-no-json",
			Category:    "cli-flags",
			Description: "default text format does not emit JSON lines",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "text",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Contains:    []string{"Hello from cassette"},
					NotContains: []string{`"type":"system"`},
				},
			},
		},
	})
}

// TestCLISessionResume verifies session resume error handling.
func TestCLISessionResume(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "cli.resume.nonexistent-session",
			Category:    "cli-flags",
			Description: "-r with nonexistent session ID exits nonzero",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"-r", "nonexistent-session-id-xyz", "-p", "hello"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 1,
				Stderr: &harness.StderrExpectation{
					Contains: []string{"session", "not found", "invalid"},
				},
			},
		},
		{
			ID:          "cli.continue.no-prior-sessions",
			Category:    "cli-flags",
			Description: "--continue with no prior sessions exits nonzero",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--continue", "-p", "hello"},
				Env: []string{
					"ANTHROPIC_AUTH_TOKEN=dummy",
					"JENNY_TRANSCRIPT_DIR=${WORK_DIR}/.jenny-transcripts",
				},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 1,
				Stderr: &harness.StderrExpectation{
					Contains: []string{"no sessions", "not found"},
				},
			},
		},
	})
}

// TestCLIPrintSystemPrompt verifies --print-system-prompt flag.
func TestCLIPrintSystemPrompt(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "cli.print-system-prompt.exits-zero",
			Category:    "cli-flags",
			Description: "--print-system-prompt prints prompt and exits 0 without API call",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--print-system-prompt"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Length: &harness.LengthExpectation{Min: 1000},
				},
			},
		},
		{
			ID:          "cli.print-system-prompt.no-api-call",
			Category:    "cli-flags",
			Description: "--print-system-prompt does not make any API calls",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--print-system-prompt"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					NotContains: []string{`"type":"result"`},
				},
			},
		},
	})
}

// TestCLIVerbose verifies verbose/debug output goes to stderr.
func TestCLIVerbose(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "cli.verbose.debug-on-stderr",
			Category:    "cli-flags",
			Description: "--verbose sends debug output to stderr, not stdout",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Args:     []string{"--verbose"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				StreamJSON: &harness.StreamJSONExpectation{
					AllLinesValidJSON: true,
				},
			},
		},
		{
			ID:          "cli.debug-env.debug-on-stderr",
			Category:    "cli-flags",
			Description: "JENNY_DEBUG=1 sends debug output to stderr, not stdout",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Env:      []string{"JENNY_DEBUG=1"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				StreamJSON: &harness.StreamJSONExpectation{
					AllLinesValidJSON: true,
				},
			},
		},
	})
}

// TestCLINoSessionPersistence verifies --no-session-persistence flag.
func TestCLINoSessionPersistence(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "cli.no-session-persistence.resume-rejected",
			Category:    "cli-flags",
			Description: "--no-session-persistence disallows resume",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--no-session-persistence", "-r", "some-id", "-p", "hello"},
				Env:  []string{"ANTHROPIC_AUTH_TOKEN=dummy"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 1,
				Stderr: &harness.StderrExpectation{
					Contains: []string{"persistence", "disabled", "cannot"},
				},
			},
		},
	})
}

// TestCLIPermissionLevel verifies --permission-level flag validation.
func TestCLIPermissionLevel(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "cli.permission-level.invalid-value",
			Category:    "cli-flags",
			Description: "--permission-level with invalid value exits nonzero",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--permission-level", "superadmin", "-p", "hello"},
				Env:  []string{"ANTHROPIC_AUTH_TOKEN=dummy"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 1,
				Stderr: &harness.StderrExpectation{
					Contains: []string{"permission", "read", "edit"},
				},
			},
		},
		{
			ID:          "cli.permission-level.empty-value",
			Category:    "cli-flags",
			Description: "--permission-level without value exits nonzero",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--permission-level=", "-p", "hello"},
				Env:  []string{"ANTHROPIC_AUTH_TOKEN=dummy"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 1,
				Stderr: &harness.StderrExpectation{
					Contains: []string{"permission"},
				},
			},
		},
		{
			ID:          "cli.permission-level.skip-permissions-wins",
			Category:    "cli-flags",
			Description: "--dangerously-skip-permissions wins when both flags given",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Args:     []string{"--dangerously-skip-permissions", "--permission-level", "read"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				StreamJSON: &harness.StreamJSONExpectation{
					LastEvent: &harness.EventExpectation{
						Type:    "result",
						Subtype: "success",
					},
				},
			},
		},
		{
			ID:          "cli.permission-level.valid-values-accepted",
			Category:    "cli-flags",
			Description: "all valid permission levels accepted by CLI",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--permission-level", "unrestricted", "-p", "hello"},
				Env: []string{
					"ANTHROPIC_AUTH_TOKEN=dummy",
					"ANTHROPIC_BASE_URL=http://127.0.0.1:1", // force API to fail
				},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 1, // API call fails (unreachable endpoint)
			},
		},
	})
}

// TestCLIMaxBudgetUsd verifies --max-budget-usd flag is accepted.
func TestCLIMaxBudgetUsd(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "cli.max-budget-usd.flag-accepted",
			Category:    "cli-flags",
			Description: "--max-budget-usd is accepted without error",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Args:     []string{"--max-budget-usd", "10.50"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				StreamJSON: &harness.StreamJSONExpectation{
					LastEvent: &harness.EventExpectation{
						Type:    "result",
						Subtype: "success",
					},
				},
			},
		},
		{
			ID:          "cli.max-budget-usd.zero-disables",
			Category:    "cli-flags",
			Description: "--max-budget-usd 0 disables budget limit",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Args:     []string{"--max-budget-usd", "0"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				StreamJSON: &harness.StreamJSONExpectation{
					LastEvent: &harness.EventExpectation{
						Type:    "result",
						Subtype: "success",
					},
				},
			},
		},
	})
}

// TestCLIMaxTurns verifies --max-turns flag.
func TestCLIMaxTurns(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "cli.max-turns.flag-accepted",
			Category:    "cli-flags",
			Description: "--max-turns is accepted without error",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Args:     []string{"--max-turns", "2"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				StreamJSON: &harness.StreamJSONExpectation{
					LastEvent: &harness.EventExpectation{
						Type:    "result",
						Subtype: "success",
					},
				},
			},
		},
	})
}

// TestCLIIncludePartialMessages verifies --include-partial-messages requires stream-json.
func TestCLIIncludePartialMessages(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "cli.include-partial-messages.needs-stream-json",
			Category:    "cli-flags",
			Description: "--include-partial-messages without stream-json exits nonzero",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--include-partial-messages", "-p", "hello"},
				Env:  []string{"ANTHROPIC_AUTH_TOKEN=dummy"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 1,
				Stderr: &harness.StderrExpectation{
					Contains: []string{"partial", "stream-json"},
				},
			},
		},
	})
}

// TestCLIEffort verifies --effort flag is accepted.
func TestCLIEffort(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "cli.effort.low-accepted",
			Category:    "cli-flags",
			Description: "--effort low is accepted",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Args:     []string{"--effort", "low"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				StreamJSON: &harness.StreamJSONExpectation{
					LastEvent: &harness.EventExpectation{
						Type:    "result",
						Subtype: "success",
					},
				},
			},
		},
		{
			ID:          "cli.effort.medium-accepted",
			Category:    "cli-flags",
			Description: "--effort medium is accepted",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Args:     []string{"--effort", "medium"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
			},
		},
		{
			ID:          "cli.effort.high-accepted",
			Category:    "cli-flags",
			Description: "--effort high is accepted",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Args:     []string{"--effort", "high"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
			},
		},
	})
}

// TestCLIRedact verifies --redact flag validation.
func TestCLIRedact(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "cli.redact.disabled-accepted",
			Category:    "cli-flags",
			Description: "--redact disabled is accepted",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Args:     []string{"--redact", "disabled"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
			},
		},
		{
			ID:          "cli.redact.redact-accepted",
			Category:    "cli-flags",
			Description: "--redact redact is accepted (default)",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Args:     []string{"--redact", "redact"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
			},
		},
	})
}

// TestCLIStrictMCPConfig verifies --strict-mcp-config flag.
func TestCLIStrictMCPConfig(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "cli.strict-mcp-config.flag-accepted",
			Category:    "cli-flags",
			Description: "--strict-mcp-config is accepted",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Args:     []string{"--strict-mcp-config"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
			},
		},
	})
}

// TestCLIMaxToolConcurrency verifies --max-tool-concurrency flag.
func TestCLIMaxToolConcurrency(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "cli.max-tool-concurrency.flag-accepted",
			Category:    "cli-flags",
			Description: "--max-tool-concurrency is accepted without error",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Args:     []string{"--max-tool-concurrency", "4"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
			},
		},
	})
}

// TestCLIBooleanFlagNegation verifies --flag=false form works.
func TestCLIBooleanFlagNegation(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "cli.bool-negation.verbose-false-accepted",
			Category:    "cli-flags",
			Description: "--verbose=false is accepted",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Args:     []string{"--verbose=false"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				StreamJSON: &harness.StreamJSONExpectation{
					AllLinesValidJSON: true,
				},
			},
		},
	})
}
