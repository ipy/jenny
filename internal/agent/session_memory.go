// Package agent provides the core agent loop and query engine.
package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ipy/jenny/internal/api"
	"github.com/ipy/jenny/internal/constants"
	"github.com/ipy/jenny/internal/log"
	"github.com/ipy/jenny/internal/tool"
)

// defaultSessionMemoryRetention is the default maximum number of session
// memory files kept in ~/.jenny/session-memory/. Override with the
// JENNY_SESSION_MEMORY_RETENTION environment variable.
const defaultSessionMemoryRetention = 200

// sessionMemoryRetention reads the JENNY_SESSION_MEMORY_RETENTION env var
// and returns the parsed positive integer, falling back to the default on
// missing/invalid input.
func sessionMemoryRetention() int {
	raw := os.Getenv("JENNY_SESSION_MEMORY_RETENTION")
	if raw == "" {
		return defaultSessionMemoryRetention
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return defaultSessionMemoryRetention
	}
	return n
}

// APIClient defines the interface for an LLM API client used for extraction.
type APIClient interface {
	SendMessage(ctx context.Context, messages []api.Message, tools []api.ToolParam, toolResults []api.ToolResult, systemPrompt []string, systemPromptSuffix string) (*api.Response, error)
}

// SessionMemory tracks session-level memory for long conversations.
// It maintains a background markdown notes file that captures session context
// incrementally as the conversation grows.
type SessionMemory struct {
	sessionID        string
	memdir           string
	compactCfg       CompactConfig
	accumTokens      int // Cumulative token count since last baseline
	toolCalls        int // Tool calls since last update
	lastBaseline     int // Token count at last memory update
	lastToolBaseline int // Tool calls at last memory update
	lastUpdateTime   time.Time
	memoryFilePath   string
	client           APIClient
	readCache        *tool.ReadFileCache
	timeoutOverride  time.Duration // If non-zero, used instead of default 15s timeout
}

// NewSessionMemory creates a new SessionMemory instance.
func NewSessionMemory(sessionID string, client APIClient, compactCfg CompactConfig) *SessionMemory {
	baseDir := constants.SessionDir(sessionID)
	memoryFilePath := filepath.Join(baseDir, "memory.md")

	return &SessionMemory{
		sessionID:        sessionID,
		memdir:           baseDir,
		compactCfg:       compactCfg,
		accumTokens:      0,
		toolCalls:        0,
		lastBaseline:     0,
		lastToolBaseline: 0,
		memoryFilePath:   memoryFilePath,
		client:           client,
		readCache:        tool.NewReadFileCache(),
	}
}

// cleanupOldSessionMemories removes the oldest *.md files in dir beyond
// the `keep` most recent (by mtime, newest first). Files that fail to stat
// or remove are skipped. A non-existent dir is fine — cleanup is a no-op.
func cleanupOldSessionMemories(dir string, keep int) {
	if keep <= 0 {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return // dir doesn't exist or is unreadable; nothing to do
	}

	type fileWithMtime struct {
		path  string
		mtime time.Time
	}
	files := make([]fileWithMtime, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, fileWithMtime{
			path:  filepath.Join(dir, e.Name()),
			mtime: info.ModTime(),
		})
	}

	if len(files) <= keep {
		return
	}

	// Newest first
	sort.Slice(files, func(i, j int) bool {
		return files[i].mtime.After(files[j].mtime)
	})

	for _, f := range files[keep:] {
		if err := os.Remove(f.path); err == nil {
			log.Debug("Evicted old session memory file", "path", f.path)
		}
	}
}

// WithMemdir sets a custom memory directory, overriding the default
// ~/.jenny/session-memory path. This is primarily for test isolation.
func (sm *SessionMemory) WithMemdir(dir string) *SessionMemory {
	sm.memdir = dir
	sm.memoryFilePath = filepath.Join(dir, sm.sessionID+".md")
	return sm
}

// SetTimeoutOverride sets a custom timeout for the Update operation.
// This is primarily for testing. A zero duration means "use default".
func (sm *SessionMemory) SetTimeoutOverride(d time.Duration) *SessionMemory {
	sm.timeoutOverride = d
	return sm
}

// effectiveTimeout returns the timeout to use for Update operations.
func (sm *SessionMemory) effectiveTimeout() time.Duration {
	if sm.timeoutOverride > 0 {
		return sm.timeoutOverride
	}
	return 15 * time.Second
}

