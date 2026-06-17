package tool

import (
	"fmt"
)

// PermissionLevel represents a five-level capability ladder for tool execution.
// Each level adds exactly one core capability over the previous, creating a predictable capability ladder.
//
// See docs/patterns/permission-levels.md for the full specification.
type PermissionLevel int

const (
	// PermissionRead blocks all Bash execution and file writes.
	// Structured read-only tools only (Read, Grep, Glob, etc.).
	PermissionRead PermissionLevel = iota

	// PermissionAnalyze extends read with Bash commands that are provably
	// non-mutating (18-command read-only allowlist). Write/Edit blocked.
	PermissionAnalyze

	// PermissionEdit extends analyze with file write capability via
	// Write/Edit tools. Bash still restricted to read-only allowlist.
	// This is the current default behavior.
	PermissionEdit

	// PermissionExecute extends edit by flipping Bash pipeline validation
	// from allowlist (default-deny) to skipped. CheckCommand() pattern
	// blocks remain enforced. Write/Edit rules unchanged from edit.
	PermissionExecute

	// PermissionUnrestricted disables all safety gates.
	// Equivalent to --dangerously-skip-permissions.
	PermissionUnrestricted
)

// DefaultPermissionLevel is the default when neither --permission-level nor
// --dangerously-skip-permissions is specified. Matches current behavior.
var DefaultPermissionLevel = PermissionEdit

// String returns the lowercase name of the permission level.
func (l PermissionLevel) String() string {
	switch l {
	case PermissionRead:
		return "read"
	case PermissionAnalyze:
		return "analyze"
	case PermissionEdit:
		return "edit"
	case PermissionExecute:
		return "execute"
	case PermissionUnrestricted:
		return "unrestricted"
	default:
		return fmt.Sprintf("unknown(%d)", l)
	}
}

// ParsePermissionLevel parses a permission level string.
// Returns an error for invalid values.
func ParsePermissionLevel(s string) (PermissionLevel, error) {
	switch s {
	case "read":
		return PermissionRead, nil
	case "analyze":
		return PermissionAnalyze, nil
	case "edit":
		return PermissionEdit, nil
	case "execute":
		return PermissionExecute, nil
	case "unrestricted":
		return PermissionUnrestricted, nil
	default:
		return 0, fmt.Errorf("invalid permission level %q; valid values: read, analyze, edit, execute, unrestricted", s)
	}
}

// BashAllowed reports whether Bash execution is allowed at this level.
// read: blocked; analyze+: allowed.
func (l PermissionLevel) BashAllowed() bool {
	return l >= PermissionAnalyze
}

// PipelineEnforced reports whether CheckPipelineSegments() is enforced.
// read: N/A (Bash blocked); analyze/edit: enforced (allowlist);
// execute/unrestricted: skipped.
func (l PermissionLevel) PipelineEnforced() bool {
	return l >= PermissionAnalyze && l <= PermissionEdit
}

// CommandChecked reports whether CheckCommand() pattern blocks are enforced.
// read: N/A (Bash blocked); analyze/edit/execute: enforced;
// unrestricted: skipped.
func (l PermissionLevel) CommandChecked() bool {
	return l >= PermissionAnalyze && l <= PermissionExecute
}

// WriteAllowed reports whether Write/Edit tools are allowed at this level.
// read/analyze: blocked; edit+: allowed.
func (l PermissionLevel) WriteAllowed() bool {
	return l >= PermissionEdit
}

// PathConstrained reports whether PathInWorkingDir() is enforced for
// Write/Edit and validateCommandPaths() for Bash.
// read–execute: enforced; unrestricted: skipped.
func (l PermissionLevel) PathConstrained() bool {
	return l <= PermissionExecute
}

// ReadBeforeWrite reports whether Write/Edit require a prior Read of the
// same path. read–execute: enforced; unrestricted: skipped.
func (l PermissionLevel) ReadBeforeWrite() bool {
	return l <= PermissionExecute
}

// ResolvePermissionLevel resolves the effective permission level from the
// --dangerously-skip-permissions flag and the --permission-level string value.
//
// AC5: --dangerously-skip-permissions maps to unrestricted.
// AC6: both specified → unrestricted + warning logged.
// If permLevelStr is empty, DefaultPermissionLevel (edit) is used as fallback.
// Returns the resolved level and an optional warning message.
func ResolvePermissionLevel(skipPerms bool, permLevelStr string) (PermissionLevel, string) {
	// Parse the permission level string (if provided)
	var permLevel PermissionLevel
	if permLevelStr != "" {
		var err error
		permLevel, err = ParsePermissionLevel(permLevelStr)
		if err != nil {
			permLevel = DefaultPermissionLevel
		}
	} else {
		permLevel = DefaultPermissionLevel
	}

	// AC5: --dangerously-skip-permissions maps to unrestricted
	if skipPerms {
		resolved := PermissionUnrestricted
		// AC6: warn if both specified and they conflict
		if permLevelStr != "" && permLevel != PermissionUnrestricted {
			warning := fmt.Sprintf(
				"warning: --dangerously-skip-permissions overrides --permission-level %s; using unrestricted",
				permLevelStr,
			)
			return resolved, warning
		}
		return resolved, ""
	}

	return permLevel, ""
}
