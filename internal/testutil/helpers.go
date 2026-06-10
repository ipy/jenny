package testutil

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"testing"
)

// CaptureStdout redirects os.Stdout to a pipe for the duration of fn and
// returns everything written. Uses a background goroutine to drain the pipe,
// avoiding deadlocks when fn produces large output.
func CaptureStdout(t *testing.T, fn func()) string {
	t.Helper()
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe error: %v", err)
	}
	os.Stdout = w

	done := make(chan struct{})
	var buf bytes.Buffer
	go func() {
		_, _ = io.Copy(&buf, r)
		close(done)
	}()

	fn()
	_ = w.Close()
	os.Stdout = oldStdout
	<-done
	return buf.String()
}

// SSELine formats an SSE event line in the format "event: <event>\ndata: <data>\n\n".
func SSELine(event, data string) string {
	return fmt.Sprintf("event: %s\ndata: %s\n\n", event, data)
}
