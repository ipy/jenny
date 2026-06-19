package config

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ipy/jenny/internal/log"
)

// test models.json fixture matching the real aidy-models schema
const testModelsJSON = `{
  "schemaVersion": 1,
  "lastUpdated": "2026-06-01T00:00:00Z",
  "models": {
    "claude-sonnet-4-6": {
      "id": "claude-sonnet-4-6",
      "provider": "anthropic",
      "family": "claude",
      "contextWindow": 200000,
      "maxOutput": 64000,
      "pricing": {
        "input": 3.0,
        "output": 15.0,
        "cacheRead": 0.3,
        "cacheCreation": 3.75
      },
      "modalities": ["text", "image"],
      "abilities": ["code", "tool_use"]
    },
    "deepseek-v4-flash": {
      "id": "deepseek-v4-flash",
      "provider": "deepseek",
      "family": "deepseek",
      "contextWindow": 1048576,
      "maxOutput": 384000,
      "pricing": {
        "input": 0.14,
        "output": 0.28,
        "cacheRead": 0.0028,
        "cacheCreation": 0
      },
      "modalities": ["text"],
      "abilities": ["code", "tool_use"]
    },
    "deepseek-v4-pro": {
      "id": "deepseek-v4-pro",
      "provider": "deepseek",
      "family": "deepseek",
      "contextWindow": 1048576,
      "maxOutput": 384000,
      "pricing": {
        "input": 0.435,
        "output": 0.87
      },
      "modalities": ["text"],
      "abilities": ["code", "tool_use"]
    },
    "unknown-only-model": {
      "id": "unknown-only-model",
      "provider": "obscure",
      "family": "unknown",
      "contextWindow": 8192,
      "maxOutput": 4096,
      "pricing": {
        "input": 0.0,
        "output": 0.0
      },
      "modalities": [],
      "abilities": []
    }
  }
}`

// testConfigJSON is a models override block as it would appear in config.json
func testConfigJSON(models map[string]any) string {
	m := map[string]any{"models": models}
	b, _ := json.Marshal(m)
	return string(b)
}

func TestModelRegistry_Capability_KnownModel(t *testing.T) {
	log.ResetForTest()
	r := newRegistryWithFixture(t, testModelsJSON)

	got, ok := r.Capability("claude-sonnet-4-6")
	if !ok {
		t.Fatal("expected Capability to return ok=true for claude-sonnet-4-6")
	}
	if got != 64000 {
		t.Errorf("Capability(claude-sonnet-4-6) = %d, want 64000", got)
	}
}

func TestModelRegistry_Capability_DeepSeekV4Flash(t *testing.T) {
	log.ResetForTest()
	r := newRegistryWithFixture(t, testModelsJSON)

	got, ok := r.Capability("deepseek-v4-flash")
	if !ok {
		t.Fatal("expected Capability to return ok=true for deepseek-v4-flash")
	}
	if got != 384000 {
		t.Errorf("Capability(deepseek-v4-flash) = %d, want 384000", got)
	}
}

func TestModelRegistry_Capability_UnknownModel(t *testing.T) {
	log.ResetForTest()
	r := newRegistryWithFixture(t, testModelsJSON)

	_, ok := r.Capability("nonexistent-model-xyz")
	if ok {
		t.Error("expected Capability to return ok=false for unknown model")
	}
}

func TestModelRegistry_Pricing(t *testing.T) {
	log.ResetForTest()
	r := newRegistryWithFixture(t, testModelsJSON)

	pricing, ok := r.Pricing("claude-sonnet-4-6")
	if !ok {
		t.Fatal("expected Pricing to return ok=true for claude-sonnet-4-6")
	}
	if pricing.InputUSD != 0.000003 {
		t.Errorf("Pricing InputUSD = %f, want 0.000003 ($3.0/1M)", pricing.InputUSD)
	}
	if pricing.OutputUSD != 0.000015 {
		t.Errorf("Pricing OutputUSD = %f, want 0.000015 ($15.0/1M)", pricing.OutputUSD)
	}
	if pricing.CacheReadUSD != 0.0000003 {
		t.Errorf("Pricing CacheReadUSD = %f, want 0.0000003", pricing.CacheReadUSD)
	}
}

func TestModelRegistry_Pricing_UnknownModel(t *testing.T) {
	log.ResetForTest()
	r := newRegistryWithFixture(t, testModelsJSON)

	_, ok := r.Pricing("nonexistent-model-xyz")
	if ok {
		t.Error("expected Pricing to return ok=false for unknown model")
	}
}

