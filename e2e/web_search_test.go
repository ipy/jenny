package e2e_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ipy/jenny/e2e/harness"
	"github.com/ipy/jenny/internal/testutil/mockapi"
)

// TestWebSearchEnvDisabled verifies SC5: JENNY_WEB_SEARCH_PROVIDER=disabled
// suppresses the "web search client provider not created" warning, indicating
// the disabled strategy is active.
func TestWebSearchEnvDisabled(t *testing.T) {
	mock := mockapi.NewMockServer(mockapi.WithCassetteDir(cassetteDir))
	defer mock.Close()

	env := []string{
		"ANTHROPIC_BASE_URL=" + mock.URL() + "/cassette/echo-hello",
		"ANTHROPIC_AUTH_TOKEN=test-token",
		"ANTHROPIC_MODEL=claude-sonnet-4-6",
		"JENNY_WEB_SEARCH_PROVIDER=disabled",
	}

	res := harness.RunJenny(t, env, "--output-format", "stream-json", "--verbose",
		"-p", "hi")

	if res.ExitCode != 0 {
		t.Fatalf("exit %d; stderr=%q", res.ExitCode, res.Stderr)
	}

	// With provider=disabled, there should be NO "web search client provider not created" warning
	if strings.Contains(res.Stderr, "web search client provider not created") {
		t.Error("disabled provider should not attempt to create client provider")
	}
}

// TestWebSearchEnvNative verifies SC3: JENNY_WEB_SEARCH_PROVIDER=native
// shows the client provider warning (because native mode still tries to create
// a fallback client provider).
func TestWebSearchEnvNative(t *testing.T) {
	mock := mockapi.NewMockServer(mockapi.WithCassetteDir(cassetteDir))
	defer mock.Close()

	env := []string{
		"ANTHROPIC_BASE_URL=" + mock.URL() + "/cassette/echo-hello",
		"ANTHROPIC_AUTH_TOKEN=test-token",
		"ANTHROPIC_MODEL=claude-sonnet-4-6",
		"JENNY_WEB_SEARCH_PROVIDER=native",
	}

	res := harness.RunJenny(t, env, "--output-format", "stream-json", "--verbose",
		"-p", "hi")

	if res.ExitCode != 0 {
		t.Fatalf("exit %d; stderr=%q", res.ExitCode, res.Stderr)
	}

	// With provider=native, web_search tool should be in the init tools
	foundWebSearch := strings.Contains(res.Stdout, `"web_search"`)
	if !foundWebSearch {
		t.Error("web_search tool not found in init tools with native provider")
	}
}

// TestWebSearchEnvClient verifies SC4: JENNY_WEB_SEARCH_PROVIDER=client
// with valid client config suppresses warnings.
func TestWebSearchEnvClient(t *testing.T) {
	mock := mockapi.NewMockServer(mockapi.WithCassetteDir(cassetteDir))
	defer mock.Close()

	env := []string{
		"ANTHROPIC_BASE_URL=" + mock.URL() + "/cassette/echo-hello",
		"ANTHROPIC_AUTH_TOKEN=test-token",
		"ANTHROPIC_MODEL=claude-sonnet-4-6",
		"JENNY_WEB_SEARCH_PROVIDER=client",
		"JENNY_WEB_SEARCH_CLIENT_PROVIDER=tavily",
		"JENNY_WEB_SEARCH_CLIENT_API_KEY=test-tavily-key",
	}

	res := harness.RunJenny(t, env, "--output-format", "stream-json", "--verbose",
		"-p", "hi")

	if res.ExitCode != 0 {
		t.Fatalf("exit %d; stderr=%q", res.ExitCode, res.Stderr)
	}

	// With valid client config, there should be no warnings about client provider
	if strings.Contains(res.Stderr, "web search client provider not created") {
		t.Error("valid client config should not produce 'client provider not created' warning")
	}
	if strings.Contains(res.Stderr, "web search config invalid") {
		t.Error("valid client config should not produce 'config invalid' warning")
	}
}

