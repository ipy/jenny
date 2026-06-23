package api

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// sseReader produces SSE events from a list of strings.
type sseReader struct {
	events []string
	pos    int
}

func (r *sseReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.events) {
		return 0, io.EOF
	}
	event := r.events[r.pos]
	r.pos++
	n := copy(p, event)
	return n, nil
}

func TestCtxSSEScanner_NormalDataFlow(t *testing.T) {
	events := []string{
		"data: {\"type\":\"message_start\"}\n\n",
		"data: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"Hello\"}}\n\n",
		"data: {\"type\":\"message_stop\"}\n\n",
		"data: [DONE]\n\n",
	}
	r := &sseReader{events: events}
	ctx := context.Background()
	scanner := newCtxSSEScanner(ctx, r)

	var results []string
	for {
		data, ok := scanner.Next(ctx)
		if !ok {
			break
		}
		results = append(results, data)
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 SSE events, got %d: %v", len(results), results)
	}
	if !strings.Contains(results[0], "message_start") {
		t.Errorf("expected message_start, got %q", results[0])
	}
	if !strings.Contains(results[1], "Hello") {
		t.Errorf("expected Hello text, got %q", results[1])
	}
	if !strings.Contains(results[2], "message_stop") {
		t.Errorf("expected message_stop, got %q", results[2])
	}
}

func TestCtxSSEScanner_ContextCancellation(t *testing.T) {
	// Use a pipe that never produces data, simulating a blocked read.
	pr, pw := io.Pipe()
	defer pw.Close()
	defer pr.Close()

	ctx, cancel := context.WithCancel(context.Background())
	scanner := newCtxSSEScanner(ctx, pr)

	// Cancel immediately in a goroutine
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	// Next should return false once cancellation propagates
	start := time.Now()
	_, ok := scanner.Next(ctx)
	elapsed := time.Since(start)

	if ok {
		t.Error("expected Next to return false after context cancellation")
	}
	if elapsed > 2*time.Second {
		t.Errorf("expected fast cancellation (under 2s), took %v", elapsed)
	}
}

func TestCtxSSEScanner_EOFHandling(t *testing.T) {
	// Empty reader should produce EOF immediately
	r := strings.NewReader("")
	ctx := context.Background()
	scanner := newCtxSSEScanner(ctx, r)

	_, ok := scanner.Next(ctx)
	if ok {
		t.Error("expected Next to return false on EOF")
	}
}

func TestCtxSSEScanner_ConcurrentCancellationSafety(t *testing.T) {
	// Multiple goroutines calling Next, one triggers cancellation.
	// This verifies no panic or data race.
	pr, pw := io.Pipe()
	defer pr.Close()

	ctx, cancel := context.WithCancel(context.Background())
	scanner := newCtxSSEScanner(ctx, pr)

	var wg sync.WaitGroup
	n := 4
	results := make([]bool, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, ok := scanner.Next(ctx)
			results[idx] = ok
		}(i)
	}

	// Cancel after a brief delay
	time.Sleep(20 * time.Millisecond)
	cancel()
	pw.Close() // unblock the reader goroutine too

	wg.Wait()

	// All should have returned (no goroutines hanging)
	// At least one should have returned false (cancellation detected)
	anyFalse := false
	for _, ok := range results {
		if !ok {
			anyFalse = true
		}
	}
	if !anyFalse {
		t.Error("expected at least one Next call to return false due to cancellation")
	}
}

func TestCtxSSEScanner_CloseUnblocksBlockedReader(t *testing.T) {
	// Simulate a reader that blocks forever (stalled TCP connection).
	// Close() must cause the inner goroutine to exit promptly.
	pr, pw := io.Pipe()
	defer pw.Close()

	ctx := context.Background()
	scanner := newCtxSSEScanner(ctx, pr)

	// Start a goroutine trying to read — it will block
	done := make(chan struct{})
	go func() {
		scanner.Next(ctx)
		close(done)
	}()

	// Give the goroutine time to enter the blocked read
	time.Sleep(20 * time.Millisecond)

	// Close the scanner — should unblock everything
	scanner.Close()

	select {
	case <-done:
		// Success: Next returned
	case <-time.After(2 * time.Second):
		t.Fatal("ctxSSEScanner.Close() did not unblock the blocked reader within 2s")
	}
}

func TestCtxSSEScanner_CloseIdempotent(t *testing.T) {
	r := strings.NewReader("data: hello\n\n")
	ctx := context.Background()
	scanner := newCtxSSEScanner(ctx, r)

	// Drain the scanner
	scanner.Next(ctx)
	scanner.Next(ctx)

	// Multiple Close calls should not panic
	scanner.Close()
	scanner.Close()
	scanner.Close()
}

func TestCancelingBody_CancelsContextBeforeClose(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	body := &cancelingBody{
		ReadCloser: pr,
		cancel:     cancel,
	}

	// Write some data
	go func() {
		pw.Write([]byte("hello"))
	}()

	buf := make([]byte, 5)
	n, err := body.Read(buf)
	if err != nil {
		t.Fatalf("unexpected read error: %v", err)
	}
	if n != 5 || string(buf) != "hello" {
		t.Fatalf("expected 'hello', got %q", string(buf[:n]))
	}

	// Close the body — should cancel context first
	body.Close()

	// Verify context was cancelled
	if ctx.Err() == nil {
		t.Error("expected context to be cancelled after body.Close()")
	}
}
