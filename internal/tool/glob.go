package tool

import (
	"bytes"
	"context"
	"fmt"
	"github.com/ipy/jenny/internal/tool/ignore"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// isGitRepo returns true if searchRoot is inside a git repository.
func isGitRepo(searchRoot string) bool {
	dir := searchRoot
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return false
}

const (
	maxResults = 100
	// AC8: maxDepth limits directory traversal depth (default 64).
	maxDepth = 64
)

// GlobTool finds files matching a glob pattern.
type GlobTool struct{}

// NewGlobTool creates a new GlobTool.
func NewGlobTool() *GlobTool {
	return &GlobTool{}
}

// Name returns the tool name.
func (t *GlobTool) Name() string {
	return "Glob"
}

// Description returns a description of the tool.
func (t *GlobTool) Description() string {
	return "Find files matching a glob pattern. Honors .gitignore and .jennyignore. Returns paths relative to cwd, sorted newest first, max 100 results."
}

// InputSchema returns the JSON schema for tool input.
func (t *GlobTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pattern": map[string]any{
				"type":        "string",
				"description": "Glob pattern to match",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "Directory to search (default: cwd)",
			},
		},
		"required": []string{"pattern"},
	}
}

// fileMatch holds a file path with its modification time for sorting.
type fileMatch struct {
	path  string
	mtime int64
}

// matchGlob handles ** glob pattern to match across directory separators.
func matchGlob(pattern, name string) bool {
	// Normalize path separators on Windows so both pattern and name use forward slashes.
	// filepath.Walk on Windows may use backslashes in relPath; the test pattern always uses forward slashes.
	if runtime.GOOS == "windows" {
		pattern = filepath.ToSlash(pattern)
		name = filepath.ToSlash(name)
	}

	// Handle ** matching any sequence of characters including separators
	if strings.Contains(pattern, "**") {
		// Split pattern by **
		segments := strings.Split(pattern, "**")
		if len(segments) == 1 {
			return pattern == name
		}

		// For ** at the start (e.g., **/*.txt), we need special handling
		// because ** can match the empty string (current directory)
		if segments[0] == "" {
			lastSegment := segments[len(segments)-1]

			// ** alone
			if lastSegment == "" {
				return true
			}

			// Extract just the filename pattern (e.g., "*.txt" from "/.txt" or "/foo/*.txt")
			parts := strings.Split(strings.TrimPrefix(lastSegment, "/"), "/")
			filenamePattern := parts[len(parts)-1]

			// Check if name ends with a filename matching the pattern
			// Find the last / to get the filename
			lastSlash := strings.LastIndex(name, "/")
			var filename string
			if lastSlash == -1 {
				filename = name
			} else {
				filename = name[lastSlash+1:]
			}

			// Check if filename matches the pattern
			matched, _ := filepath.Match(filenamePattern, filename)
			if matched {
				return true
			}

			// Also try matching the entire lastSegment pattern directly
			// This handles cases like **/foo/*.txt matching "bar/foo/baz.txt"
			if strings.Count(lastSegment, "/") >= 2 {
				// Multi-component suffix pattern
				trimmedPattern := strings.TrimPrefix(lastSegment, "/")
				matched, _ = filepath.Match(trimmedPattern, name)
				if matched {
					return true
				}
			}

			return false
		}

		// ** not at start: need to find the prefix segment in name
		prefix := segments[0]
		lastSegment := segments[len(segments)-1]
		idx := strings.Index(name, prefix)
		if idx != 0 {
			return false
		}

		nameAfterPrefix := name[len(prefix):]
		if lastSegment == "" {
			return true
		}

		// Now check if the remaining path could match the last segment
		// using the non-** pattern matcher
		matched, _ := filepath.Match(lastSegment, nameAfterPrefix)
		if matched {
			return true
		}

		// Also try matching with the first part of lastSegment removed
		// This handles cases like "src/**/test.txt" matching "src/foo/bar/test.txt"
		if strings.HasPrefix(lastSegment, "/") {
			restOfPattern := lastSegment[1:] // Remove leading /
			matched, _ = filepath.Match(restOfPattern, nameAfterPrefix)
			if matched {
				return true
			}
		}

		return false
	}

	// Simple pattern match using filepath.Match
	matched, _ := filepath.Match(pattern, name)
	return matched
}

