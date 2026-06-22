// Package tool provides tool implementations.
package tool

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ipy/jenny/internal/constants"
	"github.com/ipy/jenny/internal/sandbox"
)

// isExecFailure reports whether err came from a fork/exec syscall failure
// (binary not found, invalid ELF, permission denied on the executable, etc.)
// as opposed to a runtime or I/O error. Used to decide whether falling back
// to direct argv execution might succeed.
func isExecFailure(err error) bool {
	var pe *os.PathError
	return errors.As(err, &pe) && pe.Op == "fork/exec"
}

const (
	// maxInlineSize is the maximum size for inline output (30KB)
	maxInlineSize = 30720
	// maxHeredocLines is the max number of lines in a heredoc before suggesting Write.
	maxHeredocLines = 30
	// fallbackHint is shown when a shell is missing and the command can't be
	// parsed into a simple argv list for direct exec.
	fallbackHint = "use a simple argv command (program arg1 arg2 ...) so the exec fallback can run it"
)

// resolveShellHook, when set by tests, overrides shell resolution.
var resolveShellHook func() (string, error)

// resolveShell returns the full path to a usable shell executable.
// Tries bash, sh, dash, ash, zsh, and ksh in priority order.
func resolveShell() (string, error) {
	if resolveShellHook != nil {
		return resolveShellHook()
	}
	for _, name := range []string{"bash", "sh", "dash", "ash", "zsh", "ksh"} {
		path, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}
	return "", fmt.Errorf("no shell executable found: no usable shell in PATH")
}

// BashTool executes shell commands.
type BashTool struct {
	permissionLevel PermissionLevel
	sandbox         sandbox.SandboxManager
	mu              sync.Mutex
	commandCwd      string
	projectRoot     string
	sessionID       string
	backgroundTasks sync.Map
	taskManager     *TaskManager
	exec            *ExecTool
}

// NewBashTool creates a new BashTool with the given PermissionLevel.
func NewBashTool(level PermissionLevel) *BashTool {
	return &BashTool{
		permissionLevel: level,
	}
}

// effectiveLevel returns the effective PermissionLevel.
func (t *BashTool) effectiveLevel() PermissionLevel {
	return t.permissionLevel
}

// WithSessionID sets the session ID for the BashTool.
func (t *BashTool) WithSessionID(id string) *BashTool {
	t.mu.Lock()
	t.sessionID = id
	if t.exec != nil {
		t.exec.WithSessionID(id)
	}
	t.mu.Unlock()
	return t
}

// WithSandbox sets the sandbox manager for the BashTool.
func (t *BashTool) WithSandbox(sb sandbox.SandboxManager) *BashTool {
	t.mu.Lock()
	t.sandbox = sb
	t.mu.Unlock()
	return t
}

// WithTaskManager sets the task manager for background task tracking.
func (t *BashTool) WithTaskManager(tm *TaskManager) *BashTool {
	t.mu.Lock()
	t.taskManager = tm
	t.mu.Unlock()
	return t
}

// WithExecTool sets the ExecTool used for process execution.
func (t *BashTool) WithExecTool(e *ExecTool) *BashTool {
	t.mu.Lock()
	t.exec = e
	t.mu.Unlock()
	return t
}

func (t *BashTool) ensureExec() *ExecTool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.exec == nil {
		t.exec = NewExecTool(t.projectRoot, t.sessionID)
	}
	return t.exec
}

// GetTaskManager returns the task manager for sharing with other tools.
func (t *BashTool) GetTaskManager() *TaskManager {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.taskManager
}

// Name returns the tool name.
func (t *BashTool) Name() string {
	return "Bash"
}

// ConcurrencySafe returns false — bash commands must execute serially
// to maintain consistent cwd state and avoid interleaving output.
func (t *BashTool) ConcurrencySafe() bool {
	return false
}

// Description returns a description of the tool.
func (t *BashTool) Description() string {
	return "Execute shell commands. Use this to run bash commands like ls, cat, pwd, etc. Note: bash is a generic exec tool and does not apply .gitignore/.jennyignore filtering — use the Glob tool for ignore-aware file discovery."
}

