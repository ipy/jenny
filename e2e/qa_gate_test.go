package e2e_test

import (
	"testing"

	"github.com/ipy/jenny/e2e/harness"
)

func TestQAGate(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "qa-gate.simple-task.no-qa-eval-event",
			Category:    "qa-gate",
			Description: "simple single-turn task does not produce qa_eval events",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					NotContains: []string{"qa_eval"},
				},
				StreamJSON: &harness.StreamJSONExpectation{
					LastEvent: &harness.EventExpectation{
						Type:    "result",
						Subtype: "success",
					},
				},
			},
		},
		{
			ID:          "qa-gate.tool-use.no-qa-eval-event",
			Category:    "qa-gate",
			Description: "short tool-use flow does not trigger QA gate",
			Target: harness.TargetInvocation{
				Kind:             "prompt",
				Prompt:           "run echo hello",
				Format:           "stream-json",
				Cassette:         "tool-use-turn1",
				CassetteSequence: []string{"tool-use-turn1", "tool-use-turn2"},
				Args:             []string{"--dangerously-skip-permissions"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					NotContains: []string{"qa_eval"},
				},
				StreamJSON: &harness.StreamJSONExpectation{
					LastEvent: &harness.EventExpectation{
						Type:    "result",
						Subtype: "success",
					},
				},
			},
		},
		{
			ID:          "qa-gate.result-event.has-standard-fields",
			Category:    "qa-gate",
			Description: "result event has standard fields even with QA gate code present",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				StreamJSON: &harness.StreamJSONExpectation{
					LastEvent: &harness.EventExpectation{
						Type:          "result",
						Subtype:       "success",
						HasFields:     []string{"session_id", "uuid", "total_cost_usd", "result"},
						FieldNotEmpty: []string{"session_id", "uuid"},
					},
				},
			},
		},
	})
}
