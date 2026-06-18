// Package harness provides declarative blackbox test infrastructure for e2e tests.
// It manages the jenny binary lifecycle, mock API server, and declarative assertions.
package harness

// No external imports needed — all types are defined here or aliased from sibling files.

// Config controls how the SuiteRunner operates.
type Config struct {
	ProductName string // "jenny" or "claude" (used in error messages)
	Target      string // absolute or relative path to binary under test
	CassetteDir string // directory containing .sse cassette files
	TimeoutMs   int64  // per-run timeout in milliseconds; 0 means no timeout
	TempDir     string // temp directory prefix for test isolation
}

// TargetInvocation describes a single execution of the binary.
type TargetInvocation struct {
	// Kind is "cli" or "prompt".
	// "cli": args are passed directly to the binary.
	// "prompt": the harness synthesizes CLI args from Prompt/Format/Cassette fields.
	Kind string

	// For Kind="prompt":
	Prompt   string // user prompt passed via -p flag
	Format   string // --output-format value (e.g. "stream-json", "text")
	Cassette string // cassette file name without extension (e.g. "echo-hello")

	// For multi-turn (tool use): list of cassette IDs in order.
	// When set, the mock server serves cassettes[0] for the first request,
	// cassettes[1] for the second, etc.
	CassetteSequence []string

	// Additional CLI args appended after the synthesized args.
	Args []string

	// Env is a list of "KEY=value" environment variable overrides.
	// Recognized substitution macros:
	//   ${WORK_DIR}  — replaced with the work directory path
	//   ${MOCK_URL}  — replaced with the mock server base URL
	// If unset, the harness sets ANTHROPIC_BASE_URL to the mock server URL
	// and ANTHROPIC_AUTH_TOKEN to "test-token".
	Env []string

	// WorkDirFiles is a map of relative file paths to contents.
	// Each file is written to the work directory before the binary is spawned.
	// Use this to provision fixture files for tests.
	WorkDirFiles map[string]string

	// MockBehavior overrides mock server behavior for this invocation.
	MockBehavior *MockBehavior

	// TimeoutMs overrides the per-run timeout for this invocation only.
	// When > 0, this value takes precedence over Config.TimeoutMs.
	// A value of 0 means use Config.TimeoutMs (or the default 60000ms).
	TimeoutMs int64
}

// TestCase is a declarative end-to-end test case.
type TestCase struct {
	ID          string
	Category    string
	Description string
	Tags        []string

	// Skip marks the test as skipped with a reason.
	Skip *SkipInfo

	// Target describes how to invoke the binary.
	Target TargetInvocation

	// Expected describes the expected behavior.
	Expected ExpectedBehavior
}

// SkipInfo describes why a test should be skipped.
type SkipInfo struct {
	Reason string
}

// ExpectedBehavior encodes all assertions about a test run.
// Each field is checked independently; all must pass.
type ExpectedBehavior struct {
	// ExitCode asserts the process exit code.
	ExitCode int

	// Stdout asserts stdout content.
	Stdout *StdoutExpectation

	// Stderr asserts stderr content.
	Stderr *StderrExpectation

	// StreamJSON asserts NDJSON output (requires --output-format stream-json).
	StreamJSON *StreamJSONExpectation

	// APIRequests asserts on captured HTTP requests made to the mock API.
	// Each entry corresponds to one POST /cassette/<id>/v1/messages call.
	APIRequests []APIRequestExpectation

	// FileSystem asserts on the work directory contents after the run.
	FileSystem []FileSystemExpectation
}

// StdoutExpectation describes assertions on raw stdout text.
type StdoutExpectation struct {
	// Equals asserts exact match.
	Equals string

	// IsEmpty asserts stdout is empty.
	IsEmpty bool

	// Contains asserts that every string appears somewhere in stdout.
	Contains []string

	// NotContains asserts that no string appears in stdout.
	NotContains []string

	// Matches asserts that every pattern (regexp) matches at least one line.
	Matches []string

	// Length asserts on the character length of stdout.
	Length *LengthExpectation
}

// StderrExpectation describes assertions on raw stderr text.
// It is an alias for StdoutExpectation; both share the same assertion fields.
type StderrExpectation = StdoutExpectation

// LengthExpectation describes a lower/upper bound on a length.
type LengthExpectation struct {
	Min   int // inclusive lower bound; 0 means no minimum
	Max   int // inclusive upper bound; 0 means no maximum
	Exact int // exact length; 0 means no exact check
}

// StreamJSONExpectation describes assertions on parsed NDJSON output.
// Each event is a JSON object with at least a "type" field.
type StreamJSONExpectation struct {
	// AllLinesValidJSON asserts every stdout line parses as JSON.
	AllLinesValidJSON bool

	// SessionIDConsistent asserts session_id is identical across all events.
	SessionIDConsistent bool

	// UUIDsUnique asserts every event has a unique uuid.
	UUIDsUnique bool

	// EventCount asserts on the number of events.
	EventCount *EventCountExpectation

	// FirstEvent asserts on the first event (index 0).
	FirstEvent *EventExpectation

	// LastEvent asserts on the last event.
	LastEvent *EventExpectation

	// HasEventTypes asserts the output contains at least one event of each listed type.
	HasEventTypes []string

	// EventAssertions run per-event assertions. Index -1 means last matching event.
	EventAssertions []IndexedEventExpectation

	// CompareToReference runs the same binary with REFERENCE_BIN set and compares output.
	// Reference differences are logged but do not fail the test (informational).
	CompareToReference bool
}