// InputSchema returns the JSON schema for tool input.
func (t *BashTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "The shell command to execute",
			},
			"timeout": map[string]any{
				"type":        "number",
				"description": "Timeout in seconds (default: 30)",
			},
			"run_in_background": map[string]any{
				"type":        "boolean",
				"description": "Spawn command as background task (required for sleep >=2)",
			},
		},
		"required": []string{"command"},
	}
}

// Execute runs the bash command with context support for cancellation.
// If the context is cancelled (e.g., by sibling abort), the command will be interrupted.
func (t *BashTool) Execute(ctx context.Context, input map[string]any, cwd string) (*ToolResult, error) {
	command, ok := input["command"].(string)
	if !ok || command == "" {
		return nil, fmt.Errorf("command is required and must be a string")
	}

	// Suggest Write tool for long heredocs before spending time on security checks.
	if suggestion := suggestHeredocToFile(command); suggestion != "" {
		return &ToolResult{
			Content: suggestion,
			IsError: false,
		}, nil
	}

	t.mu.Lock()
	// Set project root from cwd
	if t.projectRoot == "" {
		t.projectRoot = cwd
	}

	// Set initial command cwd if not set
	if t.commandCwd == "" {
		t.commandCwd = cwd
	}

	// Universal Windows Security (AC4)
	if runtime.GOOS == "windows" {
		winGate := NewWindowsCommandGate(t.effectiveLevel())
		// CheckPath with timeout to prevent hanging on slow filesystem/network paths
		checkCtx, checkCancel := context.WithTimeout(context.Background(), 1*time.Second)
		checkErr := make(chan error, 1)
		go func() {
			checkErr <- winGate.CheckPath(t.commandCwd)
		}()
		select {
		case err := <-checkErr:
			checkCancel()
			if err != nil {
				t.mu.Unlock()
				return &ToolResult{
					Content: fmt.Sprintf("Security error: %v", err),
					IsError: true,
				}, nil
			}
		case <-checkCtx.Done():
			checkCancel()
			t.mu.Unlock()
			return &ToolResult{
				Content: "Security error: Windows path check timed out",
				IsError: true,
			}, nil
		}
	}

	t.mu.Unlock()

	// AC1: Block all Bash execution at read level
	if t.effectiveLevel() == PermissionRead {
		return &ToolResult{
			Content: "Error: Bash execution is not allowed at read permission level. Use --permission-level analyze or higher.",
			IsError: true,
		}, nil
	}

	// Handle sed simulation (AC5) - after security checks but before timeout/background
	if isSedInPlace(command) {
		// Validate path in sed command is within working directory
		gate := NewCommandGate(t.effectiveLevel())
		if err := gate.CheckCommand(command); err != nil {
			return &ToolResult{
				Content: fmt.Sprintf("Security error: %v", err),
				IsError: true,
			}, nil
		}
		return t.executeSed(command, t.commandCwd)
	}

	// Check for sleep >= 2 in foreground (AC3)
	if !isBackgroundExecution(input) {
		if sleepSeconds := getSleepSeconds(command); sleepSeconds >= 2 {
			return &ToolResult{
				Content: "sleep >=2 seconds is not allowed in foreground; use run_in_background: true",
				IsError: true,
			}, nil
		}
	}

	// Handle background execution (AC3)
	if isBackgroundExecution(input) {
		// Security: validate command even for background execution
		gate := NewCommandGate(t.effectiveLevel())
		if err := gate.CheckCommand(command); err != nil {
			return &ToolResult{
				Content: fmt.Sprintf("Security error: %v", err),
				IsError: true,
			}, nil
		}
		return t.executeBackground(command, t.commandCwd, input)
	}

	// Create command gate for security validation
	gate := NewCommandGate(t.effectiveLevel())

	// Check command against blocked patterns
	if err := gate.CheckCommand(command); err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("Security error: %v", err),
			IsError: true,
		}, nil
	}

	// Check for device paths in command arguments
	if err := gate.CheckDevicePathsInCommand(command); err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("Security error: %v", err),
			IsError: true,
		}, nil
	}

	// Check pipeline segments for read-only compliance (AC1)
	if err := gate.CheckPipelineSegments(command); err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("Security error: %v", err),
			IsError: true,
		}, nil
	}

	// Check if all paths in the command are within the working directory or scratchpad
	// Skip validation for cd commands since they change directory state, not file content
	// Also skip if permission level is unrestricted (dangerous bypass)
	if t.effectiveLevel().PathConstrained() && !isCdCommand(command) && !validateCommandPaths(command, t.commandCwd, constants.ScratchpadDir()) {
		return &ToolResult{
			Content: fmt.Sprintf("Error: Command '%s' is not allowed. Access outside working directory is prohibited.", command),
			IsError: true,
		}, nil
	}

	// Wrap command with sandbox if available and not bypassed
	var wrapErr error
	command, wrapErr = t.maybeWrapWithSandbox(command, input)
	if wrapErr != nil {
		return &ToolResult{
			Content: fmt.Sprintf("Sandbox error: %v", wrapErr),
			IsError: true,
		}, nil
	}

	timeout := 30
	if timeoutVal, ok := input["timeout"].(float64); ok {
		timeout = int(timeoutVal)
	}

	// First derive a context that inherits cancellation from the passed context.
	// This is critical: when the executor cancels the parent context (sibling abort),
	// we need the command to be interrupted.
	derivedCtx, derivedCancel := context.WithCancel(ctx)
	defer derivedCancel()

	// Then apply timeout on top of the cancellation-inheriting context.
	// This ensures: (1) sibling abort works via parent cancellation, (2) timeout works.
	cmdCtx, cmdCancel := context.WithTimeout(derivedCtx, time.Duration(timeout)*time.Second)
	defer cmdCancel()

	shellPath, shellErr := resolveShell()
	if shellErr != nil {
		return t.executeShellFallback(cmdCtx, command, timeout)
	}

	return t.runForegroundShell(cmdCtx, shellPath, command, timeout)
}

