package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ReadDirBatchSize is the batch size for os.ReadDir to prevent OOM on large directories.
const ReadDirBatchSize = 128

// NormalizePath resolves a path to an absolute path, expanding ~ and validating existence.
func NormalizePath(path string) (string, error) {
	if path == "" {
		var err error
		path, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("get current directory: %w", err)
		}
	}
	if suffix, ok := strings.CutPrefix(path, "~"); ok {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("get home directory: %w", err)
		}
		path = filepath.Join(homeDir, suffix)
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve path %q: %w", path, err)
	}
	return absPath, nil
}

// CountDirEntries returns the total number of entries in a directory (including hidden).
func CountDirEntries(path string) (int, error) {
	dir, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer dir.Close()

	total := 0
	for {
		batch, err := dir.ReadDir(ReadDirBatchSize)
		if err != nil {
			break
		}
		total += len(batch)
		if len(batch) < ReadDirBatchSize {
			break
		}
	}
	return total, nil
}

// ContainsPathTraversal checks if a string contains path traversal components.
func ContainsPathTraversal(s string) bool {
	if s == "" {
		return false
	}
	if s[0] == '/' || s[0] == '\\' {
		return true
	}
	if len(s) >= 2 && s[0] == '.' && (s[1] == '/' || s[1] == '\\' || (s[1] == '.' && (len(s) < 3 || s[2] == '/' || s[2] == '\\'))) {
		return true
	}
	for i := 0; i < len(s)-1; i++ {
		if s[i] == '.' && s[i+1] == '.' && (i+2 < len(s) && (s[i+2] == '/' || s[i+2] == '\\')) {
			return true
		}
	}
	return false
}