// SetLastUpdateTime sets the lastUpdateTime for testing purposes.
func (sm *SessionMemory) SetLastUpdateTime(t time.Time) {
	sm.lastUpdateTime = t
}

// MemoryFilePath returns the path to the session memory file.
func (sm *SessionMemory) MemoryFilePath() string {
	return sm.memoryFilePath
}

// CheckThreshold evaluates whether to trigger a memory action based on
// accumulated token count and tool call count.
// Returns (shouldAct, action) where action is "init", "update", or "".
func (sm *SessionMemory) CheckThreshold(turnTokens int, toolCallCount int) (bool, string) {
	// AC5: First check if auto-compact is disabled - gate on auto-compact enabled
	// Session memory shares lifecycle with auto-compact
	if sm.compactCfg.DisableAutoCompact || sm.compactCfg.DisableCompact {
		return false, "disabled"
	}

	// Accumulate tokens
	sm.accumTokens += turnTokens
	sm.toolCalls += toolCallCount

	// Check for init: >= 15K tokens and no file exists
	if sm.lastBaseline == 0 && !sm.fileExists() {
		if sm.accumTokens >= 15000 {
			return true, "init"
		}
		return false, ""
	}

	// Check for update: >= 8K tokens since last baseline AND >= 3 tool calls since last tool baseline
	tokenGrowth := sm.accumTokens - sm.lastBaseline
	toolGrowth := sm.toolCalls - sm.lastToolBaseline

	if tokenGrowth >= 8000 && toolGrowth >= 3 {
		return true, "update"
	}

	return false, ""
}

// fileExists checks if the memory file already exists.
func (sm *SessionMemory) fileExists() bool {
	_, err := os.Stat(sm.memoryFilePath)
	return err == nil
}

// Init creates the session memory file with the initial template.
func (sm *SessionMemory) Init() error {
	// Ensure directory exists
	if err := os.MkdirAll(sm.memdir, 0755); err != nil {
		return fmt.Errorf("creating session memory directory: %w", err)
	}

	// Create template
	timestamp := time.Now().UTC().Format(time.RFC3339)
	content := fmt.Sprintf("# Session Memory: %s\nCreated: %s\n\n## Context / Goals\n\n## Key Decisions\n\n## Current State\n\n## Open Questions\n\n", sm.sessionID, timestamp)

	// Write with 0600 permissions
	if err := os.WriteFile(sm.memoryFilePath, []byte(content), 0600); err != nil {
		return fmt.Errorf("creating session memory file: %w", err)
	}

	// Record read in cache for edit validation
	sm.readCache.RecordRead(sm.memoryFilePath, content, time.Now(), true, 0, 0)

	// Set baselines so Update() coalescing guards don't immediately fire
	sm.lastBaseline = sm.accumTokens
	sm.lastToolBaseline = sm.toolCalls
	sm.lastUpdateTime = time.Now()

	log.Debug("Session memory file created", "path", sm.memoryFilePath)
	return nil
}

