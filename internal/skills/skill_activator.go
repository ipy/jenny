// Package skills provides skill activation support.
package skills

import (
	"fmt"
	"os"
)

// PathSkillActivator implements the tool.SkillActivator interface for path-triggered activation.
type PathSkillActivator struct {
	skills []Skill
}

// NewPathSkillActivator creates a new PathSkillActivator with the given skills.
func NewPathSkillActivator(skills []Skill) *PathSkillActivator {
	return &PathSkillActivator{skills: skills}
}

// ActivateForPath checks if any skill matches the given path and activates it.
// Returns the names of activated skills, or nil if none match.
func (a *PathSkillActivator) ActivateForPath(path string) []string {
	var activated []string
	for _, skill := range a.skills {
		if skill.MatchesPath(path) {
			activated = append(activated, skill.Name)
			// Log activation event
			LogSkillActivated(skill.Name, path)
		}
	}
	return activated
}

// LogSkillActivated logs a skill activation event for debugging.
// Emits a stream-json event for headless compatibility.
func LogSkillActivated(skill, path string) {
	fmt.Fprintf(os.Stdout, `{"type":"skill_activated","skill":%q,"path":%q}`+"\n", skill, path)
}
