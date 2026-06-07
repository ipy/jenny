//go:build !darwin

package sandbox

import (
	"context"
	"errors"
)

// MacOSSandboxManager is a stub for non-macOS platforms.
type MacOSSandboxManager struct{}

// NewMacOSSandboxManager returns a stub macOS sandbox manager.
func NewMacOSSandboxManager() *MacOSSandboxManager {
	return &MacOSSandboxManager{}
}

func (m *MacOSSandboxManager) Initialize(ctx context.Context, cfg Config) error {
	return errors.New("macOS sandbox is only available on macOS")
}

func (m *MacOSSandboxManager) WrapWithSandbox(command string) (string, error) {
	return "", errors.New("macOS sandbox is only available on macOS")
}

func (m *MacOSSandboxManager) RefreshConfig(ctx context.Context) error {
	return errors.New("macOS sandbox is only available on macOS")
}

func (m *MacOSSandboxManager) IsAvailable() bool {
	return false
}

func (m *MacOSSandboxManager) IsActive() bool {
	return false
}

func (m *MacOSSandboxManager) RipgrepConfig() RipgrepConfig {
	return RipgrepConfig{}
}