// Update invokes a forked sub-agent to update the session memory file.
// It uses a 15-second timeout (or override) and Edit-only tool access.
func (sm *SessionMemory) Update(ctx context.Context) error {
	// AC4: Stale in-flight check - skip if last update was >60s ago
	if !sm.lastUpdateTime.IsZero() && time.Since(sm.lastUpdateTime) > 60*time.Second {
		log.Debug("Session memory update skipped: stale in-flight")
		return nil
	}

	// AC5: Coalescing window check - skip if last update was <15s ago
	if !sm.lastUpdateTime.IsZero() && time.Since(sm.lastUpdateTime) < sm.effectiveTimeout() {
		log.Debug("Session memory update skipped: within coalescing window")
		return nil
	}

	// Check if file exists - if not, recreate it
	if !sm.fileExists() {
		if err := sm.Init(); err != nil {
			return fmt.Errorf("recreating session memory file: %w", err)
		}
	}

	// Read current content and get mtime
	info, err := os.Stat(sm.memoryFilePath)
	if err != nil {
		return fmt.Errorf("reading session memory file stats: %w", err)
	}
	currentContent, err := os.ReadFile(sm.memoryFilePath)
	if err != nil {
		return fmt.Errorf("reading session memory file: %w", err)
	}

	// Record read in cache so Edit tool's read-before-write check passes
	sm.readCache.RecordRead(sm.memoryFilePath, string(currentContent), info.ModTime(), true, 0, 0)

	// Create context with timeout (default 15s, or override)
	ctx, cancel := context.WithTimeout(ctx, sm.effectiveTimeout())
	defer cancel()

	// Build prompt for the forked agent
	prompt := sm.buildUpdatePrompt(string(currentContent))

	// Create restricted tool set (Edit only, allowed path is memory file)
	editTool := tool.NewEditTool(sm.readCache)
	editTool.SetAllowedPaths([]string{sm.memoryFilePath})
	tools := []tool.Tool{editTool}

	// Build tool params
	toolParams := make([]api.ToolParam, 0, len(tools))
	for _, t := range tools {
		schema := t.InputSchema()
		props := make(map[string]any)
		if p, ok := schema["properties"].(map[string]any); ok {
			props = p
		}
		var required []string
		if req, ok := schema["required"].([]string); ok {
			required = req
		} else if reqAny, ok := schema["required"].([]any); ok {
			for _, r := range reqAny {
				if s, ok := r.(string); ok {
					required = append(required, s)
				}
			}
		}

		// Extract extra fields ($defs, $schema, etc.) for third-party API compatibility
		extraFields := make(map[string]any)
		for k, v := range schema {
			if k != "type" && k != "properties" && k != "required" {
				extraFields[k] = v
			}
		}

		toolParams = append(toolParams, api.ToolParam{
			Name:        t.Name(),
			Description: t.Description(),
			InputSchema: api.ToolInputSchema{
				Type:        "object",
				Properties:  props,
				Required:    required,
				ExtraFields: extraFields,
			},
		})
	}

	// Make the API call with timeout
	messages := []api.Message{
		{
			Role:    "user",
			Content: prompt,
		},
	}

	systemPrompt := "You are a helpful assistant that updates session memory files. You may only use the Edit tool to modify the session memory file. Focus on summarizing recent context and updating the relevant sections."

	resp, err := sm.client.SendMessage(ctx, messages, toolParams, nil, []string{systemPrompt}, "")
	if err != nil {
		// Check if context deadline exceeded
		if ctx.Err() == context.DeadlineExceeded {
			log.Warn("Session memory update timed out after 15 seconds")
			return nil // AC3: Don't block main loop on timeout
		}
		return fmt.Errorf("forked agent call: %w", err)
	}

	// Track whether any edit actually succeeded
	anyEditSucceeded := false

	// Process response - handle tool_use blocks and text content
	for _, block := range resp.Content {
		if block.Type == "tool_use" && block.ToolUse != nil {
			// Execute the edit tool
			input := block.ToolUse.Args
			cwd := "/" // Using allowedPaths, so cwd doesn't matter for path validation
			result, err := editTool.Execute(ctx, input, cwd)
			if err != nil {
				log.Warn("Session memory edit failed", "error", err, "path", sm.memoryFilePath)
				continue
			}
			if !result.IsError {
				anyEditSucceeded = true
			}
			log.Debug("Edit tool executed", "toolUseID", block.ToolUse.ID, "isError", result.IsError)
		}
		// Text blocks are informational only - no need to capture for summary
	}

	// Update baselines
	sm.lastBaseline = sm.accumTokens
	sm.lastToolBaseline = sm.toolCalls
	sm.lastUpdateTime = time.Now()

	if anyEditSucceeded {
		log.Info("Session memory updated", "path", sm.memoryFilePath)
	} else {
		log.Debug("Session memory: no changes made", "path", sm.memoryFilePath)
	}
	return nil
}

// buildUpdatePrompt builds the prompt for the forked agent.
func (sm *SessionMemory) buildUpdatePrompt(existingContent string) string {
	var sb strings.Builder
	sb.WriteString("Update the session memory markdown file at ")
	sb.WriteString(sm.memoryFilePath)
	sb.WriteString(".\n\nYou may use the Edit tool only. Current content:\n\n")
	sb.WriteString(existingContent)
	sb.WriteString("\n\nRecent:\n\nSummarize any new context, decisions, or state changes that have occurred in this session. Update the relevant sections (Context / Goals, Key Decisions, Current State, Open Questions) based on what you know about the conversation.")
	return sb.String()
}

// ResetBaselines resets the token and tool call baselines after a memory update.
func (sm *SessionMemory) ResetBaselines() {
	sm.lastBaseline = sm.accumTokens
	sm.lastToolBaseline = sm.toolCalls
	sm.lastUpdateTime = time.Now()
}
