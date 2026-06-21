package utils

import (
	"math"
	"regexp"
	"strings"
)

// GlobToRegex converts a glob pattern to a regex pattern.
func GlobToRegex(pattern string) string {
	var result strings.Builder
	result.WriteString("^")

	for i, ch := range pattern {
		switch ch {
		case '*':
			result.WriteString(".*")
		case '?':
			result.WriteString(".")
		case '.':
			result.WriteString(`\.`)
		case '\\':
			if i+1 < len(pattern) {
				result.WriteString(regexp.QuoteMeta(string(pattern[i+1])))
			} else {
				result.WriteString(`\\`)
			}
		default:
			result.WriteString(regexp.QuoteMeta(string(ch)))
		}
	}

	result.WriteString("$")
	return result.String()
}

// MatchGlobPattern checks if a command matches a glob pattern.
// Supports: * (any chars), ? (single char)
func MatchGlobPattern(pattern, command string) bool {
	regexPattern := GlobToRegex(pattern)
	matched, _ := regexp.MatchString(regexPattern, command)
	return matched
}

// ShannonEntropy computes the Shannon entropy (bits per character) of data.
func ShannonEntropy(data string) float64 {
	if data == "" {
		return 0
	}
	charCounts := make(map[rune]int)
	for _, char := range data {
		charCounts[char]++
	}
	invLength := 1.0 / float64(len(data))
	var entropy float64
	for _, count := range charCounts {
		freq := float64(count) * invLength
		entropy -= freq * math.Log2(freq)
	}
	return entropy
}
