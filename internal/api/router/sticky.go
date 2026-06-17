// Package router provides multi-provider routing with three-layer fallback logic.
package router

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/ipy/jenny/internal/api"
)

// StickyClient implements api.Requester and wraps routing logic with sticky sessions.
type StickyClient struct {
	router      *Router
	sessionID   string
	endpoint    *ActiveEndpoint
	client      api.Requester
	maxRetries  int
	backoffType   string
	randFn        func() float64
	clientFactory func(model string) (api.Requester, error)
	mu            sync.Mutex
}

// NewStickyClient creates a new StickyClient wrapping the router.
func NewStickyClient(sessionID string, router *Router) *StickyClient {
	return &StickyClient{
		router:        router,
		sessionID:     sessionID,
		maxRetries:    5,
		backoffType:   "exponential",
		randFn:        rand.Float64,
		clientFactory: func(model string) (api.Requester, error) { return api.NewClientWithModel(model) },
	}
}

// SendMessage implements api.Requester.
// It selects an endpoint, delegates to the underlying client, and handles
// three-layer fallback on errors: retry (L1) -> key failover (L2) -> model fallback (L3).
func (s *StickyClient) SendMessage(ctx context.Context, messages []api.Message, tools []api.ToolParam, toolResults []api.ToolResult, systemPrompt []string, systemPromptSuffix string) (*api.Response, error) {
	if err := s.selectEndpoint(); err != nil {
		return nil, fmt.Errorf("failed to select endpoint: %w", err)
	}

	// L1: Retry with backoff on same key+model
	for attempt := 0; attempt <= s.maxRetries; attempt++ {
		if err := s.ensureClient(); err != nil {
			return nil, fmt.Errorf("failed to create client: %w", err)
		}

		resp, err := s.client.SendMessage(ctx, messages, tools, toolResults, systemPrompt, systemPromptSuffix)
		if err == nil {
			s.router.healthRegistry.RecordSuccess(
				s.endpoint.Provider,
				s.endpoint.Account,
				s.endpoint.Model,
				s.endpoint.APIKey,
			)
			return resp, nil
		}

		var httpErr *api.HTTPError
		if errors.As(err, &httpErr) {
			cat := httpErr.ErrorCategory
			switch cat {
			case api.CategoryContextExhausted:
				return nil, err // caller/engine handles compaction
			case api.CategoryQuotaExhausted, api.CategoryPaymentRequired:
				s.recordFailure()
				if next := s.tryNextKey(); next != nil {
					s.endpoint = next
					s.client = nil
					continue
				}
				return nil, err
			case api.CategoryContentFilter, api.CategoryAuth, api.CategoryPermission:
				return nil, err // fail fast
			case api.CategoryRateLimitRPM, api.CategoryRateLimitTPM,
				api.CategoryRateLimitConcurrency, api.CategoryRateLimitGeneric,
				api.CategoryServerOverload, api.CategoryServerError, api.CategoryTimeout:
				if attempt < s.maxRetries {
					var retryAfter *time.Duration
					if re, ok := err.(*api.RetryableHTTPError); ok {
						retryAfter = re.RetryAfter
					}
					delay := s.computeCategoryBackoff(cat, attempt, retryAfter)
					select {
					case <-ctx.Done():
						return nil, ctx.Err()
					case <-time.After(delay):
					}
					continue
				}
				return nil, err
			case api.CategoryModelNotFound:
				s.recordFailure()
				if next := s.tryNextTarget(); next != nil {
					s.endpoint = next
					s.client = nil
					continue
				}
				return nil, err
			default:
				// fallback: original status code logic for unknown categories
				code := httpErr.StatusCode
				if code >= 400 && code < 500 && code != 429 && code != 408 && code != 409 {
					return nil, err
				}
				if code == http.StatusTooManyRequests || (code >= 500 && code < 600) {
					if attempt < s.maxRetries {
						delay := s.computeBackoff(attempt, nil)
						select {
						case <-ctx.Done():
							return nil, ctx.Err()
						case <-time.After(delay):
						}
						continue
					}
				}
			}
		} else {
			// Check for RetryableHTTPError if it's not an HTTPError
			var retryableErr *api.RetryableHTTPError
			if errors.As(err, &retryableErr) {
				if retryableErr.IsPermanent {
					return nil, err
				}
				if attempt < s.maxRetries {
					delay := s.computeBackoff(attempt, retryableErr.RetryAfter)
					select {
					case <-ctx.Done():
						return nil, ctx.Err()
					case <-time.After(delay):
					}
					continue
				}
			}
		}

		s.recordFailure()
	}

	return nil, fmt.Errorf("all routing layers exhausted")
}

