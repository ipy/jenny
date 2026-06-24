package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ipy/jenny/internal/api"
	"github.com/ipy/jenny/internal/log"
)

// SideQueryMode determines how a side query is executed.
type SideQueryMode int

const (
	// SideQueryModeFork reuses the exact same cache prefix (system prompt, tools,
	// message history, model, thinking config) as the main session, appending only
	// the evaluation prompt. This maximizes prompt cache hits.
	SideQueryModeFork SideQueryMode = iota

	// SideQueryModeSummary condenses the conversation history into a separate
	// request with its own system prompt. This avoids sending the full prefix,
	// which can be cheaper when caching is unavailable or when a cheaper model
	// is used.
	SideQueryModeSummary
)

func (m SideQueryMode) String() string {
	switch m {
	case SideQueryModeFork:
		return "fork"
	case SideQueryModeSummary:
		return "summary"
	default:
		return "unknown"
	}
}

// SideQueryContext holds the immutable session state needed for side queries.
// It captures the exact parameters used by the main session so that fork-mode
// queries share the same cache prefix. Created once per session and passed to
// all side queries.
type SideQueryContext struct {
	SystemPrompt   []string
	Tools          []api.ToolParam
	Model          string
	RequestedModel string
	ThinkingConfig api.ThinkingConfig

	CostState *CostState
	CostMu    *sync.Mutex

	StreamCfg *StreamConfig

	// ClientFactory creates an API client for the given model. When nil,
	// defaults to api.NewClientWithModel. Injected in tests to provide mocks.
	ClientFactory func(model string) (api.Requester, error)
}

// newClient creates an API client using ClientFactory if set, otherwise
// falling back to api.NewClientWithModel.
func (sqctx *SideQueryContext) newClient(model string) (api.Requester, error) {
	if sqctx.ClientFactory != nil {
		return sqctx.ClientFactory(model)
	}
	return api.NewClientWithModel(model)
}

// SideQuery represents a request for a side evaluation (complexity eval,
// progress eval, or any other fork-based assessment).
type SideQuery struct {
	Name     string        // "complexity_eval", "progress_eval", "compact", etc.
	Messages []api.Message // Conversation history snapshot (shallow copy)
	Prompt   string        // Evaluation prompt appended in fork mode

	MaxTokens int           // Max output tokens for the response
	Timeout   time.Duration // Per-query timeout (0 = no timeout)

	// Summary mode configuration. When SummarySystemPrompt is nil, summary
	// mode is unavailable and fork mode is always used.
	SummarySystemPrompt []string // System prompt for summary mode
	SummaryMaxMessages  int      // Keep last N messages (0 = use all)
	SummaryModel        string   // Optional cheaper model (empty = same as main)
	DisableThinking     bool     // Disable thinking in summary mode
}

// SideQueryResult holds the response from a side query execution.
type SideQueryResult struct {
	Text  string
	Model string
	Usage api.Usage
	Mode  SideQueryMode
}

// Execute runs the side query, selecting the cheapest viable mode.
// If the preferred mode fails and a fallback is available, it retries.
func (sq *SideQuery) Execute(ctx context.Context, sqctx *SideQueryContext) (*SideQueryResult, error) {
	if sqctx == nil {
		return nil, fmt.Errorf("side query %s: nil context", sq.Name)
	}

	mode := sq.SelectMode(sqctx)
	fallback := sq.fallbackMode(mode)

	result, err := sq.executeMode(ctx, sqctx, mode)
	if err != nil && fallback != nil {
		log.Debug("Side query failed, trying fallback",
			"name", sq.Name, "mode", mode, "fallback", *fallback, "error", err)
		return sq.executeMode(ctx, sqctx, *fallback)
	}
	return result, err
}

// ExecuteFork runs the side query in fork mode only.
func (sq *SideQuery) ExecuteFork(ctx context.Context, sqctx *SideQueryContext) (*SideQueryResult, error) {
	return sq.executeMode(ctx, sqctx, SideQueryModeFork)
}

// ExecuteSummary runs the side query in summary mode only.
func (sq *SideQuery) ExecuteSummary(ctx context.Context, sqctx *SideQueryContext) (*SideQueryResult, error) {
	return sq.executeMode(ctx, sqctx, SideQueryModeSummary)
}

// SelectMode estimates cost for both modes and returns the cheaper one.
// Falls back to fork mode when summary mode is not configured.
func (sq *SideQuery) SelectMode(sqctx *SideQueryContext) SideQueryMode {
	if sqctx == nil || sq.SummarySystemPrompt == nil {
		return SideQueryModeFork
	}

	forkCost := sq.estimateForkCost(sqctx)
	summaryCost := sq.estimateSummaryCost(sqctx)

	log.Debug("Side query mode selection",
		"name", sq.Name,
		"forkCost", forkCost,
		"summaryCost", summaryCost,
	)

	if forkCost <= summaryCost {
		return SideQueryModeFork
	}
	return SideQueryModeSummary
}

