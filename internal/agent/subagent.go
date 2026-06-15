// Package agent provides the core agent loop and subagent types.
package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ipy/jenny/internal/api"
	"github.com/ipy/jenny/internal/api/router"
	"github.com/ipy/jenny/internal/git"
	"github.com/ipy/jenny/internal/session"
	"github.com/ipy/jenny/internal/tool"
)

// SubagentType defines a built-in subagent type with distinct tool allowlists,
// models, and resume semantics.
type SubagentType struct {
	Name                    string
	Description             string
	allowedTools            []string
	deniedTools             []string
	model                   string
	oneShot                 bool
	omitProjectInstructions bool
	mcpServers              []string
}

// FilterTools returns a filtered allowlist excluding denied tools.
// If allowedTools contains "*", start with all known tools and subtract denied.
// Otherwise, start with allowedTools and remove any entries in denied or deniedTools.
// Deny rules support wildcard suffix (e.g., "mcp__server__*" denies all tools with that prefix).
// Returns a new slice (does not mutate the type).
func (t SubagentType) FilterTools(denied []string) []string {
	// Build combined deny list: explicit denied + type's deniedTools
	allDenied := make([]string, 0, len(denied)+len(t.deniedTools))
	allDenied = append(allDenied, denied...)
	allDenied = append(allDenied, t.deniedTools...)

	// If allowedTools contains "*", return all tools except denied
	if len(t.allowedTools) == 1 && t.allowedTools[0] == "*" {
		allTools := []string{
			"Read", "Write", "Edit", "Bash", "Glob", "Grep",
			"WebSearch", "WebFetch", "LSP", "Skill", "NotebookEdit", "ReadMcpResource",
			"TaskOutput", "TaskStop", "Task", "CronCreate", "CronDelete", "CronList",
		}
		var result []string
		for _, tool := range allTools {
			if !matchesDenyRule(tool, allDenied) {
				result = append(result, tool)
			}
		}
		return result
	}

	// Otherwise, filter from allowedTools in single pass
	var result []string
	for _, tool := range t.allowedTools {
		if !matchesDenyRule(tool, allDenied) {
			result = append(result, tool)
		}
	}
	return result
}

// matchesDenyRule checks if a tool name matches any deny rule.
// Supports exact match and wildcard suffix (e.g., "mcp__server__*").
func matchesDenyRule(toolName string, denyRules []string) bool {
	for _, rule := range denyRules {
		if rule == toolName {
			return true
		}
		if strings.HasSuffix(rule, "*") {
			prefix := strings.TrimSuffix(rule, "*")
			if strings.HasPrefix(toolName, prefix) {
				return true
			}
		}
	}
	return false
}

// CanResume returns whether this subagent type supports resuming a session.
// One-shot types return false.
func (t SubagentType) CanResume() bool {
	return !t.oneShot
}

// AllowedTools returns a copy of the type's allowed tools list.
func (t SubagentType) AllowedTools() []string {
	result := make([]string, len(t.allowedTools))
	copy(result, t.allowedTools)
	return result
}

// RequiredMCPServers returns a copy of the type's required MCP servers list.
func (t SubagentType) RequiredMCPServers() []string {
	result := make([]string, len(t.mcpServers))
	copy(result, t.mcpServers)
	return result
}

// BuiltinTypes returns all five built-in subagent types.
func BuiltinTypes() []SubagentType {
	return []SubagentType{
		GeneralPurpose,
		Explore,
		Plan,
		Shell,
		Verification,
	}
}

// FindBuiltin returns a built-in type by name, or nil if not found.
func FindBuiltin(name string) *SubagentType {
	for _, t := range BuiltinTypes() {
		if t.Name == name {
			return &t
		}
	}
	return nil
}

// GeneralPurpose is the default subagent type with all tools allowed.
var GeneralPurpose = SubagentType{
	Name:                    "general-purpose",
	Description:             "Default subagent for general tasks",
	allowedTools:            []string{"*"},
	deniedTools:             []string{},
	model:                   "inherit",
	oneShot:                 false,
	omitProjectInstructions: false,
	mcpServers:              []string{},
}

// Explore is a read-only subagent type for exploration tasks.
var Explore = SubagentType{
	Name:                    "explore",
	Description:             "Read-only exploration agent for searching and reading files",
	allowedTools:            []string{"Read", "Glob", "Grep", "Bash"},
	deniedTools:             []string{"Write", "Edit", "Agent"},
	model:                   "inherit",
	oneShot:                 true,
	omitProjectInstructions: false,
	mcpServers:              []string{},
}