func (t *BashTool) runForegroundShell(ctx context.Context, shellPath, command string, timeout int) (*ToolResult, error) {
	startTime := time.Now()

	var stdout, stderr bytes.Buffer
	handle, err := t.ensureExec().Start(ctx, ExecRequest{
		Path:   shellPath,
		Args:   []string{"-c", command},
		Dir:    t.commandCwd,
		Stdout: &stdout,
		Stderr: &stderr,
	}, ExecOptions{Timeout: time.Duration(timeout) * time.Second})
	if err != nil {
		// Shell binary passed stat checks but exec failed (e.g. missing shared
		// library, corrupted overlay FS, invalid ELF). Fall back to direct argv
		// execution instead of surfacing a cryptic error to the model.
		if isExecFailure(err) {
			return t.executeShellFallback(ctx, command, timeout)
		}
		return &ToolResult{
			Content: fmt.Sprintf("Error starting command: %v", err),
			IsError: true,
		}, nil
	}

	result, waitErr := handle.Wait()
	output := combineExecOutput(stdout.String(), stderr.String())

	t.mu.Lock()
	t.resetCwdIfOutsideProject(command)
	t.mu.Unlock()

	if result != nil && result.TimedOut {
		return &ToolResult{
			Content: fmt.Sprintf("Command timed out after %d seconds", timeout),
			IsError: true,
		}, nil
	}

	if waitErr != nil {
		return &ToolResult{
			Content: fmt.Sprintf("Error executing command: %v\n%s", waitErr, output),
			IsError: true,
		}, nil
	}

	if result != nil && result.ExitCode != 0 {
		return &ToolResult{
			Content: fmt.Sprintf("%s\n(exit code: %d)", output, result.ExitCode),
			IsError: true,
		}, nil
	}

	duration := time.Since(startTime)
	if duration > 120*time.Second {
		output += "\n(Tip: long-running commands work better with run_in_background: true)"
	}

	return t.handleOutput(output)
}

