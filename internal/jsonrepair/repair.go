// Package jsonrepair provides deterministic repair of malformed JSON emitted by LLMs.
package jsonrepair

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// ParseError describes a failed repair with structured diagnostics.
type ParseError struct {
	Message     string
	FoundKeys   []string
	MissingKeys []string
	Example     string
}

func (e *ParseError) Error() string {
	var b strings.Builder
	b.WriteString(e.Message)
	if len(e.FoundKeys) > 0 {
		fmt.Fprintf(&b, "; found keys: %s", strings.Join(e.FoundKeys, ", "))
	}
	if len(e.MissingKeys) > 0 {
		fmt.Fprintf(&b, "; missing keys: %s", strings.Join(e.MissingKeys, ", "))
	}
	if e.Example != "" {
		fmt.Fprintf(&b, "; example: %s", e.Example)
	}
	return b.String()
}

var (
	unquotedKeyRE = regexp.MustCompile(`([{,]\s*)([a-zA-Z_][a-zA-Z0-9_]*)(\s*:)`)
	// drivePathRE matches Windows drive paths with at least 2 chars in the path component,
	// avoiding false-positives on single-char JSON escape sequences like f:\n in code.
	drivePathRE = regexp.MustCompile(`[A-Za-z]:\\[^\s"']{2,}`)
	// suspiciousDrivePathRE detects Windows paths where backslash-escape-like sequences
	// (e.g., C:\temp, D:\new) need doubling. Requires a word char after the escape-like
	// char to avoid matching JSON escapes in code patterns like "as f:\n" or "line:\n".
	suspiciousDrivePathRE = regexp.MustCompile(`[A-Za-z]:\\(?:t|f|n|r|b|u)\w`)
)

// Repair attempts to parse and repair malformed JSON tool arguments.
// toolName enables tool-specific extraction for edit and write tools.
// Returns a map on success or a structured ParseError on failure.
func Repair(input string, toolName string) (map[string]any, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, newParseError(toolName, nil, "empty tool input JSON")
	}

	// Valid JSON is returned without mutation unless a drive path contains
	// single-backslash escape sequences that would corrupt Windows paths.
	if !needsWindowsPathFix(input) {
		if m, ok := tryUnmarshal(input); ok {
			return m, nil
		}
	}

	// Layer 1: pre-processing for malformed input.
	preprocessed := preprocess(input)
	if m, ok := tryUnmarshal(preprocessed); ok {
		return m, nil
	}

	// Layer 2: structural repair.
	repaired := structuralRepair(preprocessed)
	if m, ok := tryUnmarshal(repaired); ok {
		return m, nil
	}

	// Layer 3: tool-specific extraction.
	if isContentTool(toolName) {
		if m, ok := extractToolFields(repaired, toolName); ok {
			return m, nil
		}
		if m, ok := extractToolFields(input, toolName); ok {
			return m, nil
		}
	}

	// Layer 4: diagnostic error.
	found := findMentionedKeys(input + preprocessed + repaired)
	return nil, newParseError(toolName, found, "could not parse tool input JSON")
}

// ExtractJSON finds the first top-level JSON object in text using brace-depth
// tracking that respects string literals and escape sequences.
func ExtractJSON(text string) string {
	start := strings.Index(text, "{")
	if start < 0 {
		return text
	}

	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(text); i++ {
		c := text[i]
		if escaped {
			escaped = false
			continue
		}
		if inString {
			switch c {
			case '\\':
				escaped = true
			case '"':
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return text[start : i+1]
			}
		}
	}
	return text[start:]
}

func tryUnmarshal(s string) (map[string]any, bool) {
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil, false
	}
	if m == nil {
		m = make(map[string]any)
	}
	return m, true
}

func preprocess(s string) string {
	s = stripMarkdownFences(s)
	s = escapeWindowsPaths(s)
	s = stripTrailingCommas(s)
	return strings.TrimSpace(s)
}

func stripMarkdownFences(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	// Drop opening fence line.
	if idx := strings.Index(s, "\n"); idx >= 0 {
		s = s[idx+1:]
	} else {
		return s
	}
	// Drop closing fence.
	if idx := strings.LastIndex(s, "```"); idx >= 0 {
		s = s[:idx]
	}
	return strings.TrimSpace(s)
}

func escapeWindowsPaths(s string) string {
	return drivePathRE.ReplaceAllStringFunc(s, fixPathEscapes)
}

