package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// HTTPClient is a simple wrapper around http.Client with retry logic.
type HTTPClient struct {
	client *http.Client
}

func NewHTTPClient(timeout time.Duration) *HTTPClient {
	return &HTTPClient{
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

// Request performs an HTTP request and unmarshals the response.
func (c *HTTPClient) Request(ctx context.Context, method, url string, headers http.Header, body any, response any) error {
	var bodyReader io.Reader
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return err
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return err
	}

	for k, v := range headers {
		for _, val := range v {
			req.Header.Add(k, val)
		}
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return &HTTPError{
			StatusCode:    resp.StatusCode,
			Message:       string(respBody),
			ShouldRetry:   parseShouldRetry(resp.Header),
			ErrorCategory: classifyErrorCommon(resp.StatusCode, string(respBody)),
		}
	}

	if response != nil {
		return json.NewDecoder(resp.Body).Decode(response)
	}

	return nil
}

// StreamRequest performs an HTTP request and returns a channel of SSE events.
func (c *HTTPClient) StreamRequest(ctx context.Context, method, url string, headers http.Header, body any) (io.ReadCloser, error) {
	var bodyReader io.Reader
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, err
	}

	for k, v := range headers {
		for _, val := range v {
			req.Header.Add(k, val)
		}
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, &HTTPError{
			StatusCode:    resp.StatusCode,
			Message:       string(respBody),
			ShouldRetry:   parseShouldRetry(resp.Header),
			ErrorCategory: classifyErrorCommon(resp.StatusCode, string(respBody)),
		}
	}

	return resp.Body, nil
}

// parseShouldRetry extracts the x-should-retry header value.
// Returns nil if header is absent, otherwise a pointer to the boolean value.
func parseShouldRetry(h http.Header) *bool {
	v := h.Get("x-should-retry")
	if v == "" {
		return nil
	}
	val := v == "true"
	return &val
}

// HTTPError represents an HTTP error response.
type HTTPError struct {
	StatusCode    int
	Message       string
	ShouldRetry   *bool         // from x-should-retry header; nil if not present
	ErrorCategory ErrorCategory // Normalized error category
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.StatusCode, e.Message)
}

// SSEScanner scans an SSE stream and yields data blocks.
type SSEScanner struct {
	scanner *bufio.Scanner
}

func NewSSEScanner(r io.Reader) *SSEScanner {
	return &SSEScanner{
		scanner: bufio.NewScanner(r),
	}
}

func (s *SSEScanner) Next() (string, bool) {
	for s.scanner.Scan() {
		line := s.scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				return "", false
			}
			return data, true
		}
	}
	return "", false
}

func (s *SSEScanner) Err() error {
	return s.scanner.Err()
}

// ctxSSEScanner wraps an SSEScanner with context-aware Next(ctx).
// The inner scanner runs in a dedicated goroutine, sending results over a
// buffered channel. Next(ctx) selects between the channel and ctx.Done(),
// so cancellation unblocks immediately instead of waiting for the underlying
// io.Read to return.
type ctxSSEScanner struct {
	ch      chan scanResult
	cancel  context.CancelFunc
	lastErr error
}

type scanResult struct {
	data string
	ok   bool
	err  error
}

func newCtxSSEScanner(ctx context.Context, r io.Reader) *ctxSSEScanner {
	readCtx, cancel := context.WithCancel(ctx)
	s := &ctxSSEScanner{
		ch:     make(chan scanResult, 1),
		cancel: cancel,
	}
	inner := NewSSEScanner(r)
	go func() {
		defer close(s.ch)
		for {
			data, ok := inner.Next()
			result := scanResult{data: data, ok: ok}
			if !ok {
				result.err = inner.Err()
			}
			select {
			case s.ch <- result:
				if !ok {
					return
				}
			case <-readCtx.Done():
				return
			}
		}
	}()
	return s
}

func (s *ctxSSEScanner) Next(ctx context.Context) (string, bool) {
	select {
	case r, ok := <-s.ch:
		if !ok {
			return "", false
		}
		s.lastErr = r.err
		return r.data, r.ok
	case <-ctx.Done():
		s.cancel()
		return "", false
	}
}

// Err returns the underlying scanner error, if any. It is only populated
// after the inner scanner finishes (Next returns false naturally).
func (s *ctxSSEScanner) Err() error {
	return s.lastErr
}