func (t *BashTool) executeShellFallback(ctx context.Context, command string, timeout int) (*ToolResult, error) {
	if t.sandbox != nil && t.sandbox.IsActive() {
		return &ToolResult{
			Content: formatToolError(
				"sandbox_violation",
				"sandbox active; simple-exec fallback cannot honor sandbox",
				command,
				"",
				fallbackHint,
			),
			IsError: true,
		}, nil
	}

	parsed := parseSimpleCommand(command, ShellFlavorBash)
	if !parsed.OK {
		hint := fallbackHint
		return &ToolResult{
			Content: formatToolError(
				"shell_unavailable",
				"shell not found; input not runnable as simple argv",
				command,
				strings.Join(parsed.Issues, ", "),
				hint,
			),
			IsError: true,
		}, nil
	}

	return t.runForegroundExec(ctx, parsed.Argv[0], parsed.Argv[1:], timeout)
}

func (t *BashTool) runForegroundExec(ctx context.Context, path string, args []string, timeout int) (*ToolResult, error) {
	var stdout, stderr bytes.Buffer
	handle, err := t.ensureExec().Start(ctx, ExecRequest{
		Path:   path,
		Args:   args,
		Dir:    t.commandCwd,
		Stdout: &stdout,
		Stderr: &stderr,
	}, ExecOptions{Timeout: time.Duration(timeout) * time.Second})
	if err != nil {
		return &ToolResult{
			Content: formatToolError(
				"execution_failed",
				fmt.Sprintf("failed to start executable: %v", err),
				strings.Join(append([]string{path}, args...), " "),
				"",
				"",
			),
			IsError: true,
		}, nil
	}

	result, waitErr := handle.Wait()
	output := combineExecOutput(stdout.String(), stderr.String())

	if result != nil && result.TimedOut {
		return &ToolResult{
			Content: formatToolError(
				"timeout",
				fmt.Sprintf("command timed out after %d seconds", timeout),
				strings.Join(append([]string{path}, args...), " "),
				"",
				"set run_in_background: true",
			),
			IsError: true,
		}, nil
	}

	if waitErr != nil {
		return &ToolResult{
			Content: formatToolError(
				"execution_failed",
				waitErr.Error(),
				strings.Join(append([]string{path}, args...), " "),
				"",
				"",
			),
			IsError: true,
		}, nil
	}

	if result != nil && result.ExitCode != 0 {
		return &ToolResult{
			Content: fmt.Sprintf("%s\n(exit code: %d)", output, result.ExitCode),
			IsError: true,
		}, nil
	}

	return t.handleOutput(output)
}

func combineExecOutput(stdout, stderr string) string {
	output := stdout
	if stderr != "" {
		if output != "" {
			output += "\n"
		}
		output += "stderr: " + stderr
	}
	return output
}

// handleOutput checks output size and spills to disk if > maxInlineSize
func (t *BashTool) handleOutput(output string) (*ToolResult, error) {
	if len(output) <= maxInlineSize {
		return &ToolResult{
			Content: output,
			IsError: false,
		}, nil
	}

	// Spill to disk
	spillPath, err := t.writeSpillFile(output)
	if err != nil {
		// Fall back to truncated inline if spill fails
		return &ToolResult{
			Content: fmt.Sprintf("Output truncated (spill file error: %v). First %d chars:\n%s",
				err, maxInlineSize/2, output[:maxInlineSize/2]),
			IsError:   false,
			Truncated: true,
		}, nil
	}

	return &ToolResult{
		Content:   fmt.Sprintf("Output spilled to %s (%d chars)", spillPath, len(output)),
		IsError:   false,
		Truncated: true,
	}, nil
}

