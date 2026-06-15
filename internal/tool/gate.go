// Package tool provides tool implementations.
package tool

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
)

// CommandGate provides security validation for bash and read commands.
type CommandGate struct {
	skipPermissions bool
}

// NewCommandGate creates a new CommandGate.
func NewCommandGate(skipPermissions bool) *CommandGate {
	return &CommandGate{skipPermissions: skipPermissions}
}

// CheckCommand validates a command against blocked patterns.
// Returns an error with a security message if the command is blocked.
func (g *CommandGate) CheckCommand(command string) error {
	if g.skipPermissions {
		return nil
	}

	// Universal Windows Security (AC4)
	if runtime.GOOS == "windows" {
		winGate := NewWindowsCommandGate(g.skipPermissions)
		if err := winGate.CheckCommand(command); err != nil {
			return err
		}
	}

	// Check for command substitution patterns
	if err := g.checkCommandSubstitution(command); err != nil {
		return err
	}

	// Check for process/zsh substitution patterns
	if err := g.checkProcessSubstitution(command); err != nil {
		return err
	}

	// Check for ANSI-C quoting (byte-encoding bypass)
	if err := g.checkANSICQuoting(command); err != nil {
		return err
	}

	// Check for brace expansion (path construction)
	if err := g.checkBraceExpansion(command); err != nil {
		return err
	}

	// Check for carriage return smuggling
	if err := g.checkCarriageReturnSmuggling(command); err != nil {
		return err
	}

	// Check for git injection patterns
	if err := g.checkGitInjection(command); err != nil {
		return err
	}

	return nil
}

// checkCommandSubstitution checks for command substitution patterns $(...), ${...}, `...`
func (g *CommandGate) checkCommandSubstitution(command string) error {
	// Check for $(...) command substitution
	if strings.Contains(command, "$(") {
		return fmt.Errorf("command substitution $(...) is not allowed")
	}

	// Check for ${...} variable substitution
	// Block all ${...} patterns as they could expand to sensitive values
	if strings.Contains(command, "${") {
		return fmt.Errorf("command substitution ${...} is not allowed")
	}

	// Check for backtick command substitution
	if strings.Contains(command, "`") {
		return fmt.Errorf("backtick command substitution is not allowed")
	}

	return nil
}

// checkANSICQuoting blocks ANSI-C quoting ($'...') which can encode arbitrary bytes
// to bypass path or character checks (e.g., $'\x2f' is '/').
func (g *CommandGate) checkANSICQuoting(command string) error {
	if strings.Contains(command, "$'") {
		return fmt.Errorf("ANSI-C quoting $'...' is not allowed")
	}
	return nil
}

// checkBraceExpansion blocks brace expansion patterns like {a,b} or {1..10}
// which can construct dangerous paths or arguments.
func (g *CommandGate) checkBraceExpansion(command string) error {
	for i := 0; i < len(command)-2; i++ {
		if command[i] == '{' {
			end := strings.IndexByte(command[i:], '}')
			if end < 0 {
				continue
			}
			inner := command[i+1 : i+end]
			if strings.Contains(inner, ",") || strings.Contains(inner, "..") {
				return fmt.Errorf("brace expansion {,} or {..} is not allowed")
			}
		}
	}
	return nil
}

// checkProcessSubstitution checks for process/zsh substitution patterns <(), >(), =(), $[...]
func (g *CommandGate) checkProcessSubstitution(command string) error {
	// Check for <() and >() process substitution
	if strings.Contains(command, "<(") || strings.Contains(command, ">(") {
		return fmt.Errorf("process substitution<() >() is not allowed")
	}

	// Check for =() zsh style command execution
	if strings.Contains(command, "=(") {
		return fmt.Errorf("zsh style command execution =() is not allowed")
	}

	// Check for =cmd pattern (equals prefix for command)
	if strings.Contains(command, "=cmd") {
		return fmt.Errorf("command alias pattern =cmd is not allowed")
	}

	// Check for $[...] arithmetic expansion (old bash style)
	if strings.Contains(command, "$[") {
		return fmt.Errorf("arithmetic expansion $[...] is not allowed")
	}

	// Check for ~[... globbing in zsh
	if strings.Contains(command, "~[") {
		return fmt.Errorf("zsh globbing ~[...] is not allowed")
	}

	return nil
}

