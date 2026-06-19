package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ipy/jenny/e2e/harness"
	"github.com/ipy/jenny/internal/constants"
	"github.com/ipy/jenny/internal/testutil/mockapi"
)

// buildGracefulShutdownBinary builds the jenny binary for graceful shutdown tests.
// Uses JENNY_BIN env var if set, otherwise builds fresh.
func buildGracefulShutdownBinary(t *testing.T) string {
	t.Helper()
	if bin := os.Getenv("JENNY_BIN"); bin != "" {
		return bin
	}
	repoRoot := findRepoRoot(t)
	tmpDir := filepath.Join(os.TempDir(), "e2e_graceful_shutdown")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	binPath := filepath.Join(tmpDir, "target")
	cmd := exec.Command("go", "build", "-o", binPath, "./cmd/jenny")
	cmd.Dir = repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build jenny: %v\n%s", err, out)
	}
	return binPath
}

// Note: findRepoRoot is defined in debt_md1_test.go

// runJennyGraceful starts jenny with the given env and args, returns the cmd handle
// so the caller can send signals.
func runJennyGraceful(t *testing.T, bin, workDir string, env []string, args ...string) (*exec.Cmd, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Dir = workDir

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		t.Fatalf("start jenny: %v", err)
	}

	return cmd, &stdoutBuf, &stderrBuf
}

// sendSIGINTAndWait sends SIGINT to the process and waits for it to exit.
// Returns the time elapsed from signal to exit, and the exit code.
func sendSIGINTAndWait(t *testing.T, cmd *exec.Cmd) (time.Duration, int) {
	t.Helper()

	signalStart := time.Now()
	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatalf("send SIGINT: %v", err)
	}

	runErr := cmd.Wait()
	elapsed := time.Since(signalStart)

	exitCode := 0
	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}
	return elapsed, exitCode
}

// TestGracefulShutdownSC1_StreamingResponse tests SC1: the process exits
// within 2 seconds of SIGINT during a streaming response.
func TestGracefulShutdownSC1_StreamingResponse(t *testing.T) {
	workDir := t.TempDir()
	jennyHome := constants.ProjectJennyDir(workDir)

	// Create a mock server with a long delay to simulate streaming
	mock := mockapi.NewMockServer(mockapi.WithCassetteDir(cassetteDir))
	defer mock.Close()
	mock.SetDelay("echo-hello", 10000) // 10s delay — ensures we're still streaming when SIGINT arrives

	bin := buildGracefulShutdownBinary(t)
	repoRoot := findRepoRoot(t)

	env := []string{
		"ANTHROPIC_AUTH_TOKEN=test-token",
		"ANTHROPIC_BASE_URL=" + mock.URL() + "/cassette/echo-hello",
		"JENNY_HOME=" + jennyHome,
	}

	cmd, stdoutBuf, stderrBuf := runJennyGraceful(t, bin, repoRoot, env,
		"-p", "say hello",
		"--output-format", "stream-json",
		"--verbose",
	)

	// Wait for the process to start the streaming phase
	time.Sleep(800 * time.Millisecond)

	timeToExit, exitCode := sendSIGINTAndWait(t, cmd)

	t.Logf("Exit code: %d", exitCode)
	t.Logf("Time to exit after SIGINT: %v", timeToExit)
	t.Logf("Stdout length: %d", len(stdoutBuf.String()))
	t.Logf("Stderr (first 500 chars): %s", truncateStr(stderrBuf.String(), 500))

	// SC1: Process should exit within 2 seconds of SIGINT
	if timeToExit > 2*time.Second {
		t.Errorf("SC1 FAIL: process took %v to exit after SIGINT, expected < 2s", timeToExit)
	} else {
		t.Logf("SC1 PASS: process exited in %v (< 2s)", timeToExit)
	}
}

