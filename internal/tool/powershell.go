package tool

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// psShellUnavailableReason is the reason when powershell.exe is not found
	// and the command could not be parsed as simple argv for direct execution.
	psShellUnavailableReason = "powershell.exe not found and command uses shell syntax that cannot run without a shell"
)

// resolvePowerShellHook, when set by tests, overrides PowerShell resolution.
var resolvePowerShellHook func() (string, error)

// resolvePowerShell returns the full path to powershell.exe.
func resolvePowerShell() (string, error) {
	if resolvePowerShellHook != nil {
		return resolvePowerShellHook()
	}
	path, err := exec.LookPath("powershell.exe")
	if err != nil {
		return "", fmt.Errorf("powershell.exe not found in PATH")
	}
	return path, nil
}

// PowerShellTool executes PowerShell commands.
type PowerShellTool struct {
	permissionLevel PermissionLevel
	mu              sync.Mutex
	commandCwd      string
	projectRoot     string
	backgroundTasks sync.Map
	taskManager     *TaskManager
	sessionID       string
	exec            *ExecTool
}

// NewPowerShellTool creates a new PowerShellTool with the given PermissionLevel.
func NewPowerShellTool(level PermissionLevel) *PowerShellTool {
	return &PowerShellTool{
		permissionLevel: level,
	}
}

// effectiveLevel returns the effective PermissionLevel.
func (t *PowerShellTool) effectiveLevel() PermissionLevel {
	return t.permissionLevel
}

// WithTaskManager sets the task manager for background task tracking.
func (t *PowerShellTool) WithTaskManager(tm *TaskManager) *PowerShellTool {
	t.taskManager = tm
	return t
}

// WithSessionID sets the session ID for the PowerShellTool.
func (t *PowerShellTool) WithSessionID(id string) *PowerShellTool {
	t.mu.Lock()
	t.sessionID = id
	if t.exec != nil {
		t.exec.WithSessionID(id)
	}
	t.mu.Unlock()
	return t
}

// WithExecTool sets the ExecTool used for process execution.
func (t *PowerShellTool) WithExecTool(e *ExecTool) *PowerShellTool {
	t.mu.Lock()
	t.exec = e
	t.mu.Unlock()
	return t
}

func (t *PowerShellTool) ensureExec() *ExecTool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.exec == nil {
		t.exec = NewExecTool(t.projectRoot, t.sessionID)
	}
	return t.exec
}

// Name returns the tool name.
func (t *PowerShellTool) Name() string {
	return "PowerShell"
}

// ConcurrencySafe returns false — PowerShell commands must execute serially
// to maintain consistent state and avoid interleaving output.
func (t *PowerShellTool) ConcurrencySafe() bool {
	return false
}

// Description returns a description of the tool.
func (t *PowerShellTool) Description() string {
	return "Execute PowerShell commands on Windows. Use this to run PowerShell commands like Get-ChildItem, Get-Content, etc."
}

// InputSchema returns the JSON schema for tool input.
func (t *PowerShellTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "The PowerShell command to execute",
			},
			"timeout": map[string]any{
				"type":        "number",
				"description": "Timeout in seconds (default: 30)",
			},
			"run_in_background": map[string]any{
				"type":        "boolean",
				"description": "Spawn command as background task",
			},
		},
		"required": []string{"command"},
	}
}

// Execute runs the PowerShell command.
func (t *PowerShellTool) Execute(ctx context.Context, input map[string]any, cwd string) (*ToolResult, error) {
	command, ok := input["command"].(string)
	if !ok || command == "" {
		return nil, fmt.Errorf("command is required and must be a string")
	}

	t.mu.Lock()
	if t.projectRoot == "" {
		t.projectRoot = cwd
	}
	if t.commandCwd == "" {
		t.commandCwd = cwd
	}
	t.mu.Unlock()

	if isBackgroundExecution(input) {
		return t.executeBackground(command, t.commandCwd, input)
	}

	// Windows Command Gate
	if runtime.GOOS == "windows" {
		gate := NewWindowsCommandGate(t.effectiveLevel())
		if err := gate.CheckCommand(command); err != nil {
			return &ToolResult{
				Content: fmt.Sprintf("Security error: %v", err),
				IsError: true,
			}, nil
		}
	}

	timeout := 30
	if timeoutVal, ok := input["timeout"].(float64); ok {
		timeout = int(timeoutVal)
	}

	derivedCtx, derivedCancel := context.WithCancel(ctx)
	defer derivedCancel()

	cmdCtx, cmdCancel := context.WithTimeout(derivedCtx, time.Duration(timeout)*time.Second)
	defer cmdCancel()

	shellPath, shellErr := resolvePowerShell()
	if shellErr != nil {
		return t.executeShellFallback(cmdCtx, command, timeout)
	}

	return t.runForegroundPowerShell(cmdCtx, shellPath, command, timeout)
}

