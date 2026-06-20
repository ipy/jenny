// Package tool provides tool implementations.
package tool

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/ipy/jenny/internal/api"
	"github.com/ipy/jenny/internal/constants"
)

const (
	defaultMaxSizeBytes = 256 * 1024 // 256 KB
	defaultMaxTokens    = 25000
	// maxSizeHardLimit is the absolute ceiling the Read tool will never
	// exceed, regardless of the caller-supplied max_size parameter.
	// AC3: 1 GiB OOM guard — os.Stat must be consulted before any read.
	maxSizeHardLimit = 1 << 30 // 1,073,741,824 bytes
)

type ReadTool struct {
	permissionLevel PermissionLevel
	readCache       *ReadFileCache
	activator       SkillActivator
	sessionID       string
	modelName       string
}

// NewReadTool creates a new ReadTool with the given PermissionLevel.
func NewReadTool(level PermissionLevel, readCache *ReadFileCache) *ReadTool {
	return &ReadTool{permissionLevel: level, readCache: readCache}
}

// effectiveLevel returns the effective PermissionLevel.
func (t *ReadTool) effectiveLevel() PermissionLevel {
	return t.permissionLevel
}

// WithSessionID sets the session ID for the ReadTool.
func (t *ReadTool) WithSessionID(id string) *ReadTool {
	t.sessionID = id
	return t
}

// Name returns the tool name.
func (t *ReadTool) Name() string {
	return "Read"
}

// WithReadFileCache sets the read cache for read-before-write validation.
func (t *ReadTool) WithReadFileCache(cache *ReadFileCache) *ReadTool {
	t.readCache = cache
	return t
}

// WithSkillActivator sets the skill activator for path-triggered activation.
func (t *ReadTool) WithSkillActivator(activator SkillActivator) *ReadTool {
	t.activator = activator
	return t
}

// GetReadFileCache returns the read cache. Used for testing wiring verification.
func (t *ReadTool) GetReadFileCache() *ReadFileCache {
	return t.readCache
}

// WithModelName sets the model name for vision capability checking.
func (t *ReadTool) WithModelName(model string) *ReadTool {
	t.modelName = model
	return t
}

// Description returns a description of the tool.
func (t *ReadTool) Description() string {
	return "Read the contents of a file. Use this to view files with line numbers for reference."
}

// InputSchema returns the JSON schema for tool input.
func (t *ReadTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file_path": map[string]any{
				"type":        "string",
				"description": "The absolute path to the file to read",
			},
			"offset": map[string]any{
				"type":        "number",
				"description": "The line number to start reading from (1-indexed)",
			},
			"limit": map[string]any{
				"type":        "number",
				"description": "The number of lines to read",
			},
			"max_size": map[string]any{
				"type":        "number",
				"description": "Maximum file size in bytes; rejects file pre-read if exceeded",
			},
			"max_tokens": map[string]any{
				"type":        "number",
				"description": "Maximum token budget; rejects content post-read if exceeded",
			},
		},
		"required": []string{"file_path"},
	}
}

