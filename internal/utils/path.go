package utils

import (
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// ReadDirBatchSize is the batch size for file.ReadDir to prevent OOM on
// large directories. Each batch reads at most this many entries from disk.
const ReadDirBatchSize = 128

// ReadDirSafe reads directory entries in batches using file.ReadDir
// to prevent OOM on large directories. It returns all entries by default.
// If max > 0, results are capped at max entries.
func ReadDirSafe(dirname string, max int) ([]os.DirEntry, error) {
	f, err := os.Open(dirname)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var entries []os.DirEntry
	for {
		batch, err := f.ReadDir(ReadDirBatchSize)
		for _, e := range batch {
			entries = append(entries, e)
			if max > 0 && len(entries) >= max {
				return entries[:max], nil
			}
		}
		if err == io.EOF {
			return entries, nil
		}
		if err != nil {
			return entries, err
		}
	}
}

// ReadFileSafe reads up to max bytes from the file at filename.
// If the file is larger than max, only the first max bytes are returned.
func ReadFileSafe(filename string, max int64) ([]byte, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, max))
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
	for i := range len(s) - 1 {
		if s[i] == '.' && s[i+1] == '.' && (i+2 < len(s) && (s[i+2] == '/' || s[i+2] == '\\')) {
			return true
		}
	}
	return false
}

// IsSessionRunning checks whether a session directory has a pid file whose
// process is still alive. Returns true only if the pid file exists and the
// process is still running (verified via signal 0).
func IsSessionRunning(sessionsDir, sessionID string) bool {
	pidPath := filepath.Join(sessionsDir, sessionID, "pid")
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return false
	}
	pidStr := strings.TrimSpace(string(data))
	if pidStr == "" {
		return false
	}
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}
