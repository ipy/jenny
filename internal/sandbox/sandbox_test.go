//go:build !production

package sandbox

import (
	"context"
	"testing"
)

func TestMockSandboxManager_Initialize(t *testing.T) {
	m := NewMockSandboxManager()

	// Test successful initialization
	cfg := Config{
		Backend:                  BackendLinux,
		AllowUnsandboxedCommands: false,
		FailIfUnavailable:        true,
	}

	err := m.Initialize(context.Background(), cfg)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !m.IsAvailable() {
		t.Error("expected sandbox to be available")
	}
	if !m.IsActive() {
		t.Error("expected sandbox to be active")
	}
}

func TestMockSandboxManager_Initialize_MissingDeps(t *testing.T) {
	m := NewMockSandboxManager()
	m.SetMissingDeps(true, "bwrap", "sudo apt install bubblewrap")

	cfg := Config{
		Backend:           BackendLinux,
		FailIfUnavailable: true,
	}

	err := m.Initialize(context.Background(), cfg)
	if err == nil {
		t.Error("expected error when deps missing and FailIfUnavailable is true")
	}

	depErr, ok := err.(*ErrMissingDependency)
	if !ok {
		t.Errorf("expected ErrMissingDependency, got %T", err)
	}
	if depErr.Dependency != "bwrap" {
		t.Errorf("expected dependency 'bwrap', got %q", depErr.Dependency)
	}
}

func TestMockSandboxManager_Initialize_MissingDeps_Warning(t *testing.T) {
	m := NewMockSandboxManager()
	m.SetMissingDeps(true, "bwrap", "sudo apt install bubblewrap")

	cfg := Config{
		Backend:           BackendLinux,
		FailIfUnavailable: false, // Warning mode
	}

	err := m.Initialize(context.Background(), cfg)
	if err != nil {
		t.Errorf("unexpected error in warning mode: %v", err)
	}
	if m.IsAvailable() {
		t.Error("expected sandbox to be unavailable in warning mode")
	}
}

func TestMockSandboxManager_WrapWithSandbox(t *testing.T) {
	m := NewMockSandboxManager()

	cfg := Config{
		Backend: BackendLinux,
	}
	m.Initialize(context.Background(), cfg)

	// Test default behavior - returns original command
	wrapped, err := m.WrapWithSandbox("echo hello")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if wrapped != "echo hello" {
		t.Errorf("expected original command, got %q", wrapped)
	}
}

func TestMockSandboxManager_WrapWithSandbox_Excluded(t *testing.T) {
	m := NewMockSandboxManager()

	cfg := Config{
		Backend:          BackendLinux,
		ExcludedCommands: []string{"echo*", "pwd"},
	}
	m.Initialize(context.Background(), cfg)

	// Test excluded command
	wrapped, err := m.WrapWithSandbox("echo hello")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if wrapped != "echo hello" {
		t.Errorf("expected excluded command to return original, got %q", wrapped)
	}
}

func TestMockSandboxManager_WrapWithSandbox_CustomResult(t *testing.T) {
	m := NewMockSandboxManager()

	cfg := Config{
		Backend: BackendLinux,
	}
	m.Initialize(context.Background(), cfg)

	m.SetWrapResult("dangerous", "sandbox-exec -p '(deny default)' sh -c 'dangerous'")

	wrapped, err := m.WrapWithSandbox("dangerous")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if wrapped != "sandbox-exec -p '(deny default)' sh -c 'dangerous'" {
		t.Errorf("expected custom wrapped command, got %q", wrapped)
	}
}

func TestMockSandboxManager_WrapWithSandbox_Error(t *testing.T) {
	m := NewMockSandboxManager()

	cfg := Config{
		Backend: BackendLinux,
	}
	m.Initialize(context.Background(), cfg)

	m.SetWrapError(&ErrMissingDependency{
		Backend:     BackendLinux,
		Dependency:  "bwrap",
		InstallHint: "sudo apt install bubblewrap",
	})

	_, err := m.WrapWithSandbox("echo hello")
	if err == nil {
		t.Error("expected error from WrapWithSandbox")
	}
}

