// Package api provides the OpenAI API client.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"time"

	"github.com/ipy/jenny/internal/log"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/shared"
)

// openAIProvider implements the Provider interface using the OpenAI Go SDK.
type openAIProvider struct {
	client openai.Client
	model string
	maxTokens  int
	retryConfig RetryConfig
}

// newOpenAIProvider creates a new OpenAI provider.
func newOpenAIProvider(model string) (*openAIProvider, error) {
	baseURL := os.Getenv("OPENAI_BASE_URL")
	if baseURL == "" {
		return nil, errors.New("OPENAI_BASE_URL is required for OpenAI provider")
	}

	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return nil, errors.New("OPENAI_API_KEY is required for OpenAI provider")
	}

	if model == "" {
		model = os.Getenv("OPENAI_DEFAULT_MODEL")
	}
	if model == "" {
		return nil, errors.New("OPENAI_DEFAULT_MODEL is required when using OpenAI provider")
	}

	wireAPI := os.Getenv("OPENAI_WIRE_API")
	if wireAPI == "" {
		wireAPI = "chat"
	}
	if wireAPI == "responses" {
		return nil, errors.New("OpenAI Responses API not yet supported; use OPENAI_WIRE_API=chat or unset")
	}

	opts := []option.RequestOption{
		option.WithBaseURL(baseURL),
		option.WithAPIKey(apiKey),
		// Disable SDK-level retry — we handle retries via sendWithRetry like Anthropic
		option.WithMaxRetries(0),
	}

	if timeout := ResolveTimeout(os.Getenv("API_TIMEOUT_MS")); timeout > 0 {
		opts = append(opts, option.WithRequestTimeout(timeout))
	}

	client := openai.NewClient(opts...)

	return &openAIProvider{
		client:      client,
		model:       model,
		maxTokens:   64000,
		retryConfig: DefaultRetryConfig(),
	}, nil
}

// Kind returns the provider kind.
func (p *openAIProvider) Kind() ProviderKind {
	return ProviderOpenAI
}

// SetModel sets the model.
func (p *openAIProvider) SetModel(model string) {
	p.model = model
}

// GetModel returns the model.
func (p *openAIProvider) GetModel() string {
	return p.model
}

// SetMaxTokensOverride sets the max_tokens override.
func (p *openAIProvider) SetMaxTokensOverride(maxTokens int) {
	p.maxTokens = maxTokens
}

// SetRetryConfig sets the retry configuration.
func (p *openAIProvider) SetRetryConfig(cfg RetryConfig) {
	p.retryConfig = cfg
}

// SendMessage sends a non-streaming message.
func (p *openAIProvider) SendMessage(ctx context.Context, messages []Message, tools []ToolParam, toolResults []ToolResult, systemPrompt string) (*Response, error) {
	return p.sendWithRetry(ctx, func(ctx context.Context) (*Response, error) {
		return p.doSendMessage(ctx, messages, tools, toolResults, systemPrompt)
	}, false)
}

// sendWithRetry executes a function with retry logic.
func (p *openAIProvider) sendWithRetry(ctx context.Context, fn func(context.Context) (*Response, error), isBackground bool) (*Response, error) {
	cfg := p.retryConfig
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 10
	}

	var lastErr error
	consecutive529 := 0

	for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
		resp, err := fn(ctx)

		if err != nil {
			var apiErr *openai.Error
			if errors.As(err, &apiErr); apiErr != nil {
				statusCode := apiErr.StatusCode

				if isBackground && statusCode == StatusProxyError {
					return nil, &CannotRetryError{
						Message:    "Background request rejected with 529 Overloaded",
						StatusCode: statusCode,
					}
				}

				if statusCode == StatusProxyError {
					consecutive529++
					if consecutive529 > cfg.Max529Retries {
						return nil, &CannotRetryError{
							Message:    "Repeated 529 Overloaded errors",
							StatusCode: statusCode,
						}
					}
				} else {
					consecutive529 = 0
				}

				isPermanent := apiErr.StatusCode >= 400 && apiErr.StatusCode < 500 &&
					apiErr.StatusCode != 429 && apiErr.StatusCode != 408 && apiErr.StatusCode != 409
				retryableErr := &RetryableHTTPError{
					StatusCode:  apiErr.StatusCode,
					Message:     err.Error(),
					IsPermanent: isPermanent,
				}

				if retryableErr.IsPermanent || !isRetryable(statusCode, nil) {
					return nil, retryableErr
				}

				lastErr = retryableErr
			} else {
				if !isRetryable(0, err) {
					return nil, err
				}
				lastErr = err
			}
		} else {
			return resp, nil
		}

		if attempt < cfg.MaxRetries {
			delay := computeBackoff(attempt, cfg, nil)

			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, errors.New("max retries exhausted")
}

