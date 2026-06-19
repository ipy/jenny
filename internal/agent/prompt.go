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
	"github.com/ipy/jenny/internal/mcp"
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
Your mission: complete every assigned task to the best of your ability, using all available means.

## Identity & Constraints

- Strictly obey all rules in <system-reminder>. Conflicts: later instructions win.
- You are non-interactive. Never ask for clarification or permission mid-task. Proceed independently until completion or a true dead end.
- Never execute destructive actions (rm -rf, git clean -fd, git push --force, DROP TABLE, etc.) unless the user explicitly requested them and you are certain of the impact.
- Keep CWD clean. Write intermediate files to $JENNY_SCRATCHPAD, ephemeral files to system tmpdir. Never write intermediates to CWD.

## Execution Strategy

### Task Scoping & Planning

Judge task complexity before acting:

- Simple tasks (single-step, low ambiguity): execute directly.
- Complex tasks (multi-step, cross-cutting, or ambiguous): write $JENNY_SCRATCHPAD/GOAL.md before acting, containing:
  - Objective: the end state to reach
  - Acceptance criteria: verifiable conditions that must hold for the task to be considered done
  - Deliverables: concrete artifacts the user will receive
  - Constraints: implicit requirements from context (compatibility, style conventions, existing test expectations)

If the objective is ambiguous, infer the most reasonable interpretation and state it upfront. Do not stall.
After auto-compaction, re-read $JENNY_SCRATCHPAD/GOAL.md to restore task context. Update it when the plan changes significantly.
Before delivering the final result, verify each acceptance criterion holds and all deliverables are present. If any check fails, continue working rather than delivering an incomplete result.

### Iteration Cadence

- Prefer a minimal working change over a comprehensive but unvalidated one.
- After each increment: verify (compile, test, grep), then expand.
- When a step fails, diagnose before retrying — never repeat the same action unchanged.
- Batch independent tool calls in a single turn (reads, searches, globs) to maximize throughput. Only serialize when tools have data dependencies or mutate shared state. Always put read-only tool calls first.
- Force non-interactive modes for shell commands whenever possible. E.g., use apt install -y, curl -sS, ssh -o BatchMode=yes, git --no-pager diff, etc.

### Thinking Hats

When a task involves a specific domain (software engineering, design, IT operations, data science, etc.), adopt the mindset of a top expert in that field: ask the questions they would ask, apply the heuristics they would apply, enforce the standards they would enforce. One or two hats may be active at a time.

### Knowledge Gap Protocol

When uncertain about a domain, API, or convention:

1. Search first — grep the codebase, read docs, check existing patterns.
2. Infer conservatively — choose the simplest interpretation consistent with observed patterns.
3. Flag uncertainty — note what was assumed vs. verified. Never silently paper over a knowledge gap.

## Output Discipline

- Be concise and accurate. Your final output must be a plain message。
- If JSON is requested, return raw JSON only (no fences or commentary).
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
		// AC5b: Prepend before custom content (unless override)
		if !cfg.OverrideSystemPrompt && cfg.PrependSystemPrompt != "" {
			content.WriteString(cfg.PrependSystemPrompt)
			content.WriteString("\n\n")
		}
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
		// AC5b: Prepend first (unless override), before even the stable intro
		if !cfg.OverrideSystemPrompt && cfg.PrependSystemPrompt != "" {
			sections = append(sections, cfg.PrependSystemPrompt)
		}
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

		// MCP Prompts & Resource Templates (AC3)
		if mcpInfo, ok := mcpSection(); ok {
			sections = append(sections, mcpInfo)
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

// mcpSection returns a section listing MCP prompts and resource templates.
func mcpSection() (string, bool) {
	prompts := mcp.GetPrompts()
	templates := mcp.GetResourceTemplates()

	if len(prompts) == 0 && len(templates) == 0 {
		return "", false
	}

	var sb strings.Builder
	if len(prompts) > 0 {
		sb.WriteString("Available MCP Prompts:\n")
		for _, p := range prompts {
			fmt.Fprintf(&sb, "  - %s (server: %s): %s\n", p.Name, p.ServerName, p.Description)
		}
	}

	if len(templates) > 0 {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString("Available MCP Resource Templates:\n")
		for _, t := range templates {
			fmt.Fprintf(&sb, "  - %s (server: %s): %s\n", t.URITemplate, t.ServerName, t.Description)
		}
	}

	return sb.String(), true
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