func TestModelRegistry_ContextWindow(t *testing.T) {
	log.ResetForTest()
	r := newRegistryWithFixture(t, testModelsJSON)

	got, ok := r.ContextWindow("claude-sonnet-4-6")
	if !ok {
		t.Fatal("expected ContextWindow to return ok=true")
	}
	if got != 200000 {
		t.Errorf("ContextWindow = %d, want 200000", got)
	}
}

func TestModelRegistry_Modalities(t *testing.T) {
	log.ResetForTest()
	r := newRegistryWithFixture(t, testModelsJSON)

	got, ok := r.Modalities("claude-sonnet-4-6")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if len(got) != 2 || got[0] != "text" || got[1] != "image" {
		t.Errorf("Modalities = %v, want [text image]", got)
	}
}

func TestModelRegistry_Abilities(t *testing.T) {
	log.ResetForTest()
	r := newRegistryWithFixture(t, testModelsJSON)

	got, ok := r.Abilities("claude-sonnet-4-6")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if len(got) != 2 {
		t.Errorf("Abilities = %v, want 2 entries", got)
	}
}

func TestMalformedCacheFileRenamed(t *testing.T) {
	log.ResetForTest()
	tmpDir := t.TempDir()
	cachePath := filepath.Join(tmpDir, "models.json")

	// Write malformed JSON
	if err := os.WriteFile(cachePath, []byte("{this is not json"), 0644); err != nil {
		t.Fatal(err)
	}

	r := NewModelRegistry(cachePath)
	r.loadFromCache()

	// Verify the broken file was renamed
	brokenPath := cachePath + ".broken"
	if _, err := os.Stat(brokenPath); os.IsNotExist(err) {
		t.Error("expected broken cache to be renamed to .broken")
	}

	// The registry should be empty after corrupted cache
	_, ok := r.Capability("claude-sonnet-4-6")
	if ok {
		t.Error("expected no capability after corrupted cache")
	}
}

