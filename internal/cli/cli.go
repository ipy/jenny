// Package cli provides command-line interface support for jenny.
package cli

import (
	stdjson "encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/knadh/koanf/parsers/json"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/posflag"
	"github.com/knadh/koanf/v2"
	"github.com/spf13/pflag"
)

// Flags holds the parsed command-line flags.
type Flags struct {
	Prompt                 string   // prompt: joined from k.Strings("print") after unmarshal, or from --prompt-file
	PromptFile             []string // --prompt-file: read prompt from file(s); "-" means stdin
	Model                  string   `koanf:"model"`
	OutputFormat           string   `koanf:"output-format"`
	Verbose                bool     `koanf:"verbose"`
	IncludePartialMessages bool     `koanf:"include-partial-messages"`
	SkipPermissions        bool     `koanf:"dangerously-skip-permissions"`
	PermissionLevel        string   `koanf:"permission-level"`
	SessionResume          string   `koanf:"resume"`
	NoSessionPersistence   bool     `koanf:"no-session-persistence"`
	ForkSession            bool     `koanf:"fork-session"`
	Continue               bool     `koanf:"continue"`
	MCPConfig              []string `koanf:"mcp-config"`
	StrictMCP              bool     `koanf:"strict-mcp-config"`
	DeniedTools            []string `koanf:"deny-tool"`
	Bare                   bool     `koanf:"bare"`
	SwarmsEnabled          bool     `koanf:"swarm"`                 // When true, enables named agent delegation (swarm mode)
	Version                bool     `koanf:"version"`               // --version / -v: print version and exit
	PrintSystemPrompt      bool     `koanf:"print-system-prompt"`   // --print-system-prompt: print the assembled system prompt and exit
	CustomSystemPrompt     string   `koanf:"system-prompt"`         // --system-prompt: replaces default system prompt entirely
	AppendSystemPrompt     string   `koanf:"append-system-prompt"`  // --append-system-prompt: appended after assembled system prompt
	PrependSystemPrompt    string   `koanf:"prepend-system-prompt"` // --prepend-system-prompt: prepended before assembled system prompt
	MaxIterations          int      `koanf:"max-iterations"`        // --max-iterations: maximum loop iterations (0 = unlimited)
	MaxTurns               int      `koanf:"max-turns"`             // --max-turns: maximum number of turns (0 = unlimited)
	MaxBudgetUsd           float64  `koanf:"max-budget-usd"`        // --max-budget-usd: budget limit in USD (0.0 = no limit)
	Effort                 string   `koanf:"effort"`                // --effort: reasoning effort level (low, medium, high)
	ThinkingBudget         int      `koanf:"thinking-budget"`       // --thinking-budget: maximum thinking tokens for Anthropic (AC3)
	RedactMode             string   `koanf:"redact"`                // --redact: JENNY_REDACT; one of disabled|redact|recover (empty = default)
	TranscriptDir          string   `koanf:"transcript-dir"`        // --transcript-dir: JENNY_TRANSCRIPT_DIR
	MaxToolConcurrency     int      `koanf:"max-tool-concurrency"`  // --max-tool-concurrency: JENNY_MAX_TOOL_CONCURRENCY (0 = default)
	CompactKeepArchive     bool     `koanf:"compact-keep-archive"`  // --compact-keep-archive: JENNY_COMPACT_KEEP_ARCHIVE
	DisableCompact         bool     `koanf:"disable-compact"`       // --disable-compact: JENNY_DISABLE_COMPACT
	DisableAutoCompact     bool     `koanf:"disable-auto-compact"`  // --disable-auto-compact: JENNY_DISABLE_AUTO_COMPACT
	EnableSessionMemory    bool     `koanf:"enable-session-memory"` // --enable-session-memory: JENNY_ENABLE_SESSION_MEMORY
	DisableAutoMemory      bool     `koanf:"disable-auto-memory"`   // --disable-auto-memory: JENNY_DISABLE_AUTO_MEMORY
	RoutesProfile          string   `koanf:"routes-profile"`        // --routes-profile: JENNY_ROUTES_PROFILE: select active routing profile
	RefreshRegistry        bool     `koanf:"refresh-registry"`      // --refresh-registry: synchronously fetch model registry
	Offline                bool     `koanf:"offline"`               // --offline: skip all network fetch, use cache as-is
}

