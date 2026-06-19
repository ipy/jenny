package e2e_test

import (
	"strings"
	"testing"
	"time"

	"github.com/ipy/jenny/e2e/harness"
)

// TestDebtMD9BackgroundFetchCancellation verifies that the background registry
// fetch goroutine has proper cancellation (debt MD-9).
//
// Spec requirements:
//  1. The background fetch goroutine should be cancellable via context.
//  2. When the 3s soft timeout fires, the context should be cancelled,
//     aborting the in-flight HTTP request.
//  3. The goroutine should not outlive the soft timeout.
//  4. The synchronous --refresh-registry path should not be affected.
//  5. Proper logging when fetch is cancelled vs completes vs errors.
func TestDebtMD9BackgroundFetchCancellation(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "debt-md9.startup-does-not-hang",
			Category:    "debt-md9",
			Description: "jenny startup completes quickly even when registry fetch is happening in background",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--print-system-prompt"},
				Env: []string{
					"ANTHROPIC_AUTH_TOKEN=dummy",
				},
				// The soft timeout is 3s, so startup should complete well within 15s.
				// If the goroutine leaked (pre-fix behavior), it could take up to 30s.
				TimeoutMs: 15000,
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Length: &harness.LengthExpectation{Min: 100},
				},
			},
		},
		{
			ID:          "debt-md9.offline-skips-background-fetch",
			Category:    "debt-md9",
			Description: "--offline skips the background fetch entirely, no registry log messages appear",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--offline", "--verbose", "-p", "say hi", "--output-format", "stream-json"},
				Env: []string{
					"ANTHROPIC_AUTH_TOKEN=dummy",
				},
				TimeoutMs: 15000,
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				StreamJSON: &harness.StreamJSONExpectation{
					AllLinesValidJSON: true,
					LastEvent: &harness.EventExpectation{
						Type:    "result",
						Subtype: "success",
					},
				},
				// With --offline, there should be no "background fetch" or "registry fetch" messages.
				Stderr: &harness.StderrExpectation{
					NotContains: []string{"background fetch", "registry fetch"},
				},
			},
		},
	})
}

// TestDebtMD9StartupCompletesQuickly verifies that jenny startup does not
// hang when the registry endpoint is slow. This is the core of MD-9:
// the background fetch goroutine must not block startup.
//
// We run jenny with --print-system-prompt which should exit quickly
// regardless of registry fetch status. The background fetch runs
// concurrently and should not delay the main process.
func TestDebtMD9StartupCompletesQuickly(t *testing.T) {
	env := []string{
		"ANTHROPIC_AUTH_TOKEN=dummy",
	}

	start := time.Now()
	res := harness.RunJenny(t, env, "--print-system-prompt")
	elapsed := time.Since(start)

	if res.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d; stderr=%s", res.ExitCode, res.Stderr)
	}

	if len(res.Stdout) < 100 {
		t.Errorf("expected stdout content, got %d chars", len(res.Stdout))
	}

	// The soft timeout is 3s. With context cancellation, the process should
	// complete well within 15s (allowing for TCP SYN retransmission overhead).
	// Without the fix, the goroutine would leak for up to 30s (HTTP client timeout).
	if elapsed > 15*time.Second {
		t.Errorf("startup took too long: %v (expected < 15s); goroutine may be leaking", elapsed)
	} else {
		t.Logf("startup completed in %v (OK)", elapsed)
	}
}

// TestDebtMD9SyncRefreshRegistryNotBroken verifies that the synchronous
// --refresh-registry path still works correctly after the cancellation
// changes to the background fetch path.
//
// The --refresh-registry flag hits the real GitHub endpoint. It may succeed
// (exit 0) or fail with a parse error on the real data (exit 1). In either
// case it must NOT hang, crash, or panic.
func TestDebtMD9SyncRefreshRegistryNotBroken(t *testing.T) {
	env := []string{
		"ANTHROPIC_AUTH_TOKEN=dummy",
	}

	res := harness.RunJenny(t, env, "--refresh-registry", "-p", "hello")

	// The synchronous --refresh-registry should either succeed or fail
	// with a clear error. It should NOT hang, crash, or panic.
	stderr := res.Stderr
	if !strings.Contains(stderr, "registry") && !strings.Contains(stderr, "refresh") {
		t.Errorf("expected stderr to mention registry/refresh, got: %s", stderr)
	}

	// Exit code can be 0 (success) or 1 (failure), but not -1 (killed)
	if res.ExitCode != 0 && res.ExitCode != 1 {
		t.Errorf("expected exit code 0 or 1, got %d; stderr=%s", res.ExitCode, res.Stderr)
	}

	t.Logf("sync --refresh-registry: exit=%d, stderr=%s", res.ExitCode, stderr)
}