func fixPathEscapes(path string) string {
	var b strings.Builder
	b.Grow(len(path) * 2)
	for i := 0; i < len(path); i++ {
		if path[i] == '\\' {
			b.WriteString(`\\`)
		} else {
			b.WriteByte(path[i])
		}
	}
	return b.String()
}

func needsWindowsPathFix(s string) bool {
	return suspiciousDrivePathRE.MatchString(s)
}

func stripTrailingCommas(s string) string {
	prev := ""
	for prev != s {
		prev = s
		s = strings.ReplaceAll(s, ",}", "}")
		s = strings.ReplaceAll(s, ",]", "]")
		s = strings.ReplaceAll(s, ",\n}", "\n}")
		s = strings.ReplaceAll(s, ",\n]", "\n]")
	}
	return s
}

func structuralRepair(s string) string {
	s = quoteUnquotedKeys(s)
	s = convertSingleQuotes(s)
	s = closeTruncatedJSON(s)
	return s
}

func quoteUnquotedKeys(s string) string {
	return unquotedKeyRE.ReplaceAllString(s, `$1"$2"$3`)
}

func convertSingleQuotes(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inDouble := false
	inSingle := false
	escaped := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if escaped {
			b.WriteByte(c)
			escaped = false
			continue
		}
		if inDouble {
			switch c {
			case '\\':
				escaped = true
				b.WriteByte(c)
			case '"':
				inDouble = false
				b.WriteByte(c)
			default:
				b.WriteByte(c)
			}
			continue
		}
		if inSingle {
			switch c {
			case '\\':
				escaped = true
				b.WriteByte(c)
			case '\'':
				inSingle = false
				b.WriteByte('"')
			case '"':
				b.WriteString(`\"`)
			default:
				b.WriteByte(c)
			}
			continue
		}
		switch c {
		case '"':
			inDouble = true
			b.WriteByte(c)
		case '\'':
			inSingle = true
			b.WriteByte('"')
		default:
			b.WriteByte(c)
		}
	}
	if inSingle {
		b.WriteByte('"')
	}
	return b.String()
}

