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
