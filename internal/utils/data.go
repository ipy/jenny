package utils

import (
	"fmt"
	"math"
	"os"
)

// ParseTimeoutSeconds extracts and validates the timeout parameter from input.
// Returns default of 30s if not provided or if timeout <= 0, caps at 600s maximum.
func ParseTimeoutSeconds(input map[string]any) float64 {
	timeoutSecondsFloat := 30.0
	if timeoutVal, ok := input["timeout"].(float64); ok {
		if timeoutVal > 0 {
			timeoutSecondsFloat = math.Min(timeoutVal, 600.0)
		}
	}
	return timeoutSecondsFloat
}

// SplitJSONLLines splits JSONL data into lines, handling the last line without newline.
func SplitJSONLLines(data []byte) []string {
	var lines []string
	start := 0
	for i, b := range data {
		if b == '\n' {
			if line := string(data[start:i]); line != "" {
				lines = append(lines, line)
			}
			start = i + 1
		}
	}
	if remaining := string(data[start:]); remaining != "" {
		lines = append(lines, remaining)
	}
	return lines
}

// AppendJSONLLine appends a single JSONL line (data + '\n') to the file at path,
// creating it if it does not exist. The file is opened with O_APPEND so concurrent
// writes are atomic on POSIX when individual writes are <= PIPE_BUF (4 KiB).
func AppendJSONLLine(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("opening file: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("writing JSONL line: %w", err)
	}
	return nil
}
