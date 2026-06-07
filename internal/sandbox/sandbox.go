// Package sandbox provides OS-level sandbox abstraction for Bash and Grep tools.
package sandbox

import "context"

// SandboxManager defines the interface for sandbox backends.
type SandboxManager interface {
	// Initialize prepares the sandbox with the given configuration.
	// Returns an error if sandbox is enabled but dependencies are missing
	// and FailIfUnavailable is set in the config.
	Initialize(ctx context.Context, cfg Config) error

	// WrapWithSandbox wraps a command with sandbox execution.
	// Returns the wrapped command string and an error if wrapping fails.
	// If the command matches an excluded pattern, it returns the original command.
	WrapWithSandbox(command string) (string, error)

	// RefreshConfig re-reads sandbox settings without requiring a restart.
	// This allows permission changes via the admin surface to take effect.
	RefreshConfig(ctx context.Context) error

	// IsAvailable returns true if the sandbox backend is available.
	// Returns false if dependencies are missing or initialization failed.
	IsAvailable() bool

	// IsActive returns true if the sandbox is currently enabled and active.
	IsActive() bool

	// RipgrepConfig returns the sandboxed ripgrep configuration if configured.
	// Returns empty RipgrepConfig if sandboxed ripgrep is not enabled.
	RipgrepConfig() RipgrepConfig
}
