package agent

import (
	"github.com/ipy/jenny/internal/api"
)

const truncatedToolInputReminder = `<system-reminder>
Your last response was truncated before tool call arguments could be completed. Retry with shorter output: reduce thinking length, use smaller code blocks, or split the operation across multiple tool calls.
</system-reminder>`

// shouldRetryTruncatedToolInput reports whether a truncated response left tool
// arguments unparseable and the turn should be retried without persisting it.
func shouldRetryTruncatedToolInput(streamResult *api.StreamResult, toolUseBlocks []api.ToolUseBlock) bool {
	if streamResult == nil {
		return false
	}
	truncated := streamResult.StopReason == api.StopReasonMaxTokens || !streamResult.StreamComplete
	if !truncated || len(toolUseBlocks) == 0 {
		return false
	}
	for _, tb := range toolUseBlocks {
		if tb.InputParseError != "" {
			return true
		}
		if len(tb.Input) == 0 {
			return true
		}
	}
	return false
}

func buildTruncatedToolInputReminder() api.Message {
	return api.Message{
		Role:      api.RoleUser,
		Content:   truncatedToolInputReminder,
		IsVirtual: true,
	}
}