// envTransform transforms environment variable names from JENNY_* format to
// koanf key format (lowercase, underscores to hyphens).
func envTransform(s string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimPrefix(s, "JENNY_")), "_", "-")
}

// Parse parses command-line flags using koanf with layered configuration.
// Configuration precedence (highest to lowest): CLI flags > env vars > JSON config.
// Returns an error if parsing fails or if no prompt is provided.
func Parse() (*Flags, *koanf.Koanf, error) {
	// Create koanf instance with "." delimiter.
	k := koanf.New(".")

	// 1. Load JSON config from .jenny/config.json (lowest precedence).
	// Ignore error if file does not exist.
	_ = k.Load(file.Provider(".jenny/config.json"), json.Parser())

	// 2. Load environment variables prefixed with JENNY_.
	_ = k.Load(env.Provider("JENNY_", ".", envTransform), nil)

	// 3. Define and parse CLI flags using pflag.
	flags := pflag.NewFlagSet(os.Args[0], pflag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.Usage = func() {
		fmt.Fprintf(flags.Output(), "Usage: %s [-p <prompt>] [--model <model>] [--output-format <format>] [-r <session_id>]\n", os.Args[0])
		flags.PrintDefaults()
	}

	// Read defaults from koanf (env/json layers).
	modelDefault := k.String("model")
	outputFormatDefault := k.String("output-format")
	verboseDefault := k.Bool("verbose")
	includePartialDefault := k.Bool("include-partial-messages")
	skipPermsDefault := k.Bool("dangerously-skip-permissions")
	permLevelDefault := k.String("permission-level")
	resumeDefault := k.String("resume")
	noSessionPersistenceDefault := k.Bool("no-session-persistence")
	forkSessionDefault := k.Bool("fork-session")
	continueDefault := k.Bool("continue")
	mcpConfigDefault := k.Strings("mcp-config")
	strictMCPDefault := k.Bool("strict-mcp-config")
	deniedToolsDefault := k.Strings("deny-tool")
	bareDefault := k.Bool("bare")
	swarmsDefault := k.Bool("swarm")
	versionDefault := k.Bool("version")
	pspDefault := k.Bool("print-system-prompt")
	customSysDefault := k.String("system-prompt")
	appendSysDefault := k.String("append-system-prompt")
	prependSysDefault := k.String("prepend-system-prompt")
	maxIterDefault := k.Int("max-iterations")
	maxTurnsDefault := k.Int("max-turns")
	maxBudgetDefault := k.Float64("max-budget-usd")
	effortDefault := k.String("effort")
	thinkingBudgetDefault := k.Int("thinking-budget")
	redactModeDefault := k.String("redact")
	transcriptDirDefault := k.String("transcript-dir")
	maxToolConcurrencyDefault := k.Int("max-tool-concurrency")
	compactKeepArchiveDefault := k.Bool("compact-keep-archive")
	disableCompactDefault := k.Bool("disable-compact")
	disableAutoCompactDefault := k.Bool("disable-auto-compact")
	enableSessionMemoryDefault := k.Bool("enable-session-memory")
	disableAutoMemoryDefault := k.Bool("disable-auto-memory")
	refreshRegistryDefault := k.Bool("refresh-registry")
	offlineDefault := k.Bool("offline")

	// Define flags with defaults from koanf.
	var pFlag []string
	flags.StringArrayVarP(&pFlag, "print", "p", nil, "Prompt to send (can be specified multiple times; values are joined with newlines)")

	var promptFile []string
	flags.StringArrayVar(&promptFile, "prompt-file", nil, "Read prompt from file; use '-' for stdin (can be specified multiple times; values are joined with newlines)")

	var model string
	flags.StringVarP(&model, "model", "", modelDefault, "Model to use")

	var outputFormat string
	flags.StringVarP(&outputFormat, "output-format", "", outputFormatDefault, "Output format (text, stream-json)")

	var verbose bool
	flags.BoolVarP(&verbose, "verbose", "", verboseDefault, "Enable verbose output")

	var includePartial bool
	flags.BoolVarP(&includePartial, "include-partial-messages", "", includePartialDefault, "Include partial messages")

	var skipPerms bool
	flags.BoolVarP(&skipPerms, "dangerously-skip-permissions", "", skipPermsDefault, "Skip permissions")

	var permLevel string
	flags.StringVarP(&permLevel, "permission-level", "", permLevelDefault, "Permission level (read, analyze, edit, execute, unrestricted)")

	var resume string
	flags.StringVarP(&resume, "resume", "r", resumeDefault, "Session ID to resume")

	var noSessionPersistence bool
	flags.BoolVarP(&noSessionPersistence, "no-session-persistence", "", noSessionPersistenceDefault, "Disable session persistence")

	var forkSession bool
	flags.BoolVarP(&forkSession, "fork-session", "", forkSessionDefault, "Fork resumed session to new ID")

	var continueFlag bool
	flags.BoolVarP(&continueFlag, "continue", "", continueDefault, "Resume most recent session")

	var mcpConfig []string
	flags.StringSliceVarP(&mcpConfig, "mcp-config", "", mcpConfigDefault, "MCP configuration file path(s) (can be specified multiple times)")

	var strictMCP bool
	flags.BoolVarP(&strictMCP, "strict-mcp-config", "", strictMCPDefault, "Only load MCP servers from --mcp-config files")

	var deniedTools []string
	flags.StringSliceVarP(&deniedTools, "deny-tool", "", deniedToolsDefault, "Tool name to deny (can be specified multiple times)")

	var bare bool
	flags.BoolVarP(&bare, "bare", "", bareDefault, "Disable skill discovery for minimal environments")

	var swarmsEnabled bool
	flags.BoolVarP(&swarmsEnabled, "swarm", "", swarmsDefault, "Enable swarm mode for named agent delegation")

	var version bool
	flags.BoolVarP(&version, "version", "v", versionDefault, "Print version and exit")

	var psp bool
	flags.BoolVarP(&psp, "print-system-prompt", "", pspDefault, "Print the assembled system prompt and exit")

	var customSys string
	flags.StringVarP(&customSys, "system-prompt", "", customSysDefault, "Replace the default system prompt")

	var appendSys string
	flags.StringVarP(&appendSys, "append-system-prompt", "", appendSysDefault, "Append text after the system prompt")

	var prependSys string
	flags.StringVarP(&prependSys, "prepend-system-prompt", "", prependSysDefault, "Prepend text before the system prompt")

	var maxIter int
	flags.IntVarP(&maxIter, "max-iterations", "", maxIterDefault, "Maximum loop iterations (0 = unlimited)")

	var maxTurns int
	flags.IntVarP(&maxTurns, "max-turns", "", maxTurnsDefault, "Maximum number of turns (0 = unlimited)")

	var maxBudget float64
	flags.Float64VarP(&maxBudget, "max-budget-usd", "", maxBudgetDefault, "Budget limit in USD (0.0 = no limit)")

	var effort string
	flags.StringVarP(&effort, "effort", "", effortDefault, "Reasoning effort level (low, medium, high) for OpenAI o-series and DeepSeek models")

	var thinkingBudget int
	flags.IntVarP(&thinkingBudget, "thinking-budget", "", thinkingBudgetDefault, "Maximum thinking tokens for Anthropic (AC3)")

	var redactMode string
	flags.StringVarP(&redactMode, "redact", "", redactModeDefault, "Secret redaction mode (disabled, redact, recover); JENNY_REDACT env var or .jenny/config.json is used when unset")

	var transcriptDir string
	flags.StringVarP(&transcriptDir, "transcript-dir", "", transcriptDirDefault, "Override transcript directory; JENNY_TRANSCRIPT_DIR env var is used when unset")

	var maxToolConcurrency int
	flags.IntVarP(&maxToolConcurrency, "max-tool-concurrency", "", maxToolConcurrencyDefault, "Max parallel tool executions (0 = default 10); JENNY_MAX_TOOL_CONCURRENCY env var is used when unset")

	var compactKeepArchive bool
	flags.BoolVarP(&compactKeepArchive, "compact-keep-archive", "", compactKeepArchiveDefault, "Keep <id>.tar.gz after resume extraction; JENNY_COMPACT_KEEP_ARCHIVE env var is used when unset")

	var disableCompact bool
	flags.BoolVarP(&disableCompact, "disable-compact", "", disableCompactDefault, "Disable all compaction; JENNY_DISABLE_COMPACT env var is used when unset")

	var disableAutoCompact bool
	flags.BoolVarP(&disableAutoCompact, "disable-auto-compact", "", disableAutoCompactDefault, "Disable auto-compact only; JENNY_DISABLE_AUTO_COMPACT env var is used when unset")

	var enableSessionMemory bool
	flags.BoolVarP(&enableSessionMemory, "enable-session-memory", "", enableSessionMemoryDefault, "Enable session-memory compaction branch; JENNY_ENABLE_SESSION_MEMORY env var is used when unset")

	var disableAutoMemory bool
	flags.BoolVarP(&disableAutoMemory, "disable-auto-memory", "", disableAutoMemoryDefault, "Disable auto-memory directory; JENNY_DISABLE_AUTO_MEMORY env var is used when unset")

	var refreshRegistry bool
	flags.BoolVarP(&refreshRegistry, "refresh-registry", "", refreshRegistryDefault, "Synchronously fetch the latest model registry")

	var offline bool
	flags.BoolVarP(&offline, "offline", "", offlineDefault, "Skip all network fetch, use cached data as-is")

	// Parse the flags.
	if err := flags.Parse(os.Args[1:]); err != nil {
		if err == pflag.ErrHelp {
			// pflag already invoked flags.Usage() before returning ErrHelp.
			os.Exit(0)
		}
		return nil, nil, err
	}

	// Capture which flags were explicitly set on the command line.
	// Needed for validation where "set to empty" differs from "not set".
	permLevelChanged := flags.Changed("permission-level")
	includePartialChanged := flags.Changed("include-partial-messages")
	refreshRegistryChanged := flags.Changed("refresh-registry")
	offlineChanged := flags.Changed("offline")

	// Load CLI flags into koanf (highest precedence).
	_ = k.Load(posflag.Provider(flags, ".", k), nil)

	// Get remaining non-flag arguments as positional prompt.
	args := flags.Args()

	// Unmarshal into Flags struct.
	var parsed Flags
	if err := k.Unmarshal("", &parsed); err != nil {
		return nil, nil, fmt.Errorf("unmarshalling flags: %w", err)
	}

	// Handle special fields that can't be unmarshalled directly:
	// - p: stored as []string in koanf, but struct expects string (join with newline).
	parsed.Prompt = strings.Join(k.Strings("print"), "\n")

	// --version / --print-system-prompt: caller will print and exit before any
	// session or API initialisation, so a prompt is not required.
	if parsed.Version || parsed.PrintSystemPrompt {
		return &parsed, k, nil
	}

	// --prompt-file: read prompt content from file(s); "-" means stdin.
	// Only used when -p/--print is empty (p flag has precedence).
	if parsed.Prompt == "" && len(promptFile) > 0 {
		var parts []string
		for _, path := range promptFile {
			content, err := readPromptFile(path)
			if err != nil {
				return nil, nil, err
			}
			parts = append(parts, content)
		}
		parsed.Prompt = strings.Join(parts, "\n")
	}
	parsed.PromptFile = promptFile

	// Fallback: if -p/--prompt-file is empty but there are positional args, use them as the prompt.
	if parsed.Prompt == "" && len(args) > 0 {
		parsed.Prompt = strings.Join(args, " ")
	}

	// Validate: require a prompt.
	if parsed.Prompt == "" {
		flags.Usage()
		return nil, nil, fmt.Errorf("no prompt provided")
	}

	// Validate: --fork-session requires -r/--resume.
	if parsed.ForkSession && parsed.SessionResume == "" {
		return nil, nil, fmt.Errorf("--fork-session requires -r/--resume")
	}

	// Validate: --continue is mutually exclusive with -r/--resume.
	if parsed.Continue && parsed.SessionResume != "" {
		return nil, nil, fmt.Errorf("--continue is mutually exclusive with -r/--resume")
	}

	// Validate: --continue requires session persistence.
	if parsed.Continue && parsed.NoSessionPersistence {
		return nil, nil, fmt.Errorf("--continue requires session persistence")
	}

	// Validate: --refresh-registry and --offline are mutually exclusive.
	if refreshRegistryChanged && offlineChanged && parsed.RefreshRegistry && parsed.Offline {
		return nil, nil, fmt.Errorf("--refresh-registry and --offline are mutually exclusive")
	}

	// Validate: --include-partial-messages requires --output-format stream-json.
	if includePartialChanged && parsed.OutputFormat != "stream-json" {
		return nil, nil, fmt.Errorf("--include-partial-messages requires --output-format stream-json")
	}

	// Validate: --permission-level must be a valid value if provided.
	if permLevelChanged {
		validLevels := []string{"read", "analyze", "edit", "execute", "unrestricted"}
		found := false
		for _, l := range validLevels {
			if l == parsed.PermissionLevel {
				found = true
				break
			}
		}
		if !found {
			return nil, nil, fmt.Errorf("invalid --permission-level %q; valid values: %s", parsed.PermissionLevel, strings.Join(validLevels, ", "))
		}
	}

	// Validate: --redact must be a valid value if provided.
	if parsed.RedactMode != "" {
		validModes := []string{"disabled", "redact", "recover"}
		found := false
		for _, m := range validModes {
			if m == parsed.RedactMode {
				found = true
				break
			}
		}
		if !found {
			return nil, nil, fmt.Errorf("invalid --redact %q; valid values: %s", parsed.RedactMode, strings.Join(validModes, ", "))
		}
	}

	return &parsed, k, nil
}

// readPromptFile reads prompt content from a file path.
// A path of "-" reads from stdin.
func readPromptFile(path string) (string, error) {
	if path == "-" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("failed to read prompt from stdin: %w", err)
		}
		return string(data), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("prompt file not found: %s", path)
	}
	return string(data), nil
}