// writeSpillFile writes output to a temp file and returns the path
func (t *BashTool) writeSpillFile(output string) (string, error) {
	// 1. Try jenny home spills directory (~/.jenny/sessions/<id>/spills or ~/.jenny/spills)
	spillsDir := constants.SpillsDir(t.sessionID)
	if dirExists(spillsDir) || mkdirAll(spillsDir, 0755) == nil {
		f, err := os.CreateTemp(spillsDir, "spill-*")
		if err == nil {
			defer f.Close()
			if _, err := f.WriteString(output); err == nil {
				return f.Name(), nil
			}
		}
	}

	// 2. Try project .jenny/spills directory (or .jenny/sessions/<id>/spills if we wanted to be consistent,
	// but usually project-local .jenny is simpler)
	projectSpillsDir := filepath.Join(constants.ProjectJennyDir(t.projectRoot), "spills")
	if t.sessionID != "" {
		projectSpillsDir = filepath.Join(constants.ProjectJennyDir(t.projectRoot), "sessions", t.sessionID, "spills")
	}
	if dirExists(projectSpillsDir) || mkdirAll(projectSpillsDir, 0755) == nil {
		f, err := os.CreateTemp(projectSpillsDir, "spill-*")
		if err == nil {
			defer f.Close()
			if _, err := f.WriteString(output); err == nil {
				return f.Name(), nil
			}
		}
	}

	// 3. Fallback to system temp directory (/tmp/jenny-spills/<id> or /tmp/jenny-spills)
	tmpDir := filepath.Join(os.TempDir(), "jenny-spills")
	if t.sessionID != "" {
		tmpDir = filepath.Join(tmpDir, t.sessionID)
	}
	if dirExists(tmpDir) || mkdirAll(tmpDir, 0755) == nil {
		f, err := os.CreateTemp(tmpDir, "spill-*")
		if err == nil {
			defer f.Close()
			if _, err := f.WriteString(output); err == nil {
				return f.Name(), nil
			}
		}
	}

	return "", fmt.Errorf("failed to create spill file in any location")
}

// isBackgroundExecution checks if run_in_background flag is set
func isBackgroundExecution(input map[string]any) bool {
	if rib, ok := input["run_in_background"]; ok {
		if b, ok := rib.(bool); ok {
			return b
		}
	}
	return false
}

