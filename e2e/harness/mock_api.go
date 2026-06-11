package harness

import (
	"github.com/ipy/jenny/internal/testutil/mockapi"
)

// APIRequest aliases the mockapi type.
type APIRequest = mockapi.APIRequest

// MockServer aliases the mockapi type.
type MockServer = mockapi.MockServer

// MockBehavior aliases the mockapi type.
type MockBehavior = mockapi.MockBehavior

// NewMockServer delegates to mockapi.NewMockServer.
func NewMockServer(cassetteDir string) *MockServer {
	return mockapi.NewMockServer(cassetteDir)
}