func (sq *SideQuery) executeMode(ctx context.Context, sqctx *SideQueryContext, mode SideQueryMode) (*SideQueryResult, error) {
	switch mode {
	case SideQueryModeFork:
		return sq.executeFork(ctx, sqctx)
	case SideQueryModeSummary:
		return sq.executeSummary(ctx, sqctx)
	default:
		return nil, fmt.Errorf("unknown side query mode: %d", mode)
	}
}

// executeFork creates a fresh client with identical parameters to the main
// session and sends the full message history + evaluation prompt. The shared
// prefix (system prompt + tools + messages) is a cache hit.
func (sq *SideQuery) executeFork(ctx context.Context, sqctx *SideQueryContext) (*SideQueryResult, error) {
	if sq.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, sq.Timeout)
		defer cancel()
	}

	model := sqctx.Model
	if sqctx.RequestedModel != "" {
		model = sqctx.RequestedModel
	}
	model = ResolveModel(model)

	client, err := sqctx.newClient(model)
	if err != nil {
		return nil, fmt.Errorf("side query %s fork client creation: %w", sq.Name, err)
	}
	client.SetMaxTokensOverride(sq.MaxTokens)
	client.SetThinkingConfig(sqctx.ThinkingConfig)

	forkMessages := make([]api.Message, len(sq.Messages), len(sq.Messages)+1)
	copy(forkMessages, sq.Messages)
	if sq.Prompt != "" {
		forkMessages = append(forkMessages, api.Message{
			Role:    api.RoleUser,
			Content: sq.Prompt,
		})
	}

	resp, err := client.SendMessage(ctx, forkMessages, sqctx.Tools, nil, sqctx.SystemPrompt, "")
	if err != nil {
		return nil, fmt.Errorf("side query %s fork API call: %w", sq.Name, err)
	}

	sqctx.CostMu.Lock()
	if resp.Model != "" {
		AccumulateUsage(sqctx.CostState, resp.Model, resp.Usage)
	}
	sqctx.CostMu.Unlock()

	text := extractTextFromResponse(resp)
	return &SideQueryResult{
		Text:  text,
		Model: resp.Model,
		Usage: resp.Usage,
		Mode:  SideQueryModeFork,
	}, nil
}

// executeSummary renders the conversation into a single user message and sends
// it as a separate request, optionally using a cheaper model. The message
// history is flattened into a text transcript so the LLM sees a clean,
// structured input rather than a multi-turn conversation it must untangle.
func (sq *SideQuery) executeSummary(ctx context.Context, sqctx *SideQueryContext) (*SideQueryResult, error) {
	if sq.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, sq.Timeout)
		defer cancel()
	}

	model := sqctx.Model
	if sq.SummaryModel != "" {
		model = sq.SummaryModel
	} else if sqctx.RequestedModel != "" {
		model = sqctx.RequestedModel
	}
	model = ResolveModel(model)

	client, err := sqctx.newClient(model)
	if err != nil {
		return nil, fmt.Errorf("side query %s summary client creation: %w", sq.Name, err)
	}
	client.SetMaxTokensOverride(sq.MaxTokens)

	if sq.DisableThinking {
		client.SetThinkingConfig(api.ThinkingConfig{})
	} else {
		client.SetThinkingConfig(sqctx.ThinkingConfig)
	}

	msgs := sq.Messages
	if sq.SummaryMaxMessages > 0 && len(msgs) > sq.SummaryMaxMessages {
		msgs = msgs[len(msgs)-sq.SummaryMaxMessages:]
	}

	transcript := renderConversationText(msgs)

	var content string
	if sq.Prompt != "" {
		content = transcript + "\n\n" + sq.Prompt
	} else {
		content = transcript
	}

	singleMessage := []api.Message{{
		Role:    api.RoleUser,
		Content: content,
	}}

	resp, err := client.SendMessage(ctx, singleMessage, nil, nil, sq.SummarySystemPrompt, "")
	if err != nil {
		return nil, fmt.Errorf("side query %s summary API call: %w", sq.Name, err)
	}

	sqctx.CostMu.Lock()
	if resp.Model != "" {
		AccumulateUsage(sqctx.CostState, resp.Model, resp.Usage)
	}
	sqctx.CostMu.Unlock()

	text := extractTextFromResponse(resp)
	return &SideQueryResult{
		Text:  text,
		Model: resp.Model,
		Usage: resp.Usage,
		Mode:  SideQueryModeSummary,
	}, nil
}

const summaryToolResultMaxLen = 2000
const summaryToolInputMaxLen = 500