func TestETag304_DoesNotRewriteCache(t *testing.T) {
	log.ResetForTest()
	tmpDir := t.TempDir()
	cachePath := filepath.Join(tmpDir, "models.json")
	metaPath := filepath.Join(tmpDir, "meta.json")

	// Seed a valid cache with known content
	seedContent := testModelsJSON
	if err := os.WriteFile(cachePath, []byte(seedContent), 0644); err != nil {
		t.Fatal(err)
	}
	// Seed meta with an etag
	seedMeta := `{"fetchedAt":"2026-06-01T00:00:00Z","etag":"\"abc123\"","schemaVersion":1}`
	if err := os.WriteFile(metaPath, []byte(seedMeta), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a server that returns 304
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"abc123"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		// Should not reach here if etag sent correctly
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"schemaVersion":2,"models":{"new-model":{"id":"new-model"}}}`))
	}))
	defer server.Close()

	r := NewModelRegistry(cachePath)
	r.fetchURL = server.URL
	if err := r.Fetch(); err != nil {
		t.Fatal(err)
	}

	// Cache should still have original content (not the updated one from server)
	if err := r.loadFromCache(); err != nil {
		t.Fatal(err)
	}
	// The fixture has claude-sonnet-4-6, which should still be present since 304 didn't rewrite
	_, ok := r.Capability("claude-sonnet-4-6")
	if !ok {
		t.Error("expected claude-sonnet-4-6 to still be in cache after 304")
	}
}

func TestUserConfigOverride_TakesPrecedence(t *testing.T) {
	log.ResetForTest()
	r := newRegistryWithFixture(t, testModelsJSON)

	// Apply a user override that reduces maxOutput for claude-sonnet-4-6 to 32000
	userModels := map[string]*ModelOverride{
		"claude-sonnet-4-6": {MaxOutput: ptr(32000)},
	}
	if err := r.ApplyUserOverrides(userModels); err != nil {
		t.Fatal(err)
	}

	got, ok := r.Capability("claude-sonnet-4-6")
	if !ok {
		t.Fatal("expected ok=true with user override")
	}
	if got != 32000 {
		t.Errorf("user override Capability = %d, want 32000", got)
	}

	// But other fields should still fall through to registry
	pricing, ok := r.Pricing("claude-sonnet-4-6")
	if !ok {
		t.Fatal("expected pricing after user override")
	}
	if pricing.InputUSD != 0.000003 {
		t.Errorf("pricing InputUSD should fall through to registry, got %f", pricing.InputUSD)
	}
}

func TestUserConfigOverride_SingleFieldPatch(t *testing.T) {
	log.ResetForTest()
	r := newRegistryWithFixture(t, testModelsJSON)

	// Patch only input pricing, leave output alone
	userModels := map[string]*ModelOverride{
		"claude-sonnet-4-6": {
			Pricing: &PricingOverride{
				InputUSD: ptr(0.000002), // override input only
			},
		},
	}
	if err := r.ApplyUserOverrides(userModels); err != nil {
		t.Fatal(err)
	}

	pricing, ok := r.Pricing("claude-sonnet-4-6")
	if !ok {
		t.Fatal("expected pricing")
	}
	if pricing.InputUSD != 0.000002 {
		t.Errorf("InputUSD = %f, want 0.000002", pricing.InputUSD)
	}
	// Output should remain from registry
	if pricing.OutputUSD != 0.000015 {
		t.Errorf("OutputUSD = %f, want 0.000015 (fall through)", pricing.OutputUSD)
	}
}

func TestUserConfigOverride_PricingFieldByField(t *testing.T) {
	log.ResetForTest()
	r := newRegistryWithFixture(t, testModelsJSON)

	userModels := map[string]*ModelOverride{
		"deepseek-v4-flash": {
			Pricing: &PricingOverride{
				InputUSD: ptr(0.000005), // only override input
			},
		},
	}
	if err := r.ApplyUserOverrides(userModels); err != nil {
		t.Fatal(err)
	}

	pricing, ok := r.Pricing("deepseek-v4-flash")
	if !ok {
		t.Fatal("expected pricing")
	}
	if pricing.InputUSD != 0.000005 {
		t.Errorf("InputUSD = %f, want 0.000005", pricing.InputUSD)
	}
	if pricing.OutputUSD != 0.00000028 {
		t.Errorf("OutputUSD = %f, want 0.00000028 (registry fall through)", pricing.OutputUSD)
	}
	if pricing.CacheReadUSD != 0.0000000028 {
		t.Errorf("CacheReadUSD = %f, want 0.0000000028 (registry fall through)", pricing.CacheReadUSD)
	}
}

func TestUserConfigOverride_UnknownModelIgnored(t *testing.T) {
	log.ResetForTest()
	r := newRegistryWithFixture(t, testModelsJSON)

	userModels := map[string]*ModelOverride{
		"nonexistent-model": {MaxOutput: ptr(9999)},
	}
	// Should not error
	if err := r.ApplyUserOverrides(userModels); err != nil {
		t.Fatal(err)
	}

	// Override for unknown model creates a new entry with the override value.
	got, ok := r.Capability("nonexistent-model")
	if !ok {
		t.Fatal("expected Capability for override-only model to resolve")
	}
	if got != 9999 {
		t.Errorf("Capability(nonexistent-model) = %d, want 9999 (from override)", got)
	}

	// Known models should still work
	got2, ok2 := r.Capability("claude-sonnet-4-6")
	if !ok2 {
		t.Fatal("expected claude-sonnet-4-6 to still resolve")
	}
	if got2 != 64000 {
		t.Errorf("Capability = %d, want 64000 (unchanged)", got2)
	}
}

func TestUserConfigOverride_MalformedModelsBlock(t *testing.T) {
	log.ResetForTest()
	r := newRegistryWithFixture(t, testModelsJSON)

	// Apply a nil override map (simulating parse failure)
	// This should not break anything
	r.ApplyUserOverrides(nil)

	got, ok := r.Capability("claude-sonnet-4-6")
	if !ok {
		t.Fatal("expected capability still working after nil overrides")
	}
	if got != 64000 {
		t.Errorf("Capability = %d, want 64000", got)
	}
}

func TestFetch_HTTPErrorPreservesCache(t *testing.T) {
	log.ResetForTest()
	tmpDir := t.TempDir()
	cachePath := filepath.Join(tmpDir, "models.json")

	// Write a valid seed cache
	if err := os.WriteFile(cachePath, []byte(testModelsJSON), 0644); err != nil {
		t.Fatal(err)
	}

	// Point to a non-existent server
	r := NewModelRegistry(cachePath)
	r.fetchURL = "http://127.0.0.1:1" // nothing listening
	err := r.Fetch()
	if err == nil {
		t.Error("expected fetch error")
	}

	// Verify cache is still intact
	if err := r.loadFromCache(); err != nil {
		t.Fatal("cache should still be loadable:", err)
	}
	_, ok := r.Capability("claude-sonnet-4-6")
	if !ok {
		t.Error("expected claude-sonnet-4-6 after failed fetch (preserved cache)")
	}
}

func TestWarmCache_NoFetch(t *testing.T) {
	log.ResetForTest()
	tmpDir := t.TempDir()
	cachePath := filepath.Join(tmpDir, "models.json")
	metaPath := filepath.Join(tmpDir, "meta.json")

	// Write a fresh cache (< 24h old)
	if err := os.WriteFile(cachePath, []byte(testModelsJSON), 0644); err != nil {
		t.Fatal(err)
	}
	// Meta says fetched 1 hour ago
	meta := MetaFile{
		FetchedAt:     time.Now().Add(-1 * time.Hour),
		ETag:          `"some-etag"`,
		SchemaVersion: 1,
	}
	metaData, _ := json.Marshal(meta)
	if err := os.WriteFile(metaPath, metaData, 0644); err != nil {
		t.Fatal(err)
	}

	r := NewModelRegistry(cachePath)
	shouldFetch := r.ShouldFetch()
	if shouldFetch {
		t.Error("expected ShouldFetch=false for fresh cache (< 24h)")
	}
}

func TestStaleCache_ShouldFetch(t *testing.T) {
	log.ResetForTest()
	tmpDir := t.TempDir()
	cachePath := filepath.Join(tmpDir, "models.json")
	metaPath := filepath.Join(tmpDir, "meta.json")

	// Write a stale cache (> 24h old)
	if err := os.WriteFile(cachePath, []byte(testModelsJSON), 0644); err != nil {
		t.Fatal(err)
	}
	meta := MetaFile{
		FetchedAt:     time.Now().Add(-25 * time.Hour),
		ETag:          `"old-etag"`,
		SchemaVersion: 1,
	}
	metaData, _ := json.Marshal(meta)
	if err := os.WriteFile(metaPath, metaData, 0644); err != nil {
		t.Fatal(err)
	}

	r := NewModelRegistry(cachePath)
	shouldFetch := r.ShouldFetch()
	if !shouldFetch {
		t.Error("expected ShouldFetch=true for stale cache (> 24h)")
	}
}

func TestNoCache_FirstStartup(t *testing.T) {
	log.ResetForTest()
	tmpDir := t.TempDir()
	cachePath := filepath.Join(tmpDir, "models.json")

	r := NewModelRegistry(cachePath)
	shouldFetch := r.ShouldFetch()
	if !shouldFetch {
		t.Error("expected ShouldFetch=true when no cache exists")
	}
}

func TestOfflineMode_NoFetch(t *testing.T) {
	log.ResetForTest()
	tmpDir := t.TempDir()
	cachePath := filepath.Join(tmpDir, "models.json")
	metaPath := filepath.Join(tmpDir, "meta.json")

	// Write a stale cache
	if err := os.WriteFile(cachePath, []byte(testModelsJSON), 0644); err != nil {
		t.Fatal(err)
	}
	meta := MetaFile{
		FetchedAt:     time.Now().Add(-25 * time.Hour),
		ETag:          `"old-etag"`,
		SchemaVersion: 1,
	}
	metaData, _ := json.Marshal(meta)
	if err := os.WriteFile(metaPath, metaData, 0644); err != nil {
		t.Fatal(err)
	}

	r := NewModelRegistry(cachePath)
	r.offline = true
	if r.ShouldFetch() {
		t.Error("expected ShouldFetch=false in offline mode regardless of cache age")
	}
}

func TestParsePricing_MillionDollarsToPerToken(t *testing.T) {
	p := parsePricingFromRegistry(registryPricing{
		Input:         ptr(3.0),
		Output:        ptr(15.0),
		CacheRead:     ptr(0.3),
		CacheCreation: ptr(3.75),
	})

	if p.InputUSD != 0.000003 {
		t.Errorf("InputUSD = %f, want 0.000003", p.InputUSD)
	}
	if p.OutputUSD != 0.000015 {
		t.Errorf("OutputUSD = %f, want 0.000015", p.OutputUSD)
	}
	if p.CacheReadUSD != 0.0000003 {
		t.Errorf("CacheReadUSD = %f, want 0.0000003", p.CacheReadUSD)
	}
	if p.CacheCreationUSD != 0.00000375 {
		t.Errorf("CacheCreationUSD = %f, want 0.00000375", p.CacheCreationUSD)
	}
}

func TestParsePricing_ZeroFields(t *testing.T) {
	p := parsePricingFromRegistry(registryPricing{
		Input:  ptr(0.14),
		Output: ptr(0.28),
		// CacheRead and CacheCreation are nil
	})

	if p.InputUSD != 0.00000014 {
		t.Errorf("InputUSD = %f, want 0.00000014", p.InputUSD)
	}
	if p.CacheReadUSD != 0 {
		t.Errorf("CacheReadUSD = %f, want 0 (nil registry field)", p.CacheReadUSD)
	}
	if p.CacheCreationUSD != 0 {
		t.Errorf("CacheCreationUSD = %f, want 0 (nil registry field)", p.CacheCreationUSD)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newRegistryWithFixture creates a ModelRegistry pre-loaded with the given JSON fixture.
func newRegistryWithFixture(t *testing.T, fixture string) *ModelRegistry {
	t.Helper()
	r := &ModelRegistry{}
	if err := r.parseFromBytes([]byte(fixture)); err != nil {
		t.Fatalf("failed to parse fixture: %v", err)
	}
	return r
}

// ptr returns a pointer to v.
func ptr[T any](v T) *T { return &v }

// TestMain ensures log state is clean.
func TestMain(m *testing.M) {
	log.ResetForTest()
	os.Exit(m.Run())
}

// TestCacheReadFromDisk verifies that a registry can read from a file on disk.
func TestCacheReadFromDisk(t *testing.T) {
	log.ResetForTest()
	tmpDir := t.TempDir()
	cachePath := filepath.Join(tmpDir, "models.json")
	metaPath := filepath.Join(tmpDir, "meta.json")

	// Write fixture as cache
	if err := os.WriteFile(cachePath, []byte(testModelsJSON), 0644); err != nil {
		t.Fatal(err)
	}
	meta := MetaFile{
		FetchedAt:     time.Now(),
		ETag:          `"test-etag"`,
		SchemaVersion: 1,
	}
	metaData, _ := json.Marshal(meta)
	if err := os.WriteFile(metaPath, metaData, 0644); err != nil {
		t.Fatal(err)
	}

	r := NewModelRegistry(cachePath)
	if err := r.loadFromCache(); err != nil {
		t.Fatal("loadFromCache failed:", err)
	}

	// Should have models from fixture
	got, ok := r.Capability("deepseek-v4-flash")
	if !ok {
		t.Fatal("expected deepseek-v4-flash from cache")
	}
	if got != 384000 {
		t.Errorf("Capability from cache = %d, want 384000", got)
	}
}

// TestCapabilityCacheIsLazy verifies lazy parse works: first lookup triggers parse.
func TestCapabilityCacheIsLazy(t *testing.T) {
	log.ResetForTest()
	tmpDir := t.TempDir()
	cachePath := filepath.Join(tmpDir, "models.json")

	// Write a valid cache file
	if err := os.WriteFile(cachePath, []byte(testModelsJSON), 0644); err != nil {
		t.Fatal(err)
	}

	r := NewModelRegistry(cachePath)

	// Before any lookup, it's not parsed (loadCalled=false)
	if r.loadCalled {
		t.Error("expected loadCalled=false before first access")
	}

	// First lookup triggers lazy load
	got, ok := r.Capability("deepseek-v4-flash")
	if !ok {
		t.Fatal("expected Capability after lazy load")
	}
	if got != 384000 {
		t.Errorf("Capability = %d, want 384000", got)
	}
}

// TestConfigModelsMergePricingFields verifies that config.json models key
// merging works field-by-field for pricing.
func TestConfigModelsMergePricingFields(t *testing.T) {
	log.ResetForTest()
	r := newRegistryWithFixture(t, testModelsJSON)

	// config.json models with a single pricing field
	configJSON := testConfigJSON(map[string]any{
		"deepseek-v4-pro": map[string]any{
			"pricing": map[string]any{
				"output": 0.000001, // override only output (per-token), input not set -> fall through
			},
		},
	})

	models, err := r.ParseConfigModels([]byte(configJSON))
	if err != nil {
		t.Fatal("ParseConfigModels failed:", err)
	}

	if err := r.ApplyUserOverrides(models); err != nil {
		t.Fatal(err)
	}

	pricing, ok := r.Pricing("deepseek-v4-pro")
	if !ok {
		t.Fatal("expected pricing")
	}
	// Output should be overridden
	if pricing.OutputUSD != 0.000001 {
		t.Errorf("OutputUSD = %f, want 0.000001", pricing.OutputUSD)
	}
	// Input should fall through to registry (0.435 -> 0.000000435)
	if pricing.InputUSD != 0.000000435 {
		t.Errorf("InputUSD = %f, want 0.000000435", pricing.InputUSD)
	}
	// CacheRead should fall through (nil registry field -> 0)
	// The fixture doesn't have cacheRead for deepseek-v4-pro, so it should be 0
	if pricing.CacheReadUSD != 0.0 {
		t.Errorf("CacheReadUSD = %f, want 0.0", pricing.CacheReadUSD)
	}
}

// TestConfigModelsMalformedWarn verifies malformed config.json models key
// triggers a warning but does not return an error (graceful degradation).
func TestConfigModelsMalformedWarn(t *testing.T) {
	log.ResetForTest()
	r := newRegistryWithFixture(t, testModelsJSON)

	_, err := r.ParseConfigModels([]byte(`{not valid json`))
	if err != nil {
		t.Error("expected no error for malformed config JSON — should degrade gracefully")
	}

	// Registry should still work
	got, ok := r.Capability("deepseek-v4-flash")
	if !ok {
		t.Fatal("expected capability still works after parse error")
	}
	if got != 384000 {
		t.Errorf("Capability = %d, want 384000", got)
	}
}

// TestShouldRefreshCacheFileNotFound returns false when no cache exists at all
// and no fetch is possible (nil meta) - the registry is empty, triggering a full fetch.
func TestNoMetaFile_ShouldFetch(t *testing.T) {
	log.ResetForTest()
	tmpDir := t.TempDir()
	cachePath := filepath.Join(tmpDir, "models.json")

	r := NewModelRegistry(cachePath)
	if !r.ShouldFetch() {
		t.Error("expected ShouldFetch=true when cache file does not exist")
	}
}

// TestConfigModelsEmptyBlock treats empty models as no-op.
func TestConfigModelsEmptyBlock(t *testing.T) {
	log.ResetForTest()
	r := newRegistryWithFixture(t, testModelsJSON)

	configJSON := testConfigJSON(map[string]any{})

	models, err := r.ParseConfigModels([]byte(configJSON))
	if err != nil {
		t.Fatal("ParseConfigModels failed for empty models:", err)
	}
	if len(models) != 0 {
		t.Errorf("expected 0 overrides, got %d", len(models))
	}
}

// TestConfigModelsNoModelsKey treats missing models key as no-op.
func TestConfigModelsNoModelsKey(t *testing.T) {
	log.ResetForTest()
	r := newRegistryWithFixture(t, testModelsJSON)

	models, err := r.ParseConfigModels([]byte(`{"other": "stuff"}`))
	if err != nil {
		t.Fatal("ParseConfigModels failed:", err)
	}
	if len(models) != 0 {
		t.Errorf("expected 0 overrides, got %d", len(models))
	}
}

// TestModelRegistry_DeepSeekV4Pro_Capability validates the fix for the stale 8192.
func TestModelRegistry_DeepSeekV4Pro_Capability(t *testing.T) {
	log.ResetForTest()
	r := newRegistryWithFixture(t, testModelsJSON)

	got, ok := r.Capability("deepseek-v4-pro")
	if !ok {
		t.Fatal("expected Capability for deepseek-v4-pro from registry")
	}
	if got != 384000 {
		t.Errorf("Capability(deepseek-v4-pro) = %d, want 384000 (not stale 8192)", got)
	}
}

// TestCapabilityOnlyFromRegistry applies to models only in the registry (not in bundled table).
func TestCapabilityOnlyFromRegistry(t *testing.T) {
	log.ResetForTest()
	r := newRegistryWithFixture(t, testModelsJSON)

	got, ok := r.Capability("unknown-only-model")
	if !ok {
		t.Fatal("expected Capability for model only in registry")
	}
	if got != 4096 {
		t.Errorf("Capability = %d, want 4096", got)
	}
}

// TestBadConfigModelsWarnAndEmpty verifies that a corrupt models block logs a warning
// and returns empty overrides (nil, nil).
func TestBadConfigModelsWarnAndEmpty(t *testing.T) {
	log.ResetForTest()
	r := &ModelRegistry{}

	// "models" key exists but is not a valid object
	models, err := r.ParseConfigModels([]byte(`{"models": "not-an-object"}`))
	if err != nil {
		t.Error("expected no error for malformed models, just empty result")
	}
	if models != nil {
		t.Errorf("expected nil models for malformed block, got %d entries", len(models))
	}
}

// TestLazyParseMemoization verifies that parse only happens once (memoization).
func TestLazyParseMemoization(t *testing.T) {
	log.ResetForTest()
	tmpDir := t.TempDir()
	cachePath := filepath.Join(tmpDir, "models.json")

	if err := os.WriteFile(cachePath, []byte(testModelsJSON), 0644); err != nil {
		t.Fatal(err)
	}

	r := NewModelRegistry(cachePath)

	// First call triggers parse
	r.ensureLoaded()
	if !r.loadCalled {
		t.Fatal("expected loadCalled=true after first ensureLoaded")
	}

	// Record current state
	firstCap, _ := r.Capability("claude-sonnet-4-6")

	// Call again — should be memoized
	r.ensureLoaded()
	secondCap, _ := r.Capability("claude-sonnet-4-6")
	if firstCap != secondCap {
		t.Error("capability changed after second ensureLoaded")
	}
}

// TestFetchURLDefaults verifies the default URL is the aidy-models repo.
func TestFetchURLDefaults(t *testing.T) {
	r := &ModelRegistry{}
	url := r.getFetchURL()
	expected := "https://raw.githubusercontent.com/ImSingee/aidy-models/master/models.json"
	if url != expected {
		t.Errorf("fetch URL = %q, want %q", url, expected)
	}
}

// TestHTTPFetchWritesMeta verifies that a successful fetch writes meta.json.
func TestHTTPFetchWritesMeta(t *testing.T) {
	log.ResetForTest()
	tmpDir := t.TempDir()
	cachePath := filepath.Join(tmpDir, "models.json")
	metaPath := filepath.Join(tmpDir, "meta.json")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"test-etag-123"`)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(testModelsJSON))
	}))
	defer server.Close()

	r := NewModelRegistry(cachePath)
	r.fetchURL = server.URL
	if err := r.Fetch(); err != nil {
		t.Fatal("Fetch failed:", err)
	}

	// Verify models.json was written
	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal("models.json not written:", err)
	}
	if !strings.Contains(string(data), `"claude-sonnet-4-6"`) {
		t.Error("models.json missing expected content")
	}

	// Verify meta.json was written
	metaData, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatal("meta.json not written:", err)
	}
	var meta MetaFile
	if err := json.Unmarshal(metaData, &meta); err != nil {
		t.Fatal("meta.json not valid JSON:", err)
	}
	if meta.ETag != `"test-etag-123"` {
		t.Errorf("meta ETag = %q, want \"test-etag-123\"", meta.ETag)
	}
	if meta.SchemaVersion != 1 {
		t.Errorf("meta SchemaVersion = %d, want 1", meta.SchemaVersion)
	}
	if meta.FetchedAt.IsZero() {
		t.Error("meta FetchedAt is zero")
	}
}