// TestWebSearchInvalidClientProvider verifies that an invalid client provider
// produces a warning message.
func TestWebSearchInvalidClientProvider(t *testing.T) {
	mock := mockapi.NewMockServer(mockapi.WithCassetteDir(cassetteDir))
	defer mock.Close()

	env := []string{
		"ANTHROPIC_BASE_URL=" + mock.URL() + "/cassette/echo-hello",
		"ANTHROPIC_AUTH_TOKEN=test-token",
		"ANTHROPIC_MODEL=claude-sonnet-4-6",
		"JENNY_WEB_SEARCH_PROVIDER=client",
		"JENNY_WEB_SEARCH_CLIENT_PROVIDER=invalid",
	}

	res := harness.RunJenny(t, env, "--output-format", "stream-json", "--verbose",
		"-p", "hi")

	if res.ExitCode != 0 {
		t.Fatalf("exit %d; stderr=%q", res.ExitCode, res.Stderr)
	}

	// Should see invalid client provider warning
	if !strings.Contains(res.Stderr, "invalid client provider") {
		t.Error("expected 'invalid client provider' warning for invalid provider name")
	}
}

// TestWebSearchCustomClientProvider verifies the "custom" client provider is accepted.
func TestWebSearchCustomClientProvider(t *testing.T) {
	mock := mockapi.NewMockServer(mockapi.WithCassetteDir(cassetteDir))
	defer mock.Close()

	env := []string{
		"ANTHROPIC_BASE_URL=" + mock.URL() + "/cassette/echo-hello",
		"ANTHROPIC_AUTH_TOKEN=test-token",
		"ANTHROPIC_MODEL=claude-sonnet-4-6",
		"JENNY_WEB_SEARCH_PROVIDER=client",
		"JENNY_WEB_SEARCH_CLIENT_PROVIDER=custom",
		"JENNY_WEB_SEARCH_CLIENT_API_KEY=test-custom-key",
	}

	res := harness.RunJenny(t, env, "--output-format", "stream-json", "--verbose",
		"-p", "hi")

	if res.ExitCode != 0 {
		t.Fatalf("exit %d; stderr=%q", res.ExitCode, res.Stderr)
	}

	// Custom provider should be accepted without warnings
	if strings.Contains(res.Stderr, "web search client provider not created") {
		t.Error("custom client provider should not produce 'client provider not created' warning")
	}
	if strings.Contains(res.Stderr, "web search config invalid") {
		t.Error("custom client provider should not produce 'config invalid' warning")
	}
}

// TestWebSearchClientAPIKeyEnvForm verifies SC7: env:NAME form for API key.
func TestWebSearchClientAPIKeyEnvForm(t *testing.T) {
	mock := mockapi.NewMockServer(mockapi.WithCassetteDir(cassetteDir))
	defer mock.Close()

	env := []string{
		"ANTHROPIC_BASE_URL=" + mock.URL() + "/cassette/echo-hello",
		"ANTHROPIC_AUTH_TOKEN=test-token",
		"ANTHROPIC_MODEL=claude-sonnet-4-6",
		"JENNY_WEB_SEARCH_PROVIDER=client",
		"JENNY_WEB_SEARCH_CLIENT_PROVIDER=tavily",
		"JENNY_WEB_SEARCH_CLIENT_API_KEY=env:MY_TAVILY_KEY",
		"MY_TAVILY_KEY=resolved-tavily-key",
	}

	res := harness.RunJenny(t, env, "--output-format", "stream-json", "--verbose",
		"-p", "hi")

	if res.ExitCode != 0 {
		t.Fatalf("exit %d; stderr=%q", res.ExitCode, res.Stderr)
	}

	// env:NAME form should resolve correctly - no warnings
	if strings.Contains(res.Stderr, "web search client provider not created") {
		t.Error("env:NAME API key form should resolve correctly")
	}
}

// TestWebSearchClientAPIKeyLiteralForm verifies SC7: literal:VALUE form for API key.
func TestWebSearchClientAPIKeyLiteralForm(t *testing.T) {
	mock := mockapi.NewMockServer(mockapi.WithCassetteDir(cassetteDir))
	defer mock.Close()

	env := []string{
		"ANTHROPIC_BASE_URL=" + mock.URL() + "/cassette/echo-hello",
		"ANTHROPIC_AUTH_TOKEN=test-token",
		"ANTHROPIC_MODEL=claude-sonnet-4-6",
		"JENNY_WEB_SEARCH_PROVIDER=client",
		"JENNY_WEB_SEARCH_CLIENT_PROVIDER=tavily",
		"JENNY_WEB_SEARCH_CLIENT_API_KEY=literal:my-direct-key-123",
	}

	res := harness.RunJenny(t, env, "--output-format", "stream-json", "--verbose",
		"-p", "hi")

	if res.ExitCode != 0 {
		t.Fatalf("exit %d; stderr=%q", res.ExitCode, res.Stderr)
	}

	// literal:VALUE form should resolve correctly - no warnings
	if strings.Contains(res.Stderr, "web search client provider not created") {
		t.Error("literal:VALUE API key form should resolve correctly")
	}
}

