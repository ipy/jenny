package tool

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ipy/jenny/internal/skills"
)

func TestSkillTool_Name(t *testing.T) {
	tool := NewSkillTool(nil)
	if tool.Name() != "activate_skill" {
		t.Errorf("expected name 'activate_skill', got %q", tool.Name())
	}
}

func TestSkillTool_Description(t *testing.T) {
	tool := NewSkillTool(nil)
	desc := tool.Description()
	if desc == "" {
		t.Error("Description() should not be empty")
	}
	// Should mention SKILL_ROOT convention
	if !strings.Contains(desc, "SKILL_ROOT") {
		t.Error("Description should mention SKILL_ROOT convention")
	}
}

func TestSkillTool_InputSchema(t *testing.T) {
	tool := NewSkillTool(nil)
	schema := tool.InputSchema()
	if schema["type"] != "object" {
		t.Errorf("expected schema type 'object', got %v", schema["type"])
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("properties should be a map")
	}
	if _, ok := props["name"]; !ok {
		t.Error("schema should have 'name' property")
	}
	required, ok := schema["required"].([]string)
	if !ok {
		t.Fatal("required should be a []string")
	}
	found := slices.Contains(required, "name")
	if !found {
		t.Error("'name' should be in required")
	}
}

func TestSkillTool_AC1_ActivationReturnsContentAndPath(t *testing.T) {
	// Create a test skill
	testSkill := skills.Skill{
		Name:        "test-skill",
		Description: "A test skill",
		RootPath:    "/path/to/test-skill",
		Content:     "# Test Skill\n\nSome content",
	}

	tool := NewSkillTool([]skills.Skill{testSkill})
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{"name": "test-skill"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.IsError {
		t.Errorf("expected no error, got: %s", result.Content)
	}

	// Should contain the root_path attribute
	if !strings.Contains(result.Content, `root_path="/path/to/test-skill"`) {
		t.Errorf("expected root_path in output, got: %s", result.Content)
	}

	// Should contain the skill content wrapped in tags
	if !strings.Contains(result.Content, "<activated_skill") || !strings.Contains(result.Content, "</activated_skill>") {
		t.Errorf("expected activated_skill tags, got: %s", result.Content)
	}

	// Should contain the actual skill content
	if !strings.Contains(result.Content, "# Test Skill") {
		t.Errorf("expected skill content in output, got: %s", result.Content)
	}
}

func TestSkillTool_AC5_UnknownSkillError(t *testing.T) {
	// Create a test skill
	testSkill := skills.Skill{
		Name:        "existing-skill",
		Description: "An existing skill",
		RootPath:    "/path/to/skill",
		Content:     "content",
	}

	tool := NewSkillTool([]skills.Skill{testSkill})
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{"name": "nonexistent-skill"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.IsError {
		t.Error("expected error for unknown skill")
	}

	// Error message should list available skills
	if !strings.Contains(result.Content, "not found") {
		t.Errorf("expected 'not found' in error, got: %s", result.Content)
	}
	if !strings.Contains(result.Content, "existing-skill") {
		t.Errorf("expected available skill names in error, got: %s", result.Content)
	}
}

func TestSkillTool_AC5_NoSkillsAvailable(t *testing.T) {
	tool := NewSkillTool(nil)
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{"name": "any-skill"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.IsError {
		t.Error("expected error when no skills available")
	}

	if !strings.Contains(result.Content, "not found") {
		t.Errorf("expected 'not found' in error, got: %s", result.Content)
	}
}

func TestSkillTool_AC3_PathResolution(t *testing.T) {
	// Test that root_path is returned correctly for path resolution
	testSkill := skills.Skill{
		Name:        "path-test",
		Description: "Test path resolution",
		RootPath:    "/absolute/path/to/skill",
		Content:     "content",
	}

	tool := NewSkillTool([]skills.Skill{testSkill})
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{"name": "path-test"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify root_path matches actual skill directory
	if !strings.Contains(result.Content, `/absolute/path/to/skill`) {
		t.Errorf("expected root_path to match skill directory, got: %s", result.Content)
	}
}

func TestSkillTool_CaseInsensitiveLookup(t *testing.T) {
	testSkill := skills.Skill{
		Name:        "Readme-Writer",
		Description: "Creates README files",
		RootPath:    "/path/to/readme-writer",
		Content:     "# README Writer",
	}

	tool := NewSkillTool([]skills.Skill{testSkill})
	ctx := context.Background()

	// Test lowercase
	result, err := tool.Execute(ctx, map[string]any{"name": "readme-writer"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("expected no error for lowercase name, got: %s", result.Content)
	}

	// Test uppercase
	result, err = tool.Execute(ctx, map[string]any{"name": "README-WRITER"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("expected no error for uppercase name, got: %s", result.Content)
	}
}

func TestSkillTool_NameRequired(t *testing.T) {
	tool := NewSkillTool(nil)
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.IsError {
		t.Error("expected error when name is missing")
	}
	if !strings.Contains(result.Content, "required") {
		t.Errorf("expected 'required' error, got: %s", result.Content)
	}
}

func TestSkillTool_AC6_DiscoveryFromMultipleDirs(t *testing.T) {
	// This test verifies that skills discovered from multiple directories
	// are correctly passed to the tool and can be activated.
	// We'll simulate this by creating skills with different root paths.

	skillsDir := filepath.Join("testdata", "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		t.Skip("skills testdata not found, skipping")
	}

	var discoveredSkills []skills.Skill
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillPath := filepath.Join(skillsDir, entry.Name())
		skillFile := filepath.Join(skillPath, "SKILL.md")
		if _, err := os.Stat(skillFile); err != nil {
			continue
		}
		content, _ := os.ReadFile(skillFile)
		discoveredSkills = append(discoveredSkills, skills.Skill{
			Name:        entry.Name(),
			Description: "Discovered skill",
			RootPath:    skillPath,
			Content:     string(content),
		})
	}

	if len(discoveredSkills) < 2 {
		t.Skip("need at least 2 skills for this test")
	}

	tool := NewSkillTool(discoveredSkills)
	ctx := context.Background()

	// Should be able to activate any of the discovered skills
	for _, s := range discoveredSkills {
		result, err := tool.Execute(ctx, map[string]any{"name": s.Name}, "")
		if err != nil {
			t.Fatalf("unexpected error activating %s: %v", s.Name, err)
		}
		if result.IsError {
			t.Errorf("expected no error activating %s, got: %s", s.Name, result.Content)
		}
	}
}