// StreamMessage represents a message in the stream-json output.
// Field order matches the headless-agent reference format: type, then payload,
// then session_id, parent_tool_use_id, uuid, then remaining fields.
type StreamMessage struct {
	Type                    string            `json:"type"`
	Subtype                 string            `json:"subtype,omitempty"`
	Content                 string            `json:"content,omitempty"`
	SessionID               string            `json:"session_id,omitempty"`
	ParentToolUseID         *string           `json:"parent_tool_use_id"`
	Uuid                    string            `json:"uuid,omitempty"`
	Result                  string            `json:"result,omitempty"`
	Model                   string            `json:"model,omitempty"`
	CWD                     string            `json:"cwd,omitempty"`
	Tools                   []string          `json:"tools,omitempty"`
	ToolName                string            `json:"tool_name,omitempty"`
	ToolInput               any               `json:"input,omitempty"`
	IsError                 bool              `json:"is_error,omitempty"`
	IsPartial               bool              `json:"is_partial,omitempty"`
	ClaudeCodeVersion       string            `json:"claude_code_version,omitempty"`
	PermissionMode          string            `json:"permissionMode,omitempty"`
	FastModeState           string            `json:"fast_mode_state,omitempty"`
	OutputStyle             string            `json:"output_style,omitempty"`
	MCPServers              []string          `json:"mcp_servers,omitempty"`
	AnalyticsDisabled       bool              `json:"analytics_disabled,omitempty"`
	ProductFeedbackDisabled bool              `json:"product_feedback_disabled,omitempty"`
	Agents                  []string          `json:"agents,omitempty"`
	SlashCommands           []string          `json:"slash_commands,omitempty"`
	Plugins                 []PluginInitInfo  `json:"plugins,omitempty"`
	Skills                  []string          `json:"skills,omitempty"`
	MemoryPaths             map[string]string `json:"memory_paths,omitempty"`
	APIKeySource            string            `json:"apiKeySource,omitempty"`
}

