package tool

import (
	"bytes"
	"context"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestExecTool_ExitCodeZero(t *testing.T) {
	et := NewExecTool("", "")
	var stdout bytes.Buffer
	ctx := context.Background()

	handle, err := et.Start(ctx, ExecRequest{
		Path:   shellForTest(t),
		Args:   shellArgsForTest("-c", "exit 0"),
		Stdout: &stdout,
	}, ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	result, err := handle.Wait()
	if err != nil {
		t.Fatalf("Wait failed: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}
}

func TestExecTool_ExitCodeNonZero(t *testing.T) {
	et := NewExecTool("", "")
	ctx := context.Background()

	handle, err := et.Start(ctx, ExecRequest{
		Path: shellForTest(t),
		Args: shellArgsForTest("-c", "exit 42"),
	}, ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	result, err := handle.Wait()
	if err != nil {
		t.Fatalf("Wait failed: %v", err)
	}
	if result.ExitCode != 42 {
		t.Errorf("expected exit code 42, got %d", result.ExitCode)
	}
}

func TestExecTool_StdoutStderrWriters(t *testing.T) {
	et := NewExecTool("", "")
	var stdout, stderr bytes.Buffer
	ctx := context.Background()

	handle, err := et.Start(ctx, ExecRequest{
		Path:   shellForTest(t),
		Args:   shellArgsForTest("-c", "echo hello; echo err1 >&2"),
		Stdout: &stdout,
		Stderr: &stderr,
	}, ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if _, err := handle.Wait(); err != nil {
		t.Fatalf("Wait failed: %v", err)
	}
	if !strings.Contains(stdout.String(), "hello") {
		t.Errorf("expected hello in stdout, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "err1") {
		t.Errorf("expected err1 in stderr, got %q", stderr.String())
	}
}

func TestExecTool_StdinForwarding(t *testing.T) {
	et := NewExecTool("", "")
	var stdout bytes.Buffer
	ctx := context.Background()

	handle, err := et.Start(ctx, ExecRequest{
		Path:   shellForTest(t),
		Args:   shellArgsForTest("-c", "cat"),
		Stdin:  strings.NewReader("piped-input"),
		Stdout: &stdout,
	}, ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if _, err := handle.Wait(); err != nil {
		t.Fatalf("Wait failed: %v", err)
	}
	if !strings.Contains(stdout.String(), "piped-input") {
		t.Errorf("expected piped input in stdout, got %q", stdout.String())
	}
}

func TestExecTool_EnvInjection(t *testing.T) {
	sessionID := "test-session-exec"
	t.Setenv("PRESERVE_ME", "yes")
	et := NewExecTool("", sessionID)
	var stdout bytes.Buffer
	ctx := context.Background()

	handle, err := et.Start(ctx, ExecRequest{
		Path:   shellForTest(t),
		Args:   shellArgsForTest("-c", "echo $JENNY_SCRATCHPAD:$PRESERVE_ME"),
		Stdout: &stdout,
		Env:    []string{"JENNY_SCRATCHPAD=should-not-clobber"},
	}, ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if _, err := handle.Wait(); err != nil {
		t.Fatalf("Wait failed: %v", err)
	}

	out := strings.TrimSpace(stdout.String())
	if !strings.Contains(out, "yes") {
		t.Errorf("expected PRESERVE_ME to remain in environment, got %q", out)
	}
	if strings.Contains(out, "should-not-clobber") {
		t.Error("ExecTool should inject session scratchpad, not keep caller override")
	}
	if out == "" || out == ":" {
		t.Fatalf("expected scratchpad in output, got %q", out)
	}
}

func TestExecTool_OnStartCallback(t *testing.T) {
	et := NewExecTool("", "")
	ctx := context.Background()
	var gotProcess bool

	handle, err := et.Start(ctx, ExecRequest{
		Path: shellForTest(t),
		Args: shellArgsForTest("-c", "sleep 0.1"),
	}, ExecOptions{
		Timeout: 5 * time.Second,
		OnStart: func(p *os.Process) {
			gotProcess = p != nil
		},
	})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if _, err := handle.Wait(); err != nil {
		t.Fatalf("Wait failed: %v", err)
	}
	if !gotProcess {
		t.Error("OnStart callback did not receive a non-nil process")
	}
}

func TestExecTool_Timeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("timeout signal assertions are Unix-specific; Windows uses taskkill")
	}

	et := NewExecTool("", "")
	ctx := context.Background()

	handle, err := et.Start(ctx, ExecRequest{
		Path: shellForTest(t),
		Args: shellArgsForTest("-c", "sleep 10"),
	}, ExecOptions{Timeout: 200 * time.Millisecond})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	result, err := handle.Wait()
	if err != nil {
		t.Fatalf("Wait failed: %v", err)
	}
	if !result.TimedOut {
		t.Error("expected TimedOut=true")
	}
}

func TestExecTool_Cancellation(t *testing.T) {
	et := NewExecTool("", "")
	ctx, cancel := context.WithCancel(context.Background())

	handle, err := et.Start(ctx, ExecRequest{
		Path: shellForTest(t),
		Args: shellArgsForTest("-c", "sleep 10"),
	}, ExecOptions{Timeout: 30 * time.Second})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	cancel()

	result, err := handle.Wait()
	if err != nil {
		t.Fatalf("Wait failed: %v", err)
	}
	if !result.Cancelled {
		t.Error("expected Cancelled=true")
	}
}

func shellForTest(t *testing.T) string {
	t.Helper()
	path, err := resolveShell()
	if err != nil {
		t.Skip("no shell available for exec tests")
	}
	return path
}

func shellArgsForTest(args ...string) []string {
	return args
}
