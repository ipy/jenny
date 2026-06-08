// Package tool provides skill activation framework support.
package tool

import (
	"fmt"
	"os"
)

// SkillActivator defines the interface for triggering skill activation on file access.
type SkillActivator interface {
	// ActivateForPath checks if any skill matches the given path and activates it.
	// Returns the names of activated skills, or nil if none match.
	ActivateForPath(path string) []string
}

// LogSkillActivated logs a skill activation event for debugging.
// Emits a stream-json event for headless compatibility.
func LogSkillActivated(skill, path string) {
	fmt.Fprintf(os.Stdout, `{"type":"skill_activated","skill":%q,"path":%q}`+"\n", skill, path)
}
