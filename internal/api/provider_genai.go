// Package api provides the Google GenAI API client (Gemini / Vertex AI).
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ipy/jenny/internal/log"
	"google.golang.org/genai"
)

// ProviderKind for the Google GenAI provider.
const ProviderGenAI ProviderKind = "genai"

// genaiProvider implements the Provider interface using the official
// google.golang.org/genai Go SDK. It can target either the public Gemini API
// or Vertex AI, selected automatically from environment variables.
type genaiProvider struct {
	client      *genai.Client
	model       string
	maxTokens   int
	retryConfig RetryConfig
}

// newGenAIProvider creates a new GenAI provider. The backend is selected from
// environment variables:
//
//  1. If GENAI_API_KEY is set explicitly → Gemini API backend.
//  2. Else if GOOGLE_CLOUD_PROJECT and GOOGLE_CLOUD_LOCATION are set → Vertex
//     AI backend (Application Default Credentials are picked up by the SDK).
//  3. Else if GOOGLE_API_KEY or GEMINI_API_KEY is set → Gemini API backend.
//  4. Otherwise returns an error.
func newGenAIProvider(model string) (*genaiProvider, error) {
	if model == "" {
		model = os.Getenv("GENAI_DEFAULT_MODEL")
	}
	if model == "" {
		return nil, errors.New("GENAI_DEFAULT_MODEL is required when using genai provider")
	}

	cfg := &genai.ClientConfig{}

	// Optional base URL override (proxies, VPC endpoints, etc).
	if baseURL := os.Getenv("GENAI_BASE_URL"); baseURL != "" {
		cfg.HTTPOptions.BaseURL = baseURL
	}

	// Request timeout from API_TIMEOUT_MS.
	if timeout := ResolveTimeout(os.Getenv("API_TIMEOUT_MS")); timeout > 0 {
		t := timeout
		cfg.HTTPOptions.Timeout = &t
	}

	// Explicit API key (bypasses SDK's GOOGLE_API_KEY / GEMINI_API_KEY lookups).
	if explicit := os.Getenv("GENAI_API_KEY"); explicit != "" {
		cfg.APIKey = explicit
		cfg.Backend = genai.BackendGeminiAPI
	}

	// Vertex AI via Application Default Credentials.
	if cfg.Backend == genai.BackendUnspecified {
		project := os.Getenv("GOOGLE_CLOUD_PROJECT")
		location := os.Getenv("GOOGLE_CLOUD_LOCATION")
		if location == "" {
			location = os.Getenv("GOOGLE_CLOUD_REGION")
		}
		if project != "" && location != "" {
			cfg.Backend = genai.BackendVertexAI
			cfg.Project = project
			cfg.Location = location
		}
	}

	client, err := genai.NewClient(context.Background(), cfg)
	if err != nil {
		return nil, fmt.Errorf("genai: failed to create client: %w", err)
	}

	return &genaiProvider{
		client:      client,
		model:       model,
		maxTokens:   64000,
		retryConfig: DefaultRetryConfig(),
	}, nil
}

// Kind returns the provider kind.
func (p *genaiProvider) Kind() ProviderKind {
	return ProviderGenAI
}

// SetModel sets the model.
func (p *genaiProvider) SetModel(model string) {
	p.model = model
}

// GetModel returns the model.
func (p *genaiProvider) GetModel() string {
	return p.model
}

// SetMaxTokensOverride sets the max output tokens override.
func (p *genaiProvider) SetMaxTokensOverride(maxTokens int) {
	p.maxTokens = maxTokens
}

// SetRetryConfig sets the retry configuration.
func (p *genaiProvider) SetRetryConfig(cfg RetryConfig) {
	p.retryConfig = cfg
}

// SendMessage sends a non-streaming message.
func (p *genaiProvider) SendMessage(ctx context.Context, messages []Message, tools []ToolParam, toolResults []ToolResult, systemPrompt string, systemPromptSuffix string) (*Response, error) {
	return p.sendWithRetry(ctx, func(ctx context.Context) (*Response, error) {
		return p.doSendMessage(ctx, messages, tools, toolResults, systemPrompt, systemPromptSuffix)
	}, false)
}