// doSendMessage performs the actual message sending.
func (p *openAIProvider) doSendMessage(ctx context.Context, messages []Message, tools []ToolParam, toolResults []ToolResult, systemPrompt string) (*Response, error) {
	log.Debug("OpenAI provider sending message", "model", p.model)

	messages, tools, _ = NormalizeMessages(messages, tools, Capabilities{SupportsPromptCaching: false})

	sdkMessages := p.buildMessages(messages, toolResults, systemPrompt)

	var sdkTools []openai.ChatCompletionToolUnionParam
	if len(tools) > 0 {
		sdkTools = p.buildTools(tools)
	}

	maxTokens := p.maxTokens
	if maxTokens == 0 {
		maxTokens = 64000
	}

	params := openai.ChatCompletionNewParams{
		Model:               shared.ChatModel(p.model),
		Messages:            sdkMessages,
		MaxCompletionTokens: param.NewOpt(int64(maxTokens)),
	}

	if len(sdkTools) > 0 {
		params.Tools = sdkTools
	}

	resp, err := p.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, err
	}

	return p.parseResponse(resp)
}

// buildMessages converts api.Message slices to OpenAI SDK message format.
func (p *openAIProvider) buildMessages(messages []Message, toolResults []ToolResult, systemPrompt string) []openai.ChatCompletionMessageParamUnion {
	var sdkMessages []openai.ChatCompletionMessageParamUnion

	if systemPrompt != "" {
		sdkMessages = append(sdkMessages, openai.SystemMessage(systemPrompt))
	}

	for _, msg := range messages {
		switch msg.Role {
		case "user":
			if msg.Content != "" {
				sdkMessages = append(sdkMessages, openai.UserMessage(msg.Content))
			}

		case "assistant":
			if len(msg.ToolUse) > 0 {
				var msg2 openai.ChatCompletionAssistantMessageParam
				msg2.Role = "assistant"
				msg2.ToolCalls = make([]openai.ChatCompletionMessageToolCallUnionParam, 0, len(msg.ToolUse))
				for _, tu := range msg.ToolUse {
					inputJSON, _ := json.Marshal(tu.Input)
					msg2.ToolCalls = append(msg2.ToolCalls, openai.ChatCompletionMessageToolCallUnionParam{
						OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
							ID: tu.ID,
							Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{
								Name:      tu.Name,
								Arguments: string(inputJSON),
							},
						},
					})
				}
				sdkMessages = append(sdkMessages, openai.ChatCompletionMessageParamUnion{
					OfAssistant: &msg2,
				})
			} else if msg.Content != "" {
				sdkMessages = append(sdkMessages, openai.AssistantMessage(msg.Content))
			}
		}
	}

	for _, tr := range toolResults {
		sdkMessages = append(sdkMessages, openai.ToolMessage(tr.Content, tr.ToolUseID))
	}

	for _, msg := range messages {
		for _, tr := range msg.ToolResults {
			sdkMessages = append(sdkMessages, openai.ToolMessage(tr.Content, tr.ToolUseID))
		}
	}

	return sdkMessages
}