// checkCarriageReturnSmuggling checks for \r characters that could smuggle commands
func (g *CommandGate) checkCarriageReturnSmuggling(command string) error {
	if strings.Contains(command, "\r") {
		return fmt.Errorf("carriage return character is not allowed")
	}
	return nil
}

// checkGitInjection checks for git config/exec-path injection attempts
func (g *CommandGate) checkGitInjection(command string) error {
	// Split into tokens to analyze git commands
	tokens := strings.Fields(command)

	for i, token := range tokens {
		if token == "git" && i+1 < len(tokens) {
			next := tokens[i+1]
			// Block git -c injection (config key=evil value)
			if next == "-c" {
				return fmt.Errorf("git config injection via -c flag is not allowed")
			}
			// Block git --exec-path=/tmp/evil
			if strings.HasPrefix(next, "--exec-path=") {
				return fmt.Errorf("git --exec-path injection is not allowed")
			}
			// Block git --config-env=/tmp/evil
			if strings.HasPrefix(next, "--config-env=") {
				return fmt.Errorf("git --config-env injection is not allowed")
			}
			// Block standalone --exec-path with = value
			if next == "--exec-path" && i+2 < len(tokens) {
				return fmt.Errorf("git --exec-path injection is not allowed")
			}
			if next == "--config-env" && i+2 < len(tokens) {
				return fmt.Errorf("git --config-env injection is not allowed")
			}
		}
	}

	return nil
}

// CheckPipelineSegments validates that all segments in a pipeline/chain are read-only.
// Handles pipes (|), logical OR (||), logical AND (&&), and semicolons (;).
// Returns an error if any segment is mutating.
func (g *CommandGate) CheckPipelineSegments(command string) error {
	if g.skipPermissions {
		return nil
	}

	// Split on all chain operators: ||, &&, ;, and |
	// Order matters: check || before | to avoid mis-splitting
	segments := splitCommandChain(command)
	if len(segments) == 0 {
		return nil
	}

	for _, segment := range segments {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}

		// Check for redirection operators in segment
		if strings.Contains(segment, ">") {
			return fmt.Errorf("output redirection is not allowed for security reasons")
		}

		// Check for bare $VAR references that could expand to unexpected values
		if containsBareVarExpansion(segment) {
			return fmt.Errorf("bare variable expansion $VAR is not allowed for security reasons")
		}

		// Check if segment's first word is in read-only allowlist
		if !isSegmentReadOnly(segment) {
			return fmt.Errorf("pipeline segment '%s' is not allowed for security reasons", strings.TrimSpace(segment))
		}
	}

	return nil
}

// splitCommandChain splits a command on ||, &&, ;, and | operators.
func splitCommandChain(command string) []string {
	var segments []string
	var current strings.Builder
	i := 0
	for i < len(command) {
		if i+1 < len(command) && (command[i:i+2] == "||" || command[i:i+2] == "&&") {
			segments = append(segments, current.String())
			current.Reset()
			i += 2
			continue
		}
		if command[i] == '|' || command[i] == ';' {
			segments = append(segments, current.String())
			current.Reset()
			i++
			continue
		}
		current.WriteByte(command[i])
		i++
	}
	if current.Len() > 0 {
		segments = append(segments, current.String())
	}
	return segments
}

