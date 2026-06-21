package tool

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ipy/jenny/internal/sandbox"
)

func TestBashTool_ShellFallback_SimpleCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fallback PATH isolation test is Unix-oriented")
	}

	cwd := t.TempDir()
	echoPath, err := exec.LookPath("echo")
	if err != nil {
		t.Skip("echo not on PATH")
	}
	helper := filepath.Join(cwd, "hello-bin")
	if err := os.Symlink(echoPath, helper); err != nil {
		t.Fatal(err)
	}

	withNoShellPATH(t, func() {
		t.Setenv("PATH", cwd)

		tool := NewBashTool(PermissionUnrestricted).WithExecTool(NewExecTool("", ""))
		result, err := tool.Execute(context.Background(), map[string]any{
			"command": "hello-bin fallback-hello",
		}, cwd)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.IsError {
			t.Fatalf("expected success via fallback, got: %s", result.Content)
		}
		if !strings.Contains(result.Content, "fallback-hello") {
			t.Errorf("expected fallback output, got: %q", result.Content)
		}
	})
}

func TestBashTool_ShellFallback_PipelineRejected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fallback PATH isolation test is Unix-oriented")
	}

	cwd := t.TempDir()
	withNoShellPATH(t, func() {
		t.Setenv("PATH", cwd)

		tool := NewBashTool(PermissionEdit).WithExecTool(NewExecTool("", ""))
		result, err := tool.Execute(context.Background(), map[string]any{
			"command": "echo a | echo b",
		}, cwd)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.IsError {
			t.Fatal("expected error for pipeline without shell")
		}
		assertStructuredError(t, result.Content, "shell_unavailable", "unsupported:")
		if !strings.Contains(result.Content, "shell operator") {
			t.Errorf("expected operator in unsupported list: %s", result.Content)
		}
	})
}

func TestBashTool_ShellFallback_BuiltinRejected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fallback PATH isolation test is Unix-oriented")
	}

	cwd := t.TempDir()
	withNoShellPATH(t, func() {
		t.Setenv("PATH", cwd)

		tool := NewBashTool(PermissionEdit).WithExecTool(NewExecTool("", ""))
		result, err := tool.Execute(context.Background(), map[string]any{
			"command": "cd src",
		}, cwd)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.IsError {
			t.Fatal("expected error for cd builtin without shell")
		}
		assertStructuredError(t, result.Content, "shell_unavailable", "unsupported:")
		if !strings.Contains(result.Content, `"cd" is a shell builtin`) {
			t.Errorf("expected cd builtin rejection: %s", result.Content)
		}
	})
}

func TestBashTool_ShellFallback_SandboxActive(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fallback PATH isolation test is Unix-oriented")
	}

	cwd := t.TempDir()
	sb := sandbox.NewMockSandboxManager()
	sb.SetActive(true)

	withNoShellPATH(t, func() {
		t.Setenv("PATH", cwd)

		tool := NewBashTool(PermissionEdit).
			WithSandbox(sb).
			WithExecTool(NewExecTool("", ""))

		result, err := tool.Execute(context.Background(), map[string]any{
			"command": "ls -la",
		}, cwd)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.IsError {
			t.Fatal("expected sandbox_violation when sandbox active and shell missing")
		}
		assertStructuredError(t, result.Content, "sandbox_violation", "hint:")
	})
}

// TestBashTool_ShellExecFallback_BrokenShell verifies that when resolveShell
// returns a path that passes stat checks but fails at fork/exec time (e.g.
// container overlay FS corruption), the code falls through to direct argv
// execution instead of surfacing a raw ENOENT to the model.
func TestBashTool_ShellExecFallback_BrokenShell(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix-only test")
	}

	cwd := t.TempDir()

	// Simulate a shell that exists on disk but can't be exec'd:
	// resolveShellHook returns a nonexistent path (skipping the stat checks
	// that the real resolveShell does). This reproduces the Docker overlay FS
	// scenario where /usr/bin/bash's inode exists but exec fails with ENOENT.
	resolveShellHook = func() (string, error) {
		return "/nonexistent/broken-shell", nil
	}
	t.Cleanup(func() { resolveShellHook = nil })

	// "ls" is a real external binary — parseSimpleCommand can handle it
	// and runForegroundExec can execute it directly.
	if _, err := exec.LookPath("ls"); err != nil {
		t.Skip("ls not in PATH")
	}

	tool := NewBashTool(PermissionUnrestricted).WithExecTool(NewExecTool("", ""))
	result, err := tool.Execute(context.Background(), map[string]any{
		"command": "ls",
	}, cwd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should succeed via the fallback path, not return an ENOENT error.
	if result.IsError {
		t.Fatalf("expected success via exec fallback after broken shell, got error: %s", result.Content)
	}
}

// TestBashTool_ShellExecFallback_PipelineStillRejected verifies that when the
// shell is broken AND the command uses shell features (pipes), the fallback
// correctly rejects it with a structured error (not a raw ENOENT).
func TestBashTool_ShellExecFallback_PipelineStillRejected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix-only test")
	}

	cwd := t.TempDir()

	resolveShellHook = func() (string, error) {
		return "/nonexistent/broken-shell", nil
	}
	t.Cleanup(func() { resolveShellHook = nil })

	tool := NewBashTool(PermissionUnrestricted).WithExecTool(NewExecTool("", ""))
	result, err := tool.Execute(context.Background(), map[string]any{
		"command": "echo a | grep a",
	}, cwd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for pipeline with broken shell")
	}
	assertStructuredError(t, result.Content, "shell_unavailable", "unsupported:")
}

func withNoShellPATH(t *testing.T, fn func()) {
	t.Helper()
	resolveShellHook = func() (string, error) {
		return "", os.ErrNotExist
	}
	t.Cleanup(func() {
		resolveShellHook = nil
	})
	fn()
}

func assertStructuredError(t *testing.T, content, category, requiredLine string) {
	t.Helper()
	if !strings.HasPrefix(content, "error: "+category) {
		t.Errorf("expected error category %q, got:\n%s", category, content)
	}
	if !strings.Contains(content, requiredLine) {
		t.Errorf("expected %q in error:\n%s", requiredLine, content)
	}
	if !strings.Contains(content, "reason:") {
		t.Errorf("expected reason: line in error:\n%s", content)
	}
}