// buildTools converts api.ToolParam slices to OpenAI SDK tools format.
func (p *openAIProvider) buildTools(tools []ToolParam) []openai.ChatCompletionToolUnionParam {
	sdkTools := make([]openai.ChatCompletionToolUnionParam, 0, len(tools))
	for _, t := range tools {
		schema := p.buildInputSchema(t.InputSchema)
		sdkTools = append(sdkTools, openai.ChatCompletionToolUnionParam{
			OfFunction: &openai.ChatCompletionFunctionToolParam{
				Function: openai.FunctionDefinitionParam{
					Name:        t.Name,
					Description: param.NewOpt(t.Description),
					Parameters:  schema,
				},
			},
		})
	}
	return sdkTools
}

// buildInputSchema converts ToolInputSchema to OpenAI SDK parameters format.
func (p *openAIProvider) buildInputSchema(schema ToolInputSchema) map[string]any {
	result := map[string]any{
		"type": "object",
	}

	if len(schema.Properties) > 0 {
		result["properties"] = schema.Properties
	}

	if len(schema.Required) > 0 {
		result["required"] = schema.Required
	}

	maps.Copy(result, schema.ExtraFields)

	return result
}

// parseResponse converts an OpenAI SDK response to api.Response.
func (p *openAIProvider) parseResponse(resp *openai.ChatCompletion) (*Response, error) {
	response := &Response{
		Model: resp.Model,
	}

	if len(resp.Choices) == 0 {
		return response, nil
	}

	choice := resp.Choices[0]

	switch choice.FinishReason {
	case "stop":
		response.StopReason = StopReasonEndTurn
	case "tool_calls":
		response.StopReason = StopReasonToolUse
	case "length":
		response.StopReason = StopReasonMaxTokens
	default:
		response.StopReason = StopReason(choice.FinishReason)
	}

	// Extract reasoning_content from raw JSON (not in SDK types)
	if reasoning := extractReasoningFromJSON(choice.Message.RawJSON()); reasoning != "" {
		response.Content = append(response.Content, ContentBlock{
			Type:     "thinking",
			Thinking: reasoning,
		})
	}

	if choice.Message.Content != "" && choice.Message.Content != "null" {
		response.Content = append(response.Content, ContentBlock{
			Type: "text",
			Text: choice.Message.Content,
		})
	}

	for _, tc := range choice.Message.ToolCalls {
		fn := tc.AsFunction()
		var input map[string]any
		if fn.Function.Arguments != "" {
			json.Unmarshal([]byte(fn.Function.Arguments), &input)
		}
		response.Content = append(response.Content, ContentBlock{
			Type:      "tool_use",
			ToolID:    tc.ID,
			ToolName:  fn.Function.Name,
			ToolInput: input,
		})
	}

	if resp.Usage.PromptTokens > 0 {
		response.Usage.InputTokens = int(resp.Usage.PromptTokens)
	}
	if resp.Usage.CompletionTokens > 0 {
		response.Usage.OutputTokens = int(resp.Usage.CompletionTokens)
	}
	if resp.Usage.PromptTokensDetails.CachedTokens > 0 {
		response.Usage.CacheReadInputTokens = int(resp.Usage.PromptTokensDetails.CachedTokens)
	}

	return response, nil
}