// TestFetch_PreservesUserOverrides verifies that user overrides from ApplyUserOverrides
// survive a Fetch() call. The Fetch() code path calls applyOverridesLocked() at
// line 273 of registry.go after replacing r.models with freshly-fetched data.
// This test proves the fix for CD-1 (--refresh-registry skips user override application).
func TestFetch_PreservesUserOverrides(t *testing.T) {
	log.ResetForTest()
	tmpDir := t.TempDir()
	cachePath := filepath.Join(tmpDir, "models.json")

	// Fixture: only test-model with maxOutput 8000 (representing the "fresh network" data).
	serverJSON := `{
		"schemaVersion": 1,
		"lastUpdated": "2026-06-01T00:00:00Z",
		"models": {
			"test-model": {
				"id": "test-model",
				"provider": "test",
				"family": "test",
				"contextWindow": 128000,
				"maxOutput": 8000,
				"pricing": {
					"input": 1.0,
					"output": 5.0
				},
				"modalities": ["text"],
				"abilities": ["code"]
			}
		}
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"test-etag-override"`)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(serverJSON))
	}))
	defer server.Close()

	r := NewModelRegistry(cachePath)
	r.fetchURL = server.URL

	// Apply user overrides BEFORE Fetch(), simulating the fixed main.go ordering.
	// test-model: override maxOutput to 16000 (higher than registry's 8000)
	// only-override-model: a model that exists ONLY as an override, not in the registry
	userModels := map[string]*ModelOverride{
		"test-model":          {MaxOutput: ptr(16000)},
		"only-override-model": {MaxOutput: ptr(32000)},
	}
	if err := r.ApplyUserOverrides(userModels); err != nil {
		t.Fatal("ApplyUserOverrides failed:", err)
	}

	// Fetch from the test server — this replaces r.models with server data
	// and then calls applyOverridesLocked() at line 273.
	if err := r.Fetch(); err != nil {
		t.Fatal("Fetch failed:", err)
	}

	// Assert: test-model's maxOutput is the user override value (16000), not the
	// registry value (8000). This proves overrides survive Fetch().
	got, ok := r.Capability("test-model")
	if !ok {
		t.Fatal("expected Capability for test-model after Fetch")
	}
	if got != 16000 {
		t.Errorf("Capability(test-model) = %d, want 16000 (user override survived Fetch)", got)
	}

	// Assert: the model that exists only as an override (not in the registry JSON)
	// is still present after Fetch().
	got2, ok2 := r.Capability("only-override-model")
	if !ok2 {
		t.Fatal("expected Capability for only-override-model to survive Fetch")
	}
	if got2 != 32000 {
		t.Errorf("Capability(only-override-model) = %d, want 32000", got2)
	}

	// Assert: fields NOT overridden still fall through to the registry values.
	pricing, ok3 := r.Pricing("test-model")
	if !ok3 {
		t.Fatal("expected Pricing for test-model after Fetch")
	}
	// Registry pricing: input=1.0/1M = 0.000001 per token
	if pricing.InputUSD != 0.000001 {
		t.Errorf("Pricing.InputUSD = %f, want 0.000001 (registry fall through)", pricing.InputUSD)
	}
}