// containsBareVarExpansion checks for bare $VAR patterns (not ${} or $() which are
// caught by checkCommandSubstitution). Allows $? (exit status) as it's read-only.
func containsBareVarExpansion(segment string) bool {
	for i := 0; i < len(segment)-1; i++ {
		if segment[i] == '$' {
			next := segment[i+1]
			if next == '(' || next == '{' || next == '[' || next == '\'' {
				return false // handled by other checks
			}
			if next == '?' || next == '#' || next == '@' || next == '*' || next == '-' {
				i++ // skip special vars (read-only shell builtins)
				continue
			}
			if (next >= 'A' && next <= 'Z') || (next >= 'a' && next <= 'z') || next == '_' {
				return true
			}
		}
	}
	return false
}

// isSegmentReadOnly checks if a pipeline segment is read-only.
// A segment is read-only if its first word is in the read-only allowlist
// and it doesn't contain any mutating operations.
func isSegmentReadOnly(segment string) bool {
	segment = strings.TrimSpace(segment)
	if segment == "" {
		return false
	}

	// Extract the first word (command)
	parts := strings.Fields(segment)
	if len(parts) == 0 {
		return false
	}

	cmd := parts[0]

	// Read-only command allowlist for pipeline segments
	readOnlyCommands := map[string]bool{
		"ls": true, "pwd": true, "whoami": true, "cat": true, "head": true, "tail": true,
		"grep": true, "find": true, "echo": true, "date": true, "which": true,
		"file": true, "stat": true, "diff": true, "wc": true, "type": true, "sleep": true,
		"cd": true,
	}

	if !readOnlyCommands[cmd] {
		return false
	}

	// Additional check: no redirection operators
	if strings.Contains(segment, ">") {
		return false
	}

	return true
}

// CheckDevicePathsInCommand scans all tokens in a command for device paths.
// Returns an error if any token references a blocked device path.
func (g *CommandGate) CheckDevicePathsInCommand(command string) error {
	if g.skipPermissions {
		return nil
	}
	for _, token := range strings.Fields(command) {
		if strings.HasPrefix(token, "-") {
			continue
		}
		if strings.HasPrefix(token, "/dev/") || strings.HasPrefix(token, "/proc/") {
			if err := g.CheckDevicePath(token); err != nil {
				return err
			}
		}
	}
	return nil
}

// CheckDevicePath validates that a path is not a device or proc path.
// Returns an error if the path is blocked.
func (g *CommandGate) CheckDevicePath(path string) error {
	if g.skipPermissions {
		return nil
	}

	// Universal Windows Security (AC4)
	if runtime.GOOS == "windows" {
		winGate := NewWindowsCommandGate(g.skipPermissions)
		if err := winGate.CheckPath(path); err != nil {
			return err
		}
	}

	// Normalize path
	path = filepath.Clean(path)

	// Block device paths
	devicePaths := []string{
		"/dev/null",
		"/dev/zero",
		"/dev/urandom",
		"/dev/random",
		"/dev/full",
		"/dev/stdin",
		"/dev/stdout",
		"/dev/stderr",
	}

	for _, dev := range devicePaths {
		if path == dev || strings.HasPrefix(path, dev+"/") {
			return fmt.Errorf("access to device path %s is blocked", path)
		}
	}

	// Block /dev/fd/* (file descriptor paths)
	if strings.HasPrefix(path, "/dev/fd/") {
		return fmt.Errorf("access to device path %s is blocked", path)
	}

	// Block /proc/self/fd/* (self file descriptor paths)
	if strings.HasPrefix(path, "/proc/self/fd/") {
		return fmt.Errorf("access to device path %s is blocked", path)
	}

	// Block /proc/*/environ paths
	if strings.HasPrefix(path, "/proc/") && strings.HasSuffix(path, "/environ") {
		return fmt.Errorf("access to %s is blocked", path)
	}

	// Block any /proc/*/environ pattern (specific PID environ)
	if strings.Contains(path, "/environ") {
		// Check if it's in /proc/*/environ format
		parts := strings.Split(path, "/")
		if len(parts) >= 3 && parts[1] == "proc" && parts[len(parts)-1] == "environ" {
			return fmt.Errorf("access to %s is blocked", path)
		}
	}

	return nil
}
