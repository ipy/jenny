package tool

import (
	"context"
	"fmt"
	"strings"

	"github.com/ipy/jenny/internal/skills"
)

// SkillTool provides skill activation via the agent's ActivateSkill tool.
type SkillTool struct {
	skills []skills.Skill
}

// NewSkillTool creates a new SkillTool with the given discovered skills.
func NewSkillTool(skills []skills.Skill) *SkillTool {
	return &SkillTool{skills: skills}
}

// Name returns the tool name.
func (t *SkillTool) Name() string {
	return "activate_skill"
}

// Description returns a description of the tool.
func (t *SkillTool) Description() string {
	return "Activates a skill by name, returning its full content and root path. " +
		"When executing skill scripts, set SKILL_ROOT environment variable to the skill's root_path " +
		"to enable relative path resolution (e.g., $SKILL_ROOT/scripts/deploy.sh)."
}

// InputSchema returns the JSON schema for tool input.
func (t *SkillTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "The name of the skill to activate",
			},
		},
		"required": []string{"name"},
	}
}

// Execute activates a skill by name and returns its content and root path.
func (t *SkillTool) Execute(ctx context.Context, input map[string]any, cwd string) (*ToolResult, error) {
	// Get the skill name from input
	name, ok := input["name"].(string)
	if !ok || name == "" {
		return &ToolResult{
			Content: "skill name is required",
			IsError: true,
		}, nil
	}

	// Find the skill by name (case-insensitive)
	skill := skills.FindSkillByName(t.skills, name)
	if skill == nil {
		// Build error message with available skill names
		available := make([]string, 0, len(t.skills))
		for _, s := range t.skills {
			available = append(available, s.Name)
		}

		var content string
		if len(available) > 0 {
			content = fmt.Sprintf("Skill %q not found. Available skills: [%s]", name, strings.Join(available, ", "))
		} else {
			content = fmt.Sprintf("Skill %q not found. No skills are currently available.", name)
		}
		return &ToolResult{
			Content: content,
			IsError: true,
		}, nil
	}

	// Return the skill content wrapped in activated_skill tags
	// Include the root_path as an attribute for path resolution
	content := fmt.Sprintf("<activated_skill root_path=%q>\n%s\n</activated_skill>", skill.RootPath, skill.Content)

	return &ToolResult{
		Content: content,
		IsError: false,
	}, nil
}
