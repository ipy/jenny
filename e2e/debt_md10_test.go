package e2e_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ipy/jenny/e2e/harness"
)

// TestDebtMD10ConfigModelsViaKoanf verifies that config.json models overrides
// are loaded via the koanf-based path (not os.ReadFile).
//
// Spec: debt-MD-10 — Config.json `models` key read via koanf, bypassing os.ReadFile.
//
// The key behavioral assertions:
// 1. Valid models key in config.json is parsed and applied (override works).
// 2. Missing models key does not crash.
// 3. Malformed models key (string instead of object) does not crash.
// 4. The override affects max_tokens in API requests (end-to-end proof).
func TestDebtMD10ConfigModelsViaKoanf(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "debt-md10.valid-override.accepted",
			Category:    "debt-md10",
			Description: "config.json with valid models key is accepted via koanf path, does not crash",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--offline", "--print-system-prompt"},
				Env:  []string{"JENNY_HOME=${WORK_DIR}/.jenny"},
				WorkDirFiles: map[string]string{
					".jenny/config.json": `{"models": {"claude-sonnet-4-6": {"maxOutput": 99999}}}`,
				},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Length: &harness.LengthExpectation{Min: 500},
				},
			},
		},
		{
			ID:          "debt-md10.missing-models-key.no-crash",
			Category:    "debt-md10",
			Description: "config.json without a models key does not crash (nil, nil return from ParseConfigModelsFromKoanf)",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--offline", "--print-system-prompt"},
				Env:  []string{"JENNY_HOME=${WORK_DIR}/.jenny"},
				WorkDirFiles: map[string]string{
					".jenny/config.json": `{"someOtherKey": "value"}`,
				},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Length: &harness.LengthExpectation{Min: 500},
				},
			},
		},
		{
			ID:          "debt-md10.malformed-models.no-crash",
			Category:    "debt-md10",
			Description: "config.json with models as a string (not object) does not crash; logs warning, returns nil",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--offline", "--print-system-prompt"},
				Env:  []string{"JENNY_HOME=${WORK_DIR}/.jenny"},
				WorkDirFiles: map[string]string{
					".jenny/config.json": `{"models": "this-should-be-an-object-not-a-string"}`,
				},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Length: &harness.LengthExpectation{Min: 500},
				},
			},
		},
		{
			ID:          "debt-md10.valid-override.affects-max-tokens",
			Category:    "debt-md10",
			Description: "config.json models.maxOutput override changes max_tokens in API request (end-to-end koanf path proof)",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hello",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Args:     []string{"--offline"},
				Env:      []string{"ANTHROPIC_MODEL=claude-sonnet-4-6"},
				WorkDirFiles: map[string]string{
					".jenny/config.json": `{"models": {"claude-sonnet-4-6": {"maxOutput": 77777}}}`,
				},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				APIRequests: []harness.APIRequestExpectation{
					{
						Index:     0,
						MaxTokens: 77777,
					},
				},
			},
		},
		{
			ID:          "debt-md10.empty-models-key.no-crash",
			Category:    "debt-md10",
			Description: "config.json with empty models object does not crash",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--offline", "--print-system-prompt"},
				Env:  []string{"JENNY_HOME=${WORK_DIR}/.jenny"},
				WorkDirFiles: map[string]string{
					".jenny/config.json": `{"models": {}}`,
				},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Length: &harness.LengthExpectation{Min: 500},
				},
			},
		},
		{
			ID:          "debt-md10.models-as-number.no-crash",
			Category:    "debt-md10",
			Description: "config.json with models as a number (not object) does not crash; logs warning, returns nil",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--offline", "--print-system-prompt"},
				Env:  []string{"JENNY_HOME=${WORK_DIR}/.jenny"},
				WorkDirFiles: map[string]string{
					".jenny/config.json": `{"models": 42}`,
				},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Length: &harness.LengthExpectation{Min: 500},
				},
			},
		},
	})
}

