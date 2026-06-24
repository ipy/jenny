package e2e_test

import (
	"testing"

	"github.com/ipy/jenny/e2e/harness"
)

func TestDynamicMCP(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "dynamic-mcp.no-servers.call-mcp-absent",
			Category:    "dynamic-mcp",
			Description: "call_mcp tool is not registered when no MCP servers are configured",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Args:     []string{"--dynamic-mcp"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				APIRequests: []harness.APIRequestExpectation{
					{
						Index: 0,
						Tools: &harness.ToolsExpectation{
							NotHasTool: []string{"call_mcp"},
						},
					},
				},
			},
		},
		{
			ID:          "dynamic-mcp.no-servers.core-tools-present",
			Category:    "dynamic-mcp",
			Description: "core tools are still registered even with --dynamic-mcp",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Args:     []string{"--dynamic-mcp"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				APIRequests: []harness.APIRequestExpectation{
					{
						Index: 0,
						Tools: &harness.ToolsExpectation{
							HasTool: []string{"Read", "Bash", "Glob", "Grep"},
						},
					},
				},
			},
		},
	})
}