func (t *PowerShellTool) wrapPowerShellCommand(command string) string {
	return "[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new(); " + command
}

func (t *PowerShellTool) runForegroundPowerShell(ctx context.Context, shellPath, command string, timeout int) (*ToolResult, error) {
	fullCommand := t.wrapPowerShellCommand(command)

	var stdout, stderr bytes.Buffer
	handle, err := t.ensureExec().Start(ctx, ExecRequest{
		Path:   shellPath,
		Args:   []string{"-NoProfile", "-NonInteractive", "-Command", fullCommand},
		Dir:    t.commandCwd,
		Stdout: &stdout,
		Stderr: &stderr,
	}, ExecOptions{Timeout: time.Duration(timeout) * time.Second})
	if err != nil {
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

	return &ToolResult{
		Content: output,
		IsError: false,
	}, nil
}

func (t *PowerShellTool) executeShellFallback(ctx context.Context, command string, timeout int) (*ToolResult, error) {
	parsed := parseSimpleCommand(command, ShellFlavorPowerShell)
	if !parsed.OK {
		return &ToolResult{
			Content: formatToolError(
				"shell_unavailable",
				psShellUnavailableReason,
				command,
				strings.Join(parsed.Issues, ", "),
				fallbackHint,
			),
			IsError: true,
		}, nil
	}

	return t.runForegroundExec(ctx, parsed.Argv[0], parsed.Argv[1:], timeout)
}

func (t *PowerShellTool) runForegroundExec(ctx context.Context, path string, args []string, timeout int) (*ToolResult, error) {
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
				execStartFailedHint,
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
				timeoutHint,
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
				execWaitFailedHint,
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

	return &ToolResult{
		Content: output,
		IsError: false,
	}, nil
}

func (t *PowerShellTool) executeBackground(command string, cwd string, input map[string]any) (*ToolResult, error) {
	timeout := 30
	if timeoutVal, ok := input["timeout"].(float64); ok {
		timeout = int(timeoutVal)
	}

	tm := t.taskManager
	projRoot := t.projectRoot

	ctx := context.Background()
	resultCh := make(chan *ToolResult, 1)
	taskID := fmt.Sprintf("task_ps_%d", time.Now().UnixNano())

	if tm == nil {
		tm = NewTaskManager()
		t.taskManager = tm
	}
	if projRoot != "" {
		tm.WithProjectRoot(projRoot)
	}

	outputFile, _ := tm.TaskOutputPath(taskID)
	tm.Store(taskID, &TaskInfo{
		TaskID:     taskID,
		State:      TaskStateRunning,
		OutputFile: outputFile,
		StartTime:  time.Now(),
		Command:    command,
	})

	t.backgroundTasks.Store(taskID, resultCh)
	var cmdDone int32 = 0
	done := make(chan struct{})
	startTime := time.Now()
	var outputMu sync.Mutex
	var output strings.Builder

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
			output.WriteString(fmt.Sprintf("failed to start command: %v", startErr))
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

func (t *PowerShellTool) resolveExecArgv(command string) (string, []string, error) {
	shellPath, shellErr := resolvePowerShell()
	if shellErr == nil {
		fullCommand := t.wrapPowerShellCommand(command)
		return shellPath, []string{"-NoProfile", "-NonInteractive", "-Command", fullCommand}, nil
	}

	parsed := parseSimpleCommand(command, ShellFlavorPowerShell)
	if !parsed.OK {
		return "", nil, fmt.Errorf("%s", formatToolError(
			"shell_unavailable",
			psShellUnavailableReason,
			command,
			strings.Join(parsed.Issues, ", "),
			fallbackHint,
		))
	}

	return parsed.Argv[0], parsed.Argv[1:], nil
}