// SendMessageStream implements api.Requester streaming.
func (s *StickyClient) SendMessageStream(ctx context.Context, messages []api.Message, tools []api.ToolParam, toolResults []api.ToolResult, systemPrompt []string, systemPromptSuffix string, idleTimeout time.Duration, fallbackTimeout time.Duration, onStreamingFallback func(context.Context) (*api.Response, error)) (<-chan api.StreamContentBlock, *api.StreamResult) {
	if err := s.selectEndpoint(); err != nil {
		blocks := make(chan api.StreamContentBlock)
		result := &api.StreamResult{Error: err.Error()}
		close(blocks)
		return blocks, result
	}

	if err := s.ensureClient(); err != nil {
		blocks := make(chan api.StreamContentBlock)
		result := &api.StreamResult{Error: err.Error()}
		close(blocks)
		return blocks, result
	}

	return s.client.SendMessageStream(ctx, messages, tools, toolResults, systemPrompt, systemPromptSuffix, idleTimeout, fallbackTimeout, onStreamingFallback)
}

// SetMaxTokensOverride implements api.Requester.
func (s *StickyClient) SetMaxTokensOverride(maxTokens int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client != nil {
		s.client.SetMaxTokensOverride(maxTokens)
	}
}

// SetRetryConfig implements api.Requester.
func (s *StickyClient) SetRetryConfig(cfg api.RetryConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.maxRetries = cfg.MaxRetries
	if s.client != nil {
		s.client.SetRetryConfig(cfg)
	}
}

// SetBackground implements api.Requester.
func (s *StickyClient) SetBackground(isBackground bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if bg, ok := s.client.(interface{ SetBackground(bool) }); ok {
		bg.SetBackground(isBackground)
	}
}

// SetThinkingConfig implements api.Requester.
func (s *StickyClient) SetThinkingConfig(cfg api.ThinkingConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client != nil {
		s.client.SetThinkingConfig(cfg)
	}
}

func (s *StickyClient) selectEndpoint() error {
	if s.endpoint != nil {
		return nil
	}

	endpoint, err := s.router.SelectEndpoint(s.sessionID)
	if err != nil {
		return err
	}

	s.endpoint = endpoint
	return nil
}

func (s *StickyClient) ensureClient() error {
	if s.client != nil {
		return nil
	}

	client, err := s.clientFactory(s.endpoint.Model)
	if err != nil {
		return err
	}
	s.client = client
	return nil
}

func (s *StickyClient) computeCategoryBackoff(category api.ErrorCategory, attempt int, retryAfter *time.Duration) time.Duration {
	baseDelay, maxDelay := s.categoryBackoffParams(category)
	delay := baseDelay * time.Duration(1<<uint(attempt))
	delay = min(delay, maxDelay)

	jitter := time.Duration(s.randFn() * float64(delay) * 0.25)
	delay = delay + jitter

	if retryAfter != nil && *retryAfter > delay {
		delay = *retryAfter
	}

	return delay
}

func (s *StickyClient) categoryBackoffParams(category api.ErrorCategory) (base, max time.Duration) {
	switch category {
	case api.CategoryRateLimitRPM:
		return 2 * time.Second, 15 * time.Second
	case api.CategoryRateLimitTPM:
		return 5 * time.Second, 30 * time.Second
	case api.CategoryRateLimitConcurrency:
		return 10 * time.Second, 60 * time.Second
	case api.CategoryRateLimitGeneric:
		return 1 * time.Second, 32 * time.Second
	case api.CategoryServerOverload:
		return 15 * time.Second, 120 * time.Second
	case api.CategoryServerError:
		return 500 * time.Millisecond, 32 * time.Second
	case api.CategoryTimeout:
		return 2 * time.Second, 30 * time.Second
	default:
		return 500 * time.Millisecond, 32 * time.Second
	}
}

func (s *StickyClient) computeBackoff(attempt int, retryAfter *time.Duration) time.Duration {
	baseDelay := 500 * time.Millisecond
	maxDelay := 32 * time.Second

	delay := baseDelay * time.Duration(1<<uint(attempt))
	delay = min(delay, maxDelay)

	jitter := time.Duration(s.randFn() * float64(delay) * 0.25)
	delay = delay + jitter

	if retryAfter != nil && *retryAfter > delay {
		delay = *retryAfter
	}

	return delay
}

func (s *StickyClient) recordFailure() {
	if s.endpoint == nil {
		return
	}
	s.router.healthRegistry.RecordFailure(
		s.endpoint.Provider,
		s.endpoint.Account,
		s.endpoint.Model,
		s.endpoint.APIKey,
	)
}

func (s *StickyClient) tryNextKey() *ActiveEndpoint {
	if s.endpoint == nil {
		return nil
	}

	next, err := s.router.NextEndpoint(s.sessionID, s.endpoint)
	if err != nil {
		return nil
	}

	if next.Model == s.endpoint.Model {
		return next
	}
	return nil
}

func (s *StickyClient) tryNextTarget() *ActiveEndpoint {
	if s.endpoint == nil {
		return nil
	}

	s.router.mu.Lock()
	defer s.router.mu.Unlock()

	state, ok := s.router.sessions[s.sessionID]
	if !ok {
		return nil
	}

	profile, ok := s.router.config.Profiles[s.router.profileName]
	if !ok {
		return nil
	}

	if profile.AllowFallback != nil && *profile.AllowFallback {
		nextTarget := s.router.nextTargetLocked(state, s.endpoint)
		if nextTarget != nil {
			state.Endpoint = nextTarget
			state.TargetIndex++
			state.KeyIndex = 0
			return nextTarget
		}
	}
	return nil
}
