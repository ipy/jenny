// Package cli provides command-line interface support for jenny.
package cli

import (
	stdjson "encoding/json"
	"fmt"
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
	Prompt                 string            // prompt: joined from k.Strings("print") after unmarshal
	Model                  string            `koanf:"model"`
	OutputFormat           string            `koanf:"output-format"`
	Verbose                bool              `koanf:"verbose"`
	IncludePartialMessages bool              `koanf:"include-partial-messages"`
	SkipPermissions        bool              `koanf:"dangerously-skip-permissions"`
	PermissionLevel        string            `koanf:"permission-level"`
	SessionResume          string            `koanf:"resume"`
	NoSessionPersistence   bool              `koanf:"no-session-persistence"`
	ForkSession            bool              `koanf:"fork-session"`
	Continue               bool              `koanf:"continue"`
	MCPConfig              []string          `koanf:"mcp-config"`
	StrictMCP              bool              `koanf:"strict-mcp-config"`
	DeniedTools            []string          `koanf:"deny-tool"`
	Bare                   bool              `koanf:"bare"`
	SwarmsEnabled          bool              `koanf:"swarm"`                // When true, enables named agent delegation (swarm mode)
	Version                bool              `koanf:"version"`              // --version / -v: print version and exit
	PrintSystemPrompt      bool              `koanf:"print-system-prompt"`  // --print-system-prompt: print the assembled system prompt and exit
	CustomSystemPrompt     string            `koanf:"system-prompt"`        // --system-prompt: replaces default system prompt entirely
	AppendSystemPrompt     string            `koanf:"append-system-prompt"` // --append-system-prompt: appended after assembled system prompt
	MaxIterations          int               `koanf:"max-iterations"`       // --max-iterations: maximum loop iterations (0 = unlimited)
	MaxTurns               int               `koanf:"max-turns"`            // --max-turns: maximum number of turns (0 = unlimited)
	MaxBudgetUsd           float64           `koanf:"max-budget-usd"`       // --max-budget-usd: budget limit in USD (0.0 = no limit)
	Effort                 string            `koanf:"effort"`               // --effort: reasoning effort level (low, medium, high)
	ThinkingBudget         int               `koanf:"thinking-budget"`      // --thinking-budget: maximum thinking tokens for Anthropic (AC3)
	FeatureFlags           map[string]string // feature flags: set from ffv.m after unmarshal
}

// envTransform transforms environment variable names from JENNY_* format to
// koanf key format (lowercase, underscores to hyphens).
func envTransform(s string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimPrefix(s, "JENNY_")), "_", "-")
}

// Parse parses command-line flags using koanf with layered configuration.
// Configuration precedence (highest to lowest): CLI flags > env vars > JSON config.
// Returns an error if parsing fails or if no prompt is provided.
func Parse() (*Flags, error) {
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
	maxIterDefault := k.Int("max-iterations")
	maxTurnsDefault := k.Int("max-turns")
	maxBudgetDefault := k.Float64("max-budget-usd")
	effortDefault := k.String("effort")
	thinkingBudgetDefault := k.Int("thinking-budget")

	// Define flags with defaults from koanf.
	var pFlag []string
	flags.StringSliceVarP(&pFlag, "print", "p", nil, "Prompt to send (can be specified multiple times; values are joined with newlines)")

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

	// Feature flags as key=value pairs. Seed from env/json layer.
	featureFlags := make(map[string]string)
	for k, v := range k.All() {
		if strings.HasPrefix(k, "feature-flags.") {
			ffKey := strings.TrimPrefix(k, "feature-flags.")
			if sv, ok := v.(string); ok {
				featureFlags[ffKey] = sv
			}
		}
	}
	ffv := newFeatureFlagValue(featureFlags)
	flags.Var(ffv, "feature-flags", "Feature flags in key=value format (can be specified multiple times)")
	flags.Var(ffv, "ff", "Feature flags in key=value format (alias for --feature-flags)")

	// Parse the flags.
	if err := flags.Parse(os.Args[1:]); err != nil {
		if err == pflag.ErrHelp {
			// pflag already invoked flags.Usage() before returning ErrHelp.
			os.Exit(0)
		}
		return nil, err
	}

	// Load CLI flags into koanf (highest precedence).
	_ = k.Load(posflag.Provider(flags, ".", k), nil)

	// Get remaining non-flag arguments as positional prompt.
	args := flags.Args()

	// Unmarshal into Flags struct.
	var parsed Flags
	if err := k.Unmarshal("", &parsed); err != nil {
		return nil, fmt.Errorf("unmarshalling flags: %w", err)
	}

	// Handle special fields that can't be unmarshalled directly:
	// - p: stored as []string in koanf, but struct expects string (join with newline).
	// - feature-flags: stored as string in koanf (via Var.String()), but struct expects map.
	parsed.Prompt = strings.Join(k.Strings("print"), "\n")
	parsed.FeatureFlags = ffv.m

	// --version / --print-system-prompt: caller will print and exit before any
	// session or API initialisation, so a prompt is not required.
	if parsed.Version || parsed.PrintSystemPrompt {
		return &parsed, nil
	}

	// Fallback: if -p is empty but there are positional args, use them as the prompt.
	if parsed.Prompt == "" && len(args) > 0 {
		parsed.Prompt = strings.Join(args, " ")
	}

	// Validate: require a prompt.
	if parsed.Prompt == "" {
		flags.Usage()
		return nil, fmt.Errorf("no prompt provided")
	}

	// Validate: --fork-session requires -r/--resume.
	if parsed.ForkSession && parsed.SessionResume == "" {
		return nil, fmt.Errorf("--fork-session requires -r/--resume")
	}

	// Validate: --continue is mutually exclusive with -r/--resume.
	if parsed.Continue && parsed.SessionResume != "" {
		return nil, fmt.Errorf("--continue is mutually exclusive with -r/--resume")
	}

	// Validate: --continue requires session persistence.
	if parsed.Continue && parsed.NoSessionPersistence {
		return nil, fmt.Errorf("--continue requires session persistence")
	}

	// Validate: --permission-level must be a valid value if provided.
	if parsed.PermissionLevel != "" {
		validLevels := []string{"read", "analyze", "edit", "execute", "unrestricted"}
		found := false
		for _, l := range validLevels {
			if l == parsed.PermissionLevel {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("invalid --permission-level %q; valid values: %s", parsed.PermissionLevel, strings.Join(validLevels, ", "))
		}
	}

	return &parsed, nil
}

// featureFlagValue implements pflag.Value for key=value feature flags.
type featureFlagValue struct {
	m map[string]string
}

func newFeatureFlagValue(m map[string]string) *featureFlagValue {
	return &featureFlagValue{m: m}
}

func (f *featureFlagValue) Set(val string) error {
	parts := strings.SplitN(val, "=", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid feature flag format %q; expected key=value", val)
	}
	f.m[parts[0]] = parts[1]
	return nil
}

func (f *featureFlagValue) String() string {
	if f.m == nil {
		return ""
	}
	var pairs []string
	for k, v := range f.m {
		pairs = append(pairs, fmt.Sprintf("%s=%s", k, v))
	}
	return strings.Join(pairs, ",")
}

func (f *featureFlagValue) Type() string {
	return "feature-flags"
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
