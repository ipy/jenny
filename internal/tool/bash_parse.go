package tool

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ShellFlavor selects operator and shell-binary tables for simple-command parsing.
type ShellFlavor int

const (
	ShellFlavorBash ShellFlavor = iota
	ShellFlavorPowerShell
)

// simpleParseResult is the output of parseSimpleCommand.
type simpleParseResult struct {
	OK       bool
	Argv     []string
	Issues   []string
	Resolved string
}

var bashShellNames = map[string]bool{
	"sh": true, "bash": true, "dash": true, "ash": true, "zsh": true,
	"ksh": true, "csh": true, "tcsh": true, "fish": true, "busybox": true,
}

var powershellShellNames = map[string]bool{
	"pwsh": true, "powershell": true, "pwsh.exe": true, "powershell.exe": true,
	"cmd": true, "cmd.exe": true,
}

var bashAlwaysRejectBuiltins = map[string]bool{
	"cd": true, "source": true, ".": true, "export": true, "set": true,
	"unset": true, "eval": true, "exec": true, "type": true, "alias": true,
	"local": true, "readonly": true, "shift": true,
}

func bashOperators() string {
	return "|&;<>*?[]{}$()"
}

func powershellOperators() string {
	return "|&;<>*?[]{}$()`"
}

func parseSimpleCommand(input string, flavor ShellFlavor) simpleParseResult {
	return analyzeCommand(input, flavor)
}

func analyzeCommand(input string, flavor ShellFlavor) simpleParseResult {
	operators := bashOperators()
	if flavor == ShellFlavorPowerShell {
		operators = powershellOperators()
	}

	tokens, issues := scanAndTokenize(input, flavor, operators)

	if len(tokens) == 0 {
		if !containsIssue(issues, "empty command") && !containsUnterminatedQuote(issues) {
			issues = append(issues, "unsupported: empty command")
		}
		return simpleParseResult{Issues: issues}
	}

	shellNames := bashShellNames
	if flavor == ShellFlavorPowerShell {
		shellNames = powershellShellNames
	}

	base := filepath.Base(tokens[0])
	if shellNames[strings.ToLower(base)] {
		issues = append(issues, "unsupported: "+quoteIssue(base)+" is a shell")
	}

	if flavor == ShellFlavorBash && bashAlwaysRejectBuiltins[base] {
		issues = append(issues, "unsupported: "+quoteIssue(base)+" is a shell builtin")
	}

	resolved, err := exec.LookPath(tokens[0])
	if err != nil {
		issues = append(issues, "unsupported: executable "+quoteIssue(tokens[0])+" not found in PATH")
		return simpleParseResult{Issues: issues}
	}

	if flavor == ShellFlavorBash {
		if shebangIssues := checkShebang(resolved, shellNames); len(shebangIssues) > 0 {
			issues = append(issues, shebangIssues...)
		}
	}

	if len(issues) > 0 {
		return simpleParseResult{Issues: issues}
	}

	argv := make([]string, len(tokens))
	copy(argv, tokens)
	argv[0] = resolved

	return simpleParseResult{
		OK:       true,
		Argv:     argv,
		Resolved: resolved,
	}
}

func quoteIssue(s string) string {
	return `"` + s + `"`
}

func containsIssue(issues []string, substr string) bool {
	for _, issue := range issues {
		if strings.Contains(issue, substr) {
			return true
		}
	}
	return false
}

func containsUnterminatedQuote(issues []string) bool {
	return containsIssue(issues, "unterminated quote")
}

// scanAndTokenize walks the input once, detecting shell operators outside quotes
// and splitting on unquoted whitespace into tokens.
func scanAndTokenize(input string, flavor ShellFlavor, operators string) ([]string, []string) {
	var tokens []string
	var issues []string
	var current strings.Builder

	inSingle := false
	inDouble := false
	escapeNext := false

	flush := func() {
		if current.Len() > 0 {
			tokens = append(tokens, current.String())
			current.Reset()
		}
	}

	runes := []rune(input)
	for i, r := range runes {
		if escapeNext {
			current.WriteRune(r)
			escapeNext = false
			continue
		}

		if inSingle {
			if r == '\'' {
				inSingle = false
			} else {
				current.WriteRune(r)
			}
			continue
		}

		if inDouble {
			if flavor == ShellFlavorBash && r == '\\' {
				escapeNext = true
				continue
			}
			if r == '"' {
				inDouble = false
			} else {
				current.WriteRune(r)
			}
			continue
		}

		switch r {
		case '\'':
			inSingle = true
		case '"':
			inDouble = true
		case ' ', '\t', '\n', '\r':
			flush()
		default:
			if r == '&' && i+1 < len(runes) && runes[i+1] == '&' {
				issues = append(issues, "unsupported: shell operator "+quoteIssue("&&"))
				current.WriteRune(r)
				continue
			}
			if r == '|' && i+1 < len(runes) && runes[i+1] == '|' {
				issues = append(issues, "unsupported: shell operator "+quoteIssue("||"))
				current.WriteRune(r)
				continue
			}
			if r == '&' && i > 0 && runes[i-1] == '&' {
				current.WriteRune(r)
				continue
			}
			if r == '|' && i > 0 && runes[i-1] == '|' {
				current.WriteRune(r)
				continue
			}
			if strings.ContainsRune(operators, r) {
				issues = append(issues, "unsupported: shell operator "+quoteIssue(string(r)))
			}
			current.WriteRune(r)
		}
	}

	if escapeNext {
		current.WriteRune('\\')
	}
	if inSingle || inDouble {
		issues = append(issues, "unsupported: unterminated quote")
		return tokens, issues
	}
	flush()
	return tokens, issues
}

func checkShebang(resolved string, shellNames map[string]bool) []string {
	f, err := os.Open(resolved)
	if err != nil {
		return nil
	}
	defer f.Close()

	buf := make([]byte, 128)
	n, err := f.Read(buf)
	if err != nil || n < 2 {
		return nil
	}
	content := string(buf[:n])
	if !strings.HasPrefix(content, "#!") {
		return nil
	}

	line := content
	if idx := strings.Index(line, "\n"); idx >= 0 {
		line = line[:idx]
	}
	line = strings.TrimPrefix(line, "#!")
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}

	parts := strings.Fields(line)
	if len(parts) == 0 {
		return nil
	}

	interpreter := parts[0]
	if base := filepath.Base(interpreter); base == "env" || interpreter == "/usr/bin/env" || interpreter == "/usr/local/bin/env" {
		start := 1
		for start < len(parts) && strings.HasPrefix(parts[start], "-") {
			start++
		}
		if start >= len(parts) {
			return nil
		}
		interpreter = parts[start]
	}

	base := strings.ToLower(filepath.Base(interpreter))
	if shellNames[base] {
		return []string{"unsupported: shebang references missing shell " + quoteIssue(base)}
	}
	return nil
}