// Plan is a read-only subagent type for planning tasks.
var Plan = SubagentType{
	Name:                    "plan",
	Description:             "Read-only planning agent for analysis and design",
	allowedTools:            []string{"Read", "Glob", "Grep"},
	deniedTools:             []string{"Write", "Edit", "Bash", "Agent"},
	model:                   "inherit",
	oneShot:                 true,
	omitProjectInstructions: true,
	mcpServers:              []string{},
}

// Shell is a subagent type focused on shell command execution.
var Shell = SubagentType{
	Name:                    "shell",
	Description:             "Shell-focused agent for command execution",
	allowedTools:            []string{"Bash", "Read", "Glob", "Grep"},
	deniedTools:             []string{},
	model:                   "inherit",
	oneShot:                 false,
	omitProjectInstructions: false,
	mcpServers:              []string{},
}

// Verification is a subagent type for CI-style verification tasks.
var Verification = SubagentType{
	Name:                    "verification",
	Description:             "Verification agent for running tests and CI checks",
	allowedTools:            []string{"Read", "TaskOutput", "TaskStop"},
	deniedTools:             []string{"Write", "Edit"},
	model:                   "inherit",
	oneShot:                 false,
	omitProjectInstructions: false,
	mcpServers:              []string{},
}

// modelAliases maps model alias names to concrete model identifiers.
var modelAliases = map[string]string{
	"sonnet": "claude-sonnet-4-20250514",
	"opus":   "claude-opus-4-20250514",
	"haiku":  "claude-haiku-4-20250514",
}

// ResolveModel resolves a model alias to its concrete model identifier.
// If the alias is unknown, returns the input unchanged.
func ResolveModel(alias string) string {
	if resolved, ok := modelAliases[strings.ToLower(alias)]; ok {
		return resolved
	}
	return alias
}

// LocalSubagentRunner runs subagents in the local process.
type LocalSubagentRunner struct {
	client            api.Requester
	tools             []tool.Tool
	denyRules         map[string]bool
	sessionMgr        *session.Manager
	parentConfig      *StreamConfig // Parent's StreamConfig for inheritance when Name is set (AC3)
	capturedStreamCfg *StreamConfig // Captured StreamConfig for testing verification (AC2, AC4)
	parentSessionID   string        // Parent session ID for transcript/cost integration
}

// NewLocalSubagentRunner creates a new LocalSubagentRunner.
func NewLocalSubagentRunner(tools []tool.Tool, denyRules map[string]bool, client api.Requester) *LocalSubagentRunner {
	if denyRules == nil {
		denyRules = make(map[string]bool)
	}
	return &LocalSubagentRunner{
		client:    client,
		tools:     tools,
		denyRules: denyRules,
	}
}

// SetClient sets the API client for the runner.
func (r *LocalSubagentRunner) SetClient(client api.Requester) {
	r.client = client
}

// SetSessionManager sets the session manager for transcript persistence.
func (r *LocalSubagentRunner) SetSessionManager(mgr *session.Manager) {
	r.sessionMgr = mgr
}

// SetParentConfig sets the parent StreamConfig for inheritance when Name is set (AC3).
func (r *LocalSubagentRunner) SetParentConfig(cfg *StreamConfig) {
	r.parentConfig = cfg
}

// SetParentSessionID sets the parent session ID for transcript/cost integration.
func (r *LocalSubagentRunner) SetParentSessionID(sessionID string) {
	r.parentSessionID = sessionID
}

// GetCapturedStreamConfig returns the StreamConfig most recently constructed in RunSubagent.
// Used by tests to verify IsNamedAgent and inherited field propagation (AC2, AC4).
func (r *LocalSubagentRunner) GetCapturedStreamConfig() *StreamConfig {
	return r.capturedStreamCfg
}

// GetCapturedStreamConfigInfo returns info about the captured StreamConfig as a map.
// Implements tool.SubagentRunner.GetCapturedStreamConfigInfo.
func (r *LocalSubagentRunner) GetCapturedStreamConfigInfo() map[string]any {
	cfg := r.capturedStreamCfg
	if cfg == nil {
		return nil
	}
	return map[string]any{
		"IsNamedAgent":         cfg.IsNamedAgent,
		"MaxBudgetUSD":         cfg.MaxBudgetUSD,
		"MaxTurns":             cfg.MaxTurns,
		"CustomSystemPrompt":   cfg.CustomSystemPrompt,
		"AppendSystemPrompt":   cfg.AppendSystemPrompt,
		"OverrideSystemPrompt": cfg.OverrideSystemPrompt,
		"StructuredSchema":     cfg.StructuredSchema,
		"StructuredDenyRules":  cfg.StructuredDenyRules,
	}
}