// PluginInitInfo holds summary information about a loaded plugin for the init event.
type PluginInitInfo struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Source string `json:"source"`
}

// MarshalJSON implements custom marshaling for StreamMessage to:
// - Maintain correct field ordering per reference format
func (s StreamMessage) MarshalJSON() ([]byte, error) {
	var fields []string
	fields = append(fields, `"type":`+encodeString(s.Type))
	if s.Subtype != "" {
		fields = append(fields, `"subtype":`+encodeString(s.Subtype))
	}
	if s.Content != "" {
		fields = append(fields, `"content":`+encodeString(s.Content))
	}
	if s.SessionID != "" {
		fields = append(fields, `"session_id":`+encodeString(s.SessionID))
	}
	if s.ParentToolUseID != nil {
		fields = append(fields, `"parent_tool_use_id":`+encodeString(*s.ParentToolUseID))
	}
	if s.Uuid != "" {
		fields = append(fields, `"uuid":`+encodeString(s.Uuid))
	}
	if s.Result != "" {
		fields = append(fields, `"result":`+encodeString(s.Result))
	}
	if s.Model != "" {
		fields = append(fields, `"model":`+encodeString(s.Model))
	}
	if s.CWD != "" {
		fields = append(fields, `"cwd":`+encodeString(s.CWD))
	}
	if len(s.Tools) > 0 {
		toolsBytes, _ := stdjson.Marshal(s.Tools)
		fields = append(fields, `"tools":`+string(toolsBytes))
	}
	if s.ToolName != "" {
		fields = append(fields, `"tool_name":`+encodeString(s.ToolName))
	}
	if s.ToolInput != nil {
		inputBytes, _ := stdjson.Marshal(s.ToolInput)
		fields = append(fields, `"input":`+string(inputBytes))
	}
	if s.IsError {
		fields = append(fields, `"is_error":true`)
	}
	if s.IsPartial {
		fields = append(fields, `"is_partial":true`)
	}
	if s.ClaudeCodeVersion != "" {
		fields = append(fields, `"claude_code_version":`+encodeString(s.ClaudeCodeVersion))
	}
	if s.PermissionMode != "" {
		fields = append(fields, `"permissionMode":`+encodeString(s.PermissionMode))
	}
	if s.FastModeState != "" {
		fields = append(fields, `"fast_mode_state":`+encodeString(s.FastModeState))
	}
	if s.OutputStyle != "" {
		fields = append(fields, `"output_style":`+encodeString(s.OutputStyle))
	}
	// Always emit mcp_servers as array (even if empty) for init events compatibility
	if s.MCPServers != nil {
		mcpBytes, _ := stdjson.Marshal(s.MCPServers)
		fields = append(fields, `"mcp_servers":`+string(mcpBytes))
	} else {
		fields = append(fields, `"mcp_servers":[]`)
	}
	if s.AnalyticsDisabled {
		fields = append(fields, `"analytics_disabled":true`)
	}
	if s.ProductFeedbackDisabled {
		fields = append(fields, `"product_feedback_disabled":true`)
	}
	if len(s.Agents) > 0 {
		agentsBytes, _ := stdjson.Marshal(s.Agents)
		fields = append(fields, `"agents":`+string(agentsBytes))
	}
	if len(s.SlashCommands) > 0 {
		scBytes, _ := stdjson.Marshal(s.SlashCommands)
		fields = append(fields, `"slash_commands":`+string(scBytes))
	}
	if len(s.Plugins) > 0 {
		pluginsBytes, _ := stdjson.Marshal(s.Plugins)
		fields = append(fields, `"plugins":`+string(pluginsBytes))
	}
	if len(s.Skills) > 0 {
		skillsBytes, _ := stdjson.Marshal(s.Skills)
		fields = append(fields, `"skills":`+string(skillsBytes))
	}
	if len(s.MemoryPaths) > 0 {
		mpBytes, _ := stdjson.Marshal(s.MemoryPaths)
		fields = append(fields, `"memory_paths":`+string(mpBytes))
	}
	if s.APIKeySource != "" {
		fields = append(fields, `"apiKeySource":`+encodeString(s.APIKeySource))
	}

	return []byte("{" + strings.Join(fields, ",") + "}"), nil
}

func encodeString(s string) string {
	b, _ := stdjson.Marshal(s)
	return string(b)
}

// WriteStreamJSON writes a message as NDJSON line to stdout.
func WriteStreamJSON(msg StreamMessage) error {
	data, err := stdjson.Marshal(msg)
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout, string(data))
	return nil
}

// WriteStreamJSONRaw writes raw JSON as NDJSON line to stdout.
func WriteStreamJSONRaw(data []byte) error {
	fmt.Fprintln(os.Stdout, string(data))
	return nil
}