// Execute reads the file and returns its contents with line numbers.
func (t *ReadTool) Execute(ctx context.Context, input map[string]any, cwd string) (*ToolResult, error) {
	filePath, ok := input["file_path"].(string)
	if !ok || filePath == "" {
		return nil, fmt.Errorf("file_path is required and must be a string")
	}

	// Resolve $JENNY_SCRATCHPAD/ prefix before any relative path resolution
	filePath = ResolveScratchpadPrefix(filePath, t.sessionID)

	// Resolve relative paths relative to cwd
	if !filepath.IsAbs(filePath) {
		filePath = filepath.Join(cwd, filePath)
	}

	// Create command gate for device path validation
	gate := NewCommandGate(t.effectiveLevel())

	// Check device path before access
	if err := gate.CheckDevicePath(filePath); err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("Access to device path blocked: %v", err),
			IsError: true,
		}, nil
	}

	// Validate path is within working directory (no path traversal)
	absCwd, err := filepath.Abs(cwd)
	if err != nil {
		absCwd = cwd
	}
	absFilePath, err := filepath.Abs(filePath)
	if err != nil {
		return nil, fmt.Errorf("invalid file path: %v", err)
	}

	// Clean the absolute path to resolve any traversal sequences
	absFilePathClean := filepath.Clean(absFilePath)

	// Universal Windows Security (AC4)
	if runtime.GOOS == "windows" {
		winGate := NewWindowsCommandGate(t.effectiveLevel())
		if err := winGate.CheckPath(absFilePathClean); err != nil {
			return &ToolResult{
				Content: fmt.Sprintf("Security error: %v", err),
				IsError: true,
			}, nil
		}
	}

	// Normalize cwd for comparison
	absCwdClean := filepath.Clean(absCwd)

	// The file path must be within or equal to cwd, or scratchpad
	// Use proper path boundary check with separator
	sep := string(filepath.Separator)
	if t.effectiveLevel().PathConstrained() && !strings.HasPrefix(absFilePathClean+sep, constants.ScratchpadDir(t.sessionID)+sep) {
		if !strings.HasPrefix(absFilePathClean+sep, absCwdClean+sep) && absFilePathClean != absCwdClean {
			return &ToolResult{
				Content: fmt.Sprintf("Error: Access to '%s' is not allowed. File path would traverse outside working directory.", filePath),
				IsError: true,
			}, nil
		}
	}

	// Trigger skill activation based on path access (after path validation, before file I/O)
	if t.activator != nil {
		t.activator.ActivateForPath(absFilePathClean)
	}

	// Check if file exists
	info, err := os.Stat(absFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			// Record empty read in cache even if file doesn't exist (to allow subsequent Write)
			if t.readCache != nil {
				t.readCache.RecordRead(absFilePath, "", time.Now(), true, 1, 0)
			}
			return &ToolResult{
				Content: fmt.Sprintf("[Warning: file does not exist: %s]\n\n[0 lines, started at line 1, total lines in file: 0]", filePath),
				IsError: false,
			}, nil
		}
		return &ToolResult{
			Content: fmt.Sprintf("Error accessing file: %v", err),
			IsError: true,
		}, nil
	}

	if info.IsDir() {
		return &ToolResult{
			Content: fmt.Sprintf("Error: '%s' is a directory, not a file", filePath),
			IsError: true,
		}, nil
	}

	// Block device guard - reject block/char devices before reading
	if info.Mode()&os.ModeDevice != 0 || info.Mode()&os.ModeCharDevice != 0 {
		return &ToolResult{
			Content: fmt.Sprintf("cannot read block device: %s", filePath),
			IsError: true,
		}, nil
	}

	// Also check deny list for known device paths
	if strings.HasPrefix(absFilePathClean, "/dev/") || strings.HasPrefix(absFilePathClean, "/proc/self/fd/") {
		return &ToolResult{
			Content: fmt.Sprintf("cannot read block device: %s", filePath),
			IsError: true,
		}, nil
	}

	// Handle image files: encode as base64 data URI for model vision
	if isImageFile(absFilePathClean) {
		return t.readImage(absFilePathClean, info)
	}

	// Determine offset and limit early (needed for isFullRead check and dedup)
	offset := 1
	offsetExplicit := false
	if offsetVal, ok := input["offset"].(float64); ok {
		offset = max(int(offsetVal), 1)
		offsetExplicit = true
	}

	limit := 0
	limitExplicit := false
	if limitVal, ok := input["limit"].(float64); ok {
		limit = int(limitVal)
		limitExplicit = true
	}

	isFullRead := !offsetExplicit && !limitExplicit

	// AC1: TOCTOU eliminated — atomically stat+compare inside the cache mutex.
	if t.readCache != nil && isFullRead {
		hit, statErr := t.readCache.CheckAndRecord(absFilePath, "", isFullRead, offset, limit)
		if statErr == nil && hit {
			return &ToolResult{
				Content:  "[file unchanged since last read — cached content is current]",
				IsError:  false,
				CacheHit: true,
			}, nil
		}
	}

	// AC3: 1 GiB OOM guard — reject files ≥1 GiB before any read.
	if isFullRead && info.Size() > maxSizeHardLimit {
		return &ToolResult{
			Content: fmt.Sprintf("file is too large (%d bytes): exceeds maxSizeBytes limit (1 GiB)", info.Size()),
			IsError: true,
		}, nil
	}

	// maxSizeBytes check (pre-read, only for full reads)
	if isFullRead {
		maxSize := int64(defaultMaxSizeBytes)
		if maxSizeVal, ok := input["max_size"].(float64); ok {
			maxSize = int64(maxSizeVal)
		}
		if maxSize > maxSizeHardLimit {
			maxSize = maxSizeHardLimit
		}
		if info.Size() > maxSize {
			return &ToolResult{
				Content: fmt.Sprintf("file exceeds maxSizeBytes limit (%d bytes)", info.Size()),
				IsError: true,
			}, nil
		}
	}

	// Open the file
	fileBytes, err := os.ReadFile(absFilePath)
	if err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("Error reading file: %v", err),
			IsError: true,
		}, nil
	}

	// AC5: UTF-16 Detection and Decoding
	var content string
	if decoded, ok := detectAndDecodeUTF16(fileBytes); ok {
		content = decoded
	} else {
		content = string(fileBytes)
	}

	// Process content line by line
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	// Remove trailing empty line if file ends with newline
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	lineNum := len(lines)
	readLines := 0
	var resultLines []string

	for i := offset - 1; i < len(lines); i++ {
		if limit > 0 && readLines >= limit {
			break
		}
		resultLines = append(resultLines, lines[i])
		readLines++
	}

	// maxTokens check: if exceeded, set Truncated instead of erroring
	var truncated bool
	if maxTokensVal, ok := input["max_tokens"].(float64); ok && int(maxTokensVal) > 0 {
		estimatedTokens := len(strings.Join(resultLines, "\n")) / 4
		if estimatedTokens > int(maxTokensVal) {
			truncated = true
		}
	}

	// Format output with line numbers (matching cat -n format)
	var output strings.Builder
	totalLines := lineNum

	// Record the read in cache for read-before-write contract
	if t.readCache != nil {
		fullContent := ""
		if isFullRead {
			fullContent = content
		} else {
			fullContent = content
		}
		// AC1: post-read stat-and-record is atomic in the cache mutex.
		_, _ = t.readCache.StatAndRecord(absFilePath, fullContent, isFullRead, offset, limit)
	}

	// Handle empty file content - warning, not error
	if totalLines == 0 {
		fmt.Fprintf(&output, "[Warning: empty file]\n")
		return &ToolResult{
			Content: output.String(),
			IsError: false,
		}, nil
	}

	// Handle reading past EOF - warning with actual line count
	if offset > totalLines {
		fmt.Fprintf(&output, "[Warning: offset %d exceeds file line count %d]\n", offset, totalLines)
		fmt.Fprintf(&output, "\n[%d lines, started at line %d, total lines in file: %d]",
			readLines, offset, totalLines)
		return &ToolResult{
			Content: output.String(),
			IsError: false,
		}, nil
	}

	for i, line := range resultLines {
		lineStr := strconv.Itoa(offset + i)
		fmt.Fprintf(&output, "%6s\t%s\n", lineStr, line)
	}

	// Add summary
	fmt.Fprintf(&output, "\n[%d lines, started at line %d, total lines in file: %d]",
		readLines, offset, totalLines)

	return &ToolResult{
		Content:  output.String(),
		IsError:  false,
		Truncated: truncated,
	}, nil
}