// TestParseConfigModelsDeeplyNestedPricing verifies deep nested override parsing.
// TestModelRegistry_Fetch_Offline verifies that Fetch() returns an error when
// offline mode is active and does NOT make any HTTP request.
func TestModelRegistry_Fetch_Offline(t *testing.T) {
	log.ResetForTest()
	tmpDir := t.TempDir()
	cachePath := filepath.Join(tmpDir, "models.json")

	// Create a mock server that will fail the test if an HTTP request is made.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("unexpected HTTP request in offline mode")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	r := NewModelRegistry(cachePath)
	r.fetchURL = server.URL
	r.SetOffline(true)

	err := r.Fetch()
	if err == nil {
		t.Fatal("expected error from Fetch() in offline mode")
	}
	if !strings.Contains(err.Error(), "offline") {
		t.Errorf("expected error to contain 'offline', got: %v", err)
	}
}

func TestParseConfigModelsDeeplyNestedPricing(t *testing.T) {
	log.ResetForTest()
	r := &ModelRegistry{}

	configJSON := testConfigJSON(map[string]any{
		"my-model": map[string]any{
			"maxOutput":     float64(10000),
			"contextWindow": float64(128000),
			"pricing": map[string]any{
				"input":         float64(2.0),
				"output":        float64(8.0),
				"cacheRead":     float64(0.2),
				"cacheCreation": float64(1.0),
			},
			"modalities": []any{"text"},
			"abilities":  []any{"code"},
		},
	})

	models, err := r.ParseConfigModels([]byte(configJSON))
	if err != nil {
		t.Fatal("ParseConfigModels failed:", err)
	}

	m, ok := models["my-model"]
	if !ok {
		t.Fatal("expected my-model in parsed overrides")
	}
	if m.MaxOutput == nil || *m.MaxOutput != 10000 {
		t.Errorf("MaxOutput = %v, want 10000", m.MaxOutput)
	}
	if m.ContextWindow == nil || *m.ContextWindow != 128000 {
		t.Errorf("ContextWindow = %v, want 128000", m.ContextWindow)
	}
	if m.Pricing == nil {
		t.Fatal("Pricing is nil")
	}
	if m.Pricing.InputUSD == nil || *m.Pricing.InputUSD != 2.0 {
		t.Errorf("Pricing.InputUSD = %v", m.Pricing.InputUSD)
	}
	if m.Pricing.OutputUSD == nil || *m.Pricing.OutputUSD != 8.0 {
		t.Errorf("Pricing.OutputUSD = %v", m.Pricing.OutputUSD)
	}
}
