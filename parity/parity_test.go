package parity_test

import (
	"testing"

	"github.com/ipy/jenny/parity/harness"
)

// TestCLIFlags runs CLI flag parity tests against jenny.
func TestCLIFlags(t *testing.T) {
	// Get cassette directory from jenny_test fixtures
	cassetteDir := "../jenny_test/fixtures/cassettes"

	cfg := &harness.Config{
		ProductName: "jenny",
		Target:      "../cmd/jenny",
		CassetteDir: cassetteDir,
		TimeoutMs:   60000,
	}

	tests := []*harness.TestCase{
		{
			ID:          "cli.version.flag",
			Category:    "cli-flags",
			Description: "--version prints version and exits 0",
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
			ID:          "cli.version.short-v",
			Category:    "cli-flags",
			Description: "-v is an alias for --version",
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
			ID:          "cli.help.flag",
			Category:    "cli-flags",
			Description: "--help prints usage information and exits 0",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--help"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stderr: &harness.StderrExpectation{
					Contains: []string{"Usage"},
				},
			},
		},
		{
			ID:          "cli.unknown-flag",
			Category:    "cli-flags",
			Description: "An unknown flag produces a non-zero exit and an error",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--definitely-not-a-real-flag-xyz"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 1,
				Stderr: &harness.StderrExpectation{
					Contains: []string{"unknown", "unrecognized", "flag provided but not defined"},
				},
			},
		},
		{
			ID:          "cli.output-format-stream-json",
			Category:    "cli-flags",
			Description: "--output-format stream-json emits JSON lines",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "echo hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
			},
		},
	}

	runner := harness.NewSuiteRunner(cfg, tests)
	reporter := &harness.TextReporter{}
	results := runner.RunAll(reporter)

	// Check for failures
	for _, result := range results {
		if result.Status == "fail" {
			t.Errorf("parity test %s failed", result.ID)
			for _, d := range result.Diff {
				t.Logf("  - %s: expected %v, got %v", d.Path, d.Expected, d.Actual)
			}
		}
	}
}

// TestAPIProtocol runs API protocol parity tests.
func TestAPIProtocol(t *testing.T) {
	cassetteDir := "../jenny_test/fixtures/cassettes"

	cfg := &harness.Config{
		ProductName: "jenny",
		Target:      "../cmd/jenny",
		CassetteDir: cassetteDir,
		TimeoutMs:   60000,
	}

	tests := []*harness.TestCase{
		{
			ID:          "api.max-tokens",
			Category:    "api-protocol",
			Description: "max_tokens is 64000 in outbound request",
			Tags:        []string{"api"},
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "echo hello",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
			},
		},
	}

	runner := harness.NewSuiteRunner(cfg, tests)
	reporter := &harness.TextReporter{}
	results := runner.RunAll(reporter)

	for _, result := range results {
		if result.Status == "fail" {
			t.Errorf("parity test %s failed", result.ID)
		}
	}
}