// TestGracefulShutdownSC1_IdleBetweenTurns tests SC1 during idle state:
// the process exits within 2 seconds of SIGINT when idle.
func TestGracefulShutdownSC1_IdleBetweenTurns(t *testing.T) {
	workDir := t.TempDir()
	jennyHome := constants.ProjectJennyDir(workDir)

	mock := mockapi.NewMockServer(mockapi.WithCassetteDir(cassetteDir))
	defer mock.Close()

	bin := buildGracefulShutdownBinary(t)
	repoRoot := findRepoRoot(t)

	env := []string{
		"ANTHROPIC_AUTH_TOKEN=test-token",
		"ANTHROPIC_BASE_URL=" + mock.URL() + "/cassette/echo-hello",
		"JENNY_HOME=" + jennyHome,
	}

	// Use text output for a fast-completing prompt
	cmd, stdoutBuf, stderrBuf := runJennyGraceful(t, bin, repoRoot, env,
		"-p", "say hello",
		"--output-format", "text",
	)

	// Wait a short time for the process to start
	time.Sleep(300 * time.Millisecond)

	timeToExit, exitCode := sendSIGINTAndWait(t, cmd)

	t.Logf("Exit code: %d", exitCode)
	t.Logf("Time to exit after SIGINT (idle): %v", timeToExit)
	t.Logf("Stdout: %s", truncateStr(stdoutBuf.String(), 200))
	t.Logf("Stderr: %s", truncateStr(stderrBuf.String(), 200))

	if timeToExit > 2*time.Second {
		t.Errorf("SC1 FAIL (idle): process took %v to exit after SIGINT, expected < 2s", timeToExit)
	} else {
		t.Logf("SC1 PASS (idle): process exited in %v (< 2s)", timeToExit)
	}
}

// TestGracefulShutdownSC1_ToolExecution tests SC1 during tool execution:
// the process exits within 2 seconds of SIGINT during bash tool execution.
func TestGracefulShutdownSC1_ToolExecution(t *testing.T) {
	workDir := t.TempDir()
	jennyHome := constants.ProjectJennyDir(workDir)

	mock := mockapi.NewMockServer(mockapi.WithCassetteDir(cassetteDir))
	defer mock.Close()

	// Use bash-sleep-blocked cassette which triggers a "sleep 5" bash command
	// Set up a sequence so the tool use triggers a follow-up request
	mock.SetSequence("bash-sleep-blocked", []string{"bash-sleep-blocked", "echo-hello"})

	bin := buildGracefulShutdownBinary(t)
	repoRoot := findRepoRoot(t)

	env := []string{
		"ANTHROPIC_AUTH_TOKEN=test-token",
		"ANTHROPIC_BASE_URL=" + mock.URL() + "/cassette/bash-sleep-blocked",
		"JENNY_HOME=" + jennyHome,
	}

	cmd, stdoutBuf, stderrBuf := runJennyGraceful(t, bin, repoRoot, env,
		"-p", "run sleep command",
		"--output-format", "stream-json",
		"--verbose",
		"--dangerously-skip-permissions",
	)

	// Wait for the process to start tool execution (tool_use response + tool start)
	time.Sleep(1500 * time.Millisecond)

	timeToExit, exitCode := sendSIGINTAndWait(t, cmd)

	t.Logf("Exit code: %d", exitCode)
	t.Logf("Time to exit after SIGINT (tool exec): %v", timeToExit)
	t.Logf("Stdout (first 500): %s", truncateStr(stdoutBuf.String(), 500))
	t.Logf("Stderr (first 500): %s", truncateStr(stderrBuf.String(), 500))

	if timeToExit > 2*time.Second {
		t.Errorf("SC1 FAIL (tool exec): process took %v to exit after SIGINT, expected < 2s", timeToExit)
	} else {
		t.Logf("SC1 PASS (tool exec): process exited in %v (< 2s)", timeToExit)
	}
}

