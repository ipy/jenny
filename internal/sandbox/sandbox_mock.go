//go:build !production

package sandbox

import (
	"context"
	"sync"
)

// MockSandboxManager is a mock implementation for testing.
type MockSandboxManager struct {
	mu          sync.RWMutex
	initialized bool
	available   bool
	active      bool
	config      Config
	wrapCalls   []string
	wrapResults map[string]string
	wrapError   error
	missingDeps bool
	depError    *ErrMissingDependency
}

// NewMockSandboxManager creates a new mock sandbox manager.
func NewMockSandboxManager() *MockSandboxManager {
	return &MockSandboxManager{
		wrapResults: make(map[string]string),
	}
}

// SetAvailable sets whether the mock sandbox is available.
func (m *MockSandboxManager) SetAvailable(available bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.available = available
}

// SetMissingDeps sets whether the mock has missing dependencies.
func (m *MockSandboxManager) SetMissingDeps(missing bool, dep, hint string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.missingDeps = missing
	if missing {
		m.depError = &ErrMissingDependency{
			Backend:     m.config.Backend,
			Dependency:  dep,
			InstallHint: hint,
		}
	} else {
		m.depError = nil
	}
}

// SetActive sets whether the mock sandbox is active.
func (m *MockSandboxManager) SetActive(active bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.active = active
}

// SetWrapResult sets the result for a specific command pattern.
func (m *MockSandboxManager) SetWrapResult(pattern, result string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.wrapResults[pattern] = result
}

// SetWrapError sets the error to return on WrapWithSandbox.
func (m *MockSandboxManager) SetWrapError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.wrapError = err
}

// GetWrapCalls returns the list of commands passed to WrapWithSandbox.
func (m *MockSandboxManager) GetWrapCalls() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	calls := make([]string, len(m.wrapCalls))
	copy(calls, m.wrapCalls)
	return calls
}

// Initialize implements SandboxManager.Initialize.
func (m *MockSandboxManager) Initialize(ctx context.Context, cfg Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.config = cfg

	if m.missingDeps {
		if cfg.FailIfUnavailable {
			return m.depError
		}
		// Warning mode - still mark as unavailable
		m.available = false
		return nil
	}

	m.initialized = true
	m.available = true
	m.active = cfg.Backend != BackendNone
	return nil
}

// WrapWithSandbox implements SandboxManager.WrapWithSandbox.
func (m *MockSandboxManager) WrapWithSandbox(command string) (string, error) {
	m.mu.Lock()
	m.wrapCalls = append(m.wrapCalls, command)
	m.mu.Unlock()

	if m.wrapError != nil {
		return "", m.wrapError
	}

	// Check if command matches an excluded pattern
	for _, pattern := range m.config.ExcludedCommands {
		if matchPattern(pattern, command) {
			return command, nil
		}
	}

	// Return configured result if available
	if result, ok := m.wrapResults[command]; ok {
		return result, nil
	}

	// Default: return the command unchanged if no specific result
	return command, nil
}

// RefreshConfig implements SandboxManager.RefreshConfig.
func (m *MockSandboxManager) RefreshConfig(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Mock always succeeds - in real impl would re-read config
	return nil
}

// IsAvailable implements SandboxManager.IsAvailable.
func (m *MockSandboxManager) IsAvailable() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.available
}

// IsActive implements SandboxManager.IsActive.
func (m *MockSandboxManager) IsActive() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.active
}

// RipgrepConfig implements SandboxManager.RipgrepConfig.
func (m *MockSandboxManager) RipgrepConfig() RipgrepConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config.Ripgrep
}

// matchPattern checks if a command matches a simple glob pattern.
// Supports: * (any chars), ? (single char)
func matchPattern(pattern, command string) bool {
	// Simple exact match
	if pattern == command {
		return true
	}
	// Glob matching
	if pattern == "*" {
		return true
	}
	// Prefix match with *
	if len(pattern) >= 2 && pattern[len(pattern)-1] == '*' {
		prefix := pattern[:len(pattern)-1]
		if len(command) >= len(prefix) && command[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}
