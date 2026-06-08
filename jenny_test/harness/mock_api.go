// Package harness provides utilities for blackbox end-to-end tests of jenny.
//
// The mock API server intercepts requests to the Anthropic API and replays
// SSE cassettes that are committed alongside the tests. The cassette id is
// encoded as a URL path prefix (/cassette/<id>/v1/messages) so no changes
// to jenny's own code are required.
package harness

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// APIRequest is one captured request received by the mock server.
type APIRequest struct {
	Body   map[string]any
	Header http.Header
}

// MockServer is an in-process mock of the Anthropic API.
//
// The mock server captures the JSON-decoded body of every incoming request
// and serves SSE cassette content as the response. The cassette to replay
// is selected from the URL path prefix `/cassette/<id>/v1/messages`; that
// prefix is the only contract between the test and the handler.
type MockServer struct {
	Server      *httptest.Server
	CassetteDir string

	mu       sync.Mutex
	requests []APIRequest
}

// NewMockServer starts a new mock server that serves cassettes from
// cassetteDir. Call Close when done.
func NewMockServer(cassetteDir string) *MockServer {
	m := &MockServer{CassetteDir: cassetteDir}
	m.Server = httptest.NewServer(http.HandlerFunc(m.handle))
	return m
}

// URL returns the base URL of the mock server.
func (m *MockServer) URL() string {
	return m.Server.URL
}

// Close stops the mock server.
func (m *MockServer) Close() {
	m.Server.Close()
}

// Requests returns a copy of all requests captured by the mock server.
func (m *MockServer) Requests() []APIRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]APIRequest, len(m.requests))
	copy(out, m.requests)
	return out
}

func (m *MockServer) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		m.writeError(w, http.StatusMethodNotAllowed, fmt.Sprintf("method %s not allowed", r.Method))
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		m.writeError(w, http.StatusBadRequest, "read body: "+err.Error())
		return
	}

	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		m.writeError(w, http.StatusBadRequest, "decode body: "+err.Error())
		return
	}

	m.mu.Lock()
	m.requests = append(m.requests, APIRequest{Body: decoded, Header: r.Header.Clone()})
	m.mu.Unlock()

	cassetteID, ok := extractCassetteID(r.URL.Path)
	if !ok {
		m.writeError(w, http.StatusBadRequest, fmt.Sprintf("no cassette id in path %q; expected /cassette/<id>/v1/messages", r.URL.Path))
		return
	}

	cassettePath := filepath.Join(m.CassetteDir, cassetteID+".sse")
	cassetteData, err := os.ReadFile(cassettePath)
	if err != nil {
		m.writeError(w, http.StatusBadRequest, fmt.Sprintf("cassette not found: %s: %v", cassettePath, err))
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(cassetteData)
}

func (m *MockServer) writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// extractCassetteID extracts the cassette id from a path like
// /cassette/<id>/v1/messages. Returns the id and true on success.
func extractCassetteID(path string) (string, bool) {
	const prefix = "/cassette/"
	const suffix = "/v1/messages"
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	rest := path[len(prefix):]
	id, _, ok := strings.Cut(rest, suffix)
	if !ok || id == "" {
		return "", false
	}
	return id, true
}
