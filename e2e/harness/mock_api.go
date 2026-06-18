package harness

import (
	"testing"

	"github.com/ipy/jenny/internal/testutil/mockapi"
)

// APIRequest aliases the mockapi type.
type APIRequest = mockapi.APIRequest

// MockServer aliases the mockapi type.
type MockServer = mockapi.MockServer

// MockBehavior aliases the mockapi type.
type MockBehavior = mockapi.MockBehavior

// Option aliases the mockapi option type.
type Option = mockapi.Option

// NewTestServer is a convenience wrapper around mockapi.NewTestServer.
// It delegates entirely to mockapi.NewTestServer.
func NewTestServer(t *testing.T, cassetteID string, opts ...Option) *MockServer {
	return mockapi.NewTestServer(t, cassetteID, opts...)
}

// resolveCassetteDir resolves a relative cassette directory path to an absolute path
// relative to the repository root.
func resolveCassetteDir(rel string) string {
	// rel is expected to be something like "fixtures/cassettes"
	// We need to resolve it relative to the repo root.
	repoRoot, err := findRepoRoot()
	if err != nil {
		return rel
	}
	return repoRoot + "/" + rel
}