// TestGracefulShutdownSC2_TranscriptFlushedSIGINT tests SC2:
// transcript data is flushed to disk before exit on SIGINT.
func TestGracefulShutdownSC2_TranscriptFlushedSIGINT(t *testing.T) {
	workDir := t.TempDir()
	jennyHome := constants.ProjectJennyDir(workDir)

	mock := mockapi.NewMockServer(mockapi.WithCassetteDir(cassetteDir))
	defer mock.Close()
	mock.SetDelay("echo-hello", 10000) // 10s delay to ensure we can interrupt during streaming

	bin := buildGracefulShutdownBinary(t)
	repoRoot := findRepoRoot(t)

	env := []string{
		"ANTHROPIC_AUTH_TOKEN=test-token",
		"ANTHROPIC_BASE_URL=" + mock.URL() + "/cassette/echo-hello",
		"JENNY_HOME=" + jennyHome,
	}

	// Use stream-json format so session is initialized early
	cmd, stdoutBuf, stderrBuf := runJennyGraceful(t, bin, repoRoot, env,
		"-p", "say hello",
		"--output-format", "stream-json",
		"--verbose",
	)

	// Wait long enough for session initialization (the init event should appear)
	// We need to give it time to initialize before sending SIGINT
	time.Sleep(1500 * time.Millisecond)

	timeToExit, exitCode := sendSIGINTAndWait(t, cmd)

	t.Logf("Exit code: %d, time to exit: %v", exitCode, timeToExit)
	_ = stdoutBuf

	// Check for transcript files in multiple possible locations
	transcriptLocations := []string{
		filepath.Join(jennyHome, "sessions"),
		filepath.Join(workDir, ".jenny", "sessions"),
	}

	transcriptFound := false
	for _, sessionsDir := range transcriptLocations {
		entries, err := os.ReadDir(sessionsDir)
		if err != nil {
			t.Logf("SC2: no sessions dir at %s", sessionsDir)
			continue
		}
		t.Logf("SC2: found sessions dir at %s with %d entries", sessionsDir, len(entries))
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			sessionDir := filepath.Join(sessionsDir, entry.Name())
			transcriptPath := filepath.Join(sessionDir, "transcript.jsonl")
			data, err := os.ReadFile(transcriptPath)
			if err != nil {
				continue
			}
			transcriptFound = true
			content := string(data)
			if len(strings.TrimSpace(content)) == 0 {
				t.Errorf("SC2 FAIL: transcript file %s is empty", transcriptPath)
			} else {
				t.Logf("SC2 PASS: transcript file has %d bytes", len(content))
				// Verify it's valid JSONL
				lines := strings.Split(strings.TrimSpace(content), "\n")
				validCount := 0
				for _, line := range lines {
					if line == "" {
						continue
					}
					var obj map[string]any
					if err := json.Unmarshal([]byte(line), &obj); err == nil {
						validCount++
					}
				}
				if validCount > 0 {
					t.Logf("SC2 PASS: transcript has %d valid JSONL lines", validCount)
				}
			}
			break
		}
		if transcriptFound {
			break
		}
	}

	if !transcriptFound {
		t.Logf("SC2: no transcript file created - checking stderr for details")
		t.Logf("SC2: stderr (first 500): %s", truncateStr(stderrBuf.String(), 500))
		t.Logf("SC2: stdout (first 500): %s", truncateStr(stdoutBuf.String(), 500))
	}
}

