// Package tool provides tool implementations.
package tool

import (
	"path/filepath"
	"strings"

	"github.com/ipy/jenny/internal/constants"
)

const scratchpadPrefix = "$JENNY_SCRATCHPAD/"

// ResolveScratchpadPrefix replaces $JENNY_SCRATCHPAD/ prefix with the real scratchpad path.
// Returns the path unchanged if no prefix is present.
// The result is cleaned and validated to prevent escape via .. or symlinks.
func ResolveScratchpadPrefix(filePath, sessionID string) string {
	if !strings.HasPrefix(filePath, scratchpadPrefix) {
		return filePath
	}

	subPath := strings.TrimPrefix(filePath, scratchpadPrefix)
	scratchDir := filepath.Clean(constants.ScratchpadDir(sessionID))
	resolved := filepath.Clean(filepath.Join(scratchDir, subPath))

	// Security: verify the resolved path is still under scratchpad
	sep := string(filepath.Separator)
	if !strings.HasPrefix(resolved+sep, scratchDir+sep) {
		// Path escaped scratchpad via .. or symlink — reject
		return filePath
	}

	return resolved
}
