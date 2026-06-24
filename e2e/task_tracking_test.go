package e2e_test

import (
	"testing"

	"github.com/ipy/jenny/e2e/harness"
)

func TestTaskTrackingToolRegistration(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "task-tracking.tools.present-when-enabled",
			Category:    "task-tracking",
			Description: "task tracking tools appear in API request when --enable-tasks is set",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Args:     []string{"--enable-tasks"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				APIRequests: []harness.APIRequestExpectation{
					{
						Index: 0,
						Tools: &harness.ToolsExpectation{
							HasTool: []string{"TaskCreate", "TaskGet", "TaskList", "TaskUpdate"},
						},
					},
				},
			},
		},
		{
			ID:          "task-tracking.tools.absent-by-default",
			Category:    "task-tracking",
			Description: "task tracking tools are not registered by default",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				APIRequests: []harness.APIRequestExpectation{
					{
						Index: 0,
						Tools: &harness.ToolsExpectation{
							NotHasTool: []string{"TaskCreate", "TaskGet", "TaskList", "TaskUpdate"},
						},
					},
				},
			},
		},
		{
			ID:          "task-tracking.init.tools-in-init-event",
			Category:    "task-tracking",
			Description: "task tracking tool names appear in system/init event when enabled",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Args:     []string{"--enable-tasks"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				StreamJSON: &harness.StreamJSONExpectation{
					FirstEvent: &harness.EventExpectation{
						Type:    "system",
						Subtype: "init",
					},
				},
				Stdout: &harness.StdoutExpectation{
					Contains: []string{`"TaskCreate"`, `"TaskList"`},
				},
			},
		},
	})
}
