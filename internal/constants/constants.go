// Package constants holds project-wide configuration.
package constants

import (
	"os"
	"path/filepath"
)

// ProjectName is the canonical name of the binary.
const ProjectName = "jenny"

// ProjectDirName is the project-local directory name (derived from ProjectName).
const ProjectDirName = "." + ProjectName // ".jenny"

// PluginDirName is the plugin marker directory name.
const PluginDirName = "." + ProjectName + "-plugin" // ".jenny-plugin"

// IgnoreFileName is the jenny-specific ignore file name.
const IgnoreFileName = "." + ProjectName + "ignore" // ".jennyignore"

// AgentsDirName is the standard shared agent directory name (~/.agents).
const AgentsDirName = ".agents"

// ProjectJennyDir returns the project-local .jenny directory path for the given cwd.
func ProjectJennyDir(cwd string) string {
	return filepath.Join(cwd, ProjectDirName)
}

// Version is the current version of jenny. Overridable at build time via
// `-ldflags '-X github.com/ipy/jenny/internal/constants.Version=<value>'`.
var Version = "0.4.0"

// JennyHomeDirFunc is the function that returns the jenny home directory.
// It can be overridden in tests.
var JennyHomeDirFunc = func() string {
	if h := os.Getenv("JENNY_HOME"); h != "" {
		return h
	}
	dirName := "." + ProjectName
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return dirName
	}
	return filepath.Join(home, dirName)
}

// JennyHomeDir returns the user's jenny home directory (~/.jenny).
// If os.UserHomeDir() fails, it falls back to a ".jenny" directory in the
// current working directory.
func JennyHomeDir() string {
	return JennyHomeDirFunc()
}

// AgentsHomeDirFunc is the function that returns the agents home directory.
// It can be overridden in tests.
var AgentsHomeDirFunc = func() string {
	if h := os.Getenv("JENNY_AGENTS_HOME"); h != "" {
		return h
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, AgentsDirName)
}

// AgentsHomeDir returns the standard shared agent directory (~/.agents).
// This is the conventional location used by Codex, Claude Code, and other
// agent tools. Returns "" if os.UserHomeDir() fails.
func AgentsHomeDir() string {
	return AgentsHomeDirFunc()
}

// SessionDir returns the directory for a specific session (~/.jenny/sessions/<sessionID>).
func SessionDir(sessionID string) string {
	if sessionID == "" {
		return JennyHomeDir()
	}
	return filepath.Join(JennyHomeDir(), "sessions", sessionID)
}

// ScratchpadDir returns the scratchpad directory path.
// If sessionID is provided, it returns a session-specific scratchpad (~/.jenny/sessions/<sessionID>/scratchpad).
func ScratchpadDir(sessionID ...string) string {
	if len(sessionID) > 0 && sessionID[0] != "" {
		return filepath.Join(SessionDir(sessionID[0]), "scratchpad")
	}
	return filepath.Join(JennyHomeDir(), "scratchpad")
}

// EnsureScratchpadDir creates the scratchpad directory for a session if it
// does not already exist. It is called at session startup so that the
// $JENNY_SCRATCHPAD env var and $JENNY_SCRATCHPAD/ path prefix always
// resolve to an existing directory before any tool execution.
func EnsureScratchpadDir(sessionID string) error {
	if sessionID == "" {
		return nil
	}
	return os.MkdirAll(ScratchpadDir(sessionID), 0755)
}

// SpillsDir returns the spills directory path.
// If sessionID is provided, it returns a session-specific spills directory (~/.jenny/sessions/<sessionID>/spills).
func SpillsDir(sessionID ...string) string {
	if len(sessionID) > 0 && sessionID[0] != "" {
		return filepath.Join(SessionDir(sessionID[0]), "spills")
	}
	return filepath.Join(JennyHomeDir(), "spills")
}

// MaxTombstoneRewriteBytes is the maximum file size (50 MiB) before a tombstone
// rewrite or full rewrite operation is refused to prevent OOM.
const MaxTombstoneRewriteBytes = 50 * 1024 * 1024

// MaxTranscriptReadBytes is the maximum transcript file size (100 MiB) that
// will be read into memory. Larger transcripts are truncated.
const MaxTranscriptReadBytes = 100 * 1024 * 1024

// MaxMemoryFileReadBytes is the maximum size (10 MiB) for reading memory
// files (MEMORY.md, per-topic .md files). Larger files are truncated.
const MaxMemoryFileReadBytes = 10 * 1024 * 1024

// MaxCLIPromptReadBytes is the maximum size (10 MiB) for reading CLI
// prompt files and stdin. Larger input is truncated.
const MaxCLIPromptReadBytes = 10 * 1024 * 1024

// MaxHTTPBodyReadBytes is the maximum size (10 MiB) for reading HTTP
// response bodies. Larger responses are truncated.
const MaxHTTPBodyReadBytes = 10 * 1024 * 1024

// MaxMCPResponseReadBytes is the maximum size (100 MiB) for reading MCP
// HTTP response bodies. Larger responses are truncated.
const MaxMCPResponseReadBytes = 100 * 1024 * 1024