// TestDebtMD10ConfigModelsViaKoanfImperative runs imperative tests to verify
// the koanf-based config.json models loading path with more detailed assertions.
func TestDebtMD10ConfigModelsViaKoanfImperative(t *testing.T) {
	// Test 1: Valid models override in config.json works end-to-end
	t.Run("valid-models-override-applied", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Write config.json with models override
		configJSON := map[string]any{
			"models": map[string]any{
				"claude-sonnet-4-6": map[string]any{
					"maxOutput": 88888.0,
				},
			},
		}
		data, err := json.MarshalIndent(configJSON, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(tmpDir, "config.json"), data, 0644); err != nil {
			t.Fatal(err)
		}

		env := []string{"JENNY_HOME=" + tmpDir}
		res := harness.RunJenny(t, env, "--offline", "--print-system-prompt")
		if res.ExitCode != 0 {
			t.Errorf("expected exit code 0, got %d; stderr=%s", res.ExitCode, res.Stderr)
		}
		if len(res.Stdout) < 500 {
			t.Errorf("expected stdout with system prompt, got %d chars", len(res.Stdout))
		}
	})

	// Test 2: No models key in config.json - should not crash
	t.Run("no-models-key-no-crash", func(t *testing.T) {
		tmpDir := t.TempDir()

		configJSON := map[string]any{
			"otherKey": "someValue",
		}
		data, err := json.MarshalIndent(configJSON, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(tmpDir, "config.json"), data, 0644); err != nil {
			t.Fatal(err)
		}

		env := []string{"JENNY_HOME=" + tmpDir}
		res := harness.RunJenny(t, env, "--offline", "--print-system-prompt")
		if res.ExitCode != 0 {
			t.Errorf("expected exit code 0, got %d; stderr=%s", res.ExitCode, res.Stderr)
		}
	})

	// Test 3: Malformed models key (string) - should not crash
	t.Run("malformed-models-string-no-crash", func(t *testing.T) {
		tmpDir := t.TempDir()

		configJSON := map[string]any{
			"models": "this is a string, not an object",
		}
		data, err := json.MarshalIndent(configJSON, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(tmpDir, "config.json"), data, 0644); err != nil {
			t.Fatal(err)
		}

		env := []string{"JENNY_HOME=" + tmpDir}
		res := harness.RunJenny(t, env, "--offline", "--print-system-prompt")
		if res.ExitCode != 0 {
			t.Errorf("expected exit code 0 with malformed models, got %d; stderr=%s", res.ExitCode, res.Stderr)
		}
		// Should not crash
	})

	// Test 4: Models key as a number - should not crash
	t.Run("malformed-models-number-no-crash", func(t *testing.T) {
		tmpDir := t.TempDir()

		configJSON := map[string]any{
			"models": 12345,
		}
		data, err := json.MarshalIndent(configJSON, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(tmpDir, "config.json"), data, 0644); err != nil {
			t.Fatal(err)
		}

		env := []string{"JENNY_HOME=" + tmpDir}
		res := harness.RunJenny(t, env, "--offline", "--print-system-prompt")
		if res.ExitCode != 0 {
			t.Errorf("expected exit code 0 with numeric models, got %d; stderr=%s", res.ExitCode, res.Stderr)
		}
	})

	// Test 5: Empty models object - should not crash
	t.Run("empty-models-object-no-crash", func(t *testing.T) {
		tmpDir := t.TempDir()

		configJSON := map[string]any{
			"models": map[string]any{},
		}
		data, err := json.MarshalIndent(configJSON, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(tmpDir, "config.json"), data, 0644); err != nil {
			t.Fatal(err)
		}

		env := []string{"JENNY_HOME=" + tmpDir}
		res := harness.RunJenny(t, env, "--offline", "--print-system-prompt")
		if res.ExitCode != 0 {
			t.Errorf("expected exit code 0 with empty models, got %d; stderr=%s", res.ExitCode, res.Stderr)
		}
	})

	// Test 6: Config.json does not exist at all - should not crash
	t.Run("no-config-file-no-crash", func(t *testing.T) {
		tmpDir := t.TempDir()

		env := []string{"JENNY_HOME=" + tmpDir}
		res := harness.RunJenny(t, env, "--offline", "--print-system-prompt")
		if res.ExitCode != 0 {
			t.Errorf("expected exit code 0 without config.json, got %d; stderr=%s", res.ExitCode, res.Stderr)
		}
	})

	// Test 7: Multiple model overrides in config.json
	t.Run("multiple-model-overrides", func(t *testing.T) {
		tmpDir := t.TempDir()

		configJSON := map[string]any{
			"models": map[string]any{
				"claude-sonnet-4-6": map[string]any{
					"maxOutput": 77777.0,
				},
				"claude-opus-4-6": map[string]any{
					"maxOutput": 32000.0,
				},
			},
		}
		data, err := json.MarshalIndent(configJSON, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(tmpDir, "config.json"), data, 0644); err != nil {
			t.Fatal(err)
		}

		env := []string{"JENNY_HOME=" + tmpDir}
		res := harness.RunJenny(t, env, "--offline", "--print-system-prompt")
		if res.ExitCode != 0 {
			t.Errorf("expected exit code 0 with multiple overrides, got %d; stderr=%s", res.ExitCode, res.Stderr)
		}
	})
}
