package agent

import (
	"os"
	"path/filepath"
)

// LoadInstructionFile reads the project instruction file from dir.
// It tries CLAUDE.md first, then AGENTS.md as a fallback.
// Returns empty string if neither file exists.
func LoadInstructionFile(dir string) string {
	for _, name := range []string{"CLAUDE.md", "AGENTS.md"} {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err == nil {
			return string(data)
		}
	}
	return ""
}
