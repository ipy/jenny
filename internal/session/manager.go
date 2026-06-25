// Package session provides session persistence and resume functionality.
package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ipy/jenny/internal/constants"
	"github.com/ipy/jenny/internal/log"
	"github.com/ipy/jenny/internal/sweep"
	"github.com/ipy/jenny/internal/utils"
)

// Transcript entry type constants.
const (
	EntryTypeUser       = "user"
	EntryTypeAssistant  = "assistant"
	EntryTypeToolResult = "tool_result"
	EntryTypeState      = "state"
	EntryTypeSystem     = "system"
	EntryTypeProgress   = "progress"
)

// Transcript entry subtype constants.
const (
	SubtypeCompactBoundary = "compact_boundary"
	SubtypeSystemReminder  = "system_reminder"
)

// TranscriptEntry represents a single turn in the conversation transcript.
type TranscriptEntry struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	SessionID string    `json:"session_id"`
	UUID      string    `json:"uuid"`
	CWD       string    `json:"cwd"`
	Content   string    `json:"content,omitempty"`
	ToolUse   []ToolUse `json:"tool_use,omitempty"`
	ToolID    string    `json:"tool_id,omitempty"`
	IsError   bool      `json:"is_error,omitempty"`

	// Session state fields - used for session-level state persistence
	CompactFailCount int    `json:"compact_fail_count,omitempty"`
	SystemPrompt     string `json:"system_prompt,omitempty"`
	TasksJSON        string `json:"tasks_json,omitempty"`
	ToolNames        string `json:"tool_names,omitempty"`

	// Worktree state fields - used for worktree isolation and resume
	WorktreePath   string `json:"worktree_path,omitempty"`
	WorktreeBranch string `json:"worktree_branch,omitempty"`
	WorktreeCWD    string `json:"worktree_cwd,omitempty"`

	// Compaction boundary fields
	Subtype         string           `json:"subtype,omitempty"`
	CompactMetadata *CompactMetadata `json:"compact_metadata,omitempty"`

	// Thinking fields - for reasoning/thinking block persistence
	Thinking    string `json:"thinking,omitempty"`
	Signature   string `json:"signature,omitempty"`
	HasThinking bool   `json:"has_thinking,omitempty"`

	// Subagent fields - for subagent entries in the parent session transcript
	SubagentID   string `json:"subagent_id,omitempty"`
	SubagentName string `json:"subagent_name,omitempty"`

	// Pinned marks chain entries that must survive compaction verbatim.
	Pinned bool `json:"pinned,omitempty"`
}

// CompactMetadata holds metadata about a compaction boundary.
type CompactMetadata struct {
	Trigger          string `json:"trigger"`
	PreTokens        int    `json:"pre_tokens"`
	PreservedSegment int    `json:"preserved_segment"`
	Summary          string `json:"summary,omitempty"`
}

// ToolUse represents a tool call in the transcript.
type ToolUse struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Input     map[string]any `json:"input,omitempty"`
	Signature string         `json:"signature,omitempty"`
}

// Manager handles session persistence.
type Manager struct {
	jennyDir string
	Disabled bool

	// mu serializes transcript file access per Manager. We use a single
	// RWMutex rather than per-session locks because transcript files are
	// inherently a per-session resource and the Manager's working set
	// (tens of sessions at most) makes per-session locks unnecessary.
	// Locking table:
	//   AppendEntry          - Lock  (writes to the file)
	//   LoadTranscript       - RLock (reads the file)
	//   LoadCompactFailCount - RLock (reads the file)
	//   SessionExists        - RLock (reads the file metadata)
	//   UserMessageExists    - RLock (delegates to LoadTranscript)
	//   CheckRewriteSize     - RLock (reads the file size)
	//   AppendSystemPrompt   - Lock  (delegates to AppendEntry)
	mu sync.RWMutex
}

// progressTypes are entry types that are not chain participants and should be
// filtered when loading transcripts for chain rebuild.
var progressTypes = map[string]bool{
	EntryTypeProgress:     true,
	"bash_progress":       true,
	"powershell_progress": true,
	"mcp_progress":        true,
}

