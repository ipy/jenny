package harness

import (
	"fmt"
	"strings"
)

// Reporter formats test results for output.
type Reporter interface {
	OnStart(total int)
	OnResult(result TestResult)
	OnEnd(results []TestResult)
}

// TextReporter is a simple text-based reporter.
type TextReporter struct{}

func (r *TextReporter) OnStart(total int) {
	fmt.Printf("Running %d parity test cases...\n\n", total)
}

func (r *TextReporter) OnResult(result TestResult) {
	switch result.Status {
	case "pass":
		fmt.Printf("  ✓ %s\n", result.ID)
	case "fail":
		fmt.Printf("  ✗ %s\n", result.ID)
		if result.Message != "" {
			fmt.Printf("    Error: %s\n", result.Message)
		}
		for _, d := range result.Diff {
			fmt.Printf("    - %s: expected %v, got %v\n", d.Path, d.Expected, d.Actual)
		}
	case "skip":
		fmt.Printf("  - %s (skipped: %s)\n", result.ID, result.SkipReason)
	case "error":
		fmt.Printf("  ! %s\n", result.ID)
		fmt.Printf("    Error: %s\n", result.Message)
	}
}

func (r *TextReporter) OnEnd(results []TestResult) {
	var passed, failed, skipped, errd int
	for _, r := range results {
		switch r.Status {
		case "pass":
			passed++
		case "fail":
			failed++
		case "skip":
			skipped++
		case "error":
			errd++
		}
	}
	fmt.Printf("\n--- Parity Results ---\n")
	fmt.Printf("Passed:  %d\n", passed)
	fmt.Printf("Failed:  %d\n", failed)
	fmt.Printf("Skipped: %d\n", skipped)
	fmt.Printf("Errors:  %d\n", errd)
	fmt.Printf("Total:   %d\n", len(results))

	if failed > 0 || errd > 0 {
		fmt.Printf("\nFailing tests:\n")
		for _, r := range results {
			if r.Status == "fail" || r.Status == "error" {
				fmt.Printf("  - %s [%s]\n", r.ID, r.Category)
			}
		}
	}
}

// JSONReporter outputs results as JSON lines.
type JSONReporter struct{}

func (r *JSONReporter) OnStart(total int) {}

func (r *JSONReporter) OnResult(result TestResult) {
	line, _ := formatResultJSON(result)
	fmt.Println(line)
}

func (r *JSONReporter) OnEnd(results []TestResult) {}

func formatResultJSON(result TestResult) (string, error) {
	var b strings.Builder
	b.WriteString(`{"id":"`)
	b.WriteString(result.ID)
	b.WriteString(`","category":"`)
	b.WriteString(result.Category)
	b.WriteString(`","status":"`)
	b.WriteString(result.Status)
	b.WriteString(`","duration_ms":`)
	b.WriteString(itoa(int(result.Duration)))
	if result.Message != "" {
		b.WriteString(`,"message":"`)
		b.WriteString(result.Message)
		b.WriteString(`"`)
	}
	if result.SkipReason != "" {
		b.WriteString(`,"skip_reason":"`)
		b.WriteString(result.SkipReason)
		b.WriteString(`"`)
	}
	if len(result.Diff) > 0 {
		b.WriteString(`,"diff":[`)
		for i, d := range result.Diff {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString(`{"path":"`)
			b.WriteString(d.Path)
			b.WriteString(`","expected":`)
			fmt.Fprintf(&b, "%v", d.Expected)
			b.WriteString(`,"actual":`)
			fmt.Fprintf(&b, "%v", d.Actual)
			b.WriteString(`}`)
		}
		b.WriteString(`]`)
	}
	b.WriteString("}")
	return b.String(), nil
}
