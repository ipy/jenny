// Package skills provides skill activation support.
package skills

import (
	"fmt"
	"os"
)

// ActivatedSkill records a skill that has been activated in the current session.
type ActivatedSkill struct {
	Name     string
	RootPath string
}

// PathSkillActivator implements the tool.SkillActivator interface for path-triggered activation.
type PathSkillActivator struct {
	skills          []Skill
	activatedSkills []ActivatedSkill // Tracks skills that have been activated
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
			// Register the activation for tracking
			a.RegisterActivation(skill.Name, skill.RootPath)
			// Log activation event
			LogSkillActivated(skill.Name, path)
		}
	}
	return activated
}

// RegisterActivation records a skill activation by name.
// Implements the tool.SkillActivator interface.
func (a *PathSkillActivator) RegisterActivation(name string, rootPath string) {
	// Deduplication: check if already registered
	for _, s := range a.activatedSkills {
		if s.Name == name {
			return
		}
	}
	a.activatedSkills = append(a.activatedSkills, ActivatedSkill{
		Name:     name,
		RootPath: rootPath,
	})
}

// GetActivatedSkills returns the list of activated skills.
func (a *PathSkillActivator) GetActivatedSkills() []ActivatedSkill {
	return a.activatedSkills
}

// LogSkillActivated logs a skill activation event for debugging.
// Emits a stream-json event for headless compatibility.
func LogSkillActivated(skill, path string) {
	fmt.Fprintf(os.Stdout, `{"type":"skill_activated","skill":%q,"path":%q}`+"\n", skill, path)
}
