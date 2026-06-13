// Package skills provides skill activation support.
package skills

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// ActivatedSkill records a skill that has been activated in the current session.
type ActivatedSkill struct {
	Name     string
	RootPath string
}

// PathSkillActivator implements the tool.SkillActivator interface for path-triggered activation.
type PathSkillActivator struct {
	mu              sync.Mutex
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
	a.mu.Lock()
	defer a.mu.Unlock()
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

// GetActivatedSkills returns a copy of the activated skills list.
func (a *PathSkillActivator) GetActivatedSkills() []ActivatedSkill {
	a.mu.Lock()
	defer a.mu.Unlock()
	result := make([]ActivatedSkill, len(a.activatedSkills))
	copy(result, a.activatedSkills)
	return result
}

// LogSkillActivated logs a skill activation event for debugging.
// Emits a stream-json event for headless compatibility.
func LogSkillActivated(skill, path string) {
	event := struct {
		Type  string `json:"type"`
		Skill string `json:"skill"`
		Path  string `json:"path"`
	}{
		Type:  "skill_activated",
		Skill: skill,
		Path:  path,
	}
	data, _ := json.Marshal(event)
	fmt.Fprintln(os.Stdout, string(data))
}
