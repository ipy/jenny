// Package sandbox provides OS-level sandbox abstraction for Bash and Grep tools.
package sandbox

import "errors"

// Backend represents the sandbox backend type.
type Backend string

const (
	BackendMacOS Backend = "macos"
	BackendLinux Backend = "linux"
	BackendWSL2  Backend = "wsl2"
	BackendNone  Backend = "none"
)

// NetworkPolicy defines the network restriction level.
type NetworkPolicy string

const (
	// NetworkPolicyNormal allows all domains (merged with WebFetch allow rules).
	NetworkPolicyNormal NetworkPolicy = "normal"
	// NetworkPolicyManagedDomainsOnly restricts network to policy list only.
	NetworkPolicyManagedDomainsOnly NetworkPolicy = "managed-domains-only"
)

// RipgrepConfig holds the sandboxed ripgrep configuration.
type RipgrepConfig struct {
	// Command is the path to the ripgrep binary.
	Command string
	// Args are the arguments to pass to ripgrep.
	Args []string
	// Argv0 is the argv[0] for the ripgrep process.
	Argv0 string
}

// Config holds the sandbox configuration.
type Config struct {
	// Backend specifies which sandbox backend to use.
	Backend Backend
	// ExcludedCommands is a list of command patterns that should not be sandboxed.
	ExcludedCommands []string
	// AllowUnsandboxedCommands allows commands to run without sandbox when true.
	// When false and sandbox is enabled, unsandboxed commands return an error.
	AllowUnsandboxedCommands bool
	// FailIfUnavailable returns an error if sandbox deps are missing.
	FailIfUnavailable bool
	// NetworkPolicy controls network access restrictions.
	NetworkPolicy NetworkPolicy
	// AllowedDomains is the list of allowed domains for network access.
	AllowedDomains []string
	// DeniedDomains is the list of denied domains (always blocked).
	DeniedDomains []string
	// Ripgrep is the sandboxed ripgrep configuration.
	Ripgrep RipgrepConfig
	// FilesystemAllowedDirs is the list of allowed directory paths.
	FilesystemAllowedDirs []string
	// FilesystemDenyDirs is the list of denied directory paths.
	FilesystemDenyDirs []string
}

// ErrSandboxUnavailable is returned when sandbox is required but unavailable.
var ErrSandboxUnavailable = errors.New("sandbox unavailable")

// ErrMissingDependency is returned when a sandbox dependency is missing.
type ErrMissingDependency struct {
	Backend     Backend
	Dependency  string
	InstallHint string
}

func (e *ErrMissingDependency) Error() string {
	return "sandbox backend " + string(e.Backend) + " missing dependency: " + e.Dependency + ". " + e.InstallHint
}
