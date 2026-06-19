// Package tool provides skill activation framework support.
package tool

// SkillActivator defines the interface for triggering skill activation on file access.
type SkillActivator interface {
	// ActivateForPath checks if any skill matches the given path and activates it.
	// Returns the names of activated skills, or nil if none match.
	ActivateForPath(path string) []string

	// RegisterActivation records a skill activation by name.
	// The activator should track activations for later retrieval.
	RegisterActivation(name string, rootPath string, allowedTools []string)

	// GetActivatedTools returns the union of allowed tools for all active skills.
	// If no skills are active, returns nil (no skill-based restrictions).
	GetActivatedTools() []string
}