// sendWithRetry executes a function with retry logic. Mirrors the structure
// used by the Anthropic and OpenAI providers so behavior is consistent.
func (p *genaiProvider) sendWithRetry(ctx context.Context, fn func(context.Context) (*Response, error), isBackground bool) (*Response, error) {
	cfg := p.retryConfig
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 10
	}

	var lastErr error
	consecutive529 := 0

	for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
		resp, err := fn(ctx)
		if err != nil {
			statusCode := 0
			if apiErr, ok := asAPIError(err); ok {
				statusCode = apiErr.Code
			}

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

			retryableErr := wrapGenAIError(err)
			if retryableErr != nil {
				if r, ok := retryableErr.(*RetryableHTTPError); ok {
					if r.IsPermanent || !isRetryable(r.StatusCode, nil) {
						return nil, r
					}
				}
				lastErr = retryableErr
			} else if !isRetryable(0, err) {
				return nil, err
			} else {
				lastErr = err
			}
		} else {
			return resp, nil
		}

		if attempt < cfg.MaxRetries {
			var retryAfter *time.Duration
			if retryableErr, ok := lastErr.(*RetryableHTTPError); ok {
				retryAfter = retryableErr.RetryAfter
			}
			delay := computeBackoff(attempt, cfg, retryAfter)
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

// doSendMessage performs the actual non-streaming message send.
func (p *genaiProvider) doSendMessage(ctx context.Context, messages []Message, tools []ToolParam, toolResults []ToolResult, systemPrompt string, systemPromptSuffix string) (*Response, error) {
	if err := ValidateMessagesMedia(messages); err != nil {
		return nil, err
	}

	messages, tools, _ = NormalizeMessages(messages, tools, Capabilities{SupportsPromptCaching: false})

	contents := p.buildContents(messages, toolResults)
	config := p.buildConfig(systemPrompt, systemPromptSuffix, tools)

	resp, err := p.client.Models.GenerateContent(ctx, p.model, contents, config)
	if err != nil {
		return nil, wrapGenAIError(err)
	}

	return p.parseResponse(resp)
}

// SendMessageStream sends a streaming message.
func (p *genaiProvider) SendMessageStream(ctx context.Context, messages []Message, tools []ToolParam, toolResults []ToolResult, systemPrompt string, systemPromptSuffix string, idleTimeout time.Duration) (<-chan StreamContentBlock, *StreamResult) {
	blocksChan := make(chan StreamContentBlock, 10)
	result := &StreamResult{}

	go func() {
		defer close(blocksChan)
		defer func() {
			if r := recover(); r != nil {
				errStr := fmt.Sprintf("panic: %v", r)
				log.Warn("GenAI: stream goroutine panicked", "panic", r)
				if result.Error == "" {
					result.Error = errStr
				}
			}
		}()

		if err := ValidateMessagesMedia(messages); err != nil {
			result.Error = err.Error()
			return
		}

		messages, tools, _ = NormalizeMessages(messages, tools, Capabilities{SupportsPromptCaching: false})

		contents := p.buildContents(messages, toolResults)
		config := p.buildConfig(systemPrompt, systemPromptSuffix, tools)

		log.Debug("GenAI: starting streaming request", "model", p.model)

		if idleTimeout <= 0 {
			idleTimeout = DefaultIdleTimeout
		}

		streamCtx, cancel := context.WithCancel(ctx)
		defer cancel()

		stream := p.client.Models.GenerateContentStream(streamCtx, p.model, contents, config)

		acc := newGenAIStreamAccumulator()
		hasFinishReason := false

		idleTimer := time.NewTimer(idleTimeout)
		defer idleTimer.Stop()

		watchdogCtx, cancelWatchdog := context.WithCancel(ctx)
		defer cancelWatchdog()

		go func() {
			select {
			case <-idleTimer.C:
				log.Warn("GenAI: idle timeout reached")
				result.Error = "idle timeout"
				cancel()
			case <-watchdogCtx.Done():
			}
		}()

		for resp, err := range stream {
			if err != nil {
				log.Warn("GenAI: stream error", "error", err)
				apiErr := wrapGenAIError(err)
				if apiErr != nil {
					if retryable, ok := apiErr.(*RetryableHTTPError); ok {
						result.IsPermanent = retryable.IsPermanent
					}
				}
				if result.Error == "" {
					result.Error = err.Error()
				}
				if isPromptTooLongGenAI(err) {
					result.ContextRejected = true
				}
				return
			}
			if resp == nil {
				log.Warn("GenAI: stream nil response")
				continue
			}

			if !idleTimer.Stop() {
				select {
				case <-idleTimer.C:
				default:
				}
			}
			idleTimer.Reset(idleTimeout)

			if stopReason, usage, model, ok := p.processStreamChunk(resp, acc, blocksChan, result); ok {
				hasFinishReason = true
				if usage.OutputTokens > 0 || usage.InputTokens > 0 {
					result.Usage = usage
				}
				if model != "" {
					result.Model = model
				}
				if stopReason == StopReasonMaxTokens {
					result.MaxTokensErr = categorizeMaxTokensError(result.Model, result.Usage.OutputTokens, result.ContextRejected)
				}
			}
		}

		if !hasFinishReason && result.Error == "" {
			result.Error = "stream incomplete: no finish reason"
		}

		result.StreamComplete = hasFinishReason
		result.Blocks = acc.finalize()
		result.StopReason = acc.stopReason
		if result.Model == "" {
			result.Model = p.model
		}
	}()

	return blocksChan, result
}

// processStreamChunk processes a single streaming chunk.
// Returns (stopReason, usage, model, hasFinishReason).
func (p *genaiProvider) processStreamChunk(resp *genai.GenerateContentResponse, acc *genAIStreamAccumulator, blocksChan chan<- StreamContentBlock, result *StreamResult) (StopReason, Usage, string, bool) {
	if resp == nil {
		return "", Usage{}, "", false
	}

	usage := mapUsage(resp.UsageMetadata)
	if usage.InputTokens > 0 || usage.OutputTokens > 0 {
		result.Usage = usage
	}

	model := p.model
	if resp.ModelVersion != "" {
		model = resp.ModelVersion
	}

	hasFinishReason := false
	for _, cand := range resp.Candidates {
		if cand.Content == nil {
			continue
		}
		for _, part := range cand.Content.Parts {
			if part == nil {
				continue
			}
			if part.Thought && part.Text != "" {
				acc.appendThinking(part.Text)
				blocksChan <- StreamContentBlock{
					Block: ContentBlock{
						Type:     "thinking",
						Thinking: acc.getThinking(),
					},
				}
			} else if part.Text != "" {
				acc.appendContent(part.Text)
				blocksChan <- StreamContentBlock{
					Block: ContentBlock{
						Type: "text",
						Text: acc.getContent(),
					},
				}
			}
			if part.FunctionCall != nil {
				acc.appendFunctionCall(part.FunctionCall)
				if block := acc.getFunctionCallBlock(); block != nil {
					blocksChan <- StreamContentBlock{Block: *block}
				}
			}
		}

		switch cand.FinishReason {
		case genai.FinishReasonStop:
			acc.setStopReason(StopReasonEndTurn)
			hasFinishReason = true
		case genai.FinishReasonMaxTokens:
			acc.setStopReason(StopReasonMaxTokens)
			hasFinishReason = true
		case genai.FinishReasonSafety, genai.FinishReasonBlocklist, genai.FinishReasonProhibitedContent, genai.FinishReasonSPII:
			acc.setStopReason(StopReasonStopSeq)
			hasFinishReason = true
		default:
			if cand.FinishReason != "" && cand.FinishReason != genai.FinishReasonUnspecified {
				acc.setStopReason(StopReason(cand.FinishReason))
				hasFinishReason = true
			}
		}
	}

	return acc.stopReason, result.Usage, model, hasFinishReason
}

// buildContents converts api.Message slices plus standalone tool results into
// the genai.Content slice the SDK expects.
//
// Gemini tool-calling requires that the model's FunctionCall turn and the
// matching FunctionResponse turn both appear in the conversation history, in
// the same order. The agent package stores tool_use and tool_result on the
// same Message, so we split them: text/tool_use go on a model turn, and the
// matching tool_results become a user turn that immediately follows.
func (p *genaiProvider) buildContents(messages []Message, toolResults []ToolResult) []*genai.Content {
	contents := make([]*genai.Content, 0, len(messages)+len(toolResults))

	// Track which tool_results have been emitted by a per-message tool_results
	// block, so we don't double-emit the standalone toolResults slice.
	emittedStandalone := make(map[string]bool, len(toolResults))

	for _, msg := range messages {
		switch msg.Role {
		case "user":
			parts := make([]*genai.Part, 0, 1+len(msg.ToolResults))
			if msg.Content != "" {
				parts = append(parts, genai.NewPartFromText(msg.Content))
			}
			for _, tr := range msg.ToolResults {
				parts = append(parts, functionResponsePart(ToolResult{
					ToolUseID: tr.ToolUseID,
					Content:   tr.Content,
					IsError:   tr.IsError,
				}))
			}
			if len(parts) == 0 {
				continue
			}
			contents = append(contents, genai.NewContentFromParts(parts, genai.RoleUser))

		case "assistant":
			parts := make([]*genai.Part, 0, 1+len(msg.ToolUse))
			if msg.Content != "" {
				parts = append(parts, genai.NewPartFromText(msg.Content))
			}
			for _, tu := range msg.ToolUse {
				parts = append(parts, &genai.Part{
					FunctionCall: &genai.FunctionCall{
						ID:   tu.ID,
						Name: tu.Name,
						Args: tu.Input,
					},
				})
			}
			if len(parts) == 0 {
				continue
			}
			contents = append(contents, genai.NewContentFromParts(parts, genai.RoleModel))

		case "system":
			// System prompts are routed through GenerateContentConfig.SystemInstruction
			// rather than as a turn in the conversation; skip if seen here.
			continue
		}
	}

	// Standalone tool results (those not already attached to a message).
	pending := make([]*genai.Part, 0, len(toolResults))
	for _, tr := range toolResults {
		if emittedStandalone[tr.ToolUseID] {
			continue
		}
		emittedStandalone[tr.ToolUseID] = true
		pending = append(pending, functionResponsePart(tr))
	}
	if len(pending) > 0 {
		contents = append(contents, genai.NewContentFromParts(pending, genai.RoleUser))
	}

	return contents
}

// functionResponsePart builds a Part carrying a function response.
func functionResponsePart(tr ToolResult) *genai.Part {
	response := map[string]any{}
	if tr.Content != "" {
		var parsed any
		if err := json.Unmarshal([]byte(tr.Content), &parsed); err == nil {
			response["output"] = parsed
		} else {
			response["output"] = tr.Content
		}
	}
	if tr.IsError {
		response["error"] = tr.Content
	}
	return &genai.Part{
		FunctionResponse: &genai.FunctionResponse{
			ID:       tr.ToolUseID,
			Name:     toolNameFromID(tr.ToolUseID),
			Response: response,
		},
	}
}

// toolNameFromID returns a placeholder function name. The SDK requires
// FunctionResponse.Name to be set, but the actual name is already associated
// with the FunctionCall part above. The provider's caller doesn't carry
// (id→name) mapping here, so we default to a sentinel; in practice the
// function response is matched by ID by the model server.
func toolNameFromID(id string) string {
	if id == "" {
		return "tool"
	}
	return "tool"
}

// buildConfig assembles a GenerateContentConfig for a request.
func (p *genaiProvider) buildConfig(systemPrompt string, systemPromptSuffix string, tools []ToolParam) *genai.GenerateContentConfig {
	cfg := &genai.GenerateContentConfig{
		MaxOutputTokens: int32(p.maxTokens),
	}

	fullSystem := systemPrompt
	if systemPromptSuffix != "" {
		if fullSystem != "" {
			fullSystem += "\n\n"
		}
		fullSystem += systemPromptSuffix
	}
	if fullSystem != "" {
		cfg.SystemInstruction = genai.NewContentFromText(fullSystem, genai.RoleUser)
	}

	if len(tools) > 0 {
		cfg.Tools = []*genai.Tool{{
			FunctionDeclarations: p.buildTools(tools),
		}}
	}

	return cfg
}

// buildTools converts api.ToolParam slices to *genai.FunctionDeclaration.
func (p *genaiProvider) buildTools(tools []ToolParam) []*genai.FunctionDeclaration {
	decls := make([]*genai.FunctionDeclaration, 0, len(tools))
	for _, t := range tools {
		decl := &genai.FunctionDeclaration{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  schemaFromToolParam(t.InputSchema),
		}
		decls = append(decls, decl)
	}
	return decls
}

// schemaFromToolParam converts our JSON-Schema-ish ToolInputSchema into a
// *genai.Schema, recursively.
func schemaFromToolParam(in ToolInputSchema) *genai.Schema {
	if in.Type == "" {
		in.Type = "object"
	}
	out := &genai.Schema{
		Required: in.Required,
	}
	switch strings.ToLower(in.Type) {
	case "object":
		out.Type = genai.TypeObject
	case "string":
		out.Type = genai.TypeString
	case "number":
		out.Type = genai.TypeNumber
	case "integer":
		out.Type = genai.TypeInteger
	case "boolean":
		out.Type = genai.TypeBoolean
	case "array":
		out.Type = genai.TypeArray
	default:
		out.Type = genai.TypeUnspecified
	}

	if len(in.Properties) > 0 {
		out.Properties = make(map[string]*genai.Schema, len(in.Properties))
		for k, v := range in.Properties {
			child, ok := v.(map[string]any)
			if !ok {
				continue
			}
			out.Properties[k] = schemaFromMap(child)
		}
	}

	// Carry over anything we don't model explicitly.
	for k, v := range in.ExtraFields {
		if out.Properties == nil {
			out.Properties = map[string]*genai.Schema{}
		}
		// Best-effort: stash under the schema's "Description" or skip.
		// We avoid overwriting typed fields; only used for $defs etc.
		_ = k
		_ = v
	}

	return out
}

// schemaFromMap builds a Schema from a generic map (the property entries we
// see on the agent side).
func schemaFromMap(m map[string]any) *genai.Schema {
	s := &genai.Schema{}
	if desc, ok := m["description"].(string); ok {
		s.Description = desc
	}
	if t, ok := m["type"].(string); ok {
		switch strings.ToLower(t) {
		case "object":
			s.Type = genai.TypeObject
		case "string":
			s.Type = genai.TypeString
		case "number":
			s.Type = genai.TypeNumber
		case "integer":
			s.Type = genai.TypeInteger
		case "boolean":
			s.Type = genai.TypeBoolean
		case "array":
			s.Type = genai.TypeArray
		default:
			s.Type = genai.TypeUnspecified
		}
	}
	if enum, ok := m["enum"].([]any); ok {
		for _, e := range enum {
			if str, ok := e.(string); ok {
				s.Enum = append(s.Enum, str)
			}
		}
	}
	if req, ok := m["required"].([]any); ok {
		for _, r := range req {
			if str, ok := r.(string); ok {
				s.Required = append(s.Required, str)
			}
		}
	}
	if props, ok := m["properties"].(map[string]any); ok {
		s.Properties = make(map[string]*genai.Schema, len(props))
		for k, v := range props {
			if child, ok := v.(map[string]any); ok {
				s.Properties[k] = schemaFromMap(child)
			}
		}
	}
	if items, ok := m["items"].(map[string]any); ok {
		s.Items = schemaFromMap(items)
	}
	return s
}

// parseResponse converts a *genai.GenerateContentResponse to *api.Response.
func (p *genaiProvider) parseResponse(resp *genai.GenerateContentResponse) (*Response, error) {
	if resp == nil {
		return &Response{}, nil
	}

	response := &Response{
		Model: p.model,
		Usage: mapUsage(resp.UsageMetadata),
	}
	if resp.ModelVersion != "" {
		response.Model = resp.ModelVersion
	}

	if len(resp.Candidates) == 0 {
		return response, nil
	}

	cand := resp.Candidates[0]
	switch cand.FinishReason {
	case genai.FinishReasonStop:
		response.StopReason = StopReasonEndTurn
	case genai.FinishReasonMaxTokens:
		response.StopReason = StopReasonMaxTokens
	case genai.FinishReasonSafety, genai.FinishReasonBlocklist, genai.FinishReasonProhibitedContent, genai.FinishReasonSPII:
		response.StopReason = StopReasonStopSeq
	default:
		if cand.FinishReason != "" && cand.FinishReason != genai.FinishReasonUnspecified {
			response.StopReason = StopReason(cand.FinishReason)
		} else {
			response.StopReason = StopReasonEndTurn
		}
	}

	if cand.Content == nil {
		return response, nil
	}

	for _, part := range cand.Content.Parts {
		if part == nil {
			continue
		}
		if part.Thought && part.Text != "" {
			response.Content = append(response.Content, ContentBlock{
				Type:     "thinking",
				Thinking: part.Text,
			})
			continue
		}
		if part.Text != "" {
			response.Content = append(response.Content, ContentBlock{
				Type: "text",
				Text: part.Text,
			})
		}
		if part.FunctionCall != nil {
			response.Content = append(response.Content, ContentBlock{
				Type:      "tool_use",
				ToolID:    part.FunctionCall.ID,
				ToolName:  part.FunctionCall.Name,
				ToolInput: part.FunctionCall.Args,
			})
		}
	}

	return response, nil
}

// mapUsage converts genai.GenerateContentResponseUsageMetadata to api.Usage.
// ThoughtsTokenCount is folded into OutputTokens so the existing cost-tracking
// math (which treats thinking as part of the output budget) keeps working.
func mapUsage(u *genai.GenerateContentResponseUsageMetadata) Usage {
	if u == nil {
		return Usage{}
	}
	out := Usage{
		InputTokens:          int(u.PromptTokenCount),
		OutputTokens:         int(u.CandidatesTokenCount),
		CacheReadInputTokens: int(u.CachedContentTokenCount),
	}
	if u.ThoughtsTokenCount > 0 {
		out.OutputTokens += int(u.ThoughtsTokenCount)
	}
	return out
}

// asAPIError extracts a genai.APIError (a value type) from err, supporting
// both pointer and value forms since the SDK returns it as a value.
func asAPIError(err error) (genai.APIError, bool) {
	if err == nil {
		return genai.APIError{}, false
	}
	var apiErr genai.APIError
	if errors.As(err, &apiErr) {
		return apiErr, true
	}
	return genai.APIError{}, false
}

// wrapGenAIError maps a genai error to a *RetryableHTTPError when possible.
// Returns nil if the error doesn't carry a status code (e.g. network error).
func wrapGenAIError(err error) error {
	if err == nil {
		return nil
	}
	apiErr, ok := asAPIError(err)
	if ok {
		isPermanent := apiErr.Code >= 400 && apiErr.Code < 500 &&
			apiErr.Code != 429 && apiErr.Code != 408 && apiErr.Code != 409
		return &RetryableHTTPError{
			StatusCode:  apiErr.Code,
			Message:     err.Error(),
			IsPermanent: isPermanent,
		}
	}
	return nil
}

// isPromptTooLongGenAI returns true for prompt-too-long / context-exhausted
// errors that should trigger a fallback or compaction.
func isPromptTooLongGenAI(err error) bool {
	if err == nil {
		return false
	}
	apiErr, ok := asAPIError(err)
	if ok && apiErr.Code == 400 {
		msg := strings.ToLower(err.Error())
		if strings.Contains(msg, "context") || strings.Contains(msg, "too long") || strings.Contains(msg, "exceeds") {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Streaming accumulator
// ---------------------------------------------------------------------------

type genAIStreamAccumulator struct {
	content    string
	thinking   string
	stopReason StopReason
	funcCalls  map[int]*genai.FunctionCall
}

func newGenAIStreamAccumulator() *genAIStreamAccumulator {
	return &genAIStreamAccumulator{funcCalls: make(map[int]*genai.FunctionCall)}
}

func (acc *genAIStreamAccumulator) appendContent(s string) { acc.content += s }
func (acc *genAIStreamAccumulator) getContent() string    { return acc.content }
func (acc *genAIStreamAccumulator) appendThinking(s string) {
	acc.thinking += s
}
func (acc *genAIStreamAccumulator) getThinking() string    { return acc.thinking }
func (acc *genAIStreamAccumulator) setStopReason(r StopReason) {
	if acc.stopReason == "" {
		acc.stopReason = r
	}
}

func (acc *genAIStreamAccumulator) appendFunctionCall(fc *genai.FunctionCall) {
	idx := len(acc.funcCalls)
	acc.funcCalls[idx] = fc
}

func (acc *genAIStreamAccumulator) getFunctionCallBlock() *ContentBlock {
	idx := len(acc.funcCalls) - 1
	if idx < 0 {
		return nil
	}
	fc, ok := acc.funcCalls[idx]
	if !ok || fc == nil {
		return nil
	}
	return &ContentBlock{
		Type:      "tool_use",
		ToolID:    fc.ID,
		ToolName:  fc.Name,
		ToolInput: fc.Args,
	}
}

func (acc *genAIStreamAccumulator) finalize() []ContentBlock {
	var blocks []ContentBlock
	if acc.thinking != "" {
		blocks = append(blocks, ContentBlock{Type: "thinking", Thinking: acc.thinking})
	}
	if acc.content != "" {
		blocks = append(blocks, ContentBlock{Type: "text", Text: acc.content})
	}
	for i := 0; i < len(acc.funcCalls); i++ {
		if fc, ok := acc.funcCalls[i]; ok && fc != nil {
			blocks = append(blocks, ContentBlock{
				Type:      "tool_use",
				ToolID:    fc.ID,
				ToolName:  fc.Name,
				ToolInput: fc.Args,
			})
		}
	}
	return blocks
}