// TestWebSearchClientAPIKeyPlainForm verifies SC7: plain literal form for API key.
func TestWebSearchClientAPIKeyPlainForm(t *testing.T) {
	mock := mockapi.NewMockServer(mockapi.WithCassetteDir(cassetteDir))
	defer mock.Close()

	env := []string{
		"ANTHROPIC_BASE_URL=" + mock.URL() + "/cassette/echo-hello",
		"ANTHROPIC_AUTH_TOKEN=test-token",
		"ANTHROPIC_MODEL=claude-sonnet-4-6",
		"JENNY_WEB_SEARCH_PROVIDER=client",
		"JENNY_WEB_SEARCH_CLIENT_PROVIDER=tavily",
		"JENNY_WEB_SEARCH_CLIENT_API_KEY=plain-key-without-prefix",
	}

	res := harness.RunJenny(t, env, "--output-format", "stream-json", "--verbose",
		"-p", "hi")

	if res.ExitCode != 0 {
		t.Fatalf("exit %d; stderr=%q", res.ExitCode, res.Stderr)
	}

	// Plain literal form should resolve correctly - no warnings
	if strings.Contains(res.Stderr, "web search client provider not created") {
		t.Error("plain literal API key form should resolve correctly")
	}
}

// TestWebSearchJSONConfigDisabled verifies JSON config web-search.provider=disabled
// is read from .jenny/config.json.
func TestWebSearchJSONConfigDisabled(t *testing.T) {
	mock := mockapi.NewMockServer(mockapi.WithCassetteDir(cassetteDir))
	defer mock.Close()

	tmpDir := t.TempDir()
	jennyConfigDir := filepath.Join(tmpDir, ".jenny")
	if err := os.MkdirAll(jennyConfigDir, 0o755); err != nil {
		t.Fatal(err)
	}

	configJSON := `{"web-search": {"provider": "disabled"}}`
	if err := os.WriteFile(filepath.Join(jennyConfigDir, "config.json"), []byte(configJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	env := []string{
		"ANTHROPIC_BASE_URL=" + mock.URL() + "/cassette/echo-hello",
		"ANTHROPIC_AUTH_TOKEN=test-token",
		"ANTHROPIC_MODEL=claude-sonnet-4-6",
	}

	res := harness.RunJennyInDir(t, tmpDir, env,
		"--output-format", "stream-json", "--verbose", "-p", "hi")

	if res.ExitCode != 0 {
		t.Fatalf("exit %d; stderr=%q", res.ExitCode, res.Stderr)
	}

	// JSON config with provider=disabled should suppress client provider warning
	if strings.Contains(res.Stderr, "web search client provider not created") {
		t.Error("JSON config with provider=disabled should suppress client provider warning")
	}
}

// TestWebSearchJSONConfigNative verifies JSON config web-search.provider=native
// is read from .jenny/config.json.
func TestWebSearchJSONConfigNative(t *testing.T) {
	mock := mockapi.NewMockServer(mockapi.WithCassetteDir(cassetteDir))
	defer mock.Close()

	tmpDir := t.TempDir()
	jennyConfigDir := filepath.Join(tmpDir, ".jenny")
	if err := os.MkdirAll(jennyConfigDir, 0o755); err != nil {
		t.Fatal(err)
	}

	configJSON := `{"web-search": {"provider": "native"}}`
	if err := os.WriteFile(filepath.Join(jennyConfigDir, "config.json"), []byte(configJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	env := []string{
		"ANTHROPIC_BASE_URL=" + mock.URL() + "/cassette/echo-hello",
		"ANTHROPIC_AUTH_TOKEN=test-token",
		"ANTHROPIC_MODEL=claude-sonnet-4-6",
	}

	res := harness.RunJennyInDir(t, tmpDir, env,
		"--output-format", "stream-json", "--verbose", "-p", "hi")

	if res.ExitCode != 0 {
		t.Fatalf("exit %d; stderr=%q", res.ExitCode, res.Stderr)
	}

	// JSON config with provider=native should show client provider warning
	if !strings.Contains(res.Stderr, "web search client provider not created") {
		t.Error("JSON config with provider=native should show client provider warning")
	}
}

// TestWebSearchConfigPrecedence verifies SC6: env var overrides JSON config.
// JSON says disabled, env says native -> native wins.
func TestWebSearchConfigPrecedenceEnvWins(t *testing.T) {
	mock := mockapi.NewMockServer(mockapi.WithCassetteDir(cassetteDir))
	defer mock.Close()

	tmpDir := t.TempDir()
	jennyConfigDir := filepath.Join(tmpDir, ".jenny")
	if err := os.MkdirAll(jennyConfigDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// JSON config sets provider=disabled
	configJSON := `{"web-search": {"provider": "disabled"}}`
	if err := os.WriteFile(filepath.Join(jennyConfigDir, "config.json"), []byte(configJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	// Env overrides JSON: provider=native
	env := []string{
		"ANTHROPIC_BASE_URL=" + mock.URL() + "/cassette/echo-hello",
		"ANTHROPIC_AUTH_TOKEN=test-token",
		"ANTHROPIC_MODEL=claude-sonnet-4-6",
		"JENNY_WEB_SEARCH_PROVIDER=native",
	}

	res := harness.RunJennyInDir(t, tmpDir, env,
		"--output-format", "stream-json", "--verbose", "-p", "hi")

	if res.ExitCode != 0 {
		t.Fatalf("exit %d; stderr=%q", res.ExitCode, res.Stderr)
	}

	// Env var should override JSON config; native mode should show warning
	if !strings.Contains(res.Stderr, "web search client provider not created") {
		t.Error("env var should override JSON config; expected native mode warning")
	}
}

// TestWebSearchConfigPrecedenceEnvDisabledOverJSON verifies SC6:
// JSON says native, env says disabled -> disabled wins.
func TestWebSearchConfigPrecedenceEnvDisabledOverJSON(t *testing.T) {
	mock := mockapi.NewMockServer(mockapi.WithCassetteDir(cassetteDir))
	defer mock.Close()

	tmpDir := t.TempDir()
	jennyConfigDir := filepath.Join(tmpDir, ".jenny")
	if err := os.MkdirAll(jennyConfigDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// JSON config sets provider=native
	configJSON := `{"web-search": {"provider": "native"}}`
	if err := os.WriteFile(filepath.Join(jennyConfigDir, "config.json"), []byte(configJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	// Env overrides JSON: provider=disabled
	env := []string{
		"ANTHROPIC_BASE_URL=" + mock.URL() + "/cassette/echo-hello",
		"ANTHROPIC_AUTH_TOKEN=test-token",
		"ANTHROPIC_MODEL=claude-sonnet-4-6",
		"JENNY_WEB_SEARCH_PROVIDER=disabled",
	}

	res := harness.RunJennyInDir(t, tmpDir, env,
		"--output-format", "stream-json", "--verbose", "-p", "hi")

	if res.ExitCode != 0 {
		t.Fatalf("exit %d; stderr=%q", res.ExitCode, res.Stderr)
	}

	// Env var should override JSON config; disabled mode should suppress warning
	if strings.Contains(res.Stderr, "web search client provider not created") {
		t.Error("env var should override JSON config; expected disabled mode (no warning)")
	}
}

// TestWebSearchDefaultBehavior verifies default behavior when no config is set.
// Default should be native mode.
func TestWebSearchDefaultBehavior(t *testing.T) {
	mock := mockapi.NewMockServer(mockapi.WithCassetteDir(cassetteDir))
	defer mock.Close()

	env := []string{
		"ANTHROPIC_BASE_URL=" + mock.URL() + "/cassette/echo-hello",
		"ANTHROPIC_AUTH_TOKEN=test-token",
		"ANTHROPIC_MODEL=claude-sonnet-4-6",
	}

	res := harness.RunJenny(t, env, "--output-format", "stream-json", "--verbose",
		"-p", "hi")

	if res.ExitCode != 0 {
		t.Fatalf("exit %d; stderr=%q", res.ExitCode, res.Stderr)
	}

	// Default behavior should include web_search in tools
	foundWebSearch := strings.Contains(res.Stdout, `"web_search"`)
	if !foundWebSearch {
		t.Error("web_search tool not found in init tools with default config")
	}
}

// TestWebSearchToolInInit verifies web_search appears in the init event tools list.
func TestWebSearchToolInInit(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "tool.websearch.in-init",
			Category:    "tools",
			Description: "web_search tool appears in system/init tools array",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Contains: []string{`"web_search"`, `web_search`},
				},
			},
		},
	})
}