// NewManager creates a new session manager rooted at the given Jenny home
// directory. All session data is stored under <rootDir>/sessions/<id>/.
// If disabled is true, no files are created, appended, or modified.
func NewManager(rootDir string, disabled bool) (*Manager, error) {
	if rootDir == "" {
		rootDir = constants.JennyHomeDir()
	}
	if disabled {
		return &Manager{jennyDir: rootDir, Disabled: true}, nil
	}
	return &Manager{jennyDir: rootDir}, nil
}

// NewSessionID generates a new session ID as a lowercase UUID v7 string.
func NewSessionID() (string, error) {
	return utils.NewUUID(), nil
}

// ParseSessionID validates a session ID format.
func ParseSessionID(id string) string {
	if id == "" {
		return ""
	}
	// Accept any non-empty string as session ID (for resume)
	return id
}

// ValidateSessionID checks if a session ID is valid (no path traversal).
func ValidateSessionID(id string) error {
	if id == "" {
		return fmt.Errorf("session ID is required")
	}
	if utils.ContainsPathTraversal(id) {
		return fmt.Errorf("invalid session ID: path traversal not allowed")
	}
	return nil
}

// pidPath returns the path to the pid file for a session.
func (m *Manager) pidPath(sessionID string) string {
	if sessionID == "" || utils.ContainsPathTraversal(sessionID) {
		return ""
	}
	return filepath.Join(m.jennyDir, "sessions", sessionID, "pid")
}

// WritePID writes the current process PID to the session's pid file.
func (m *Manager) WritePID(sessionID string) error {
	if m.Disabled {
		return nil
	}
	path := m.pidPath(sessionID)
	if path == "" {
		return fmt.Errorf("invalid session ID")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating pid parent dir: %w", err)
	}
	return os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644)
}

// RemovePID removes the session's pid file.
func (m *Manager) RemovePID(sessionID string) error {
	if m.Disabled {
		return nil
	}
	path := m.pidPath(sessionID)
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// transcriptPath returns the path to the transcript file for a session.
func (m *Manager) transcriptPath(sessionID string) string {
	// Validate session ID doesn't contain path traversal attempts
	if sessionID == "" || utils.ContainsPathTraversal(sessionID) {
		return ""
	}
	return filepath.Join(m.jennyDir, "sessions", sessionID, "transcript.jsonl")
}

// AppendEntry appends a transcript entry to the session's transcript file.
// The write lock is held for the entire file I/O sequence (OpenFile, Fprintln,
// Close) so that a concurrent LoadTranscript cannot observe a partial line
// from an in-progress AppendEntry.
func (m *Manager) AppendEntry(sessionID string, entry TranscriptEntry) error {
	if m.Disabled {
		return nil
	}
	if sessionID == "" {
		return fmt.Errorf("session ID is required")
	}

	// Validate session ID doesn't contain path traversal
	if utils.ContainsPathTraversal(sessionID) {
		return fmt.Errorf("invalid session ID: path traversal not allowed")
	}

	entry.Timestamp = time.Now().UTC()
	entry.SessionID = sessionID
	entry.UUID = utils.NewUUID()
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshaling transcript entry: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	path := m.transcriptPath(sessionID)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating transcript parent directory: %w", err)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("opening transcript file: %w", err)
	}
	defer f.Close()

	if _, err := fmt.Fprintln(f, string(data)); err != nil {
		return fmt.Errorf("writing transcript entry: %w", err)
	}

	return nil
}

// UserMessageExists checks if a user message with the given content
// already exists in the transcript. This is used to avoid duplicate
// user message persistence when resuming a session.
// Returns false if the session does not exist.
func (m *Manager) UserMessageExists(sessionID string, content string) (bool, error) {
	entries, err := m.LoadTranscript(sessionID)
	if err != nil {
		if strings.Contains(err.Error(), "session not found") {
			return false, nil
		}
		return false, err
	}
	for _, entry := range entries {
		if entry.Type == EntryTypeUser && entry.Content == content {
			return true, nil
		}
	}
	return false, nil
}