func closeTruncatedJSON(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}

	inString := false
	escaped := false
	stack := []byte{}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if escaped {
			escaped = false
			continue
		}
		if inString {
			switch c {
			case '\\':
				escaped = true
			case '"':
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{', '[':
			stack = append(stack, c)
		case '}':
			if len(stack) > 0 && stack[len(stack)-1] == '{' {
				stack = stack[:len(stack)-1]
			}
		case ']':
			if len(stack) > 0 && stack[len(stack)-1] == '[' {
				stack = stack[:len(stack)-1]
			}
		}
	}

	var b strings.Builder
	b.WriteString(s)
	if inString {
		b.WriteByte('"')
	}
	for i := len(stack) - 1; i >= 0; i-- {
		switch stack[i] {
		case '{':
			b.WriteByte('}')
		case '[':
			b.WriteByte(']')
		}
	}
	return b.String()
}

func isContentTool(name string) bool {
	switch strings.ToLower(name) {
	case "edit", "write":
		return true
	default:
		return false
	}
}

func requiredKeys(toolName string) []string {
	switch strings.ToLower(toolName) {
	case "edit":
		return []string{"file_path", "old_string", "new_string"}
	case "write":
		return []string{"file_path", "content"}
	default:
		return nil
	}
}

func exampleFormat(toolName string) string {
	switch strings.ToLower(toolName) {
	case "edit":
		return `{"file_path": "path/to/file.go", "old_string": "exact text", "new_string": "replacement"}`
	case "write":
		return `{"file_path": "path/to/file.go", "content": "file contents"}`
	default:
		return `{"key": "value"}`
	}
}

func newParseError(toolName string, found []string, msg string) *ParseError {
	required := requiredKeys(toolName)
	missing := missingKeys(found, required)
	sort.Strings(found)
	return &ParseError{
		Message:     msg,
		FoundKeys:   found,
		MissingKeys: missing,
		Example:     exampleFormat(toolName),
	}
}

func missingKeys(found, required []string) []string {
	if len(required) == 0 {
		return nil
	}
	set := make(map[string]bool, len(found))
	for _, k := range found {
		set[k] = true
	}
	var missing []string
	for _, k := range required {
		if !set[k] {
			missing = append(missing, k)
		}
	}
	return missing
}

func findMentionedKeys(s string) []string {
	seen := make(map[string]bool)
	for _, k := range requiredKeys("edit") {
		if strings.Contains(s, k) {
			seen[k] = true
		}
	}
	for _, k := range requiredKeys("write") {
		if strings.Contains(s, k) {
			seen[k] = true
		}
	}
	keyRE := regexp.MustCompile(`"?([a-zA-Z_][a-zA-Z0-9_]*)"?\s*:`)
	for _, m := range keyRE.FindAllStringSubmatch(s, -1) {
		if len(m) > 1 {
			seen[m[1]] = true
		}
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func extractToolFields(s, toolName string) (map[string]any, bool) {
	name := strings.ToLower(toolName)
	switch name {
	case "edit":
		return extractEditFields(s)
	case "write":
		return extractWriteFields(s)
	default:
		return nil, false
	}
}

func extractEditFields(s string) (map[string]any, bool) {
	filePath := extractFieldValue(s, "file_path")
	oldStr := extractFieldValue(s, "old_string")
	newStr := extractFieldValue(s, "new_string")
	if filePath == "" || oldStr == "" || newStr == "" {
		return nil, false
	}
	return map[string]any{
		"file_path":  filePath,
		"old_string": oldStr,
		"new_string": newStr,
	}, true
}

func extractWriteFields(s string) (map[string]any, bool) {
	filePath := extractFieldValue(s, "file_path")
	content := extractFieldValue(s, "content")
	if filePath == "" || content == "" {
		return nil, false
	}
	return map[string]any{
		"file_path": filePath,
		"content":   content,
	}, true
}

func extractFieldValue(s, key string) string {
	// Quoted key with JSON-ish string value.
	patterns := []string{
		fmt.Sprintf(`"%s"\s*:\s*"`, key),
		fmt.Sprintf(`'%s'\s*:\s*'`, key),
		fmt.Sprintf(`%s\s*:\s*"`, key),
		fmt.Sprintf(`%s\s*:\s*'`, key),
	}

	for _, prefix := range patterns {
		if v := extractAfterPrefix(s, prefix); v != "" {
			return v
		}
	}

	// Unquoted value until the next known key.
	allKeys := []string{"file_path", "old_string", "new_string", "content"}
	var nextKeys []string
	for _, k := range allKeys {
		if k != key {
			nextKeys = append(nextKeys, k)
		}
	}
	keyMarker := key + `\s*:`
	re := regexp.MustCompile(`(?is)(?:^|[,{]\s*)` + keyMarker + `\s*(.+?)(?:\s*,\s*(?:` + strings.Join(nextKeys, "|") + `)\s*:|$|\s*})`)
	if m := re.FindStringSubmatch(s); len(m) > 1 {
		return trimQuotes(strings.TrimSpace(m[1]))
	}
	return ""
}

func extractAfterPrefix(s, prefix string) string {
	idx := strings.Index(s, prefix)
	if idx < 0 {
		// Case-insensitive search for unquoted keys.
		lower := strings.ToLower(s)
		lowerPrefix := strings.ToLower(prefix)
		idx = strings.Index(lower, lowerPrefix)
		if idx < 0 {
			return ""
		}
	}
	start := idx + len(prefix)
	if start >= len(s) {
		return ""
	}

	quote := s[start-1]
	if quote != '"' && quote != '\'' {
		return extractUnquotedValue(s[start:])
	}

	var b strings.Builder
	escaped := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if escaped {
			switch c {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			default:
				b.WriteByte(c)
			}
			escaped = false
			continue
		}
		if c == '\\' {
			escaped = true
			continue
		}
		// Stop at closing quote when followed by comma or end key.
		if c == quote {
			rest := strings.TrimSpace(s[i+1:])
			if rest == "" || rest[0] == ',' || rest[0] == '}' {
				return b.String()
			}
			// Embedded quote in broken JSON — keep scanning.
			b.WriteByte(c)
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

func extractUnquotedValue(s string) string {
	s = strings.TrimSpace(s)
	end := len(s)
	for i, c := range s {
		if c == ',' && !isInsideWord(s, i) {
			end = i
			break
		}
	}
	return strings.TrimSpace(s[:end])
}

func trimQuotes(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

func isInsideWord(s string, idx int) bool {
	if idx <= 0 || idx >= len(s) {
		return false
	}
	prev := rune(s[idx-1])
	return unicode.IsLetter(prev) || unicode.IsDigit(prev)
}