// TestDebtMD9BackgroundFetchDoesNotBlockShutdown verifies the core MD-9 fix:
// the background fetch goroutine does not block process shutdown.
// When jenny completes its main task quickly, the process exits without
// waiting for the background fetch to finish.
func TestDebtMD9BackgroundFetchDoesNotBlockShutdown(t *testing.T) {
	env := []string{
		"ANTHROPIC_AUTH_TOKEN=dummy",
	}

	// Run with --verbose to see all log messages
	start := time.Now()
	res := harness.RunJenny(t, env, "--verbose", "-p", "say hi", "--output-format", "stream-json")
	elapsed := time.Since(start)

	// Main task should complete successfully
	if res.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d; stderr=%s", res.ExitCode, res.Stderr)
	}

	// Verify NDJSON output
	lines := strings.Split(strings.TrimSpace(res.Stdout), "\n")
	if len(lines) < 3 {
		t.Errorf("expected at least 3 NDJSON lines, got %d", len(lines))
	}

	// The crucial assertion: startup should be fast.
	// With MD-9 fix, the background fetch is cancelled when the main flow
	// completes, so total time should be well under 30s.
	// Without the fix, the goroutine could leak for up to 30s.
	if elapsed > 20*time.Second {
		t.Errorf("process took too long: %v (expected < 20s); background goroutine may be leaking", elapsed)
	} else {
		t.Logf("process completed in %v (OK - background fetch does not block shutdown)", elapsed)
	}

	// Check stderr for registry-related messages (may or may not appear
	// depending on timing of background fetch vs process shutdown)
	t.Logf("stderr: %s", res.Stderr)
}

// TestDebtMD9NormalStartupWithMockAPI verifies that normal startup
// with a reachable mock API still works correctly after the
// cancellation changes.
func TestDebtMD9NormalStartupWithMockAPI(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "debt-md9.normal-startup.with-mock-api",
			Category:    "debt-md9",
			Description: "normal startup with reachable mock API completes successfully; background fetch does not interfere",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Args:     []string{"--verbose"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				StreamJSON: &harness.StreamJSONExpectation{
					AllLinesValidJSON: true,
					LastEvent: &harness.EventExpectation{
						Type:    "result",
						Subtype: "success",
					},
				},
			},
		},
	})
}

// TestDebtMD9BackgroundFetchCancelledOnShutdown verifies that the background
// fetch goroutine's context is cancelled when the process shuts down.
// This is tested by checking that:
// 1. The process exits cleanly (exit code 0)
// 2. The process does not hang waiting for the background fetch
// 3. No panic or crash occurs
func TestDebtMD9BackgroundFetchCancelledOnShutdown(t *testing.T) {
	env := []string{
		"ANTHROPIC_AUTH_TOKEN=dummy",
	}

	// Run multiple times to ensure consistency
	for i := 0; i < 3; i++ {
		start := time.Now()
		res := harness.RunJenny(t, env, "--print-system-prompt")
		elapsed := time.Since(start)

		if res.ExitCode != 0 {
			t.Errorf("iteration %d: expected exit code 0, got %d; stderr=%s", i, res.ExitCode, res.Stderr)
		}

		if len(res.Stdout) < 100 {
			t.Errorf("iteration %d: expected stdout content, got %d chars", i, len(res.Stdout))
		}

		if elapsed > 15*time.Second {
			t.Errorf("iteration %d: took too long: %v (expected < 15s)", i, elapsed)
		} else {
			t.Logf("iteration %d: completed in %v", i, elapsed)
		}
	}
}
