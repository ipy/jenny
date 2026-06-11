// Package tool provides tool implementations.
package tool

import (
	"os"
	"sync"
	"time"
)

// ReadFileEntry represents a cached read state for a file.
type ReadFileEntry struct {
	Path       string
	Content    string
	Mtime      time.Time
	IsFullRead bool
	Offset     int
	Limit      int
}

// ReadFileCache tracks file read state for the read-before-write contract.
type ReadFileCache struct {
	mu      sync.Mutex
	entries map[string]*ReadFileEntry
}

// NewReadFileCache creates a new ReadFileCache.
func NewReadFileCache() *ReadFileCache {
	return &ReadFileCache{
		entries: make(map[string]*ReadFileEntry),
	}
}

// RecordRead records a file read operation.
func (c *ReadFileCache) RecordRead(path, content string, mtime time.Time, isFullRead bool, offset, limit int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[path] = &ReadFileEntry{
		Path:       path,
		Content:    content,
		Mtime:      mtime,
		IsFullRead: isFullRead,
		Offset:     offset,
		Limit:      limit,
	}
}

// GetRead returns the cached read entry for a path and whether it exists.
func (c *ReadFileCache) GetRead(path string) (*ReadFileEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[path]
	return entry, ok
}

// Remove removes the cached entry for a path.
func (c *ReadFileCache) Remove(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, path)
}

// UpdateAfterWrite updates the cache after a successful write.
func (c *ReadFileCache) UpdateAfterWrite(path, content string, mtime time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[path] = &ReadFileEntry{
		Path:       path,
		Content:    content,
		Mtime:      mtime,
		IsFullRead: true,
		Offset:     0,
		Limit:      0,
	}
}

// Add adds a pre-seeded entry to the cache (used for resume seeding from transcript).
func (c *ReadFileCache) Add(path, content string, mtime time.Time, isFullRead bool, offset, limit int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[path] = &ReadFileEntry{
		Path:       path,
		Content:    content,
		Mtime:      mtime,
		IsFullRead: isFullRead,
		Offset:     offset,
		Limit:      limit,
	}
}

// CheckAndRecord atomically stats the file and checks for a cache hit under the
// cache mutex, returning (cacheHit, error). The content/mtime/flags are recorded
// only on a miss. This eliminates the TOCTOU race between an external os.Stat
// and the cache dedup compare.
//
// AC1: read.go TOCTOU eliminated — stat+compare is performed inside the cache
// mutex so concurrent writers cannot introduce a stale mtime into the cache.
func (c *ReadFileCache) CheckAndRecord(path, content string, isFullRead bool, offset, limit int) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	info, err := os.Stat(path)
	if err != nil {
		// Surface the stat error; do not record on a miss-due-to-error.
		return false, err
	}

	if entry, ok := c.entries[path]; ok {
		if entry.Mtime.Equal(info.ModTime()) && entry.Offset == offset && entry.Limit == limit && entry.IsFullRead == isFullRead {
			return true, nil
		}
	}

	c.entries[path] = &ReadFileEntry{
		Path:       path,
		Content:    content,
		Mtime:      info.ModTime(),
		IsFullRead: isFullRead,
		Offset:     offset,
		Limit:      limit,
	}
	return false, nil
}

// StatAndRecord stats the file and records the (content, mtime) entry under the
// cache mutex. Used by the post-read path where the file has just been read and
// we need a fresh mtime for the cache entry. Returns the recorded mtime.
//
// AC1: post-read path — the re-stat and RecordRead happen as a single
// critical section so a concurrent writer cannot sneak a mtime change between
// them.
func (c *ReadFileCache) StatAndRecord(path, content string, isFullRead bool, offset, limit int) (time.Time, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	info, err := os.Stat(path)
	if err != nil {
		// On stat failure, fall back to zero mtime so the entry still records.
		var zero time.Time
		c.entries[path] = &ReadFileEntry{
			Path:       path,
			Content:    content,
			Mtime:      zero,
			IsFullRead: isFullRead,
			Offset:     offset,
			Limit:      limit,
		}
		return zero, err
	}

	c.entries[path] = &ReadFileEntry{
		Path:       path,
		Content:    content,
		Mtime:      info.ModTime(),
		IsFullRead: isFullRead,
		Offset:     offset,
		Limit:      limit,
	}
	return info.ModTime(), nil
}
