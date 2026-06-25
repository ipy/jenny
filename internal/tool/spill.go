package tool

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ipy/jenny/internal/constants"
)

const (
	// DefaultMaxInlineSize is the maximum size for inline tool output (~30 KB).
	DefaultMaxInlineSize = 30720

	spillPreviewHead = 1024
	spillPreviewTail = 512
)

// SpillConfig holds configuration for tool result spilling.
type SpillConfig struct {
	MaxInlineSize int    // Maximum chars to keep inline (default DefaultMaxInlineSize)
	SessionID     string // For session-scoped spill directory
	ProjectRoot   string // For project-scoped spill directory
}

// SpillResult holds the result of a spill check.
type SpillResult struct {
	Content   string // The content to return (original if under limit, or preview + path)
	Spilled   bool   // True if content was spilled to disk
	SpillPath string // Path to spill file (empty if not spilled)
	Truncated bool   // True if content was truncated (spill or inline truncation)
}

// CheckAndSpill checks if content exceeds the inline size limit.
// If so, writes it to disk and returns a preview with the spill path.
func CheckAndSpill(content string, config SpillConfig) SpillResult {
	maxSize := config.MaxInlineSize
	if maxSize <= 0 {
		maxSize = DefaultMaxInlineSize
	}

	if len(content) <= maxSize {
		return SpillResult{Content: content}
	}

	spillPath, err := writeSpillFile(content, config)
	if err != nil {
		half := maxSize / 2
		if half > len(content) {
			half = len(content)
		}
		return SpillResult{
			Content: fmt.Sprintf("Output truncated (spill file error: %v). First %d chars:\n%s",
				err, half, content[:half]),
			Truncated: true,
		}
	}

	return SpillResult{
		Content:   formatSpillPreview(content, spillPath),
		Spilled:   true,
		SpillPath: spillPath,
		Truncated: true,
	}
}

// ApplySpillToToolResult runs CheckAndSpill and returns a ToolResult.
// Error results pass through without spilling.
func ApplySpillToToolResult(content string, config SpillConfig, isError bool) *ToolResult {
	if isError {
		return &ToolResult{Content: content, IsError: true}
	}
	sr := CheckAndSpill(content, config)
	return &ToolResult{
		Content:   sr.Content,
		IsError:   false,
		Truncated: sr.Truncated,
	}
}

func formatSpillPreview(content, spillPath string) string {
	n := len(content)
	head := content
	if len(head) > spillPreviewHead {
		head = head[:spillPreviewHead]
	}
	tail := content
	if len(tail) > spillPreviewTail {
		tail = tail[len(tail)-spillPreviewTail:]
	}

	return fmt.Sprintf(
		"[Output too large for inline display (%d chars). Full output saved to: %s]\n\n"+
			"First 1024 chars:\n%s\n\n"+
			"Last 512 chars:\n%s\n\n"+
			"Use the Read tool to view the full output if needed.",
		n, spillPath, head, tail,
	)
}

func writeSpillFile(output string, config SpillConfig) (string, error) {
	sessionID := config.SessionID
	projectRoot := config.ProjectRoot

	// 1. Session home spills directory (~/.jenny/sessions/<id>/spills).
	spillsDir := constants.SpillsDir(sessionID)
	if dirExists(spillsDir) || mkdirAll(spillsDir, 0755) == nil {
		if path, err := createSpillTempFile(spillsDir, output); err == nil {
			return path, nil
		}
	}

	// 2. Project-local .jenny/sessions/<id>/spills/.
	if projectRoot != "" {
		projectSpillsDir := filepath.Join(constants.ProjectJennyDir(projectRoot), "spills")
		if sessionID != "" {
			projectSpillsDir = filepath.Join(constants.ProjectJennyDir(projectRoot), "sessions", sessionID, "spills")
		}
		if dirExists(projectSpillsDir) || mkdirAll(projectSpillsDir, 0755) == nil {
			if path, err := createSpillTempFile(projectSpillsDir, output); err == nil {
				return path, nil
			}
		}
	}

	// 3. System temp fallback (/tmp/jenny-spills/<id>/).
	tmpDir := filepath.Join(os.TempDir(), "jenny-spills")
	if sessionID != "" {
		tmpDir = filepath.Join(tmpDir, sessionID)
	}
	if dirExists(tmpDir) || mkdirAll(tmpDir, 0755) == nil {
		if path, err := createSpillTempFile(tmpDir, output); err == nil {
			return path, nil
		}
	}

	return "", fmt.Errorf("failed to create spill file in any location")
}

func createSpillTempFile(dir, output string) (string, error) {
	f, err := os.CreateTemp(dir, "spill-*")
	if err != nil {
		return "", err
	}
	name := f.Name()
	if _, err := f.WriteString(output); err != nil {
		f.Close()
		os.Remove(name)
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(name)
		return "", err
	}
	return name, nil
}

func spillConfig(sessionID, projectRoot string) SpillConfig {
	return SpillConfig{
		MaxInlineSize: DefaultMaxInlineSize,
		SessionID:     sessionID,
		ProjectRoot:   projectRoot,
	}
}