// LoadTranscript loads all transcript entries for a session.
// Progress/ephemeral entries (progress, bash_progress, powershell_progress, mcp_progress)
// are filtered out since they are not chain participants.
// A read lock is held for the duration of file read + JSON parsing so that
// no AppendEntry can write a partial line that would be visible to the reader.
func (m *Manager) LoadTranscript(sessionID string) ([]TranscriptEntry, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("session ID is required")
	}

	if utils.ContainsPathTraversal(sessionID) {
		return nil, fmt.Errorf("invalid session ID: path traversal not allowed")
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	path := m.transcriptPath(sessionID)
	data, err := utils.ReadFileSafe(path, constants.MaxTranscriptReadBytes)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("session not found: %s", sessionID)
		}
		return nil, fmt.Errorf("reading transcript file: %w", err)
	}

	var entries []TranscriptEntry
	lines := utils.SplitLines(string(data))
	for _, line := range lines {
		if line == "" {
			continue
		}
		var entry TranscriptEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			// Log malformed lines as warnings to aid debugging,
			// but continue parsing remaining lines.
			log.Warn("Malformed JSON line in transcript",
				"session", sessionID,
				"line", truncateForLog(line, 200),
				"error", err.Error())
			continue
		}
		// Filter out progress/ephemeral entries
		if progressTypes[entry.Type] {
			continue
		}
		entries = append(entries, entry)
	}

	return entries, nil
}

// truncateForLog returns s truncated to at most max bytes using rune-aware
// slicing, ensuring no multi-byte code points are split. Appends "..."
// when truncation occurs. Uses utils.SafeTruncate internally.
func truncateForLog(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if len(s) <= max {
		return s
	}
	return utils.SafeTruncate(s, max) + "..."
}

// LoadCompactFailCount loads the most recent compactFailCount from the transcript.
// Returns 0 if no state entry is found.
func (m *Manager) LoadCompactFailCount(sessionID string) (int, error) {
	if sessionID == "" {
		return 0, fmt.Errorf("session ID is required")
	}

	if utils.ContainsPathTraversal(sessionID) {
		return 0, fmt.Errorf("invalid session ID: path traversal not allowed")
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	path := m.transcriptPath(sessionID)
	data, err := utils.ReadFileSafe(path, constants.MaxTranscriptReadBytes)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil // No transcript yet
		}
		return 0, fmt.Errorf("reading transcript file: %w", err)
	}

	var latestCount int
	lines := utils.SplitLines(string(data))
	for _, line := range lines {
		if line == "" {
			continue
		}
		var entry TranscriptEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		// Track the most recent state entry (regardless of count value)
		// to properly handle reset-to-zero after successful compaction
		if entry.Type == EntryTypeState {
			latestCount = entry.CompactFailCount
		}
	}

	return latestCount, nil
}

// SessionExists checks if a session transcript exists.
func (m *Manager) SessionExists(sessionID string) bool {
	if sessionID == "" {
		return false
	}
	if utils.ContainsPathTraversal(sessionID) {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	path := m.transcriptPath(sessionID)
	_, err := os.Stat(path)
	return err == nil
}

// CheckRewriteSize checks if the transcript file exceeds the maximum size for
// tombstone rewrite operations. Returns nil if the file is within limits or
// does not exist. Returns an error if the file is too large, preventing OOM
// during full rewrite operations.
func (m *Manager) CheckRewriteSize(sessionID string) error {
	if m.Disabled {
		return nil
	}
	if sessionID == "" {
		return fmt.Errorf("session ID is required")
	}
	if utils.ContainsPathTraversal(sessionID) {
		return fmt.Errorf("invalid session ID: path traversal not allowed")
	}

	path := m.transcriptPath(sessionID)
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // File doesn't exist, no rewrite needed
		}
		return fmt.Errorf("checking transcript file size: %w", err)
	}
	if info.Size() > constants.MaxTombstoneRewriteBytes {
		return fmt.Errorf("transcript file size %d exceeds maximum %d bytes for rewrite", info.Size(), constants.MaxTombstoneRewriteBytes)
	}
	return nil
}

