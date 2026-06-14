// Package agent provides the core agent loop.
package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/ipy/jenny/internal/git"
	"github.com/ipy/jenny/internal/redact"
	"github.com/ipy/jenny/internal/skills"
	"github.com/ipy/jenny/internal/tool"
)

const (
	// maxGitStatusChars is the maximum length of git status output before truncation.
	maxGitStatusChars = 2000
)

// defaultIntroSection returns the default introduction section of the system prompt.
func defaultIntroSection() (string, bool) {
	return `You are an autonomous AI assistant with tools to search, read, write, and execute safe operations.
Your mission: autonomously complete every assigned task to the best of your ability, using all available means.

**Core mandates:**
- Strictly obey all rules and instructions in the <system-reminder> block. In case of conflict, subsequent instructions take precedence.
- You are running non-interactively. Never ask the user for clarification, input, or permission mid-task. Never invoke shell tools that require interactive input.
- Exhaust every available avenue on your own: search, read files, run diagnostics, reason step-by-step. Keep trying until the task is done or you have truly reached a dead end.
- Be thorough before acting. Gather all necessary context first. Verify assumptions from actual data; never guess about current implementation details.
- Do not execute destructive or irreversible actions (rm -rf, git clean -fd, etc.) unless the user explicitly requested them and you are certain of the impact.
- Never write intermediate files to CWD. Put important intermediates (docs, scripts) in $JENNY_SCRATCHPAD (env for shell, prefix for Read/Write/Edit tools), ephemeral files (logs, etc.) in system tmpdir.
- Be concise and accurate. Your final output must be a plain message (if JSON is required, output only the raw JSON, no extra commentary or fences).
`, true
}

// toolListSection returns a section listing all available tools.
func toolListSection(tools []tool.Tool) (string, bool) {
	if len(tools) == 0 {
		return "", false
	}

	var names []string
	for _, t := range tools {
		names = append(names, t.Name())
	}
	return fmt.Sprintf("Available tools: %s", strings.Join(names, ", ")), true
}

// gitStatusSection returns a section with git status information if in a git repo.
func gitStatusSection(cwd string) (string, bool) {
	root, err := git.GetRoot(cwd)
	if err != nil {
		// Not in a git repository
		return "", false
	}

	branch, err := git.GetBranch(root)
	if err != nil {
		return "", false
	}

	head, err := git.GetHead(root)
	if err != nil {
		return "", false
	}

	// Get git status --short output
	statusOutput, _ := getGitStatusShort(cwd)

	var section strings.Builder
	section.WriteString("Git context:\n")
	fmt.Fprintf(&section, "  Branch: %s\n", branch)
	if head != "" {
		fmt.Fprintf(&section, "  HEAD: %s\n", head)
	}
	if statusOutput != "" {
		section.WriteString("  Status:\n")
		// Cap at maxGitStatusChars
		if len(statusOutput) > maxGitStatusChars {
			statusOutput = statusOutput[:maxGitStatusChars] + "\n... (truncated)"
		}
		// Indent each line
		for line := range strings.SplitSeq(statusOutput, "\n") {
			if line != "" {
				fmt.Fprintf(&section, "    %s\n", line)
			}
		}
	}

	return section.String(), true
}

// getGitStatusShort runs `git status --short` and returns the output.
func getGitStatusShort(cwd string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "status", "--short")
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// platformSection returns a section with OS/Arch platform context.
func platformSection() (string, bool) {
	platform := fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH)
	section := fmt.Sprintf("Platform: %s", platform)

	// Add Windows-specific hints when running on Windows
	if runtime.GOOS == "windows" {
		section += "\n\nYou are running on Windows. Use the PowerShell tool for system commands. Be aware of Windows file path conventions (e.g., C:\\path\\to\\file)."
	}

	return section, true
}

// contextSection returns a section with CWD context.
func contextSection(cwd string) (string, bool) {
	if cwd == "" {
		cwd, _ = os.Getwd()
		if cwd == "" {
			cwd, _ = os.UserHomeDir()
		}
	}
	section := fmt.Sprintf("Cwd: %s", cwd)
	return section, true
}

// environmentSection returns a section with current date.
func environmentSection() (string, bool) {
	date := time.Now().Format("2006-01-02")
	section := fmt.Sprintf("Date: %s", date)
	return section, true
}

// appendSection returns the append prompt if set and not overridden.
func appendSection(appendPrompt string, override bool) (string, bool) {
	if override || appendPrompt == "" {
		return "", false
	}
	return appendPrompt, true
}

// AssembleSystemPrompt builds the system prompt from sections based on configuration.
// Each section is a function returning (content, shouldInclude).
// On the first call, the result should be frozen by the caller into cfg.CachedSystemPrompt
// so that subsequent calls return the identical blocks, protecting Anthropic's prompt
// caching from dynamic variation (date, git status).
func AssembleSystemPrompt(cfg *StreamConfig, tools []tool.Tool, cwd string) []string {
	// Return frozen prompt if already assembled
	if len(cfg.CachedSystemPrompt) > 0 {
		return cfg.CachedSystemPrompt
	}
	return buildSystemPrompt(cfg, tools, cwd)
}

