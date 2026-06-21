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
