package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ipy/jenny/internal/api"
)

func TestSticky_CategoryAwareRoutingTable(t *testing.T) {
	tests := []struct {
		name          string
		cat           api.ErrorCategory
		statusCode    int
		expectRetry   bool
		expectL2      bool
		expectL3      bool
		expectFail    bool
		expectNoRetry bool // fail fast
	}{
		{
			name:        "ContextExhausted - return to engine",
			cat:         api.CategoryContextExhausted,
			statusCode:  400,
			expectRetry: false,
			expectFail:  true,
		},
		{
			name:       "QuotaExhausted - L2 failover",
			cat:        api.CategoryQuotaExhausted,
			statusCode: 403,
			expectL2:   true,
		},
		{
			name:          "ContentFilter - fail fast",
			cat:           api.CategoryContentFilter,
			statusCode:    400,
			expectNoRetry: true,
			expectFail:    true,
		},
		{
			name:        "RateLimit - retry",
			cat:         api.CategoryRateLimitRPM,
			statusCode:  429,
			expectRetry: true,
		},
		{
			name:       "ModelNotFound - L3 fallback",
			cat:        api.CategoryModelNotFound,
			statusCode: 404,
			expectL3:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Mock server that returns the specified error once, then success
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				c := calls.Add(1)
				if c == 1 {
					w.WriteHeader(tt.statusCode)
					return
				}
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{}`))
			}))
			defer srv.Close()

			cfg := twoProviderConfig() // Has L1, L2 (k1 in p1), L3 (p2)
			// Add a second key to provider-a for L2 testing
			cfg.Providers[0].Accounts[0].Keys = []string{"k1", "k2"}
			cfg.Providers[0].BaseURL = srv.URL
			cfg.Providers[1].BaseURL = srv.URL

			r := NewRouter(cfg)
			sc := NewStickyClient("s1", r)
			sc.maxRetries = 1
			// Inject clientFactory to return directHandler with category
			sc.clientFactory = func(model string) (api.Requester, error) {
				return &directHandler{h: srv.Config.Handler, cat: tt.cat}, nil
			}
			// Inject fixed rand for deterministic backoff (0 means no jitter)
			sc.randFn = func() float64 { return 0 }

			// Select initial endpoint
			ep, _ := r.SelectEndpoint("s1")
			sc.endpoint = ep

			_, err := sc.SendMessage(context.Background(), nil, nil, nil, nil, "")

			if tt.expectFail {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("expected success, got error: %v", err)
			}

			finalCalls := int(calls.Load())
			if tt.expectRetry && finalCalls != 2 {
				t.Errorf("expected 2 calls (1 retry), got %d", finalCalls)
			}
			if tt.expectL2 && sc.endpoint.APIKey != "k2" {
				t.Errorf("expected L2 failover to k2, got %s", sc.endpoint.APIKey)
			}
			if tt.expectL3 && sc.endpoint.Provider != "provider-b" {
				t.Errorf("expected L3 fallback to provider-b, got %s", sc.endpoint.Provider)
			}
		})
	}
}

func TestSticky_ComputeCategoryBackoff(t *testing.T) {
	sc := &StickyClient{
		randFn: func() float64 { return 0.5 }, // 50% jitter of 25% max = 12.5% jitter
	}

	tests := []struct {
		cat         api.ErrorCategory
		attempt     int
		retryAfter  *time.Duration
		expectRange [2]time.Duration // [min, max] without jitter
	}{
		{
			cat:         api.CategoryRateLimitRPM,
			attempt:     0,
			expectRange: [2]time.Duration{2 * time.Second, 2 * time.Second},
		},
		{
			cat:         api.CategoryRateLimitTPM,
			attempt:     1,
			expectRange: [2]time.Duration{10 * time.Second, 10 * time.Second},
		},
		{
			cat:         api.CategoryServerOverload,
			attempt:     0,
			expectRange: [2]time.Duration{15 * time.Second, 15 * time.Second},
		},
	}

	for _, tt := range tests {
		delay := sc.computeCategoryBackoff(tt.cat, tt.attempt, tt.retryAfter)

		base, _ := sc.categoryBackoffParams(tt.cat)
		expectedBase := base * time.Duration(1<<uint(tt.attempt))
		// with jitter 0.5 * 0.25 * expectedBase = 0.125 * expectedBase
		expectedDelay := expectedBase + time.Duration(0.125*float64(expectedBase))

		if delay != expectedDelay {
			t.Errorf("category %s attempt %d: expected %v, got %v", tt.cat, tt.attempt, expectedDelay, delay)
		}
	}
}

func TestSticky_RespectRetryAfter(t *testing.T) {
	sc := &StickyClient{
		randFn: func() float64 { return 0 },
	}

	ra := 10 * time.Second
	delay := sc.computeCategoryBackoff(api.CategoryRateLimitRPM, 0, &ra)
	if delay != 10*time.Second {
		t.Errorf("expected to respect Retry-After 10s, got %v", delay)
	}

	ra2 := 1 * time.Second
	delay2 := sc.computeCategoryBackoff(api.CategoryRateLimitRPM, 0, &ra2)
	if delay2 != 2*time.Second {
		t.Errorf("expected to use computed delay 2s (longer than Retry-After 1s), got %v", delay2)
	}
}
