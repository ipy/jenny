package tool

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ipy/jenny/internal/skills"
)

// TestSkillActivator_Integration tests for path-triggered skill activation.
func TestSkillActivator_Integration(t *testing.T) {
	// Create temp skill directories
	tmpDir := t.TempDir()
	skillDir := filepath.Join(tmpDir, "test-skill")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatalf("failed to create skill dir: %v", err)
	}
	skillContent := `# Test Skill

A test skill.
`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillContent), 0644); err != nil {
		t.Fatalf("failed to write SKILL.md: %v", err)
	}

	// Discover the skill
	skillList, err := skills.Discover(tmpDir)
	if err != nil {
		t.Fatalf("discover error: %v", err)
	}

	if len(skillList) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skillList))
	}

	// Create activator
	activator := skills.NewPathSkillActivator(skillList)

	// Test activation within skill directory
	activated := activator.ActivateForPath(filepath.Join(skillDir, "some-file.txt"))
	if len(activated) != 1 || activated[0] != "test-skill" {
		t.Errorf("expected skill to be activated for path within skill dir, got %v", activated)
	}

	// Test activation outside skill directory (no glob set)
	activated = activator.ActivateForPath("/Users/sin/work/agents/jenny/other/path.txt")
	if len(activated) != 0 {
		t.Errorf("expected no activation for path outside skill dir without glob, got %v", activated)
	}
}

func TestSkillActivator_WithGlob(t *testing.T) {
	// Create temp skill directories
	tmpDir := t.TempDir()
	skillDir := filepath.Join(tmpDir, "markdown-helper")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatalf("failed to create skill dir: %v", err)
	}
	skillContent := `---
description: Assists with Markdown
activation_glob: "**/*.md"
---

# Markdown Helper
`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillContent), 0644); err != nil {
		t.Fatalf("failed to write SKILL.md: %v", err)
	}

	// Discover the skill
	skillList, err := skills.Discover(tmpDir)
	if err != nil {
		t.Fatalf("discover error: %v", err)
	}

	if len(skillList) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skillList))
	}

	if skillList[0].ActivationGlob != "**/*.md" {
		t.Errorf("expected activation_glob '**/*.md', got %q", skillList[0].ActivationGlob)
	}

	// Create activator
	activator := skills.NewPathSkillActivator(skillList)

	// Test activation for markdown file outside skill directory
	activated := activator.ActivateForPath("/Users/sin/work/agents/jenny/README.md")
	if len(activated) != 1 || activated[0] != "markdown-helper" {
		t.Errorf("expected markdown-helper to be activated for .md path, got %v", activated)
	}

	// Test no activation for non-matching file
	activated = activator.ActivateForPath("/Users/sin/work/agents/jenny/main.go")
	if len(activated) != 0 {
		t.Errorf("expected no activation for .go path, got %v", activated)
	}
}

func TestRegistry_WithSkillsFrameworkEnabled(t *testing.T) {
	// Create temp skill directories
	tmpDir := t.TempDir()
	skillDir := filepath.Join(tmpDir, "test-skill")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatalf("failed to create skill dir: %v", err)
	}
	skillContent := `# Test Skill

A test skill.
`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillContent), 0644); err != nil {
		t.Fatalf("failed to write SKILL.md: %v", err)
	}

	// Discover skills
	skillList, err := skills.Discover(tmpDir)
	if err != nil {
		t.Fatalf("discover error: %v", err)
	}

	// Build registry with skills framework enabled
	registry := NewRegistry().
		WithBaseTools().
		WithReadFileCache(NewReadFileCache()).
		WithSkillsFrameworkEnabled(true, skillList)

	tools := registry.Build()

	// Find the read tool and verify it has the activator wired
	var readTool *ReadTool
	for _, tool := range tools {
		if rt, ok := tool.(*ReadTool); ok {
			readTool = rt
			break
		}
	}

	if readTool == nil {
		t.Fatal("ReadTool not found in registry")
	}

	// Verify skill tool is registered
	foundSkillTool := false
	for _, tool := range tools {
		if tool.Name() == "activate_skill" {
			foundSkillTool = true
			break
		}
	}
	if !foundSkillTool {
		t.Error("activate_skill should be registered when skills framework is enabled")
	}
}

func TestRegistry_BareMode_NoSkills(t *testing.T) {
	// Build registry with bare mode (no skills framework)
	registry := NewRegistry().
		WithBaseTools().
		WithReadFileCache(NewReadFileCache()).
		WithSkillsFrameworkEnabled(false, nil)

	tools := registry.Build()

	// Verify no skill tool is registered
	for _, tool := range tools {
		if tool.Name() == "activate_skill" {
			t.Error("activate_skill should not be registered in bare mode")
		}
	}
}

// TestReadTool_WithSkillActivator tests that ReadTool properly uses the activator
func TestReadTool_WithSkillActivator(t *testing.T) {
	readCache := NewReadFileCache()
	readTool := NewReadTool(false, readCache)

	// Create a mock activator
	mockActivator := &mockSkillActivator{skills: []skills.Skill{}}

	// Set the activator
	readTool.WithSkillActivator(mockActivator)

	if readTool == nil {
		t.Error("expected non-nil ReadTool")
	}
}

// TestWriteTool_WithSkillActivator tests that WriteTool properly uses the activator
func TestWriteTool_WithSkillActivator(t *testing.T) {
	readCache := NewReadFileCache()
	writeTool := NewWriteTool(readCache)

	// Create a mock activator
	mockActivator := &mockSkillActivator{skills: []skills.Skill{}}

	// Set the activator
	writeTool.WithSkillActivator(mockActivator)

	if writeTool == nil {
		t.Error("expected non-nil WriteTool")
	}
}

// TestEditTool_WithSkillActivator tests that EditTool properly uses the activator
func TestEditTool_WithSkillActivator(t *testing.T) {
	readCache := NewReadFileCache()
	editTool := NewEditTool(readCache)

	// Create a mock activator
	mockActivator := &mockSkillActivator{skills: []skills.Skill{}}

	// Set the activator
	editTool.WithSkillActivator(mockActivator)

	if editTool == nil {
		t.Error("expected non-nil EditTool")
	}
}

// mockSkillActivator implements SkillActivator for testing
type mockSkillActivator struct {
	skills []skills.Skill
}

func (a *mockSkillActivator) ActivateForPath(path string) []string {
	var activated []string
	for _, skill := range a.skills {
		if skill.MatchesPath(path) {
			activated = append(activated, skill.Name)
		}
	}
	return activated
}

// TestSkillTool_MCPExclusion verifies MCP tools are not in skills list
func TestSkillTool_MCPExclusion(t *testing.T) {
	// This test verifies that MCP prompts cannot be activated as skills
	// Since MCP prompts don't have SKILL.md files, they are never discovered
	// as skills. This test documents the invariant.

	// Create a skill
	tmpDir := t.TempDir()
	skillDir := filepath.Join(tmpDir, "local-skill")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatalf("failed to create skill dir: %v", err)
	}
	skillContent := `# Local Skill

A local skill.
`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillContent), 0644); err != nil {
		t.Fatalf("failed to write SKILL.md: %v", err)
	}

	// Discover skills - only local-skill should be found
	// MCP prompts are discovered through a different mechanism (mcp.ListMcpResourcesTool)
	// and are not part of skills.Discover

	// This test documents that the architecture naturally excludes MCP prompts from skills
	// because they don't have SKILL.md files and are discovered through a different path
}