// RunSubagent runs a subagent with the given parameters.
func (r *LocalSubagentRunner) RunSubagent(ctx context.Context, params tool.SubagentParams) (*tool.SubagentResult, error) {
	// Validate subagent type
	subagentType := FindBuiltin(params.SubagentType)
	if subagentType == nil {
		validTypes := make([]string, 0, len(BuiltinTypes()))
		for _, t := range BuiltinTypes() {
			validTypes = append(validTypes, t.Name)
		}
		return nil, fmt.Errorf("invalid subagent_type %q: valid types are [%s]", params.SubagentType, strings.Join(validTypes, ", "))
	}

	// Build deny list from runner's deny rules
	var denyList []string
	for name := range r.denyRules {
		denyList = append(denyList, name)
	}

	// Build the tool list for the subagent
	var subagentTools []tool.Tool

	// AC3-tool-inheritance: Named agents (params.Name != "") get the parent's full tool registry
	// Unnamed subagents get the subagent-type-filtered tool list
	if params.Name != "" {
		// Named agent: inherit parent's full tool registry (no filtering)
		subagentTools = r.tools
	} else {
		// Unnamed subagent: filter by subagent type
		allowedToolNames := subagentType.FilterTools(denyList)
		for _, toolName := range allowedToolNames {
			t := tool.FindTool(r.tools, toolName)
			if t != nil {
				subagentTools = append(subagentTools, t)
			}
		}
	}

	// Determine working directory and handle worktree isolation
	cwd := params.CWD
	var worktreePath string
	var cleanupWorktree bool

	// AC2: Worktree isolation - mutually exclusive with cwd
	if params.Isolation == "worktree" {
		if params.CWD != "" {
			return nil, fmt.Errorf("worktree isolation is mutually exclusive with cwd")
		}
		// Validate we're in a git repo
		repoRoot, err := git.GetRoot("")
		if err != nil {
			return nil, fmt.Errorf("worktree isolation requires git repository: %w", err)
		}
		// Generate unique branch name based on agent type and timestamp
		branchName := fmt.Sprintf("worktree-%s-%d", params.SubagentType, time.Now().UnixNano())
		worktreePath, err = git.CreateWorktree(repoRoot, branchName)
		if err != nil {
			return nil, fmt.Errorf("creating worktree: %w", err)
		}
		cwd = worktreePath
		cleanupWorktree = true

		// Persist worktree state to transcript if session manager available (AC5)
		if r.sessionMgr != nil {
			// Generate a session ID for the subagent to track worktree state
			subagentSessionID := "subagent-" + params.SubagentType
			_ = r.sessionMgr.AppendEntry(subagentSessionID, session.TranscriptEntry{
				Type:           "worktree_state",
				WorktreePath:   worktreePath,
				WorktreeBranch: branchName,
				WorktreeCWD:    cwd,
			})
		}
	}

	// Build stream config for the subagent
	streamCfg := &StreamConfig{
		Enabled:      false, // Subagent runs without streaming
		Verbose:      false,
		IsForkChild:  true,              // Mark as fork child for recursive fork detection
		IsNamedAgent: params.Name != "", // Mark as named agent for nested name blocking (AC1)
	}

	// AC3-streamconfig-inheritance: Named agents inherit parent config fields
	if params.Name != "" && r.parentConfig != nil {
		streamCfg.MCPConfig = r.parentConfig.MCPConfig
		streamCfg.AutoMemoryEnabled = r.parentConfig.AutoMemoryEnabled
		streamCfg.MemoryContent = r.parentConfig.MemoryContent
		streamCfg.ReadFileCache = r.parentConfig.ReadFileCache
		streamCfg.Skills = r.parentConfig.Skills
		streamCfg.MaxBudgetUSD = r.parentConfig.MaxBudgetUSD
		streamCfg.MaxTurns = r.parentConfig.MaxTurns
		streamCfg.CustomSystemPrompt = r.parentConfig.CustomSystemPrompt
		streamCfg.AppendSystemPrompt = r.parentConfig.AppendSystemPrompt
		streamCfg.OverrideSystemPrompt = r.parentConfig.OverrideSystemPrompt
		streamCfg.StructuredSchema = r.parentConfig.StructuredSchema
		streamCfg.StructuredDenyRules = r.parentConfig.StructuredDenyRules
	}

	// Subagent integration: use parent's session ID so transcript/cost land in parent's directory
	if r.parentSessionID != "" {
		streamCfg.SessionID = r.parentSessionID
	}

	// Capture streamCfg for test verification (AC2, AC4)
	r.capturedStreamCfg = streamCfg

	// Ensure cleanup of worktree on exit (AC2)
	if cleanupWorktree {
		defer func() {
			_ = git.RemoveWorktree(worktreePath)
		}()
	}

	// Run the subagent synchronously
	// If a router profile is specified, switch to it for the duration of this
	// subagent call, then restore the parent's profile so routing for the
	// main session is unaffected.
	if params.Profile != "" && router.IsInitialized() {
		if r := router.GetRouter(); r != nil {
			prevProfile := r.GetProfile()
			r.SetProfile(params.Profile)
			defer r.SetProfile(prevProfile)
		}
	}
	_, output, _, err := RunStream(ctx, params.Prompt, subagentTools, cwd, streamCfg, params.Model, WithClient(r.client))

	// AC4: Interrupt yields partial result - capture output even on cancellation
	if ctx.Err() != nil {
		return &tool.SubagentResult{
			Output:  output,
			AgentID: "",
		}, ctx.Err()
	}

	if err != nil {
		return &tool.SubagentResult{
			Output: output,
		}, err
	}

	// Merge subagent cost back into parent's cost state.
	// When ParentEngine is available, merge into its in-memory costState.
	// Fall back to disk merge: load subagent cost, load parent cost, merge, save.
	if r.parentSessionID != "" {
		loadSubagentCost := func() *CostState {
			c, e := LoadCostState(r.parentSessionID)
			if e != nil || c == nil {
				return nil
			}
			return c
		}
		if r.parentConfig != nil && r.parentConfig.ParentEngine != nil {
			if subagentCost := loadSubagentCost(); subagentCost != nil {
				r.parentConfig.ParentEngine.costState.Merge(subagentCost)
			}
		} else {
			// Fallback: merge to parent session file on disk
			if subagentCost := loadSubagentCost(); subagentCost != nil {
				if parentCost, e := LoadCostState(r.parentSessionID); e == nil && parentCost != nil {
					parentCost.Merge(subagentCost)
					parentCost.LastSessionID = r.parentSessionID
					_ = SaveCostState(parentCost)
				}
			}
		}
	}

	return &tool.SubagentResult{
		Output: output,
	}, nil
}

