package mcp

import "context"

// Transport defines the interface for MCP client-server communication.
// Both stdio and HTTP transports implement this interface.
type Transport interface {
	// SendRequest sends a JSON-RPC request and returns the response.
	SendRequest(ctx context.Context, req jsonRPCRequest) (*jsonRPCResponse, error)

	// SendNotification sends a JSON-RPC notification (no response expected).
	SendNotification(ctx context.Context, notif jsonRPCRequest) error

	// Close shuts down the transport and releases resources.
	Close() error
}

// SessionExpiredError indicates the server returned HTTP 404
// meaning the session has expired and client must re-initialize.
type SessionExpiredError struct {
	SessionID string
}

func (e *SessionExpiredError) Error() string {
	return "MCP session expired (HTTP 404), must re-initialize"
}

// IsSessionExpired returns true if the error is a session expired error.
func IsSessionExpired(err error) bool {
	_, ok := err.(*SessionExpiredError)
	return ok
}