// AppendSystemPrompt persists the frozen system prompt as a state entry.
// The entry is only written when the session is active and prompt is non-empty.
func (m *Manager) AppendSystemPrompt(sessionID string, systemPrompt string) error {
	if m.Disabled || sessionID == "" || systemPrompt == "" {
		return nil
	}
	return m.AppendEntry(sessionID, TranscriptEntry{
		Type:         EntryTypeState,
		SystemPrompt: systemPrompt,
	})
}

// LoadSystemPrompt loads the most recent system prompt from the transcript.
// Returns empty string if no state entry with system_prompt is found.
func (m *Manager) LoadSystemPrompt(sessionID string) (string, error) {
	if sessionID == "" {
		return "", nil
	}

	entries, err := m.LoadTranscript(sessionID)
	if err != nil {
		return "", err
	}

	var latest string
	for _, entry := range entries {
		if entry.Type == EntryTypeState && entry.SystemPrompt != "" {
			latest = entry.SystemPrompt
		}
	}
	return latest, nil
}

// AppendTaskStore persists the current task store state as a state entry.
func (m *Manager) AppendTaskStore(sessionID string, tasksJSON string) error {
	if m.Disabled || sessionID == "" {
		return nil
	}
	return m.AppendEntry(sessionID, TranscriptEntry{
		Type:      EntryTypeState,
		TasksJSON: tasksJSON,
	})
}

// LoadTaskStore loads the most recent task store state from the transcript.
// Returns empty string if no state entry with tasks_json is found.
func (m *Manager) LoadTaskStore(sessionID string) (string, error) {
	if sessionID == "" {
		return "", nil
	}

	entries, err := m.LoadTranscript(sessionID)
	if err != nil {
		return "", err
	}

	var latest string
	for _, entry := range entries {
		if entry.Type == EntryTypeState && entry.TasksJSON != "" {
			latest = entry.TasksJSON
		}
	}
	return latest, nil
}

// AppendToolNames persists the current tool name list as a state entry.
func (m *Manager) AppendToolNames(sessionID string, toolNames string) error {
	if m.Disabled || sessionID == "" || toolNames == "" {
		return nil
	}
	return m.AppendEntry(sessionID, TranscriptEntry{
		Type:      EntryTypeState,
		ToolNames: toolNames,
	})
}

// LoadToolNames loads the most recent tool name list from the transcript.
// Returns empty string if no state entry with tool_names is found.
func (m *Manager) LoadToolNames(sessionID string) (string, error) {
	if sessionID == "" {
		return "", nil
	}

	entries, err := m.LoadTranscript(sessionID)
	if err != nil {
		return "", err
	}

	var latest string
	for _, entry := range entries {
		if entry.Type == EntryTypeState && entry.ToolNames != "" {
			latest = entry.ToolNames
		}
	}
	return latest, nil
}

// LoadPostBoundaryMessages loads transcript entries after the last compaction boundary.
// This is used during session resume to exclude pre-boundary messages that have already
// been summarized.
//
// Returns all entries if no compaction boundary is found (preserves current behavior).
// A read lock is held for the duration of file read + JSON parsing.
func (m *Manager) LoadPostBoundaryMessages(sessionID string) ([]TranscriptEntry, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("session ID is required")
	}

	if utils.ContainsPathTraversal(sessionID) {
		return nil, fmt.Errorf("invalid session ID: path traversal not allowed")
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	path := m.transcriptPath(sessionID)
	data, err := utils.ReadFileSafe(path, constants.MaxTranscriptReadBytes)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("session not found: %s", sessionID)
		}
		return nil, fmt.Errorf("reading transcript file: %w", err)
	}

	// Single pass: build entries slice while tracking the last boundary position.
	// lastBoundaryIdx tracks the index in the filtered entries slice (not raw line index).
	lines := utils.SplitLines(string(data))
	var entries []TranscriptEntry
	var lastBoundaryIdx int = -1

	for _, line := range lines {
		if line == "" {
			continue
		}
		var entry TranscriptEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			log.Warn("Malformed JSON line in transcript",
				"session", sessionID,
				"line", truncateForLog(line, 200),
				"error", err.Error())
			continue
		}
		if progressTypes[entry.Type] {
			continue
		}
		if entry.Type == EntryTypeSystem && entry.Subtype == SubtypeCompactBoundary {
			lastBoundaryIdx = len(entries)
		}
		entries = append(entries, entry)
	}

	// If no boundary found, return all entries (current behavior)
	if lastBoundaryIdx == -1 {
		return entries, nil
	}

	// Return entries from the last boundary onwards (boundary is included)
	return entries[lastBoundaryIdx:], nil
}

