package tool

import (
	"context"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/ipy/jenny/internal/constants"
)

const (
	// maxEnvTotalBytes is the soft limit for total environment block size.
	// Linux's default ARG_MAX is ~2 MiB (shared between argv + env). We
	// target 1 MiB for env alone to leave room for command arguments.
	maxEnvTotalBytes = 1 << 20

	// maxSingleEnvBytes is the limit for a single environment variable.
	// Variables exceeding this are truncated with a warning suffix.
	maxSingleEnvBytes = 32 << 10

	// coreEnvPrefixes are env vars always preserved even during sanitization.
	coreEnvPrefixes = "PATH=,HOME=,USER=,TERM=,LANG=,LC_,SHELL=,TMPDIR=,TMP=,TEMP=,XDG_,DISPLAY=,SSH_,JENNY_,ANTHROPIC_,OPENAI_,DEEPSEEK_"
)

func mergeExecEnv(sessionID string, extra []string) []string {
	const prefix = "JENNY_SCRATCHPAD="
	scratchpad := prefix + constants.ScratchpadDir(sessionID)

	filtered := make([]string, 0, len(os.Environ())+len(extra)+1)
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, prefix) {
			continue
		}
		filtered = append(filtered, entry)
	}
	for _, entry := range extra {
		if strings.HasPrefix(entry, prefix) {
			continue
		}
		filtered = append(filtered, entry)
	}
	filtered = append(filtered, scratchpad)

	return sanitizeEnv(filtered)
}

// sanitizeEnv enforces size limits on the environment block to prevent
// ENAMETOOLONG / E2BIG errors from oversized env after commands like
// setup.py install bloat PATH or PYTHONPATH.
//
// Strategy:
//  1. Truncate any single variable exceeding maxSingleEnvBytes.
//  2. If total size still exceeds maxEnvTotalBytes, drop non-core variables
//     from largest to smallest until under budget.
func sanitizeEnv(env []string) []string {
	totalSize := 0
	for i, entry := range env {
		if len(entry) > maxSingleEnvBytes {
			env[i] = entry[:maxSingleEnvBytes] + "...(truncated)"
		}
		totalSize += len(env[i])
	}

	if totalSize <= maxEnvTotalBytes {
		return env
	}

	corePrefixes := strings.Split(coreEnvPrefixes, ",")
	isCore := func(entry string) bool {
		for _, p := range corePrefixes {
			if strings.HasPrefix(entry, p) {
				return true
			}
		}
		return false
	}

	type envEntry struct {
		idx  int
		size int
		core bool
	}
	entries := make([]envEntry, len(env))
	for i, e := range env {
		entries[i] = envEntry{idx: i, size: len(e), core: isCore(e)}
	}

	// Sort non-core entries by size descending so we drop the largest first.
	// Use a simple selection: mark non-core entries for removal until under budget.
	for totalSize > maxEnvTotalBytes {
		largestIdx := -1
		largestSize := 0
		for i, e := range entries {
			if !e.core && e.size > largestSize {
				largestSize = e.size
				largestIdx = i
			}
		}
		if largestIdx < 0 {
			break // only core entries left
		}
		totalSize -= entries[largestIdx].size
		entries[largestIdx].size = 0 // mark for removal
	}

	result := make([]string, 0, len(env))
	for i, e := range entries {
		if e.size > 0 {
			result = append(result, env[i])
		}
	}
	return result
}