// executeBackground runs command as background task.
// Caller must hold t.mu.
func (t *BashTool) executeBackground(command string, cwd string, input map[string]any) (*ToolResult, error) {
	timeout := 30
	if timeoutVal, ok := input["timeout"].(float64); ok {
		timeout = int(timeoutVal)
	}

	// Snapshot shared fields under lock before spawning goroutines.
	tm := t.taskManager
	projRoot := t.projectRoot

	// Create a background context (NOT for timeout - we handle timeout manually for AC5)
	ctx := context.Background()

	// Create result channel (buffered to prevent deadlock when outer select is stuck)
	resultCh := make(chan *ToolResult, 1)

	// Generate string task ID
	taskID := fmt.Sprintf("task_%d", time.Now().UnixNano())

	// Initialize task manager if nil
	if tm == nil {
		tm = NewTaskManager()
		t.taskManager = tm
	}

	// Set project root on task manager for project-relative paths
	if projRoot != "" {
		tm.WithProjectRoot(projRoot)
	}

	// Get output file path
	outputFile := ""
	if tm != nil {
		path, err := tm.TaskOutputPath(taskID)
		if err == nil {
			outputFile = path
			// Store task info (Process will be set after cmd.Start())
			tm.Store(taskID, &TaskInfo{
				TaskID:     taskID,
				State:      TaskStateRunning,
				OutputFile: outputFile,
				StartTime:  time.Now(),
				Command:    command,
			})
		}
	}

	// Store in background tasks (using string key for compatibility)
	t.backgroundTasks.Store(taskID, resultCh)

	// Track if command completed (for synchronization)
	var cmdDone int32 = 0

	// Channel to signal command completion
	done := make(chan struct{})

	// Start time for duration tracking
	startTime := time.Now()

	// Track output for progress events (shared between goroutines)
	var outputMu sync.Mutex
	var output strings.Builder

	// Spawn goroutine
	go func() {
		execPath, execArgs, shellErr := t.resolveExecArgv(command)
		if shellErr != nil {
			errContent := shellErr.Error()
			if tm != nil {
				_ = tm.WriteTaskResult(taskID, errContent, 1, time.Since(startTime).Seconds())
				tm.CancelKillTimer(taskID)
				tm.UpdateState(taskID, TaskStateCompleted)
				tm.EnqueueCompletion(TaskCompletion{
					TaskID:          taskID,
					DurationSeconds: time.Since(startTime).Seconds(),
					ExitCode:        1,
					Output:          errContent,
				})
			}
			resultCh <- &ToolResult{
				Content: errContent,
				IsError: true,
			}
			atomic.StoreInt32(&cmdDone, 1)
			close(done)
			return
		}

		var stdoutBuf, stderrBuf bytes.Buffer
		execTool := t.ensureExec()

		waitDone := make(chan struct {
			result *ExecResult
			err    error
		}, 1)

		handle, startErr := execTool.Start(ctx, ExecRequest{
			Path:   execPath,
			Args:   execArgs,
			Dir:    cwd,
			Stdout: &stdoutBuf,
			Stderr: &stderrBuf,
		}, ExecOptions{
			Timeout: time.Duration(timeout) * time.Second,
			OnStart: func(p *os.Process) {
				if tm != nil && p != nil {
					tm.UpdateProcess(taskID, p)
				}
			},
		})
		if startErr != nil {
			outputMu.Lock()
			fmt.Fprintf(&output, "failed to start command: %v", startErr)
			outputMu.Unlock()
			atomic.StoreInt32(&cmdDone, 1)
			close(done)
			return
		}

		go func() {
			r, err := handle.Wait()
			waitDone <- struct {
				result *ExecResult
				err    error
			}{r, err}
		}()

		progressTimer := time.NewTimer(2 * time.Second)
		flushTicker := time.NewTicker(5 * time.Second)
		defer progressTimer.Stop()
		defer flushTicker.Stop()

	waitLoop:
		for {
			select {
			case <-progressTimer.C:
				outputMu.Lock()
				snapshot := combineExecOutput(stdoutBuf.String(), stderrBuf.String())
				outputMu.Unlock()
				if tm != nil {
					EmitTaskProgress(taskID, 2.0, snapshot)
				}
			case <-flushTicker.C:
				outputMu.Lock()
				snapshot := combineExecOutput(stdoutBuf.String(), stderrBuf.String())
				outputMu.Unlock()
				if tm != nil {
					duration := time.Since(startTime).Seconds()
					_ = tm.FlushPartialOutput(taskID, snapshot, duration)
				}
			case wr := <-waitDone:
				outputMu.Lock()
				outputSnapshot := combineExecOutput(stdoutBuf.String(), stderrBuf.String())
				output.WriteString(outputSnapshot)
				outputMu.Unlock()

				exitCode := 0
				if wr.result != nil {
					exitCode = wr.result.ExitCode
				}
				if wr.err != nil && exitCode == 0 {
					exitCode = -1
				}

				duration := time.Since(startTime).Seconds()

				if tm != nil {
					_ = tm.WriteTaskResult(taskID, outputSnapshot, exitCode, duration)
					tm.CancelKillTimer(taskID)
					tm.UpdateState(taskID, TaskStateCompleted)
					tm.EnqueueCompletion(TaskCompletion{
						TaskID:          taskID,
						DurationSeconds: duration,
						ExitCode:        exitCode,
						Output:          outputSnapshot,
					})
				}

				var result *ToolResult
				if wr.result != nil && wr.result.Signal != "" {
					result = &ToolResult{
						Content: fmt.Sprintf("%s\n(exit code: %d, signal: %s)", outputSnapshot, exitCode, wr.result.Signal),
						IsError: true,
					}
				} else if exitCode != 0 {
					result = &ToolResult{
						Content: fmt.Sprintf("%s\n(exit code: %d)", outputSnapshot, exitCode),
						IsError: true,
					}
				} else {
					result = &ToolResult{
						Content: outputSnapshot,
						IsError: false,
					}
				}

				resultCh <- result
				atomic.StoreInt32(&cmdDone, 1)
				close(done)
				break waitLoop
			}
		}

		for atomic.LoadInt32(&cmdDone) == 0 {
			time.Sleep(10 * time.Millisecond)
		}

		close(resultCh)
		t.backgroundTasks.Delete(taskID)

		if tm != nil {
			tm.Delete(taskID)
		}
	}()

	return &ToolResult{
		Content:    fmt.Sprintf("Background task %s started", taskID),
		OutputFile: outputFile,
		IsError:    false,
	}, nil
}