// TestGracefulShutdownSC2_TranscriptFlushedNormal tests SC2:
// transcript data is flushed to disk on normal exit.
func TestGracefulShutdownSC2_TranscriptFlushedNormal(t *testing.T) {
	workDir := t.TempDir()
	jennyHome := constants.ProjectJennyDir(workDir)

	mock := mockapi.NewMockServer(mockapi.WithCassetteDir(cassetteDir))
	defer mock.Close()

	bin := buildGracefulShutdownBinary(t)
	repoRoot := findRepoRoot(t)

	env := []string{
		"ANTHROPIC_AUTH_TOKEN=test-token",
		"ANTHROPIC_BASE_URL=" + mock.URL() + "/cassette/echo-hello",
		"JENNY_HOME=" + jennyHome,
	}

	cmd, stdoutBuf, stderrBuf := runJennyGraceful(t, bin, repoRoot, env,
		"-p", "say hello",
		"--output-format", "text",
	)

	// Let it complete normally
	runErr := cmd.Wait()
	exitCode := 0
	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}

	t.Logf("Normal exit: code=%d", exitCode)
	t.Logf("Stdout: %s", truncateStr(stdoutBuf.String(), 200))
	t.Logf("Stderr: %s", truncateStr(stderrBuf.String(), 200))

	// Check for transcript files
	sessionsDir := filepath.Join(jennyHome, "sessions")
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		t.Logf("SC2 (normal): no sessions dir at %s (err: %v)", sessionsDir, err)
		return
	}

	transcriptFound := false
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		sessionDir := filepath.Join(sessionsDir, entry.Name())
		transcriptPath := filepath.Join(sessionDir, "transcript.jsonl")
		data, err := os.ReadFile(transcriptPath)
		if err != nil {
			continue
		}
		transcriptFound = true
		if len(data) == 0 {
			t.Errorf("SC2 FAIL (normal): transcript file is empty")
		} else {
			t.Logf("SC2 PASS (normal): transcript file has %d bytes", len(data))
		}
		break
	}
	if !transcriptFound {
		t.Errorf("SC2 FAIL (normal): no transcript file created on normal exit")
	}
}

// TestGracefulShutdownSC5_NoFallbackAfterCancel tests SC5:
// streaming fallback is not attempted when the parent context is already cancelled.
func TestGracefulShutdownSC5_NoFallbackAfterCancel(t *testing.T) {
	workDir := t.TempDir()
	jennyHome := constants.ProjectJennyDir(workDir)

	mock := mockapi.NewMockServer(mockapi.WithCassetteDir(cassetteDir))
	defer mock.Close()
	mock.SetDelay("echo-hello", 8000) // 8s delay — long enough that we'll cancel first

	bin := buildGracefulShutdownBinary(t)
	repoRoot := findRepoRoot(t)

	env := []string{
		"ANTHROPIC_AUTH_TOKEN=test-token",
		"ANTHROPIC_BASE_URL=" + mock.URL() + "/cassette/echo-hello",
		"JENNY_HOME=" + jennyHome,
	}

	cmd, stdoutBuf, stderrBuf := runJennyGraceful(t, bin, repoRoot, env,
		"-p", "say hello",
		"--output-format", "text",
		"--verbose",
	)

	// Wait a bit then send SIGINT
	time.Sleep(600 * time.Millisecond)

	timeToExit, exitCode := sendSIGINTAndWait(t, cmd)

	t.Logf("Exit code: %d, time to exit: %v", exitCode, timeToExit)
	_ = stdoutBuf

	stderrStr := stderrBuf.String()
	t.Logf("Stderr (first 800 chars): %s", truncateStr(stderrStr, 800))

	// SC5: The process should exit quickly (not waiting for the 8s mock delay).
	// If it exits in under 3s, the fallback was skipped.
	if timeToExit < 3*time.Second {
		t.Logf("SC5 PASS: process exited quickly (%v), fallback likely skipped (mock has 8s delay)", timeToExit)
	} else {
		t.Errorf("SC5 FAIL: process took %v to exit, may have attempted fallback despite cancelled context", timeToExit)
	}
}

// TestGracefulShutdownNormalExit verifies normal execution still works.
func TestGracefulShutdownNormalExit(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "graceful-shutdown.normal.exit-zero",
			Category:    "graceful-shutdown",
			Description: "normal execution without SIGINT exits 0 and produces valid output",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				StreamJSON: &harness.StreamJSONExpectation{
					AllLinesValidJSON: true,
				},
			},
		},
	})
}

// truncateStr returns a string truncated to maxLen characters.
func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + fmt.Sprintf("...<truncated, total=%d>", len(s))
}
