// Package skills provides skill discovery and management.
package skills

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
)

// Skill represents a discovered skill with its metadata and content.
type Skill struct {
	Name        string
	Description string
	RootPath    string
	Content     string
}

// Discover scans the given directories for skills.
// A skill is a directory containing a SKILL.md file.
// Returns all discovered skills from all directories.
func Discover(dirs ...string) ([]Skill, error) {
	var skills []Skill

	for _, dir := range dirs {
		if dir == "" {
			continue
		}

		entries, err := os.ReadDir(dir)
		if err != nil {
			// Skip directories that don't exist
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}

			skillPath := filepath.Join(dir, entry.Name())
			skillFile := filepath.Join(skillPath, "SKILL.md")

			info, err := os.Stat(skillFile)
			if err != nil {
				// Skip if SKILL.md doesn't exist
				if os.IsNotExist(err) {
					continue
				}
				continue
			}

			if info.IsDir() {
				continue
			}

			content, err := os.ReadFile(skillFile)
			if err != nil {
				continue
			}

			// Extract name and description from the skill
			name := entry.Name()
			description := extractDescription(content)

			skills = append(skills, Skill{
				Name:        name,
				Description: description,
				RootPath:    skillPath,
				Content:     string(content),
			})
		}
	}

	return skills, nil
}

// extractDescription extracts the description from SKILL.md content.
// It first looks for YAML frontmatter description field, then falls back
// to the first line of the file.
func extractDescription(content []byte) string {
	// Check for YAML frontmatter
	contentStr := string(content)

	// Look for description in frontmatter
	if strings.HasPrefix(contentStr, "---\n") || strings.HasPrefix(contentStr, "---\r\n") {
		parts := strings.SplitN(contentStr[4:], "\n---", 2)
		if len(parts) >= 2 {
			frontmatter := parts[0]
			// Look for description: in frontmatter
			for line := range strings.SplitSeq(frontmatter, "\n") {
				if desc, ok := strings.CutPrefix(line, "description:"); ok {
					desc = strings.TrimSpace(desc)
					// Remove quotes if present
					desc = strings.Trim(desc, "\"'")
					if desc != "" {
						return desc
					}
				}
			}
		}
	}

	// Fall back to first non-empty line as description
	for line := range strings.SplitSeq(contentStr, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Remove markdown headers
		if line, ok := strings.CutPrefix(line, "# "); ok {
			// continue
		} else if line, ok = strings.CutPrefix(line, "## "); ok {
			// continue
		} else if line, ok = strings.CutPrefix(line, "### "); ok {
			// continue
		}
		if line != "" {
			// Truncate to a reasonable description length
			if len(line) > 100 {
				line = line[:97] + "..."
			}
			return line
		}
	}

	return "No description available"
}

// ReadSkillContent reads the full content of a SKILL.md file.
func ReadSkillContent(rootPath string) (string, error) {
	skillFile := filepath.Join(rootPath, "SKILL.md")
	content, err := os.ReadFile(skillFile)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

// NormalizeSkillName normalizes a skill name for case-insensitive lookup.
// It converts to lowercase and trims whitespace.
func NormalizeSkillName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// FindSkillByName finds a skill by name (case-insensitive) from a list of skills.
func FindSkillByName(skills []Skill, name string) *Skill {
	normalized := NormalizeSkillName(name)
	for i := range skills {
		if NormalizeSkillName(skills[i].Name) == normalized {
			return &skills[i]
		}
	}
	return nil
}

// SkillsManifest generates a manifest string for the system prompt.
func SkillsManifest(skills []Skill) string {
	if len(skills) == 0 {
		return ""
	}

	var buf bytes.Buffer
	buf.WriteString("\n\nAvailable Skills:\n")

	for _, skill := range skills {
		buf.WriteString("- ")
		buf.WriteString(skill.Name)
		buf.WriteString(": ")
		buf.WriteString(skill.Description)
		buf.WriteString("\n")
	}

	return buf.String()
}
