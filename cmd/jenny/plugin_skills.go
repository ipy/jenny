package main

import (
	"os"
	"path/filepath"

	"github.com/ipy/jenny/internal/constants"
	"github.com/ipy/jenny/internal/mcp"
	"github.com/ipy/jenny/internal/plugin"
	"github.com/ipy/jenny/internal/skills"
)

// loadPluginFromRoot tries plugin marker directories in priority order
// (.jenny-plugin, .claude-plugin, .codex-plugin) and returns the first valid
// LoadedPlugin, or nil if none found.
func loadPluginFromRoot(pluginRoot string) *plugin.LoadedPlugin {
	for _, marker := range plugin.PluginDirNames() {
		manifestPath := filepath.Join(pluginRoot, marker, "plugin.json")
		manifest, err := plugin.LoadManifest(manifestPath)
		if err != nil {
			continue
		}
		loaded := &plugin.LoadedPlugin{
			RootPath:     pluginRoot,
			Manifest:     manifest,
			ManifestPath: manifestPath,
		}
		if err := loaded.Validate(); err != nil {
			continue
		}
		return loaded
	}
	return nil
}

// discoverAndMergePluginSkills discovers plugins from the given roots, loads
// their skills, and merges them into the provided skills slice.
// Plugin skills with duplicate names (case-insensitive via NormalizeSkillName)
// are skipped. Plugins with invalid manifests, validation errors, or load
// errors are silently skipped.
func discoverAndMergePluginSkills(skillsList []skills.Skill, pluginRoots []string) []skills.Skill {
	for _, pluginRoot := range pluginRoots {
		loaded := loadPluginFromRoot(pluginRoot)
		if loaded == nil {
			continue
		}

		pluginSkills, err := plugin.LoadPluginSkills(loaded)
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

// loadPluginMCPServers discovers plugins from cwd, jennyHomeDir, and agentsHomeDir,
// loads their MCP server definitions, and returns them as a map. Only first-seen wins (no
// overwrites across plugins) to keep behavior deterministic. Plugins with
// invalid manifests, validation errors, or load errors are silently skipped.
func loadPluginMCPServers(cwd, jennyHomeDir, agentsHomeDir string) map[string]mcp.MCPServerDef {
	serverDefs := make(map[string]mcp.MCPServerDef)

	roots := plugin.FindPluginRoots(cwd)
	if jennyHomeDir != "" {
		homePluginRoots := plugin.FindPluginRoots(jennyHomeDir)
		roots = append(roots, homePluginRoots...)
	}
	if agentsHomeDir != "" {
		agentsPluginRoots := plugin.FindPluginRoots(agentsHomeDir)
		roots = append(roots, agentsPluginRoots...)
	}

	for _, root := range roots {
		loaded := loadPluginFromRoot(root)
		if loaded == nil {
			continue
		}

		pluginDefs, err := plugin.LoadPluginMCPServers(loaded)
		if err != nil {
			continue
		}

		for name, def := range pluginDefs {
			if _, exists := serverDefs[name]; !exists {
				serverDefs[name] = def
			}
		}
	}

	return serverDefs
}

// collectDefaultMCPPaths returns MCP config file paths in precedence order
// (lowest to highest priority). Only paths that exist on disk are included.
// Precedence: ~/.agents/mcp.json → <cwd>/.agents/mcp.json → ~/.jenny/mcp.json
func collectDefaultMCPPaths(cwd, jennyHomeDir, agentsHomeDir string) []string {
	var paths []string

	// Lowest priority: ~/.agents/mcp.json (cross-tool shared user config)
	if agentsHomeDir != "" {
		p := filepath.Join(agentsHomeDir, "mcp.json")
		if _, err := os.Stat(p); err == nil {
			paths = append(paths, p)
		}
	}

	// Project-local: <cwd>/.agents/mcp.json (cross-tool shared project config)
	if cwd != "" {
		p := filepath.Join(cwd, constants.AgentsDirName, "mcp.json")
		if _, err := os.Stat(p); err == nil {
			paths = append(paths, p)
		}
	}

	// Highest priority: ~/.jenny/mcp.json (jenny-specific user config)
	if jennyHomeDir != "" {
		p := filepath.Join(jennyHomeDir, "mcp.json")
		if _, err := os.Stat(p); err == nil {
			paths = append(paths, p)
		}
	}

	return paths
}