// resolveExecArgv returns the executable path and args for shell or fallback execution.
func (t *BashTool) resolveExecArgv(command string) (string, []string, error) {
	shellPath, shellErr := resolveShell()
	if shellErr == nil {
		return shellPath, []string{"-c", command}, nil
	}

	if t.sandbox != nil && t.sandbox.IsActive() {
		return "", nil, fmt.Errorf("%s", formatToolError(
			"sandbox_violation",
			"sandbox active; simple-exec fallback cannot honor sandbox",
			command,
			"",
			fallbackHint,
		))
	}

	parsed := parseSimpleCommand(command, ShellFlavorBash)
	if !parsed.OK {
		hint := fallbackHint
		return "", nil, fmt.Errorf("%s", formatToolError(
			"shell_unavailable",
			"shell not found; input not runnable as simple argv",
			command,
			strings.Join(parsed.Issues, ", "),
			hint,
		))
	}

	return parsed.Argv[0], parsed.Argv[1:], nil
}

// maybeWrapWithSandbox wraps the command with sandbox if available and not bypassed.
func (t *BashTool) maybeWrapWithSandbox(command string, input map[string]any) (string, error) {
	// If no sandbox is configured, return original command
	if t.sandbox == nil {
		return command, nil
	}

	// If sandbox is not active, return original command
	if !t.sandbox.IsActive() {
		return command, nil
	}

	// Check for dangerouslyDisableSandbox flag
	if disableSandbox, ok := input["dangerouslyDisableSandbox"].(bool); ok && disableSandbox {
		return command, nil
	}

	// Wrap with sandbox
	return t.sandbox.WrapWithSandbox(command)
}

// suggestHeredocToFile checks the first line for a heredoc delimiter and returns a
// suggestion if the heredoc body exceeds maxHeredocLines.
func suggestHeredocToFile(command string) string {
	first, rest, hasNewline := strings.Cut(command, "\n")
	if !hasNewline {
		return ""
	}
	delim := heredocDelim(first)
	if delim == "" {
		return ""
	}
	// Count body lines up to the closing delimiter line.
	bodyLines := strings.Count(rest, "\n")
	if idx := strings.Index(rest, "\n"+delim+"\n"); idx >= 0 {
		bodyLines = strings.Count(rest[:idx], "\n") + 1
	} else if idx := strings.Index(rest, "\n"+delim); idx >= 0 && idx+len(delim)+1 == len(rest) {
		bodyLines = strings.Count(rest[:idx], "\n") + 1
	} else {
		return "" // unterminated
	}
	if bodyLines <= maxHeredocLines {
		return ""
	}
	return fmt.Sprintf("This command contains a %d-line heredoc. Consider using the Write tool to create a file instead and reference it in the command.", bodyLines)
}

// heredocDelim returns the heredoc delimiter if line contains an unquoted << that starts
// a heredoc, stripping <<-, quotes, and trailing content. Returns "" if none found.
func heredocDelim(line string) string {
	// Look for << not followed by < (exclude <<< here-strings).
	idx := strings.Index(line, "<<")
	if idx < 0 || strings.HasPrefix(line[idx+2:], "<") {
		return ""
	}
	// Check << is not inside quotes by scanning bytes before it.
	quoted := false
	for i := range idx {
		if line[i] == '\'' || line[i] == '"' {
			quoted = !quoted
		}
	}
	if quoted {
		return ""
	}
	rest := strings.TrimSpace(line[idx+2:])
	if strings.HasPrefix(rest, "-") {
		rest = strings.TrimSpace(rest[1:])
	}
	if len(rest) > 0 && (rest[0] == '\'' || rest[0] == '"') {
		q := rest[0]
		rest = rest[1:]
		if j := strings.IndexByte(rest, q); j >= 0 {
			return rest[:j]
		}
		return ""
	}
	// Unquoted delimiter: take up to first whitespace.
	if j := strings.IndexAny(rest, " \t"); j >= 0 {
		return rest[:j]
	}
	return rest
}
