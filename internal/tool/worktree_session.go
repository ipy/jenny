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

// SetWorktree marks the session as in a worktree with the given path.
func (s *WorktreeSession) SetWorktree(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inWorktree = true
	s.worktreeDir = path
}

// ClearWorktree marks the session as no longer in a worktree.
func (s *WorktreeSession) ClearWorktree() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inWorktree = false
	s.worktreeDir = ""
}
