package tool

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestParseSimpleCommand(t *testing.T) {
	lsPath, lsErr := findExecutable("ls", "echo")
	if lsErr != nil {
		t.Skip("no ls or echo on PATH for parse tests")
	}

	tests := []struct {
		name    string
		input   string
		flavor  ShellFlavor
		wantOK  bool
		check   func(t *testing.T, r simpleParseResult)
		issues  []string
	}{
		{
			name:   "simple ls",
			input:  "ls -la",
			flavor: ShellFlavorBash,
			wantOK: true,
			check: func(t *testing.T, r simpleParseResult) {
				if len(r.Argv) < 2 || r.Argv[1] != "-la" {
					t.Errorf("unexpected argv: %v", r.Argv)
				}
				if !filepath.IsAbs(r.Resolved) {
					t.Errorf("expected absolute resolved path, got %q", r.Resolved)
				}
			},
		},
		{
			name:   "quoted args",
			input:  `echo "foo bar"`,
			flavor: ShellFlavorBash,
			wantOK: true,
			check: func(t *testing.T, r simpleParseResult) {
				if len(r.Argv) < 2 || r.Argv[1] != "foo bar" {
					t.Errorf("unexpected argv: %v", r.Argv)
				}
			},
		},
		{
			name:   "pipeline rejected",
			input:  "foo | bar",
			flavor: ShellFlavorBash,
			wantOK: false,
			issues: []string{"shell operator"},
		},
		{
			name:   "bash builtin cd rejected",
			input:  "cd src",
			flavor: ShellFlavorBash,
			wantOK: false,
			issues: []string{`"cd" is a shell builtin`},
		},
		{
			name:   "echo allowed via lookpath",
			input:  "echo hi",
			flavor: ShellFlavorBash,
			wantOK: true,
		},
		{
			name:   "shell binary bash rejected",
			input:  `bash -c "echo"`,
			flavor: ShellFlavorBash,
			wantOK: false,
			issues: []string{`"bash" is a shell`},
		},
		{
			name:   "missing executable",
			input:  "notacommand_xyz123",
			flavor: ShellFlavorBash,
			wantOK: false,
			issues: []string{"not found in PATH"},
		},
		{
			name:   "operator and builtin both reported",
			input:  "cd src && make",
			flavor: ShellFlavorBash,
			wantOK: false,
			issues: []string{"shell operator", `"cd" is a shell builtin`},
		},
		{
			name:   "powershell backtick rejected",
			input:  "Get-ChildItem `",
			flavor: ShellFlavorPowerShell,
			wantOK: false,
			issues: []string{"shell operator"},
		},
		{
			name:   "powershell backslash in path not operator",
			input:  `C:\path\to\file.exe`,
			flavor: ShellFlavorPowerShell,
			wantOK: false,
			issues: []string{"not found in PATH"},
		},
		{
			name:   "empty command",
			input:  "   ",
			flavor: ShellFlavorBash,
			wantOK: false,
			issues: []string{"empty command"},
		},
		{
			name:   "unterminated quote",
			input:  `echo "hello`,
			flavor: ShellFlavorBash,
			wantOK: false,
			issues: []string{"unterminated quote"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := parseSimpleCommand(tt.input, tt.flavor)
			if r.OK != tt.wantOK {
				t.Errorf("OK = %v, want %v; issues=%v argv=%v", r.OK, tt.wantOK, r.Issues, r.Argv)
			}
			for _, want := range tt.issues {
				found := false
				for _, issue := range r.Issues {
					if strings.Contains(issue, want) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected issue containing %q in %v", want, r.Issues)
				}
			}
			if tt.check != nil {
				tt.check(t, r)
			}
			_ = lsPath
		})
	}
}

func TestParseSimpleCommand_ShebangEnvBash(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shebang tests are Unix-only")
	}

	dir := t.TempDir()
	script := filepath.Join(dir, "fake-ls")
	content := "#!/usr/bin/env bash\necho hi\n"
	if err := os.WriteFile(script, []byte(content), 0755); err != nil {
		t.Fatal(err)
	}

	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath)

	r := parseSimpleCommand("fake-ls", ShellFlavorBash)
	if r.OK {
		t.Fatalf("expected shebang shell rejection, got OK argv=%v", r.Argv)
	}
	found := false
	for _, issue := range r.Issues {
		if strings.Contains(issue, "shebang") && strings.Contains(issue, "bash") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected shebang issue about bash, got %v", r.Issues)
	}
}

func TestParseSimpleCommand_PowerShellLsOK(t *testing.T) {
	if _, err := findExecutable("ls"); err != nil {
		t.Skip("ls not on PATH")
	}
	r := parseSimpleCommand("ls -la", ShellFlavorPowerShell)
	if !r.OK {
		t.Errorf("expected PowerShell flavor to allow ls, issues=%v", r.Issues)
	}
}

func findExecutable(names ...string) (string, error) {
	for _, name := range names {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", os.ErrNotExist
}
