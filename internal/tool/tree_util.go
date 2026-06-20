// Package tool provides tree formatting utilities shared by tree and glob tools.
package tool

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ReadDirBatchSize is the batch size for os.ReadDir to prevent OOM on large directories.
const ReadDirBatchSize = 128

// EntryType represents the type of a directory entry.
type EntryType int

const (
	EntryTypeOther EntryType = iota
	EntryTypeFile
	EntryTypeDir
	EntryTypeSymlink
)

func (e EntryType) String() string {
	switch e {
	case EntryTypeDir:
		return "dir"
	case EntryTypeSymlink:
		return "symlink"
	case EntryTypeFile:
		return "file"
	default:
		return "other"
	}
}

// DirEntry represents a single directory entry with metadata.
type DirEntry struct {
	Name    string
	Type    EntryType
	Size    int64
	Mtime   time.Time
	Path    string
	Count   int // For directories: total entry count
}

// Hint returns a display hint for the entry.
func (e DirEntry) Hint() string {
	if e.Type == EntryTypeDir {
		if e.Count < 0 {
			return "[read error]"
		}
		if e.Count == 0 {
			return "[empty]"
		}
		if e.Count == 1 {
			return "[1 entry]"
		}
		return fmt.Sprintf("[%d entries]", e.Count)
	}
	// File: show size and date
	if e.Size == 0 {
		return "[0 bytes]"
	}
	return fmt.Sprintf("[%d bytes, %s]", e.Size, e.Mtime.Format("2006-01-02"))
}

// Format formats the entry as a single line with tree-style prefix.
// prefix uses the standard tree format: ├── │   └──
func (e DirEntry) Format(prefix string) string {
	name := e.Name
	if e.Type == EntryTypeDir {
		name += "/"
	}
	return fmt.Sprintf("%s%s\t%s", prefix, name, e.Hint())
}

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

// ReadChildDir reads directory entries in path, returning total count and a slice.
// It uses batched os.ReadDir to limit memory usage.
func ReadChildDir(path string, maxEntries int) (total int, entries []os.DirEntry, err error) {
	dir, err := os.Open(path)
	if err != nil {
		return 0, nil, fmt.Errorf("open directory %q: %w", path, err)
	}
	defer dir.Close()

	var budget int
	if maxEntries > 0 {
		budget = maxEntries
	}

	for {
		batch, err := dir.ReadDir(ReadDirBatchSize)
		if err != nil {
			break
		}
		total += len(batch)
		if budget > 0 && len(entries) < budget {
			remaining := budget - len(entries)
			if len(batch) > remaining {
				batch = batch[:remaining]
			}
			entries = append(entries, batch...)
		}
		if len(batch) < ReadDirBatchSize {
			break
		}
	}
	return total, entries, nil
}

// StatDirEntries takes a list of os.DirEntry and returns DirEntry structs with metadata.
// It calls os.Stat concurrently for each entry.
func StatDirEntries(entries []os.DirEntry, basePath string) []DirEntry {
	type result struct {
		i    int
		e    DirEntry
		err  error
	}

	// Collect stat results concurrently
	results := make([]result, len(entries))
	for i, de := range entries {
		go func(i int, de os.DirEntry) {
			path := filepath.Join(basePath, de.Name())
			var size int64
			var mtime time.Time
			if info, err := de.Info(); err == nil {
				size = info.Size()
				mtime = info.ModTime()
			}
			var et EntryType
			mode := de.Type()
			switch {
			case mode.IsRegular():
				et = EntryTypeFile
			case mode.IsDir():
				et = EntryTypeDir
			case mode&os.ModeSymlink != 0:
				et = EntryTypeSymlink
			default:
				et = EntryTypeOther
			}
			results[i] = result{i, DirEntry{
				Name:  de.Name(),
				Type:  et,
				Size:  size,
				Mtime: mtime,
				Path:  path,
			}, nil}
		}(i, de)
	}

	// Wait for results
	for i := range results {
		if results[i].err != nil {
			results[i].e = DirEntry{Name: entries[i].Name(), Type: EntryTypeOther}
		}
	}

	// Sort: directories first, then alphabetically
	sort.Slice(results, func(i, j int) bool {
		a, b := results[i].e, results[j].e
		if a.Type != b.Type {
			return a.Type == EntryTypeDir
		}
		return a.Name < b.Name
	})

	out := make([]DirEntry, len(results))
	for i, r := range results {
		out[i] = r.e
	}
	return out
}

// FormatTree formats a flat list of DirEntry as a tree using standard tree prefixes.
// The entries should be pre-sorted (directories first, then files).
// prefix is the per-entry indentation prefix for nested display.
func FormatTree(entries []DirEntry, prefix string) string {
	if len(entries) == 0 {
		return ""
	}
	var sb strings.Builder
	for i, e := range entries {
		isLast := i == len(entries)-1
		var linePrefix string
		if isLast {
			linePrefix = prefix + "└── "
		} else {
			linePrefix = prefix + "├── "
		}
		sb.WriteString(e.Format(linePrefix))
		sb.WriteString("\n")
	}
	return strings.TrimRight(sb.String(), "\n")
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