// buildSystemPrompt assembles the system prompt sections (the actual builder).
// Exported for testing; use AssembleSystemPrompt in production.
func buildSystemPrompt(cfg *StreamConfig, tools []tool.Tool, cwd string) []string {
	// AC1: Custom prompt replaces all defaults
	if cfg.CustomSystemPrompt != "" {
		var content strings.Builder
		content.WriteString(cfg.CustomSystemPrompt)

		// AC5: Append section always checked last, independent of custom/default
		if !cfg.OverrideSystemPrompt && cfg.AppendSystemPrompt != "" {
			content.WriteString("\n\n")
			content.WriteString(cfg.AppendSystemPrompt)
		}

		return []string{content.String() + "\n"}
	}

	// Assemble sections in stable-to-volatile order for optimal caching.
	var blocks []string

	// --- Block 1: Global Identity (Most stable) ---
	{
		var sections []string
		// Default intro first — this is the stable, cache-friendly part
		if intro, ok := defaultIntroSection(); ok {
			sections = append(sections, intro)
		}

		// AC2: Tool list sync
		if toolList, ok := toolListSection(tools); ok {
			sections = append(sections, toolList)
		}

		// AC9: Redaction instruction in system prompt when enabled
		if cfg.RedactMode != redact.ModeDisabled {
			prompt := "This session has secret redaction enabled."
			switch redact.ParseRedactMode(string(cfg.RedactMode)) {
			case redact.ModeRecover:
				prompt += " Tool results may contain `[REDACTED:<hex>]` placeholders (e.g. `[REDACTED:a3f1b2c9]`)." +
					" They will be automatically recovered when you use them in tool calls, so you can refer to them directly as needed." +
					" Copy them verbatim - including the full hex suffix - and never simplify, abbreviate, or otherwise modify them."
			case redact.ModeRedact:
				prompt += " Tool results may contain `[REDACTED:<hex>]` markers." +
					" You can still use the original content internally (e.g., through local scripts), but you are strictly prohibited from exposing it in any way."
			}
			sections = append(sections, prompt)
		}
		if len(sections) > 0 {
			blocks = append(blocks, strings.Join(sections, "\n\n"))
		}
	}

	// --- Block 2: Runtime Platform & Skills (Global-ish / Machine-stable) ---
	{
		var sections []string
		// AC4: Platform context (OS/Arch)
		if platform, ok := platformSection(); ok {
			sections = append(sections, platform)
		}

		// Skills manifest (AC2)
		if len(cfg.Skills) > 0 {
			if manifest := skills.SkillsManifest(cfg.Skills); manifest != "" {
				sections = append(sections, manifest)
			}
		}
		if len(sections) > 0 {
			blocks = append(blocks, strings.Join(sections, "\n\n"))
		}
	}

	// --- Block 3: Project Memory & Identity (Project-stable) ---
	{
		var sections []string
		// CWD is project-specific but stable compared to memory/git
		if ctx, ok := contextSection(cwd); ok {
			sections = append(sections, ctx)
		}

		// AC1: Memory content injected as <system-reminder> block
		// MemoryContent is per-session/per-conversation.
		if cfg.MemoryContent != "" {
			sections = append(sections, "<system-reminder>\n"+cfg.MemoryContent+"\n</system-reminder>")
		}
		if len(sections) > 0 {
			blocks = append(blocks, strings.Join(sections, "\n\n"))
		}
	}

	// --- Block 4: Volatile Environment & Git (Most volatile) ---
	{
		var sections []string
		// Current date
		if env, ok := environmentSection(); ok {
			sections = append(sections, env)
		}

		// AC3: Git status injection (only inside repo) — captured once at session start
		if gitStatus, ok := gitStatusSection(cwd); ok {
			sections = append(sections, gitStatus)
		}
		if len(sections) > 0 {
			blocks = append(blocks, strings.Join(sections, "\n\n")+"\n")
		}
	}

	// AC5: Append section must be last (after all blocks, before return)
	if appendContent, ok := appendSection(cfg.AppendSystemPrompt, cfg.OverrideSystemPrompt); ok {
		blocks = append(blocks, appendContent+"\n")
	}

	return blocks
}

// DynamicSystemSuffix is intentionally empty. All dynamic content (active skills,
// cwd changes, date changes) is communicated through virtual user messages in the
// message chain instead of via the system prompt. This ensures the system prompt
// prefix is byte-stable across turns, preventing cache invalidation of the entire
// message chain when the suffix changes.
func DynamicSystemSuffix(cfg *StreamConfig, cwd string) string {
	return ""
}

// activeSkillsSection returns the "Active Skills" section for the system prompt.
// Returns empty string if no skills are active.
func activeSkillsSection(activeSkills []ActivatedSkill) string {
	if len(activeSkills) == 0 {
		return ""
	}

	var lines []string
	lines = append(lines, "Active Skills:")
	for _, skill := range activeSkills {
		lines = append(lines, fmt.Sprintf("- %s: %s", skill.Name, skill.RootPath))
	}
	return strings.Join(lines, "\n")
}
