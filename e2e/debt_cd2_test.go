package e2e_test

import (
	"testing"

	"github.com/ipy/jenny/e2e/harness"
)

// TestDebtCD2_RefreshRegistryAndOffline verifies the fix for debt-CD-2:
// --refresh-registry and --offline are mutually exclusive when used together.
func TestDebtCD2_RefreshRegistryAndOffline(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "debt-CD-2.mutual-exclusive.exits-nonzero",
			Category:    "cli-flags",
			Description: "--refresh-registry --offline exits nonzero with mutual exclusivity error",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--refresh-registry", "--offline", "-p", "hello"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 1,
				Stderr: &harness.StderrExpectation{
					Contains: []string{"mutually exclusive"},
				},
			},
		},
		{
			ID:          "debt-CD-2.offline-reversed-order.flags",
			Category:    "cli-flags",
			Description: "--offline --refresh-registry (reversed order) also exits nonzero",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--offline", "--refresh-registry", "-p", "hello"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 1,
				Stderr: &harness.StderrExpectation{
					Contains: []string{"mutually exclusive"},
				},
			},
		},
		{
			ID:          "debt-CD-2.refresh-registry-alone.no-mutual-exclusion",
			Category:    "cli-flags",
			Description: "--refresh-registry alone does not produce mutual exclusivity error (without -p, exits immediately with no-prompt error)",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--refresh-registry"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 1, // "no prompt provided" error occurs before registry fetch
				Stderr: &harness.StderrExpectation{
					Contains:    []string{"no prompt"},
					NotContains: []string{"mutually exclusive"},
				},
			},
		},
		{
			ID:          "debt-CD-2.offline-alone.accepted",
			Category:    "cli-flags",
			Description: "--offline alone is accepted (no mutual exclusivity error)",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Args:     []string{"--offline"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stderr: &harness.StderrExpectation{
					NotContains: []string{"mutually exclusive"},
				},
			},
		},
		{
			ID:          "debt-CD-2.no-mutual-exclusion-without-both-flags",
			Category:    "cli-flags",
			Description: "neither --refresh-registry nor --offline produces no mutual exclusivity error",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stderr: &harness.StderrExpectation{
					NotContains: []string{"mutually exclusive"},
				},
			},
		},
		{
			ID:          "debt-CD-2.error-message-on-stderr",
			Category:    "cli-flags",
			Description: "mutual exclusivity error appears on stderr, not stdout",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--refresh-registry", "--offline", "-p", "hello"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 1,
				Stdout: &harness.StdoutExpectation{
					NotContains: []string{"mutually exclusive"},
				},
				Stderr: &harness.StderrExpectation{
					Contains: []string{"mutually exclusive"},
				},
			},
		},
	})
}