// SendMessageStream sends a streaming message.
func (p *openAIProvider) SendMessageStream(ctx context.Context, messages []Message, tools []ToolParam, toolResults []ToolResult, systemPrompt string, idleTimeout time.Duration) (<-chan StreamContentBlock, *StreamResult) {
	blocksChan := make(chan StreamContentBlock, 10)
	result := &StreamResult{}

	go func() {
		defer close(blocksChan)

		log.Debug("OpenAI provider streaming message", "model", p.model)

		messages, tools, _ = NormalizeMessages(messages, tools, Capabilities{SupportsPromptCaching: false})

		sdkMessages := p.buildMessages(messages, toolResults, systemPrompt)

		var sdkTools []openai.ChatCompletionToolUnionParam
		if len(tools) > 0 {
			sdkTools = p.buildTools(tools)
		}

		maxTokens := p.maxTokens
		if maxTokens == 0 {
			maxTokens = 64000
		}

		params := openai.ChatCompletionNewParams{
			Model:               shared.ChatModel(p.model),
			Messages:            sdkMessages,
			MaxCompletionTokens: param.NewOpt(int64(maxTokens)),
			StreamOptions: openai.ChatCompletionStreamOptionsParam{
				IncludeUsage: param.NewOpt(true),
			},
		}

		if len(sdkTools) > 0 {
			params.Tools = sdkTools
		}

		if idleTimeout <= 0 {
			idleTimeout = DefaultIdleTimeout
		}

		streamCtx, cancel := context.WithCancel(ctx)
		defer cancel()

		stream := p.client.Chat.Completions.NewStreaming(streamCtx, params)

		if stream.Err() != nil {
			result.Error = stream.Err().Error()
			return
		}

		acc := newOpenAIStreamAccumulator()
		hasStopReason := false

		idleTimer := time.NewTimer(idleTimeout)
		defer idleTimer.Stop()

		watchdogCtx, cancelWatchdog := context.WithCancel(ctx)
		defer cancelWatchdog()

		go func() {
			select {
			case <-idleTimer.C:
				log.Warn("OpenAI: Idle timeout reached")
				result.Error = "idle timeout"
				cancel()
			case <-watchdogCtx.Done():
			}
		}()

		for {
			streamReady := stream.Next()

			if !streamReady {
				break
			}

			chunk := stream.Current()

			if !idleTimer.Stop() {
				select {
				case <-idleTimer.C:
				default:
				}
			}
			idleTimer.Reset(idleTimeout)

			hasStopReason = p.processStreamChunk(chunk, acc, blocksChan, result) || hasStopReason
		}

		if stream.Err() != nil && result.Error == "" {
			result.Error = stream.Err().Error()
		}

		if !hasStopReason && result.Error == "" {
			result.Error = "stream incomplete: no stop reason"
		}

		result.StreamComplete = hasStopReason
		result.Blocks = acc.finalize()
		result.StopReason = acc.stopReason
		result.Model = p.model
	}()

	return blocksChan, result
}

// processStreamChunk processes a single OpenAI stream chunk.
// Returns true if a stop reason was set.
func (p *openAIProvider) processStreamChunk(chunk openai.ChatCompletionChunk, acc *openAIStreamAccumulator, blocksChan chan<- StreamContentBlock, result *StreamResult) bool {
	if chunk.Model != "" {
		result.Model = chunk.Model
	}

	if len(chunk.Choices) == 0 {
		return false
	}

	choice := chunk.Choices[0]
	hasStopReason := choice.FinishReason != ""

	switch choice.FinishReason {
	case "stop":
		acc.setStopReason(StopReasonEndTurn)
	case "tool_calls":
		acc.setStopReason(StopReasonToolUse)
	case "length":
		acc.setStopReason(StopReasonMaxTokens)
	}

	delta := choice.Delta

	if delta.Content != "" && delta.Content != "null" {
		acc.appendContent(delta.Content)
		blocksChan <- StreamContentBlock{
			Block: ContentBlock{
				Type: "text",
				Text: acc.getContent(),
			},
		}
	}

	// Extract reasoning_content delta from raw JSON (not in SDK Delta type)
	reasoningDelta := extractReasoningFromJSON(choice.Delta.RawJSON())
	if reasoningDelta != "" {
		acc.appendThinking(reasoningDelta)
		blocksChan <- StreamContentBlock{
			Block: ContentBlock{
				Type:     "thinking",
				Thinking: acc.getThinking(),
			},
		}
	}

	for _, tc := range delta.ToolCalls {
		acc.appendToolCall(int(tc.Index), tc.ID, tc.Function.Name, tc.Function.Arguments)
		if toolBlock := acc.getToolUseBlock(int(tc.Index)); toolBlock != nil {
			blocksChan <- StreamContentBlock{
				Block: *toolBlock,
			}
		}
	}

	// Usage is only non-zero on the last chunk when include_usage is set
	if chunk.Usage.PromptTokens > 0 {
		result.Usage.InputTokens = int(chunk.Usage.PromptTokens)
	}
	if chunk.Usage.CompletionTokens > 0 {
		result.Usage.OutputTokens = int(chunk.Usage.CompletionTokens)
	}
	if chunk.Usage.PromptTokensDetails.CachedTokens > 0 {
		result.Usage.CacheReadInputTokens = int(chunk.Usage.PromptTokensDetails.CachedTokens)
	}

	return hasStopReason
}

