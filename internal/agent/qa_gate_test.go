package agent

import (
	"encoding/json"
	"testing"

	"github.com/ipy/jenny/internal/api"
)

func TestShouldTriggerQA_ForkChild(t *testing.T) {
	e := &QueryEngine{
		streamCfg:       &StreamConfig{Enabled: true, IsForkChild: true},
		maxTokensCtrl:   NewMaxTokensController("test-model"),
		progressMonitor: NewProgressMonitor(),
		turnCount:       25,
	}
	if e.shouldTriggerQA() {
		t.Error("should not trigger QA inside fork child")
	}
}

func TestShouldTriggerQA_StreamDisabled(t *testing.T) {
	e := &QueryEngine{
		streamCfg:       &StreamConfig{Enabled: false},
		maxTokensCtrl:   NewMaxTokensController("test-model"),
		progressMonitor: NewProgressMonitor(),
		turnCount:       25,
	}
	if e.shouldTriggerQA() {
		t.Error("should not trigger QA when stream mode is disabled")
	}
}

func TestShouldTriggerQA_RetriesExhausted(t *testing.T) {
	e := &QueryEngine{
		streamCfg:       &StreamConfig{Enabled: true},
		maxTokensCtrl:   NewMaxTokensController("test-model"),
		progressMonitor: NewProgressMonitor(),
		qaRetryCount:    qaMaxRetries,
		turnCount:       25,
	}
	if e.shouldTriggerQA() {
		t.Error("should not trigger QA when retries are exhausted")
	}
}

func TestShouldTriggerQA_ShortSession(t *testing.T) {
	e := &QueryEngine{
		streamCfg:       &StreamConfig{Enabled: true},
		maxTokensCtrl:   NewMaxTokensController("test-model"),
		progressMonitor: NewProgressMonitor(),
		turnCount:       10,
	}
	if e.shouldTriggerQA() {
		t.Error("should not trigger QA for short sessions (<20 turns)")
	}
}

func TestShouldTriggerQA_ManyTurns(t *testing.T) {
	e := &QueryEngine{
		streamCfg:       &StreamConfig{Enabled: true},
		maxTokensCtrl:   NewMaxTokensController("test-model"),
		progressMonitor: NewProgressMonitor(),
		turnCount:       25,
	}
	if !e.shouldTriggerQA() {
		t.Error("should trigger QA for sessions with >=20 turns")
	}
}

func TestShouldTriggerQA_Disabled(t *testing.T) {
	e := &QueryEngine{
		streamCfg:       &StreamConfig{Enabled: true, DisableQAGate: true},
		maxTokensCtrl:   NewMaxTokensController("test-model"),
		progressMonitor: NewProgressMonitor(),
		turnCount:       25,
	}
	if e.shouldTriggerQA() {
		t.Error("should not trigger QA when DisableQAGate is true")
	}
}

func TestShouldTriggerQA_ResumeSession(t *testing.T) {
	e := &QueryEngine{
		streamCfg:       &StreamConfig{Enabled: true, IsResume: true},
		maxTokensCtrl:   NewMaxTokensController("test-model"),
		progressMonitor: NewProgressMonitor(),
		turnCount:       25,
	}
	if e.shouldTriggerQA() {
		t.Error("should not trigger QA for resumed sessions")
	}
}

