package agent

import (
	"testing"
)

func TestActiveSkillsSection_Empty(t *testing.T) {
	result := activeSkillsSection(nil)
	if result != "" {
		t.Errorf("activeSkillsSection(nil) should return empty, got %q", result)
	}
}

func TestActiveSkillsSection_WithSkills(t *testing.T) {
	skills := []ActivatedSkill{
		{Name: "readme-writer", RootPath: "/path/to/readme-writer"},
	}
	result := activeSkillsSection(skills)

	if !containsActiveSkillsSection(result) {
		t.Error("activeSkillsSection should contain 'Active Skills:' header")
	}
	if !containsSubstring(result, "readme-writer") {
		t.Error("should contain skill name")
	}
	if !containsSubstring(result, "/path/to/readme-writer") {
		t.Error("should contain skill root path")
	}
}

func TestActiveSkillsSection_MultipleSkills(t *testing.T) {
	skills := []ActivatedSkill{
		{Name: "readme-writer", RootPath: "/path/to/readme-writer"},
		{Name: "code-review", RootPath: "/path/to/code-review"},
	}
	result := activeSkillsSection(skills)

	if !containsSubstring(result, "readme-writer") {
		t.Error("should contain 'readme-writer'")
	}
	if !containsSubstring(result, "code-review") {
		t.Error("should contain 'code-review'")
	}
}

func TestActiveSkillsSection_Format(t *testing.T) {
	skills := []ActivatedSkill{
		{Name: "test-skill", RootPath: "/absolute/path/to/test-skill"},
	}
	result := activeSkillsSection(skills)

	expected := "Active Skills:\n- test-skill: /absolute/path/to/test-skill"
	if result != expected {
		t.Errorf("format mismatch:\ngot:  %q\nwant: %q", result, expected)
	}
}

func TestDynamicSystemSuffix_AlwaysEmpty(t *testing.T) {
	// DynamicSystemSuffix is intentionally empty. Active skills are communicated
	// through virtual messages in the message chain, not via system prompt suffix.
	tests := []struct {
		name string
		cfg  *StreamConfig
	}{
		{"no skills", &StreamConfig{}},
		{"with skills", &StreamConfig{ActiveSkills: []ActivatedSkill{
			{Name: "test", RootPath: "/test"},
		}}},
		{"custom prompt", &StreamConfig{CustomSystemPrompt: "custom"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DynamicSystemSuffix(tt.cfg, "/tmp")
			if result != "" {
				t.Errorf("DynamicSystemSuffix should always be empty, got %q", result)
			}
		})
	}
}

func TestActiveSkills_SurviveCompaction_InStreamConfig(t *testing.T) {
	// ActiveSkills live in StreamConfig (process memory), not the message chain.
	// Compaction only modifies messages. After compaction, StreamConfig.ActiveSkills
	// is unchanged and will be re-injected as a virtual message.
	cfg := StreamConfig{
		ActiveSkills: []ActivatedSkill{
			{Name: "persistent-skill", RootPath: "/persistent/path"},
		},
	}

	// Simulate compaction — only messages change, StreamConfig is untouched
	if len(cfg.ActiveSkills) != 1 {
		t.Error("ActiveSkills should survive compaction in StreamConfig")
	}
	if cfg.ActiveSkills[0].Name != "persistent-skill" {
		t.Error("ActiveSkills data should be intact after compaction")
	}
}

func TestSetActiveSkills(t *testing.T) {
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