// openAIStreamAccumulator accumulates streaming chunks.
type openAIStreamAccumulator struct {
	content    string
	thinking   string
	stopReason StopReason
	toolCalls  map[int]*toolCallAccumulator
}

// toolCallAccumulator accumulates tool call arguments.
type toolCallAccumulator struct {
	ID string
	Name string
	Args string
	Input map[string]any
}

func newOpenAIStreamAccumulator() *openAIStreamAccumulator {
	return &openAIStreamAccumulator{
		toolCalls: make(map[int]*toolCallAccumulator),
	}
}

func (acc *openAIStreamAccumulator) appendContent(text string) {
	acc.content += text
}

func (acc *openAIStreamAccumulator) getContent() string {
	return acc.content
}

func (acc *openAIStreamAccumulator) appendThinking(text string) {
	acc.thinking += text
}

func (acc *openAIStreamAccumulator) getThinking() string {
	return acc.thinking
}

func (acc *openAIStreamAccumulator) setStopReason(reason StopReason) {
	if acc.stopReason == "" {
		acc.stopReason = reason
	}
}

func (acc *openAIStreamAccumulator) appendToolCall(index int, id, name, args string) {
	tc, exists := acc.toolCalls[index]
	if !exists {
		tc = &toolCallAccumulator{}
		acc.toolCalls[index] = tc
	}

	if id != "" {
		tc.ID = id
	}
	if name != "" {
		tc.Name = name
	}
	if args != "" {
		tc.Args += args
		if tc.Input == nil {
			var input map[string]any
			if err := json.Unmarshal([]byte(tc.Args), &input); err == nil {
				tc.Input = input
			}
		}
	}
}

func (acc *openAIStreamAccumulator) getToolUseBlock(index int) *ContentBlock {
	if tc, exists := acc.toolCalls[index]; exists && tc.ID != "" {
		return &ContentBlock{
			Type:      "tool_use",
			ToolID:    tc.ID,
			ToolName:  tc.Name,
			ToolInput: tc.Input,
		}
	}
	return nil
}

func (acc *openAIStreamAccumulator) finalize() []ContentBlock {
	var blocks []ContentBlock

	if acc.thinking != "" {
		blocks = append(blocks, ContentBlock{
			Type:     "thinking",
			Thinking: acc.thinking,
		})
	}

	if acc.content != "" {
		blocks = append(blocks, ContentBlock{
			Type: "text",
			Text: acc.content,
		})
	}

	for i := 0; i < len(acc.toolCalls); i++ {
		if tc, exists := acc.toolCalls[i]; exists && tc.ID != "" {
			blocks = append(blocks, ContentBlock{
				Type:      "tool_use",
				ToolID:    tc.ID,
				ToolName:  tc.Name,
				ToolInput: tc.Input,
			})
		}
	}

	return blocks
}

// extractReasoningFromJSON extracts reasoning_content from raw JSON.
// The OpenAI SDK does not model reasoning_content on ChatCompletionMessage.
func extractReasoningFromJSON(rawJSON string) string {
	if rawJSON == "" {
		return ""
	}
	var msg struct {
		ReasoningContent string `json:"reasoning_content"`
	}
	if err := json.Unmarshal([]byte(rawJSON), &msg); err != nil {
		return ""
	}
	return msg.ReasoningContent
}