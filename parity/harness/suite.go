package harness

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SuiteRunner orchestrates running parity test cases.
type SuiteRunner struct {
	Config *Config
	Tests  []*TestCase
}

// NewSuiteRunner creates a new test suite runner.
func NewSuiteRunner(cfg *Config, tests []*TestCase) *SuiteRunner {
	return &SuiteRunner{
		Config: cfg,
		Tests:  tests,
	}
}

// RunAll runs all test cases and reports results.
func (sr *SuiteRunner) RunAll(reporter Reporter) []TestResult {
	reporter.OnStart(len(sr.Tests))
	results := make([]TestResult, 0, len(sr.Tests))

	for _, tc := range sr.Tests {
		result := sr.RunOne(tc)
		results = append(results, result)
		reporter.OnResult(result)
	}

	reporter.OnEnd(results)
	return results
}

// RunOne runs a single test case.
func (sr *SuiteRunner) RunOne(tc *TestCase) TestResult {
	start := time.Now()

	// Check skip conditions
	if tc.Skip != nil {
		return TestResult{
			ID:         tc.ID,
			Category:   tc.Category,
			Status:     "skip",
			Duration:   time.Since(start).Milliseconds(),
			SkipReason: tc.Skip.Reason,
		}
	}

	// Setup work directory
	workDir, err := os.MkdirTemp(sr.Config.TempDir, "parity-test-")
	if err != nil {
		return TestResult{
			ID:       tc.ID,
			Category: tc.Category,
			Status:   "error",
			Message:  "failed to create temp dir: " + err.Error(),
		}
	}
	defer os.RemoveAll(workDir)

	// Build args based on invocation kind
	args := sr.buildArgs(tc)

	// Run the target
	res := RunTargetInDir(nil, sr.Config, workDir, nil, args...)

	// Capture output
	actual := &CapturedOutput{
		ExitCode: res.ExitCode,
		Stdout:   res.Stdout,
		Stderr:   res.Stderr,
	}

	// Compare against expectations
	cmp := Compare(tc, actual)

	if cmp.Pass {
		return TestResult{
			ID:       tc.ID,
			Category: tc.Category,
			Status:   "pass",
			Duration: time.Since(start).Milliseconds(),
		}
	}

	return TestResult{
		ID:       tc.ID,
		Category: tc.Category,
		Status:   "fail",
		Duration: time.Since(start).Milliseconds(),
		Diff:     cmp.Diff,
		Actual:   actual,
	}
}

// buildArgs constructs CLI args from the invocation spec.
func (sr *SuiteRunner) buildArgs(tc *TestCase) []string {
	var args []string

	switch tc.Target.Kind {
	case "cli":
		args = tc.Target.Args
	case "prompt":
		args = []string{"--output-format", tc.Target.Format, "-p", tc.Target.Prompt}
	case "subprocess":
		args = tc.Target.Args
	default:
		args = tc.Target.Args
	}

	return args
}

// LoadCasesFromDir discovers test cases from Go test files in the directory.
func LoadCasesFromDir(dir string) ([]*TestCase, error) {
	var cases []*TestCase

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			subCases, err := LoadCasesFromDir(filepath.Join(dir, entry.Name()))
			if err != nil {
				return nil, err
			}
			cases = append(cases, subCases...)
			continue
		}

		if strings.HasSuffix(entry.Name(), "_test.go") {
			// For now, we don't auto-discover - tests are registered explicitly
		}
	}

	return cases, nil
}