// EventCountExpectation describes bounds on event count.
type EventCountExpectation struct {
	Min int // inclusive lower bound; 0 means no minimum
	Max int // inclusive upper bound; 0 means no maximum
}

// EventExpectation describes assertions on a single NDJSON event.
type EventExpectation struct {
	Type string // event type field (e.g. "system", "assistant", "result")

	// Subtype e.g. "init", "success", "error".
	Subtype string

	// HasFields asserts the event has these top-level keys.
	HasFields []string

	// FieldNotEmpty asserts the named fields exist and are non-empty.
	FieldNotEmpty []string

	// FieldEquals asserts exact field values.
	FieldEquals map[string]any

	// FieldContains asserts a field contains a substring.
	FieldContains map[string]string

	// Nested asserts on nested objects. Key is the JSON path (e.g. "usage", "message.content").
	Nested map[string]*EventExpectation
}

// IndexedEventExpectation runs assertions on the event at a specific index.
// Index -1 means the last event matching TypeFilter.
type IndexedEventExpectation struct {
	Index         int    // event index; -1 means last matching event
	TypeFilter    string // only consider events with this type; "" means all types
	SubtypeFilter string
	Expect        EventExpectation
}

// APIRequestExpectation describes assertions on a single captured HTTP request.
type APIRequestExpectation struct {
	Index int // request index; 0 means first POST

	// Model asserts the model field matches this pattern (regex).
	Model string

	// MaxTokens asserts max_tokens field equals this value.
	MaxTokens int

	// HasSystemPrompt asserts the request has a non-empty system field.
	HasSystemPrompt bool

	// System asserts on the system prompt content.
	System *SystemExpectation

	// HasField asserts the request body has these keys.
	HasField []string

	// FieldEquals asserts exact field values.
	FieldEquals map[string]any

	// Tools asserts on the tools array in the request.
	Tools *ToolsExpectation
}

// SystemExpectation describes assertions on the system prompt.
type SystemExpectation struct {
	Contains    []string
	NotContains []string
}

// ToolsExpectation describes assertions on a tools array in an API request.
type ToolsExpectation struct {
	// MinCount asserts the tools array has at least this many elements.
	MinCount int

	// HasTool asserts each named tool is present in the array.
	HasTool []string

	// NotHasTool asserts these tools are NOT present.
	NotHasTool []string

	// EachHasFields asserts every tool has these keys.
	EachHasFields []string
}

// FileSystemExpectation describes assertions on the work directory after a run.
type FileSystemExpectation struct {
	// Path is an exact path (relative to workDir).
	Path string

	// Pattern is a glob pattern relative to the work directory.
	// Supports ** for recursive matching.
	Pattern string

	// ExpectedCount asserts the number of files matching Pattern.
	// Use with MustNotExist=false.
	// 0 with MustNotExist=true asserts no file matches.
	ExpectedCount int

	// MustNotExist asserts no file matches.
	MustNotExist bool

	// Content asserts the file content equals this exact string.
	Content string

	// JSONL asserts on JSONL file contents. Requires ExpectedCount >= 1.
	JSONL *JSONLExpectation
}

// JSONLExpectation describes assertions on a JSONL (JSON Lines) file.
type JSONLExpectation struct {
	// AllLinesValidJSON asserts every line parses as JSON.
	AllLinesValidJSON bool

	// RequiredFields asserts each line has these top-level keys.
	RequiredFields []string

	// AllLinesHaveUUID asserts every line has a "uuid" field with a non-empty value.
	AllLinesHaveUUID bool

	// HasTypes asserts the JSONL contains lines with these event types.
	HasTypes []string

	// MinCount asserts at least this many lines.
	MinCount int

	// MaxCount asserts at most this many lines.
	MaxCount int

	// SessionIDMatchesStdout asserts the stem of the transcript file matches
	// the session_id from stdout events. This requires passing a CapturedOutput
	// to Compare() rather than running through SuiteRunner.
	SessionIDMatchesStdout bool
}

// CapturedOutput holds output from a single run, used for imperative comparisons.
type CapturedOutput struct {
	ExitCode int
	Stdout   string
	Stderr   string
	Requests []RecordedRequest
}

// RecordedRequest represents a captured HTTP request for comparison.
type RecordedRequest struct {
	Body map[string]any
}

// TestResult describes the outcome of a test case execution.
type TestResult struct {
	ID            string
	Category      string
	Status        string // "pass", "fail", "error", "skip"
	Message       string
	Duration      int64 // milliseconds
	SkipReason    string
	Diff          []DiffDetail
	Actual        *CapturedOutput
	ReferenceDiff []string // differences vs reference binary (informational)
}

// DiffDetail describes a single failed assertion.
type DiffDetail struct {
	Path     string // path to the field (e.g. "exitCode", "stdout")
	Expected any
	Actual   any
	Message  string
}

// Diff is an alias for DiffDetail for compatibility.
type Diff = DiffDetail

// E2ETB is a testing.TB wrapper used by imperative test helpers.
type E2ETB interface {
	Helper()
	Logf(format string, args ...any)
	Errorf(format string, args ...any)
	Fatalf(format string, args ...any)
	Skipf(format string, args ...any)
	TempDir() string
}

// T wraps *testing.T with helper methods used by the harness.
type T interface {
	Helper()
	Logf(format string, args ...any)
	Errorf(format string, args ...any)
	Fatalf(format string, args ...any)
	TempDir() string
}
