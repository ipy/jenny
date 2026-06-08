// Package e2e_test contains blackbox end-to-end tests for jenny.
//
// The tests in this package spawn the jenny binary as a subprocess and
// assert on its stdout, stderr, exit code, and (for stream-json tests)
// the HTTP traffic it emits against an in-process mock server.
package e2e_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/ipy/jenny/jenny_test/harness"
)

// versionRe matches a semver-like version string X.Y.Z.
var versionRe = regexp.MustCompile(`\d+\.\d+\.\d+`)

// TestVersionFlag is the AC2 smoke test for the `--version` flag and
// its `-v` short alias. Both must exit 0 and emit a line matching the
// semver pattern on stdout; the binary exits before any session or
// API setup, so no network call is made.
func TestVersionFlag(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{name: "long", args: []string{"--version"}},
		{name: "short", args: []string{"-v"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := harness.RunJenny(t, nil, tc.args...)

			if res.ExitCode != 0 {
				t.Fatalf("%s: expected exit 0, got %d; stderr=%q", tc.args[0], res.ExitCode, res.Stderr)
			}

			if len(res.Lines) == 0 {
				t.Fatalf("%s: expected at least one line of stdout; got 0; stderr=%q", tc.args[0], res.Stderr)
			}
			if !versionRe.MatchString(res.Lines[0]) {
				t.Errorf(
					"%s: first stdout line %q does not match version pattern %q",
					tc.args[0], res.Lines[0], versionRe,
				)
			}
		})
	}
}

// TestHelpFlag is the AC3 smoke test for the `--help` flag.
func TestHelpFlag(t *testing.T) {
	res := harness.RunJenny(t, nil, "--help")

	if res.ExitCode != 0 {
		t.Fatalf("--help exited %d; stderr=%q", res.ExitCode, res.Stderr)
	}

	combined := strings.ToLower(strings.Join(res.Lines, "\n") + "\n" + res.Stderr)
	if !strings.Contains(combined, "usage") {
		t.Errorf(
			"expected combined stdout+stderr to contain 'usage' (case-insensitive); got: %q",
			combined,
		)
	}

	if !strings.Contains(combined, "print-system-prompt") {
		t.Errorf("--help output does not mention 'print-system-prompt'")
	}
}
