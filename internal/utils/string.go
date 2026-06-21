package utils

import (
	"unicode/utf8"
)

// SplitLines splits a string into lines (without trailing newlines).
func SplitLines(s string) []string {
	var lines []string
	for i := 0; i < len(s); {
		j := i
		for j < len(s) && s[j] != '\n' {
			j++
		}
		lines = append(lines, s[i:j])
		if j < len(s) {
			j++ // skip newline
		}
		i = j
	}
	return lines
}

// SafeTruncate returns s truncated to at most maxBytes bytes,
// ensuring the result is valid UTF-8 with no split code points.
// If maxBytes < 0, returns "".
func SafeTruncate(s string, maxBytes int) string {
	if maxBytes < 0 {
		return ""
	}
	if len(s) <= maxBytes {
		return s
	}
	result := s[:maxBytes]
	if utf8.ValidString(result) {
		return result
	}
	for len(result) > 0 {
		if utf8.ValidString(result) {
			return result
		}
		result = result[:len(result)-1]
	}
	return ""
}