// Execute finds files matching the glob pattern.
func (t *GlobTool) Execute(ctx context.Context, input map[string]any, cwd string) (*ToolResult, error) {
	pattern, ok := input["pattern"].(string)
	if !ok || pattern == "" {
		return nil, fmt.Errorf("pattern is required and must be a string")
	}

	searchRoot := cwd
	if pathVal, ok := input["path"].(string); ok && pathVal != "" {
		searchRoot = pathVal

		// Resolve relative path relative to cwd
		if !filepath.IsAbs(searchRoot) {
			searchRoot = filepath.Join(cwd, searchRoot)
		}

		// Check if path is a directory
		info, err := os.Stat(searchRoot)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("path is not a directory: %s (use cwd if unsure)", pathVal)
			}
			return nil, fmt.Errorf("path is not a directory: %s (use cwd if unsure)", pathVal)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("path is not a directory: %s (use cwd if unsure)", pathVal)
		}
	}

	// Universal Windows Security (AC4)
	if runtime.GOOS == "windows" {
		winGate := NewWindowsCommandGate(PermissionEdit)
		if err := winGate.CheckPath(searchRoot); err != nil {
			return &ToolResult{
				Content: fmt.Sprintf("Security error: %v", err),
				IsError: true,
			}, nil
		}
	}

	// Try ripgrep --files first (fast, honors .gitignore in git repos).
	// Only attempt ripgrep when inside a git repo, since ripgrep relies on
	// .git presence for .gitignore filtering.
	if isGitRepo(searchRoot) {
		matches, err := t.globWithRipgrep(searchRoot, pattern)
		if err == nil && len(matches) > 0 {
			return t.buildResult(matches, searchRoot, cwd)
		}
	}

	// Fallback: manual filepath.Walk (always respects .gitignore/.jennyignore)
	matches, err := t.globWithWalk(searchRoot, pattern)
	if err != nil {
		return nil, fmt.Errorf("glob error: %v", err)
	}
	return t.buildResult(matches, searchRoot, cwd)
}

// globWithRipgrep uses ripgrep --files --glob to find matching files.
// Returns nil if ripgrep is not available.
func (t *GlobTool) globWithRipgrep(searchRoot, pattern string) ([]fileMatch, error) {
	rgPath, err := exec.LookPath("rg")
	if err != nil {
		return nil, fmt.Errorf("ripgrep not found")
	}

	args := []string{
		"--files",
		"--glob", pattern,
		"--sortr", "modified",
		"--max-count", fmt.Sprintf("%d", maxResults+1),
		"--", searchRoot,
	}

	cmd := exec.Command(rgPath, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	output, err := cmd.Output()
	// ripgrep exit 1 means no matches found — fall through to walk.
	// Any other error (crash, bad flag) should surface.
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			// No matches from ripgrep — return nil matches so caller falls back
			return nil, nil
		}
		return nil, fmt.Errorf("ripgrep failed: %s", strings.TrimSpace(stderr.String()))
	}

	var matches []fileMatch
	absRoot, _ := filepath.Abs(searchRoot)
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line == "" {
			continue
		}
		// ripgrep returns absolute paths; relativize against searchRoot
		relPath := line
		if filepath.IsAbs(line) {
			relPath, _ = filepath.Rel(absRoot, line)
		}
		// Filter by depth (same as walk-based approach)
		depth := 1 + strings.Count(relPath, string(filepath.Separator))
		if depth > maxDepth {
			continue
		}
		// Stat for mtime to enable proper sorting
		var mtime int64
		if info, err := os.Stat(line); err == nil {
			mtime = info.ModTime().UnixNano()
		}
		matches = append(matches, fileMatch{path: relPath, mtime: mtime})
	}
	return matches, nil
}

// globWithWalk is the fallback when ripgrep is not available.
// It walks the directory tree and matches files manually.
func (t *GlobTool) globWithWalk(searchRoot, pattern string) ([]fileMatch, error) {
	ignorePatterns := ignore.LoadPatterns(searchRoot)
	var matches []fileMatch

	err := filepath.Walk(searchRoot, func(path string, info os.FileInfo, err error) error {
		relPath, err := filepath.Rel(searchRoot, path)
		if err != nil {
			return nil
		}

		depth := 1 + strings.Count(relPath, string(filepath.Separator))
		if depth > maxDepth {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if relPath != "." && ignore.Match(relPath, ignorePatterns) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if matchGlob(pattern, relPath) {
			if !info.IsDir() {
				mtime := info.ModTime().UnixNano()
				matches = append(matches, fileMatch{path: relPath, mtime: mtime})
			}
		}

		return nil
	})

	return matches, err
}

// buildResult sorts, truncates, and formats the matches for output.
func (t *GlobTool) buildResult(matches []fileMatch, searchRoot, cwd string) (*ToolResult, error) {
	// Sort by mtime descending (newest first), but only if mtimes are populated
	hasMtime := false
	for _, m := range matches {
		if m.mtime != 0 {
			hasMtime = true
			break
		}
	}
	if hasMtime {
		sort.Slice(matches, func(i, j int) bool {
			return matches[i].mtime > matches[j].mtime
		})
	}

	// Empty result
	if len(matches) == 0 {
		return &ToolResult{
			Content: "No files found",
			IsError: false,
		}, nil
	}

	// Cap at maxResults
	truncated := len(matches) > maxResults
	if truncated {
		matches = matches[:maxResults]
	}

	var content strings.Builder
	for i, m := range matches {
		if i > 0 {
			content.WriteString("\n")
		}
		var path string
		if searchRoot == cwd {
			path = m.path
		} else {
			absPath := filepath.Join(searchRoot, m.path)
			relToCwd, err := filepath.Rel(cwd, absPath)
			if err != nil {
				path = m.path
			} else {
				path = relToCwd
			}
		}
		if runtime.GOOS == "windows" {
			path = filepath.ToSlash(path)
		}
		content.WriteString(path)
	}

	return &ToolResult{
		Content:   content.String(),
		IsError:   false,
		Truncated: truncated,
	}, nil
}