// Flush flushes any pending writes to disk. Since writes are currently
// synchronous, this is a NOP but provides a hook for future buffered
// write implementation.
func (m *Manager) Flush() error {
	// Synchronous writes are already flushed by the OS.
	// This exists for future buffered write implementation.
	return nil
}

// sessionEntry is an intermediate type used by ListSessions for sorting.
type sessionEntry struct {
	id        string
	mtimeNano int64
}

// ListSessions returns session IDs sorted by modification time (most recent first).
// Only returns sessions with transcript.jsonl files.
func (m *Manager) ListSessions() ([]string, error) {
	var sessions []sessionEntry

	// Scan new sessions directory
	sessionsBaseDir := filepath.Join(m.jennyDir, "sessions")
	entries, err := utils.ReadDirSafe(sessionsBaseDir, 0)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading sessions directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		sessionID := entry.Name()
		// Check for transcript.jsonl inside the session directory
		transcriptPath := filepath.Join(sessionsBaseDir, sessionID, "transcript.jsonl")
		info, err := os.Stat(transcriptPath)
		if err != nil {
			continue
		}
		sessions = append(sessions, sessionEntry{
			id:        sessionID,
			mtimeNano: info.ModTime().UnixNano(),
		})
	}

	if len(sessions) == 0 {
		return nil, nil
	}

	// Sort by mtime descending (most recent first)
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].mtimeNano > sessions[j].mtimeNano
	})

	ids := make([]string, len(sessions))
	for i, s := range sessions {
		ids[i] = s.id
	}
	return ids, nil
}

// MaybeExtractArchive transparently rebuilds a session directory from a
// companion <id>.tar.gz archive created by `jenny sweep`.
//
// Resume-time flow:
//   - If <JENNY_HOME>/sessions/<id>/ already exists, this is a no-op.
//   - If the directory is absent but <JENNY_HOME>/sessions/<id>.tar.gz is
//     present, the archive is extracted into the sessions directory. After a
//     successful extraction the archive is removed unless keepArchive is
//     true (sourced from JENNY_SWEEP_KEEP_ARCHIVE / --sweep-keep-archive
//     via the unified koanf config layer; see docs/arch/koanf-config.md).
//   - If neither exists, the function returns nil — callers downstream will
//     surface the usual "session not found" error.
//
// This is the entry point used by `jenny --resume <id>` to make archive
// handling invisible to the rest of the resume flow.
func MaybeExtractArchive(sessionID string, keepArchive bool) error {
	if sessionID == "" {
		return fmt.Errorf("session ID is required")
	}
	if utils.ContainsPathTraversal(sessionID) {
		return fmt.Errorf("invalid session ID: path traversal not allowed")
	}

	home := constants.JennyHomeDir()
	sessionsDir := filepath.Join(home, "sessions")
	sessDir := filepath.Join(sessionsDir, sessionID)
	archivePath := filepath.Join(sessionsDir, sessionID+".tar.gz")

	// Already in directory form → nothing to do. We intentionally do NOT
	// delete a sibling archive here; if both exist we trust the directory
	// (the user may have placed the archive there for other reasons).
	if _, err := os.Stat(sessDir); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("checking session directory %s: %w", sessDir, err)
	}

	// No archive → nothing to extract. Caller will see "session not found".
	if _, err := os.Stat(archivePath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("checking archive %s: %w", archivePath, err)
	}

	if err := sweep.ExtractArchive(archivePath, sessionsDir, sessionID, keepArchive); err != nil {
		return fmt.Errorf("extracting archive for session %s: %w", sessionID, err)
	}
	return nil
}
