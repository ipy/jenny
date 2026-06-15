// Package tool provides the tool interface and implementations.
package tool

import (
	"sync"
)

// WorktreeSession tracks the current git worktree session state.
// It is shared between EnterWorktreeTool and ExitWorktreeTool to ensure
// ExitWorktree can observe and act on worktrees created by EnterWorktree.
type WorktreeSession struct {
	mu          sync.Mutex
	inWorktree  bool
	worktreeDir string
	originalCwd string // cwd before entering worktree, restored on exit
}

// IsInWorktree returns true if currently in a worktree session.
func (s *WorktreeSession) IsInWorktree() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inWorktree
}

// WorktreeDir returns the current worktree directory path.
func (s *WorktreeSession) WorktreeDir() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.worktreeDir
}

// OriginalCwd returns the cwd from before the worktree was entered.
func (s *WorktreeSession) OriginalCwd() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.originalCwd
}

// EffectiveCwd returns the worktree dir if in a worktree, otherwise empty.
// Callers should use this to override cwd when a worktree is active.
func (s *WorktreeSession) EffectiveCwd() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inWorktree {
		return s.worktreeDir
	}
	return ""
}

// SetWorktree marks the session as in a worktree with the given path.
// originalCwd is saved for restoration on exit.
func (s *WorktreeSession) SetWorktree(path string, originalCwd string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inWorktree = true
	s.worktreeDir = path
	s.originalCwd = originalCwd
}

// ClearWorktree marks the session as no longer in a worktree.
// Returns the original cwd that should be restored.
func (s *WorktreeSession) ClearWorktree() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inWorktree = false
	s.worktreeDir = ""
	orig := s.originalCwd
	s.originalCwd = ""
	return orig
}
