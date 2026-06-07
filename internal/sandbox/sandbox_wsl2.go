//go:build linux

package sandbox

import (
	"context"
	"os/exec"
	"sync"
)

// WSL2SandboxManager implements SandboxManager for WSL2.
// It reuses the Linux bubblewrap backend since WSL2 shares the Linux kernel.
type WSL2SandboxManager struct {
	linux *LinuxSandboxManager
	mu    sync.RWMutex
}

// NewWSL2SandboxManager creates a new WSL2 sandbox manager.
func NewWSL2SandboxManager() *WSL2SandboxManager {
	return &WSL2SandboxManager{
		linux: NewLinuxSandboxManager(),
	}
}

// Initialize implements SandboxManager.Initialize.
func (m *WSL2SandboxManager) Initialize(ctx context.Context, cfg Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// WSL2 uses Linux backend with adjusted config
	cfg.Backend = BackendWSL2
	return m.linux.Initialize(ctx, cfg)
}

// WrapWithSandbox implements SandboxManager.WrapWithSandbox.
func (m *WSL2SandboxManager) WrapWithSandbox(command string) (string, error) {
	return m.linux.WrapWithSandbox(command)
}

// RefreshConfig implements SandboxManager.RefreshConfig.
func (m *WSL2SandboxManager) RefreshConfig(ctx context.Context) error {
	return m.linux.RefreshConfig(ctx)
}

// IsAvailable implements SandboxManager.IsAvailable.
func (m *WSL2SandboxManager) IsAvailable() bool {
	return m.linux.IsAvailable()
}

// IsActive implements SandboxManager.IsActive.
func (m *WSL2SandboxManager) IsActive() bool {
	return m.linux.IsActive()
}

// RipgrepConfig implements SandboxManager.RipgrepConfig.
func (m *WSL2SandboxManager) RipgrepConfig() RipgrepConfig {
	return m.linux.RipgrepConfig()
}

// verifyWSL2 checks if we're running under WSL2.
func verifyWSL2() bool {
	// Check for WSL2 marker file
	if err := exec.Command("ls", "/proc/sys/fs/binfmt_misc/WSLInterop").Run(); err == nil {
		return true
	}
	// Check for WSL_ENV variable
	if wslEnv := exec.Command("cmd.exe", "/c", "echo", "%WSL_ENV%"); wslEnv.Run() == nil {
		return true
	}
	return false
}
