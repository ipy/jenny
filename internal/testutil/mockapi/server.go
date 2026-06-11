package mockapi

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
type MockServer struct {
	Server       *httptest.Server
	CassetteDir  string
	mockBehavior *MockBehavior

	mu       sync.Mutex
	requests []APIRequest

	seqMu  sync.Mutex
	seqDef map[string][]string
	seqIdx map[string]int
}

// MockBehavior defines custom mock behaviors.
type MockBehavior struct {
	RejectEmptyToolProperties bool
}

// NewMockServer starts a new mock server that serves cassettes from cassetteDir.
func NewMockServer(cassetteDir string) *MockServer {
	m := &MockServer{CassetteDir: cassetteDir}
	m.Server = httptest.NewServer(http.HandlerFunc(m.handle))
	return m
}

// SetMockBehavior sets custom mock API behaviors.
func (m *MockServer) SetMockBehavior(mb *MockBehavior) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mockBehavior = mb
}

// URL returns the base URL of the mock server.
func (m *MockServer) URL() string {
	return m.Server.URL
}

// Close stops the mock server.
func (m *MockServer) Close() {
	m.Server.Close()
}

// Requests returns a copy of all captured requests.
func (m *MockServer) Requests() []APIRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]APIRequest, len(m.requests))
	copy(out, m.requests)
	return out
}

// ClearRequests resets the recorded requests list.
func (m *MockServer) ClearRequests() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests = nil
}

// SetSequence registers an ordered list of cassette file names to serve.
func (m *MockServer) SetSequence(cassetteID string, cassettes []string) {
	m.seqMu.Lock()
	defer m.seqMu.Unlock()
	if m.seqDef == nil {
		m.seqDef = make(map[string][]string)
	}
	if m.seqIdx == nil {
		m.seqIdx = make(map[string]int)
	}
	m.seqDef[cassetteID] = cassettes
	m.seqIdx[cassetteID] = 0
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
	mb := m.mockBehavior
	m.mu.Unlock()

	// Apply custom mock behaviors if any
	if mb != nil && mb.RejectEmptyToolProperties {
		if tools, ok := decoded["tools"].([]any); ok {
			for _, toolEntry := range tools {
				tool, ok := toolEntry.(map[string]any)
				if !ok {
					continue
				}
				// Skip web_search tools as they don't have properties in some dialects
				if toolType, ok := tool["type"].(string); ok && (toolType == "web_search" || toolType == "web_search_20250305") {
					continue
				}
				inputSchema, ok := tool["input_schema"].(map[string]any)
				if !ok || inputSchema == nil {
					m.writeError(w, http.StatusBadRequest, "function name or parameters is empty (2013)")
					return
				}
				props, ok := inputSchema["properties"]
				if !ok || props == nil {
					m.writeError(w, http.StatusBadRequest, "function name or parameters is empty (2013)")
					return
				}
				propsMap, ok := props.(map[string]any)
				if !ok || len(propsMap) == 0 {
					m.writeError(w, http.StatusBadRequest, "function name or parameters is empty (2013)")
					return
				}
			}
		} else {
			// No tools array at all
			m.writeError(w, http.StatusBadRequest, "function name or parameters is empty (2013)")
			return
		}
	}

	cassetteID, ok := extractCassetteID(r.URL.Path)
	if !ok {
		m.writeError(w, http.StatusBadRequest, fmt.Sprintf("no cassette id in path %q", r.URL.Path))
		return
	}

	effectiveID := cassetteID
	m.seqMu.Lock()
	if seq, hasSeq := m.seqDef[cassetteID]; hasSeq {
		idx := m.seqIdx[cassetteID]
		if idx >= len(seq) {
			m.seqMu.Unlock()
			m.writeError(w, http.StatusBadRequest,
				fmt.Sprintf("cassette sequence %q exhausted after %d requests", cassetteID, len(seq)))
			return
		}
		effectiveID = seq[idx]
		m.seqIdx[cassetteID]++
	}
	m.seqMu.Unlock()

	cassettePath := filepath.Join(m.CassetteDir, effectiveID+".sse")
	cassetteData, err := os.ReadFile(cassettePath)
	if err != nil {
		m.writeError(w, http.StatusBadRequest, fmt.Sprintf("cassette not found: %s: %v", cassettePath, err))
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	_, _ = w.Write(cassetteData)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (m *MockServer) writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// extractCassetteID extracts the cassette id from a path containing
// /cassette/<id>/v1/messages.
func extractCassetteID(path string) (string, bool) {
	const prefix = "/cassette/"
	const suffix = "/v1/messages"

	_, after, ok := strings.Cut(path, prefix)
	if !ok {
		return "", false
	}

	rest := after
	id, _, ok := strings.Cut(rest, suffix)
	if !ok || id == "" {
		return "", false
	}
	return id, true
}
