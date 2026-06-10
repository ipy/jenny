package main

import (
	"path/filepath"

	"github.com/ipy/jenny/internal/plugin"
	"github.com/ipy/jenny/internal/skills"
)

// discoverAndMergePluginSkills discovers plugins from the given roots, loads
// their skills, and merges them into the provided skills slice.
// Plugin skills with duplicate names (case-insensitive via NormalizeSkillName)
// are skipped. Plugins with invalid manifests, validation errors, or load
// errors are silently skipped.
func discoverAndMergePluginSkills(skillsList []skills.Skill, pluginRoots []string) []skills.Skill {
	for _, pluginRoot := range pluginRoots {
		manifestPath := filepath.Join(pluginRoot, ".codex-plugin", "plugin.json")
		manifest, err := plugin.LoadManifest(manifestPath)
		if err != nil {
			continue
		}

		loadedPlugin := &plugin.LoadedPlugin{
			RootPath:     pluginRoot,
			Manifest:     manifest,
			ManifestPath: manifestPath,
		}

		if err := loadedPlugin.Validate(); err != nil {
			continue
		}

		pluginSkills, err := plugin.LoadPluginSkills(loadedPlugin)
		if err != nil {
			continue
		}

		for _, ps := range pluginSkills {
			if skills.FindSkillByName(skillsList, ps.Name) != nil {
				continue
			}
			skillsList = append(skillsList, ps)
		}
	}
	return skillsList
}