// AsyncSubagentRunner wraps a LocalSubagentRunner to provide async execution.
type AsyncSubagentRunner struct {
	runner          *LocalSubagentRunner
	sessionMgr      *session.Manager
	parentSessionID string
}

// NewAsyncSubagentRunner creates a new AsyncSubagentRunner.
func NewAsyncSubagentRunner(tools []tool.Tool, denyRules map[string]bool, client api.Requester) *AsyncSubagentRunner {
	return &AsyncSubagentRunner{
		runner: NewLocalSubagentRunner(tools, denyRules, client),
	}
}

// SetSessionManager sets the session manager for transcript persistence.
func (r *AsyncSubagentRunner) SetSessionManager(mgr *session.Manager) {
	r.sessionMgr = mgr
	r.runner.sessionMgr = mgr
}

// SetParentConfig sets the parent StreamConfig for inheritance when Name is set (AC3).
func (r *AsyncSubagentRunner) SetParentConfig(cfg *StreamConfig) {
	r.runner.parentConfig = cfg
}

// SetParentSessionID sets the parent session ID for transcript/cost integration.
func (r *AsyncSubagentRunner) SetParentSessionID(sessionID string) {
	r.parentSessionID = sessionID
	r.runner.parentSessionID = sessionID
}

// RunSubagentAsync launches a subagent asynchronously.
// It returns immediately with an AsyncResult without blocking.
func (r *AsyncSubagentRunner) RunSubagentAsync(params tool.SubagentParams) (*tool.AsyncResult, error) {
	// Generate agent ID
	agentID, err := session.NewSessionID()
	if err != nil {
		return nil, fmt.Errorf("generating agent ID: %w", err)
	}

	// Done channel so caller can wait for completion
	done := make(chan struct{})

	// Launch subagent in goroutine with result capture
	go func() {
		defer close(done)

		result, err := r.runner.RunSubagent(context.Background(), params)

		// Write result to parent's transcript instead of root transcripts/ file
		if r.sessionMgr != nil && r.parentSessionID != "" {
			entry := session.TranscriptEntry{
				Type:       "subagent_result",
				SubagentID: agentID,
				Content:    "",
				IsError:    err != nil,
			}
			if result != nil {
				entry.Content = result.Output
			}
			if err != nil {
				entry.Content = err.Error()
			}
			_ = r.sessionMgr.AppendEntry(r.parentSessionID, entry)
			// Cost merge already handled inside RunSubagent above — no duplicate here
		}
	}()

	return &tool.AsyncResult{
		Status:  "async_launched",
		AgentID: agentID,
		Done:    done,
	}, nil
}