// renderConversationText converts a message history into a plain-text
// transcript suitable for sending as a single user message. Each message
// is labeled with its role; tool calls and results get structured labels.
// Media, thinking blocks, and signatures are stripped.
func renderConversationText(messages []api.Message) string {
	if len(messages) == 0 {
		return ""
	}

	var sb strings.Builder
	for i, msg := range messages {
		if i > 0 {
			sb.WriteByte('\n')
		}

		// Render tool results (user messages carrying tool responses)
		if len(msg.ToolResults) > 0 {
			for j, tr := range msg.ToolResults {
				if j > 0 {
					sb.WriteByte('\n')
				}
				if tr.IsError {
					sb.WriteString("[tool_result: error]\n")
				} else {
					sb.WriteString("[tool_result]\n")
				}
				content := tr.Content
				if len(content) > summaryToolResultMaxLen {
					half := summaryToolResultMaxLen / 2
					content = content[:half] + "\n... [truncated] ...\n" + content[len(content)-half:]
				}
				sb.WriteString(content)
				sb.WriteByte('\n')
			}
			// Also render any text content on the same message
			if msg.Content != "" {
				sb.WriteString("[user]\n")
				sb.WriteString(stripMediaMarkers(msg.Content))
				sb.WriteByte('\n')
			}
			continue
		}

		// Render text content
		switch msg.Role {
		case "user":
			sb.WriteString("[user]\n")
			sb.WriteString(stripMediaMarkers(msg.Content))
			sb.WriteByte('\n')
		case "assistant":
			if msg.Content != "" {
				sb.WriteString("[assistant]\n")
				sb.WriteString(msg.Content)
				sb.WriteByte('\n')
			}
			for _, tu := range msg.ToolUse {
				sb.WriteString("[tool_use: ")
				sb.WriteString(tu.Name)
				if len(tu.Input) > 0 {
					if inputJSON, err := json.Marshal(tu.Input); err == nil {
						s := string(inputJSON)
						if len(s) > summaryToolInputMaxLen {
							s = s[:summaryToolInputMaxLen] + "...truncated"
						}
						sb.WriteByte(' ')
						sb.WriteString(s)
					}
				}
				sb.WriteString("]\n")
			}
		default:
			sb.WriteString("[")
			sb.WriteString(msg.Role)
			sb.WriteString("]\n")
			sb.WriteString(msg.Content)
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

// estimateForkCost estimates the cost of fork mode. The entire message prefix
// is expected to be a cache hit (only the appended eval prompt is new).
func (sq *SideQuery) estimateForkCost(sqctx *SideQueryContext) float64 {
	totalInputTokens := estimateTokens(sq.Messages) + estimateStringTokens(sq.Prompt)

	// Fork mode: most of the prefix is cached. The eval prompt and a small
	// overhead are new (cache_creation), the rest is cache_read.
	newTokens := estimateStringTokens(sq.Prompt) + 200 // eval prompt + overhead
	cachedTokens := totalInputTokens - newTokens
	if cachedTokens < 0 {
		cachedTokens = 0
		newTokens = totalInputTokens
	}

	pricing := GetModelPricing(ResolveModel(sqctx.Model))
	return float64(cachedTokens)*pricing.CacheReadUSD +
		float64(newTokens)*pricing.CacheCreationUSD +
		float64(sq.MaxTokens)*pricing.OutputUSD
}

// estimateSummaryCost estimates the cost of summary mode. The estimate uses
// the raw message token count as a proxy for the rendered transcript size
// (thinking/signature are excluded but the text overhead of labels is minor).
func (sq *SideQuery) estimateSummaryCost(sqctx *SideQueryContext) float64 {
	msgs := sq.Messages
	if sq.SummaryMaxMessages > 0 && len(msgs) > sq.SummaryMaxMessages {
		msgs = msgs[len(msgs)-sq.SummaryMaxMessages:]
	}

	// Approximate: rendered transcript ≈ text content of messages (thinking/sig stripped)
	var textTokens int
	for _, m := range msgs {
		textTokens += estimateStringTokens(m.Content)
		textTokens += len(m.ToolUse) * 10 // label overhead per tool call
		for _, tr := range m.ToolResults {
			content := tr.Content
			if len(content) > summaryToolResultMaxLen {
				content = content[:summaryToolResultMaxLen]
			}
			textTokens += estimateStringTokens(content)
		}
	}
	inputTokens := textTokens + estimateStringTokens(sq.Prompt)

	model := sqctx.Model
	if sq.SummaryModel != "" {
		model = sq.SummaryModel
	}
	pricing := GetModelPricing(ResolveModel(model))

	return float64(inputTokens)*pricing.InputUSD +
		float64(sq.MaxTokens)*pricing.OutputUSD
}

func (sq *SideQuery) fallbackMode(primary SideQueryMode) *SideQueryMode {
	switch primary {
	case SideQueryModeFork:
		if sq.SummarySystemPrompt != nil {
			m := SideQueryModeSummary
			return &m
		}
	case SideQueryModeSummary:
		m := SideQueryModeFork
		return &m
	}
	return nil
}

func extractTextFromResponse(resp *api.Response) string {
	var sb strings.Builder
	for _, block := range resp.Content {
		if block.Type == "text" || block.Text != "" {
			sb.WriteString(block.Text)
		}
	}
	return sb.String()
}
