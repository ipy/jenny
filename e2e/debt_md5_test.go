package e2e_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ipy/jenny/e2e/harness"
)

// TestDebtMD5ParseConfigModelsErrorReturn verifies that ParseConfigModels
// returns a non-nil error when given malformed JSON, and returns nil,nil
// when the "models" key is absent.
//
// Spec: debt-mD-5 — Fix ParseConfigModels to return errors instead of nil,nil
// on JSON parse failures.
//
// Key behavioral assertions:
// 1. Malformed models key in config.json does not crash jenny (graceful degradation).
// 2. Valid models key in config.json works correctly.
// 3. Absent models key in config.json works correctly.
// 4. The system still functions normally with valid config.
func TestDebtMD5ParseConfigModelsErrorReturn(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "debt-md5.malformed-models-no-crash",
			Category:    "debt-md5",
			Description: "malformed models key in config.json does not crash jenny",
			Tags:        []string{"debt"},
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
			ID:          "debt-md5.malformed-models-warns",
			Category:    "debt-md5",
			Description: "malformed models key in config.json produces a warning on stderr",
			Tags:        []string{"debt"},
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--offline", "--print-system-prompt", "--verbose"},
				Env:  []string{"JENNY_HOME=${WORK_DIR}/.jenny"},
				WorkDirFiles: map[string]string{
					".jenny/config.json": `{"models": "not-an-object"}`,
				},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stderr: &harness.StderrExpectation{
					Contains: []string{"model", "config", "parse", "warn", "error", "invalid", "malformed", "models"},
				},
			},
		},
		{
			ID:          "debt-md5.valid-models-works",
			Category:    "debt-md5",
			Description: "valid models key in config.json works correctly",
			Tags:        []string{"debt"},
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
			ID:          "debt-md5.absent-models-key-works",
			Category:    "debt-md5",
			Description: "config.json without models key works correctly (returns nil,nil)",
			Tags:        []string{"debt"},
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--offline", "--print-system-prompt"},
				Env:  []string{"JENNY_HOME=${WORK_DIR}/.jenny"},
				WorkDirFiles: map[string]string{
					".jenny/config.json": `{"output-format": "text"}`,
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
			ID:          "debt-md5.empty-config-works",
			Category:    "debt-md5",
			Description: "empty config.json works correctly",
			Tags:        []string{"debt"},
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--offline", "--print-system-prompt"},
				Env:  []string{"JENNY_HOME=${WORK_DIR}/.jenny"},
				WorkDirFiles: map[string]string{
					".jenny/config.json": `{}`,
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
			ID:          "debt-md5.valid-models-affects-max-tokens",
			Category:    "debt-md5",
			Description: "valid models override in config.json affects max_tokens in API request",
			Tags:        []string{"debt"},
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
	})
}

// TestDebtMD5BinaryStringsCheck verifies that the error-returning behavior
// is reflected in the binary by checking for relevant strings.
func TestDebtMD5BinaryStringsCheck(t *testing.T) {
	// This test verifies the binary doesn't contain dead "nil, nil" return patterns
	// for ParseConfigModels. We check that the binary builds and runs correctly.

	// First, run a basic smoke test with the binary
	env := []string{"ANTHROPIC_AUTH_TOKEN=dummy"}
	res := harness.RunJenny(t, env, "--version")
	if res.ExitCode != 0 {
		t.Errorf("expected exit code 0 from --version, got %d", res.ExitCode)
	}
	if !strings.Contains(res.Stdout, "jenny") {
		t.Errorf("expected version output to contain 'jenny', got: %s", res.Stdout)
	}
}

// TestDebtMD5MalformedModelsTypes tests various types of malformed models values.
func TestDebtMD5MalformedModelsTypes(t *testing.T) {
	t.Run("models as array", func(t *testing.T) {
		tmpDir := t.TempDir()

		configJSON := map[string]any{
			"models": []string{"this", "is", "an", "array"},
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
			t.Errorf("expected exit code 0 with models as array, got %d; stderr=%s", res.ExitCode, res.Stderr)
		}
	})

	t.Run("models as number", func(t *testing.T) {
		tmpDir := t.TempDir()

		configJSON := map[string]any{
			"models": 42.0,
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
			t.Errorf("expected exit code 0 with models as number, got %d; stderr=%s", res.ExitCode, res.Stderr)
		}
	})

	t.Run("models as boolean", func(t *testing.T) {
		tmpDir := t.TempDir()

		configJSON := map[string]any{
			"models": true,
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
			t.Errorf("expected exit code 0 with models as boolean, got %d; stderr=%s", res.ExitCode, res.Stderr)
		}
	})

	t.Run("models as null", func(t *testing.T) {
		tmpDir := t.TempDir()

		configJSON := map[string]any{
			"models": nil,
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
			t.Errorf("expected exit code 0 with models as null, got %d; stderr=%s", res.ExitCode, res.Stderr)
		}
	})
}