func TestMockSandboxManager_RefreshConfig(t *testing.T) {
	m := NewMockSandboxManager()

	cfg := Config{
		Backend: BackendLinux,
	}
	m.Initialize(context.Background(), cfg)

	err := m.RefreshConfig(context.Background())
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestMockSandboxManager_IsActive(t *testing.T) {
	m := NewMockSandboxManager()

	// Not initialized - should not be active
	if m.IsActive() {
		t.Error("expected not active before initialization")
	}

	cfg := Config{
		Backend: BackendNone, // Disabled
	}
	m.Initialize(context.Background(), cfg)

	// BackendNone - should not be active
	if m.IsActive() {
		t.Error("expected not active with BackendNone")
	}

	m.SetActive(true)
	if !m.IsActive() {
		t.Error("expected active after SetActive(true)")
	}
}

func TestMockSandboxManager_RipgrepConfig(t *testing.T) {
	m := NewMockSandboxManager()

	cfg := Config{
		Backend: BackendLinux,
		Ripgrep: RipgrepConfig{
			Command: "/usr/local/bin/rg",
			Args:    []string{"--hidden"},
			Argv0:   "rg",
		},
	}
	m.Initialize(context.Background(), cfg)

	rgCfg := m.RipgrepConfig()
	if rgCfg.Command != "/usr/local/bin/rg" {
		t.Errorf("expected command '/usr/local/bin/rg', got %q", rgCfg.Command)
	}
	if len(rgCfg.Args) != 1 || rgCfg.Args[0] != "--hidden" {
		t.Errorf("expected args ['--hidden'], got %v", rgCfg.Args)
	}
}

func TestMockSandboxManager_GetWrapCalls(t *testing.T) {
	m := NewMockSandboxManager()

	cfg := Config{
		Backend: BackendLinux,
	}
	m.Initialize(context.Background(), cfg)

	m.WrapWithSandbox("echo hello")
	m.WrapWithSandbox("ls /tmp")
	m.WrapWithSandbox("pwd")

	calls := m.GetWrapCalls()
	if len(calls) != 3 {
		t.Errorf("expected 3 wrap calls, got %d", len(calls))
	}
	if calls[0] != "echo hello" {
		t.Errorf("expected first call 'echo hello', got %q", calls[0])
	}
}

func TestErrMissingDependency_Error(t *testing.T) {
	err := &ErrMissingDependency{
		Backend:     BackendLinux,
		Dependency:  "bwrap",
		InstallHint: "sudo apt install bubblewrap",
	}

	expected := "sandbox backend linux missing dependency: bwrap. sudo apt install bubblewrap"
	if err.Error() != expected {
		t.Errorf("expected error message %q, got %q", expected, err.Error())
	}
}

func TestNetworkPolicy_Values(t *testing.T) {
	if NetworkPolicyNormal != "normal" {
		t.Errorf("expected NetworkPolicyNormal to be 'normal', got %q", NetworkPolicyNormal)
	}
	if NetworkPolicyManagedDomainsOnly != "managed-domains-only" {
		t.Errorf("expected NetworkPolicyManagedDomainsOnly to be 'managed-domains-only', got %q", NetworkPolicyManagedDomainsOnly)
	}
}

func TestBackend_Values(t *testing.T) {
	if BackendMacOS != "macos" {
		t.Errorf("expected BackendMacOS to be 'macos', got %q", BackendMacOS)
	}
	if BackendLinux != "linux" {
		t.Errorf("expected BackendLinux to be 'linux', got %q", BackendLinux)
	}
	if BackendWSL2 != "wsl2" {
		t.Errorf("expected BackendWSL2 to be 'wsl2', got %q", BackendWSL2)
	}
	if BackendNone != "none" {
		t.Errorf("expected BackendNone to be 'none', got %q", BackendNone)
	}
}