// detectAndDecodeUTF16 detects UTF-16 LE/BE with BOM and decodes it.
func detectAndDecodeUTF16(data []byte) (string, bool) {
	if len(data) < 2 {
		return "", false
	}
	// UTF-16 BE BOM: FE FF
	if data[0] == 0xFE && data[1] == 0xFF {
		u16 := make([]uint16, (len(data)-2)/2)
		for i := 0; i < len(u16); i++ {
			u16[i] = uint16(data[2+i*2])<<8 | uint16(data[2+i*2+1])
		}
		return string(utf16.Decode(u16)), true
	}
	// UTF-16 LE BOM: FF FE
	if data[0] == 0xFF && data[1] == 0xFE {
		u16 := make([]uint16, (len(data)-2)/2)
		for i := 0; i < len(u16); i++ {
			u16[i] = uint16(data[2+i*2+1])<<8 | uint16(data[2+i*2])
		}
		return string(utf16.Decode(u16)), true
	}
	return "", false
}

// Image file support

var imageExtensions = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
}

func isImageFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	_, ok := imageExtensions[ext]
	return ok
}

const maxImageSize = 10 * 1024 * 1024 // 10 MB

func (t *ReadTool) readImage(path string, info os.FileInfo) (*ToolResult, error) {
	if info.Size() > maxImageSize {
		return &ToolResult{
			Content: fmt.Sprintf("Image file too large (%d bytes, max %d bytes)", info.Size(), maxImageSize),
			IsError: true,
		}, nil
	}

	// Fail fast if the model does not support vision
	if t.modelName != "" && !api.SupportsVision(t.modelName) {
		return &ToolResult{
			Content: fmt.Sprintf("[Error: model %q does not support image/vision input — cannot read image file %s]", t.modelName, filepath.Base(path)),
			IsError: true,
		}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("Error reading image: %v", err),
			IsError: true,
		}, nil
	}

	ext := strings.ToLower(filepath.Ext(path))
	mimeType := imageExtensions[ext]
	encoded := base64.StdEncoding.EncodeToString(data)

	content := fmt.Sprintf("data:%s;base64,%s", mimeType, encoded)

	return &ToolResult{
		Content: content,
		IsError: false,
	}, nil
}