// ExecRequest describes a process to launch.
type ExecRequest struct {
	Path   string
	Args   []string
	Dir    string
	Env    []string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// ExecOptions configures process execution behavior.
type ExecOptions struct {
	Timeout time.Duration
	OnStart func(p *os.Process)
}

// ExecHandle represents a running process. Call Wait to block until completion.
type ExecHandle struct {
	process   *os.Process
	cmd       *exec.Cmd
	execCtx   context.Context
	cancel    context.CancelFunc
	startTime time.Time
	waitOnce  sync.Once
	result    *ExecResult
	waitErr   error
}

// Process returns the underlying OS process for TaskManager registration.
func (h *ExecHandle) Process() *os.Process {
	return h.process
}

// Wait blocks until the process exits or the execution context is cancelled/times out.
func (h *ExecHandle) Wait() (*ExecResult, error) {
	h.waitOnce.Do(func() {
		h.result, h.waitErr = h.waitInternal()
	})
	return h.result, h.waitErr
}

// ExecResult summarizes how a process exited.
type ExecResult struct {
	ExitCode  int
	Signal    string
	TimedOut  bool
	Cancelled bool
	Duration  time.Duration
}

// ExecBackend launches processes; ExecTool implements this interface.
type ExecBackend interface {
	Start(ctx context.Context, req ExecRequest, opts ExecOptions) (*ExecHandle, error)
}

// ExecTool owns process lifecycle plumbing shared by BashTool and PowerShellTool.
type ExecTool struct {
	projectRoot string
	sessionID   string
}

// NewExecTool creates an ExecTool with session-scoped env injection.
func NewExecTool(projectRoot, sessionID string) *ExecTool {
	return &ExecTool{
		projectRoot: projectRoot,
		sessionID:   sessionID,
	}
}

// WithSessionID updates the session ID used for JENNY_SCRATCHPAD injection.
func (e *ExecTool) WithSessionID(id string) *ExecTool {
	e.sessionID = id
	return e
}

// Start launches the process and returns immediately after cmd.Start succeeds.
func (e *ExecTool) Start(ctx context.Context, req ExecRequest, opts ExecOptions) (*ExecHandle, error) {
	cmd := exec.Command(req.Path, req.Args...)
	cmd.Dir = req.Dir
	setupProcessGroup(cmd)

	env := mergeExecEnv(e.sessionID, req.Env)
	cmd.Env = env

	if req.Stdin != nil {
		cmd.Stdin = req.Stdin
	}
	if req.Stdout != nil {
		cmd.Stdout = req.Stdout
	}
	if req.Stderr != nil {
		cmd.Stderr = req.Stderr
	}

	execCtx := ctx
	var cancel context.CancelFunc
	if opts.Timeout > 0 {
		execCtx, cancel = context.WithTimeout(ctx, opts.Timeout)
	} else {
		execCtx, cancel = context.WithCancel(ctx)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}

	if opts.OnStart != nil && cmd.Process != nil {
		opts.OnStart(cmd.Process)
	}

	return &ExecHandle{
		process:   cmd.Process,
		cmd:       cmd,
		execCtx:   execCtx,
		cancel:    cancel,
		startTime: time.Now(),
	}, nil
}

func (h *ExecHandle) waitInternal() (*ExecResult, error) {
	defer h.cancel()

	done := make(chan error, 1)
	killEscalationDone := make(chan struct{})
	go func() {
		done <- h.cmd.Wait()
		close(killEscalationDone)
	}()

	var runErr error
	select {
	case runErr = <-done:
	case <-h.execCtx.Done():
		terminateProcessEscalating(h.cmd.Process, killEscalationDone)
		runErr = <-done
	}

	duration := time.Since(h.startTime)
	result := &ExecResult{Duration: duration}

	if h.execCtx.Err() != nil {
		result.TimedOut = h.execCtx.Err() == context.DeadlineExceeded
		result.Cancelled = h.execCtx.Err() == context.Canceled
		if runErr == nil || isWaitInterrupted(runErr) {
			if result.TimedOut {
				return result, nil
			}
			if result.Cancelled {
				return result, nil
			}
		}
	}

	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
			if h.cmd.ProcessState != nil {
				if code, sig, signaled := getSignalInfo(h.cmd.ProcessState); signaled {
					result.ExitCode = code
					result.Signal = sig
				}
			}
			return result, nil
		}
		if result.TimedOut || result.Cancelled {
			return result, nil
		}
		return nil, runErr
	}

	if h.cmd.ProcessState != nil {
		result.ExitCode = h.cmd.ProcessState.ExitCode()
		if code, sig, signaled := getSignalInfo(h.cmd.ProcessState); signaled {
			result.ExitCode = code
			result.Signal = sig
		}
	}
	return result, nil
}

func isWaitInterrupted(err error) bool {
	if err == nil {
		return false
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode() != 0
	}
	return true
}

// terminateProcessEscalating kills the process tree on timeout/cancellation.
// Unix: SIGTERM to process, then SIGKILL to process group after 5s unless done is closed first.
// Windows: single taskkill /F /T.
func terminateProcessEscalating(proc *os.Process, done <-chan struct{}) {
	if proc == nil {
		return
	}
	if runtime.GOOS == "windows" {
		_ = killProcessGroup(proc)
		return
	}
	_ = signalProcess(proc, false)
	go func() {
		select {
		case <-done:
			return
		case <-time.After(5 * time.Second):
			_ = killProcessGroup(proc)
		}
	}()
}