func TestParseQAVerdict_Pass(t *testing.T) {
	input := `{"verdict":"PASS","reasoning":"All criteria met","gaps":[],"suggestions":[]}`
	v, err := parseQAVerdict(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.Verdict != "PASS" {
		t.Errorf("verdict = %q, want PASS", v.Verdict)
	}
	if v.Reasoning != "All criteria met" {
		t.Errorf("reasoning = %q", v.Reasoning)
	}
	if len(v.Gaps) != 0 {
		t.Errorf("gaps = %v, want empty", v.Gaps)
	}
}

func TestParseQAVerdict_Fail(t *testing.T) {
	input := `{"verdict":"FAIL","reasoning":"Missing test coverage","gaps":["No unit tests"],"suggestions":["Add unit tests for the new function"]}`
	v, err := parseQAVerdict(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.Verdict != "FAIL" {
		t.Errorf("verdict = %q, want FAIL", v.Verdict)
	}
	if len(v.Gaps) != 1 || v.Gaps[0] != "No unit tests" {
		t.Errorf("gaps = %v", v.Gaps)
	}
	if len(v.Suggestions) != 1 {
		t.Errorf("suggestions = %v", v.Suggestions)
	}
}

func TestParseQAVerdict_WrappedInText(t *testing.T) {
	input := `Here is my analysis: {"verdict":"PASS","reasoning":"ok","gaps":[],"suggestions":[]} done.`
	v, err := parseQAVerdict(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.Verdict != "PASS" {
		t.Errorf("verdict = %q, want PASS", v.Verdict)
	}
}

func TestParseQAVerdict_InvalidJSON(t *testing.T) {
	_, err := parseQAVerdict("not json at all")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParseQAVerdict_InvalidVerdict(t *testing.T) {
	input := `{"verdict":"MAYBE","reasoning":"unclear","gaps":[],"suggestions":[]}`
	_, err := parseQAVerdict(input)
	if err == nil {
		t.Error("expected error for invalid verdict value")
	}
}

func TestBuildQAFeedbackMessage(t *testing.T) {
	v := &qaVerdict{
		Verdict:     "FAIL",
		Reasoning:   "Missing tests",
		Gaps:        []string{"No unit tests", "No integration tests"},
		Suggestions: []string{"Add unit tests", "Add integration tests"},
	}
	msg := buildQAFeedbackMessage(v, 1, qaMaxRetries)
	if msg.Role != api.RoleUser {
		t.Errorf("role = %q, want user", msg.Role)
	}
	if !msg.IsVirtual {
		t.Error("message should be virtual")
	}
	if len(msg.Content) == 0 {
		t.Error("content should not be empty")
	}
	for _, gap := range v.Gaps {
		if !qaContains(msg.Content, gap) {
			t.Errorf("content should contain gap %q", gap)
		}
	}
}

func TestBuildQAVerificationPrompt(t *testing.T) {
	userRequest := "Add a login page with email validation and error handling"
	fileSummary := "Modified: main.go, main_test.go"
	prompt := buildQAVerificationPrompt(userRequest, "/tmp/work", fileSummary)
	if len(prompt) == 0 {
		t.Error("prompt should not be empty")
	}
	if !qaContains(prompt, userRequest) {
		t.Error("prompt should contain user request")
	}
	if !qaContains(prompt, "/tmp/work") {
		t.Error("prompt should contain working directory")
	}
	if !qaContains(prompt, "as few turns as possible") {
		t.Error("prompt should instruct minimal turns")
	}
}

func TestExtractUserRequest(t *testing.T) {
	messages := []api.Message{
		{Role: api.RoleUser, Content: "Build a REST API", IsVirtual: false},
		{Role: api.RoleAssistant, Content: "Done"},
		{Role: api.RoleUser, Content: "<system-reminder>progress check</system-reminder>", IsVirtual: true},
	}
	got := extractUserRequest(messages)
	if got != "Build a REST API" {
		t.Errorf("expected user request, got %q", got)
	}
}

func TestExtractUserRequest_Empty(t *testing.T) {
	messages := []api.Message{
		{Role: api.RoleUser, Content: "<system-reminder>virtual</system-reminder>", IsVirtual: true},
	}
	got := extractUserRequest(messages)
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestBuildFileSummary(t *testing.T) {
	messages := []api.Message{
		{Role: api.RoleUser, Content: "Write a function"},
		{Role: api.RoleAssistant, Content: "Done", ToolUse: []api.ToolUseBlock{
			{Name: "Write", Input: map[string]any{"file_path": "/tmp/main.go"}},
			{Name: "Edit", Input: map[string]any{"file_path": "/tmp/util.go"}},
			{Name: "Read", Input: map[string]any{"file_path": "/tmp/other.go"}},
			{Name: "Bash", Input: map[string]any{"command": "go test ./..."}},
		}},
	}
	summary := buildFileSummary(messages)
	if !qaContains(summary, "/tmp/main.go") {
		t.Error("summary should contain written file")
	}
	if !qaContains(summary, "/tmp/util.go") {
		t.Error("summary should contain edited file")
	}
	if !qaContains(summary, "go test") {
		t.Error("summary should contain bash commands")
	}
}

func TestBuildFileSummary_Empty(t *testing.T) {
	messages := []api.Message{
		{Role: api.RoleUser, Content: "Hello"},
		{Role: api.RoleAssistant, Content: "Hi there"},
	}
	summary := buildFileSummary(messages)
	if summary != "" {
		t.Errorf("expected empty summary for no tool use, got %q", summary)
	}
}

func TestQAEvalEventJSON(t *testing.T) {
	v := &qaVerdict{
		Verdict:   "PASS",
		Reasoning: "All good",
	}
	j := qaEvalResultJSON(v, 0, qaMaxRetries)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(j), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if parsed["verdict"] != "PASS" {
		t.Errorf("verdict = %v", parsed["verdict"])
	}
	if parsed["retry_count"] != float64(0) {
		t.Errorf("retry_count = %v", parsed["retry_count"])
	}
}

func qaContains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && containsSubstring(s, substr)
}
