package utils

import (
	"fmt"
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

// max returns the larger of two integers.
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// DiffLine represents a single line in a diff.
type DiffLine struct {
	Type    string // "context", "add", "delete"
	Content string
}

// diffEntry represents a line in the computed diff
type diffEntry struct {
	Type    string
	OldIdx  int
	NewIdx  int
	Content string
}

// maxDiffSize caps diff output to prevent bloating results.
const maxDiffSize = 50 * 1024

// GenerateUnifiedDiff generates a unified diff string between old and new content.
func GenerateUnifiedDiff(oldContent, newContent, path string) string {
	oldLines := strings.Split(oldContent, "\n")
	newLines := strings.Split(newContent, "\n")

	if len(oldLines) > 0 && oldLines[len(oldLines)-1] == "" {
		oldLines = oldLines[:len(oldLines)-1]
	}
	if len(newLines) > 0 && newLines[len(newLines)-1] == "" {
		newLines = newLines[:len(newLines)-1]
	}

	diff := computeLCS(oldLines, newLines)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("--- a/%s\n", path))
	sb.WriteString(fmt.Sprintf("+++ b/%s\n", path))

	var hunks []string
	var currentHunk []DiffLine

	for i := range diff {
		diffLine := diff[i]
		switch diffLine.Type {
		case "context":
			if len(currentHunk) == 0 {
				currentHunk = append(currentHunk, DiffLine{Type: "context", Content: " " + diffLine.Content})
			} else {
				currentHunk = append(currentHunk, DiffLine{Type: "context", Content: " " + diffLine.Content})
			}
		case "delete":
			currentHunk = append(currentHunk, DiffLine{Type: "delete", Content: "-" + diffLine.Content})
		case "add":
			currentHunk = append(currentHunk, DiffLine{Type: "add", Content: "+" + diffLine.Content})
		}

		if len(currentHunk) >= 4 && i < len(diff)-1 {
			hunks = append(hunks, formatHunk(currentHunk, diff, i))
			currentHunk = nil
		}
	}

	if len(currentHunk) > 0 {
		hunks = append(hunks, formatHunk(currentHunk, diff, len(diff)-1))
	}

	for _, hunk := range hunks {
		if sb.Len()+len(hunk) > maxDiffSize {
			break
		}
		sb.WriteString(hunk)
	}

	result := sb.String()
	if len(result) > maxDiffSize {
		result = result[:maxDiffSize] + "\n... (diff truncated)"
	}
	return result
}

func computeLCS(oldLines, newLines []string) []diffEntry {
	m := len(oldLines)
	n := len(newLines)

	lcs := make([][]int, m+1)
	for i := range lcs {
		lcs[i] = make([]int, n+1)
	}
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if oldLines[i-1] == newLines[j-1] {
				lcs[i][j] = lcs[i-1][j-1] + 1
			} else {
				lcs[i][j] = max(lcs[i-1][j], lcs[i][j-1])
			}
		}
	}

	var result []diffEntry
	i, j := m, n
	for i > 0 || j > 0 {
		if i > 0 && j > 0 && oldLines[i-1] == newLines[j-1] {
			result = append(result, diffEntry{Type: "context", OldIdx: i - 1, NewIdx: j - 1, Content: oldLines[i-1]})
			i--
			j--
		} else if j > 0 && (i == 0 || lcs[i][j-1] >= lcs[i-1][j]) {
			result = append(result, diffEntry{Type: "add", OldIdx: i, NewIdx: j - 1, Content: newLines[j-1]})
			j--
		} else if i > 0 {
			result = append(result, diffEntry{Type: "delete", OldIdx: i - 1, NewIdx: j, Content: oldLines[i-1]})
			i--
		}
	}

	for left, right := 0, len(result)-1; left < right; left, right = left+1, right-1 {
		result[left], result[right] = result[right], result[left]
	}

	return result
}

func formatHunk(lines []DiffLine, fullDiff []diffEntry, endIdx int) string {
	if len(lines) == 0 {
		return ""
	}

	oldStart := 1
	newStart := 1
	for _, line := range lines {
		if line.Type != "context" {
			break
		}
		oldStart++
		newStart++
	}

	for i := endIdx - len(lines) + 1; i <= endIdx; i++ {
		if i < 0 || i >= len(fullDiff) {
			continue
		}
		entry := fullDiff[i]
		if entry.Type == "context" {
			oldStart = entry.OldIdx + 1
			newStart = entry.NewIdx + 1
			break
		}
	}

	oldCount := 0
	newCount := 0
	for _, line := range lines {
		if line.Type == "delete" {
			oldCount++
		} else if line.Type == "add" {
			newCount++
		} else {
			oldCount++
			newCount++
		}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("@@ -%d,%d +%d,%d @@\n", oldStart, oldCount, newStart, newCount))
	for _, line := range lines {
		sb.WriteString(line.Content + "\n")
	}
	return sb.String()
}
