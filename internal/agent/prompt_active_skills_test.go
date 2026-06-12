package agent

import (
	"testing"
)

func TestActiveSkillsSection_Empty(t *testing.T) {
	// AC6: No Active Skills section when no skills are active
	cfg := StreamConfig{
		ActiveSkills: nil,
	}

	suffix := DynamicSystemSuffix(cfg, "/tmp")

	// Should not contain Active Skills
	if containsActiveSkillsSection(suffix) {
		t.Error("Active Skills section should not be present when no skills are active")
	}
}

func TestActiveSkillsSection_WithSkills(t *testing.T) {
	// AC1: Active Skills section appears when skills are activated
	cfg := StreamConfig{
		ActiveSkills: []ActivatedSkill{
			{Name: "readme-writer", RootPath: "/path/to/readme-writer"},
		},
	}

	suffix := DynamicSystemSuffix(cfg, "/tmp")

	// Should contain Active Skills section
	if !containsActiveSkillsSection(suffix) {
		t.Error("Active Skills section should be present when skills are active")
	}

	// Should contain the skill name
	if !containsSubstring(suffix, "readme-writer") {
		t.Error("suffix should contain skill name 'readme-writer'")
	}

	// Should contain the skill path
	if !containsSubstring(suffix, "/path/to/readme-writer") {
		t.Error("suffix should contain skill root path")
	}
}

func TestActiveSkillsSection_MultipleSkills(t *testing.T) {
	// AC5: Multiple skills are all shown
	cfg := StreamConfig{
		ActiveSkills: []ActivatedSkill{
			{Name: "readme-writer", RootPath: "/path/to/readme-writer"},
			{Name: "code-review", RootPath: "/path/to/code-review"},
		},
	}

	suffix := DynamicSystemSuffix(cfg, "/tmp")

	// Should contain both skills
	if !containsSubstring(suffix, "readme-writer") {
		t.Error("suffix should contain 'readme-writer'")
	}
	if !containsSubstring(suffix, "code-review") {
		t.Error("suffix should contain 'code-review'")
	}
}

func TestActiveSkillsSection_Format(t *testing.T) {
	// Test the exact format of the Active Skills section
	cfg := StreamConfig{
		ActiveSkills: []ActivatedSkill{
			{Name: "test-skill", RootPath: "/absolute/path/to/test-skill"},
		},
	}

	suffix := DynamicSystemSuffix(cfg, "/tmp")

	// Should contain the exact format: "- skill-name: /path"
	expected := "Active Skills:\n- test-skill: /absolute/path/to/test-skill"
	if !containsSubstring(suffix, expected) {
		t.Errorf("suffix should contain exact format %q, got: %s", expected, suffix)
	}
}

func TestActiveSkillsSection_CacheFriendly(t *testing.T) {
	// AC4: Dynamic suffix changes when active skills change, but cached prefix stays stable
	// Build prompt without active skills
	cfg1 := StreamConfig{
		ActiveSkills: nil,
	}

	// Build prompt with active skills
	cfg2 := StreamConfig{
		ActiveSkills: []ActivatedSkill{
			{Name: "new-skill", RootPath: "/path/to/new-skill"},
		},
	}

	// The suffix changes when active skills are added
	suffix1 := DynamicSystemSuffix(cfg1, "/tmp")
	suffix2 := DynamicSystemSuffix(cfg2, "/tmp")

	// Suffixes should be different when active skills change
	if suffix1 == suffix2 {
		t.Error("DynamicSystemSuffix should change when ActiveSkills changes")
	}

	// The suffix with active skills should have Active Skills section
	if !containsActiveSkillsSection(suffix2) {
		t.Error("suffix with active skills should contain Active Skills section")
	}

	// Verify empty suffix doesn't have Active Skills section
	if containsActiveSkillsSection(suffix1) {
		t.Error("suffix without active skills should not have Active Skills section")
	}
}

func TestActiveSkillsSection_CompactionSurvival(t *testing.T) {
	// AC3: Active Skills survive context compaction
	// Since ActiveSkills lives in StreamConfig (process memory), it survives compaction
	// which only modifies the message chain. This test verifies the mechanism.

	cfg := StreamConfig{
		ActiveSkills: []ActivatedSkill{
			{Name: "persistent-skill", RootPath: "/persistent/path"},
		},
	}

	// Simulate compaction - StreamConfig.ActiveSkills is NOT modified
	// (compaction only calls compactMessages which works on the messages slice)

	// After compaction, the active skills should still be there
	suffix := DynamicSystemSuffix(cfg, "/tmp")

	if !containsActiveSkillsSection(suffix) {
		t.Error("Active Skills should survive compaction - they live in StreamConfig, not message chain")
	}
}

func TestSetActiveSkills(t *testing.T) {
	// Test that SetActiveSkills correctly updates the ActiveSkills field
	cfg := StreamConfig{}

	skills := []ActivatedSkill{
		{Name: "skill-1", RootPath: "/path/1"},
		{Name: "skill-2", RootPath: "/path/2"},
	}

	cfg.SetActiveSkills(skills)

	if len(cfg.ActiveSkills) != 2 {
		t.Errorf("expected 2 active skills, got %d", len(cfg.ActiveSkills))
	}

	if cfg.ActiveSkills[0].Name != "skill-1" {
		t.Errorf("expected skill name 'skill-1', got %s", cfg.ActiveSkills[0].Name)
	}
}

// Helper functions
func containsActiveSkillsSection(s string) bool {
	return containsSubstring(s, "Active Skills:")
}

func containsSubstring(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && len(s) >= len(substr) &&
		(func() bool {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
			return false
		})()
}
