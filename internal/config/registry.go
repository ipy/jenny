// Package config provides external model registry fetching, caching, and lookup.
package config

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ipy/jenny/internal/log"
)

// DefaultRegistryURL is the community-maintained model registry.
const DefaultRegistryURL = "https://raw.githubusercontent.com/ImSingee/aidy-models/master/models.json"

// CacheValidity is the maximum age of a cached registry before a refresh is attempted.
const CacheValidity = 24 * time.Hour

// ---------------------------------------------------------------------------
// Registry JSON schema types (matching aidy-models format)
// ---------------------------------------------------------------------------

type registryModel struct {
	ID            string          `json:"id"`
	Provider      string          `json:"provider"`
	Family        string          `json:"family"`
	ContextWindow int             `json:"contextWindow"`
	MaxOutput     int             `json:"maxOutput"`
	Pricing       registryPricing `json:"pricing"`
	Modalities    []string        `json:"modalities"`
	Abilities     []string        `json:"abilities"`
}

type registryPricing struct {
	Input         *float64 `json:"input"`
	Output        *float64 `json:"output"`
	CacheRead     *float64 `json:"cacheRead"`
	CacheCreation *float64 `json:"cacheCreation"`
}

type registryEnvelope struct {
	SchemaVersion int                      `json:"schemaVersion"`
	LastUpdated   string                   `json:"lastUpdated"`
	Models        map[string]registryModel `json:"models"`
}

// MetaFile stores fetch metadata alongside the cached models.json.
type MetaFile struct {
	FetchedAt     time.Time `json:"fetchedAt"`
	ETag          string    `json:"etag"`
	SchemaVersion int       `json:"schemaVersion"`
}

// ---------------------------------------------------------------------------
// Lookup result types
// ---------------------------------------------------------------------------

// ModelPricing is the per-token USD pricing for a model.
// Rates are per-token (not per-million-tokens).
type ModelPricing struct {
	InputUSD         float64
	OutputUSD        float64
	CacheReadUSD     float64
	CacheCreationUSD float64
	UnknownModel     bool
}

// PricingOverride allows partial field-level overrides from config.json.
// JSON tags match the upstream registry format (camelCase), consistent with
// how the models key in config.json mirrors the registry schema directly.
type PricingOverride struct {
	InputUSD         *float64 `json:"input"`
	OutputUSD        *float64 `json:"output"`
	CacheReadUSD     *float64 `json:"cacheRead"`
	CacheCreationUSD *float64 `json:"cacheCreation"`
}

// ModelOverride defines user-configurable overrides for a single model.
// JSON tags match the upstream registry format (camelCase), not snake_case,
// so that config.json models blocks mirror the registry schema directly.
type ModelOverride struct {
	MaxOutput     *int             `json:"maxOutput"`
	ContextWindow *int             `json:"contextWindow"`
	Pricing       *PricingOverride `json:"pricing"`
	Modalities    []string         `json:"modalities"`
	Abilities     []string         `json:"abilities"`
}

// ---------------------------------------------------------------------------
// ModelRegistry
// ---------------------------------------------------------------------------

// ModelRegistry holds the parsed model registry data and user overrides.
// It supports lazy loading from a cache file and field-level user overrides.
// Resolution order: user config.json override > upstream snapshot > (caller's bundled defaults).
type ModelRegistry struct {
	mu      sync.RWMutex
	fetchMu sync.Mutex // serializes fetch calls, not held during HTTP request

	// Configuration
	cachePath string
	fetchURL  string
	offline   bool

	// Cached state
	meta       *MetaFile
	models     map[string]*modelSnapshot
	overrides  map[string]*ModelOverride
	loadCalled bool
}

// modelSnapshot is the parsed representation of a single model from the registry,
// plus any user overrides applied on top.
type modelSnapshot struct {
	contextWindow int
	maxOutput     int
	pricing       ModelPricing
	modalities    []string
	abilities     []string
}

// NewModelRegistry creates a new registry backed by the given cache file path.
// It does not perform a fetch or parse; the first lookup triggers lazy loading.
func NewModelRegistry(cachePath string) *ModelRegistry {
	return &ModelRegistry{
		cachePath:  cachePath,
		fetchURL:   DefaultRegistryURL,
		models:     make(map[string]*modelSnapshot),
		overrides:  make(map[string]*ModelOverride),
		loadCalled: false,
	}
}

// SetOffline enables or disables offline mode (disables all fetch attempts).
func (r *ModelRegistry) SetOffline(offline bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.offline = offline
}

// ShouldFetch returns true if the registry should be fetched:
//   - false if offline mode is active
//   - true if no cache file exists
//   - true if the cache is older than CacheValidity
//   - false if the cache is fresh
func (r *ModelRegistry) ShouldFetch() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.offline {
		return false
	}

	// Check if cache file exists
	if _, err := os.Stat(r.cachePath); os.IsNotExist(err) {
		return true
	}

	// Check meta for age
	metaPath := r.metaPath()
	meta, _ := r.readMetaLocked(metaPath)
	if meta == nil {
		return true
	}
	return time.Since(meta.FetchedAt) > CacheValidity
}

// Fetch downloads the model registry, respecting ETags for conditional requests.
// It writes the response to the cache file and updates meta.json.
// On network errors, the existing cache is preserved (if any).
// The HTTP request is performed outside the write lock so lookups are not blocked.
func (r *ModelRegistry) Fetch() error {
	// Serialize fetch calls so only one fetch runs at a time.
	r.fetchMu.Lock()
	defer r.fetchMu.Unlock()

	// Check offline mode — Fetch() is a direct call (not background), so it must
	// respect the offline flag just like ShouldFetch() does.
	r.mu.RLock()
	offline := r.offline
	r.mu.RUnlock()
	if offline {
		return fmt.Errorf("registry fetch: offline mode is active")
	}

	req, err := http.NewRequest(http.MethodGet, r.getFetchURL(), nil)
	if err != nil {
		log.Warn("registry fetch: failed to create request", "error", err)
		return err
	}

	// Add ETag header from meta if available (read-only, no lock needed for meta path).
	metaPath := r.metaPath()
	r.mu.RLock()
	etag := ""
	if meta, err := r.readMetaLocked(metaPath); err == nil && meta != nil && meta.ETag != "" {
		etag = meta.ETag
	}
	r.mu.RUnlock()
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}

	// Perform HTTP request OUTSIDE the write lock.
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Warn("registry fetch: network error, preserving cache", "error", err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		// 304: cache is still valid, update fetchedAt
		r.mu.Lock()
		r.updateMetaTimestampLocked(metaPath)
		r.mu.Unlock()
		log.Debug("registry fetch: 304 Not Modified, cache is fresh")
		return nil
	}

	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("registry fetch: HTTP %d", resp.StatusCode)
		log.Warn("registry fetch: unexpected status", "status", resp.StatusCode)
		return err
	}

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Warn("registry fetch: failed to read body", "error", err)
		return err
	}

	// Parse into a temporary map first — do NOT mutate r.models yet.
	tmpModels, err := parseBodyToModels(body)
	if err != nil {
		log.Warn("registry fetch: parse error, preserving cache", "error", err)
		return err
	}

	// Write cache file atomically (write to temp, then rename).
	tmpPath := r.cachePath + ".tmp"
	if err := os.WriteFile(tmpPath, body, 0644); err != nil {
		log.Warn("registry fetch: failed to write cache", "error", err)
		return err
	}
	if err := os.Rename(tmpPath, r.cachePath); err != nil {
		log.Warn("registry fetch: failed to rename cache", "error", err)
		os.Remove(tmpPath)
		return err
	}

	// Write meta.
	newETag := resp.Header.Get("ETag")
	meta := MetaFile{
		FetchedAt:     time.Now(),
		ETag:          newETag,
		SchemaVersion: 1,
	}
	metaData, _ := json.Marshal(meta)
	os.WriteFile(metaPath, metaData, 0644)

	// Only now, after cache and meta are persisted, swap in-memory state.
	r.mu.Lock()
	r.models = tmpModels
	r.meta = &meta
	r.loadCalled = true
	r.applyOverridesLocked()
	modelCount := len(r.models)
	r.mu.Unlock()

	log.Debug("registry fetch: successful", "models", modelCount)
	return nil
}

// ---------------------------------------------------------------------------
// Lookup methods
// ---------------------------------------------------------------------------

// Capability returns the maxOutput tokens for a model from the registry.
// Returns (value, true) if the model was found, or (0, false) for unknown models
// so callers can fall back to bundled defaults.
func (r *ModelRegistry) Capability(model string) (int, bool) {
	r.ensureLoaded()

	r.mu.RLock()
	defer r.mu.RUnlock()

	snap, ok := r.models[model]
	if !ok {
		return 0, false
	}
	return snap.maxOutput, true
}

// Pricing returns the per-token USD pricing for a model from the registry.
// Returns (pricing, true) if found, or (zero pricing, false) for unknown models.
func (r *ModelRegistry) Pricing(model string) (ModelPricing, bool) {
	r.ensureLoaded()

	r.mu.RLock()
	defer r.mu.RUnlock()

	snap, ok := r.models[model]
	if !ok {
		return ModelPricing{UnknownModel: true}, false
	}
	return snap.pricing, true
}

// ContextWindow returns the context window for a model.
func (r *ModelRegistry) ContextWindow(model string) (int, bool) {
	r.ensureLoaded()

	r.mu.RLock()
	defer r.mu.RUnlock()

	snap, ok := r.models[model]
	if !ok {
		return 0, false
	}
	return snap.contextWindow, true
}

// Modalities returns the modalities for a model.
func (r *ModelRegistry) Modalities(model string) ([]string, bool) {
	r.ensureLoaded()

	r.mu.RLock()
	defer r.mu.RUnlock()

	snap, ok := r.models[model]
	if !ok {
		return nil, false
	}
	out := make([]string, len(snap.modalities))
	copy(out, snap.modalities)
	return out, true
}

// Abilities returns the abilities for a model.
func (r *ModelRegistry) Abilities(model string) ([]string, bool) {
	r.ensureLoaded()

	r.mu.RLock()
	defer r.mu.RUnlock()

	snap, ok := r.models[model]
	if !ok {
		return nil, false
	}
	out := make([]string, len(snap.abilities))
	copy(out, snap.abilities)
	return out, true
}

// ---------------------------------------------------------------------------
// User override (config.json models key)
// ---------------------------------------------------------------------------

// ParseConfigModels parses the "models" key from config.json.
// Returns a map of model ID -> override, or an error if the JSON is malformed.
// A missing or unexpected "models" type returns a nil error with nil map.
func (r *ModelRegistry) ParseConfigModels(configData []byte) (map[string]*ModelOverride, error) {
	var raw struct {
		Models json.RawMessage `json:"models"`
	}
	if err := json.Unmarshal(configData, &raw); err != nil {
		log.Warn("registry: failed to parse config.json", "error", err)
		return nil, nil // non-fatal: return nil map so defaults work
	}

	if len(raw.Models) == 0 {
		return nil, nil
	}

	var models map[string]*ModelOverride
	if err := json.Unmarshal(raw.Models, &models); err != nil {
		log.Warn("registry: malformed models block in config.json, treating as empty", "error", err)
		return nil, nil // non-fatal: treat as empty
	}
	return models, nil
}

// ApplyUserOverrides applies model overrides from config.json.
// Overrides are applied on top of the cached registry data; they don't affect
// models that don't exist in the registry. If the cache has not been loaded yet,
// the overrides are stored and will be applied when the cache is loaded.
func (r *ModelRegistry) ApplyUserOverrides(overrides map[string]*ModelOverride) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if overrides == nil {
		return nil
	}

	r.overrides = overrides
	if r.loadCalled {
		r.applyOverridesLocked()
	}
	return nil
}

// applyOverridesLocked re-applies all user overrides on top of the base registry data.
// For models that exist in the registry, override fields patch the existing snapshot.
// For models that only exist as overrides (not in the cache), a new snapshot is created
// from the override fields so the override takes effect even without a cache hit.
// Must be called with r.mu already held (write lock).
func (r *ModelRegistry) applyOverridesLocked() {
	if len(r.overrides) == 0 {
		return
	}

	for modelID, override := range r.overrides {
		snap, ok := r.models[modelID]
		if !ok {
			// Model not in cache — create a new snapshot from the override fields.
			// Fields not specified in the override remain at their zero value;
			// callers fall back to bundled defaults for those fields.
			snap = &modelSnapshot{}
			r.models[modelID] = snap
		}

		if override.MaxOutput != nil {
			snap.maxOutput = *override.MaxOutput
		}
		if override.ContextWindow != nil {
			snap.contextWindow = *override.ContextWindow
		}
		if override.Pricing != nil {
			r.mergePricingLocked(&snap.pricing, override.Pricing)
		}
		if override.Modalities != nil {
			snap.modalities = append([]string(nil), override.Modalities...)
		}
		if override.Abilities != nil {
			snap.abilities = append([]string(nil), override.Abilities...)
		}
	}
}

// mergePricingLocked applies a PricingOverride field-by-field on top of base pricing.
// Override values are in per-token USD (same unit as base).
func (r *ModelRegistry) mergePricingLocked(base *ModelPricing, override *PricingOverride) {
	if override.InputUSD != nil {
		base.InputUSD = *override.InputUSD
	}
	if override.OutputUSD != nil {
		base.OutputUSD = *override.OutputUSD
	}
	if override.CacheReadUSD != nil {
		base.CacheReadUSD = *override.CacheReadUSD
	}
	if override.CacheCreationUSD != nil {
		base.CacheCreationUSD = *override.CacheCreationUSD
	}
}

// ---------------------------------------------------------------------------
// Lazy loading
// ---------------------------------------------------------------------------

// ensureLoaded triggers lazy loading from the cache file on first access.
// Safe to call multiple times; subsequent calls are no-ops.
func (r *ModelRegistry) ensureLoaded() {
	r.mu.RLock()
	if r.loadCalled {
		r.mu.RUnlock()
		return
	}
	r.mu.RUnlock()

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.loadCalled {
		return // double-check
	}

	if err := r.loadFromCacheLocked(); err != nil {
		log.Warn("registry: failed to load cache, starting empty", "error", err)
	}
	// Re-apply any user overrides that were set before the cache was loaded.
	// This handles both the case where the cache was just loaded and the case
	// where there was no cache file (empty models).
	r.applyOverridesLocked()
	r.loadCalled = true
}

// loadFromCache reads the cache file and parses it. Exported for testing.
func (r *ModelRegistry) loadFromCache() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.loadFromCacheLocked()
}

func (r *ModelRegistry) loadFromCacheLocked() error {
	data, err := os.ReadFile(r.cachePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // no cache yet, not an error
		}
		return err
	}

	if err := r.parseFromBytesLocked(data); err != nil {
		// Corrupt cache: rename to .broken and treat as missing
		log.Warn("registry: corrupt cache file, renaming to .broken", "path", r.cachePath, "error", err)
		brokenPath := r.cachePath + ".broken"
		if renameErr := os.Rename(r.cachePath, brokenPath); renameErr != nil {
			log.Warn("registry: failed to rename broken cache", "path", r.cachePath, "error", renameErr)
		}
		r.models = make(map[string]*modelSnapshot)
		return err
	}

	// Read meta for ETag tracking
	metaPath := r.metaPath()
	if meta, err := r.readMetaLocked(metaPath); err == nil && meta != nil {
		r.meta = meta
	}

	return nil
}

// parseFromBytes parses registry JSON from bytes. Exported for testing.
func (r *ModelRegistry) parseFromBytes(data []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.parseFromBytesLocked(data)
}

func (r *ModelRegistry) parseFromBytesLocked(data []byte) error {
	models, err := parseBodyToModels(data)
	if err != nil {
		return err
	}
	r.models = models
	r.loadCalled = true
	return nil
}

// parseBodyToModels parses registry JSON from bytes and returns the model map
// WITHOUT mutating the receiver. Used by Fetch() so in-memory state is only
// updated after the cache file is successfully written.
func parseBodyToModels(data []byte) (map[string]*modelSnapshot, error) {
	var env registryEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, err
	}

	models := make(map[string]*modelSnapshot, len(env.Models))
	for id, m := range env.Models {
		models[id] = &modelSnapshot{
			contextWindow: m.ContextWindow,
			maxOutput:     m.MaxOutput,
			pricing:       parsePricingFromRegistry(m.Pricing),
			modalities:    copyStrings(m.Modalities),
			abilities:     copyStrings(m.Abilities),
		}
	}
	return models, nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func (r *ModelRegistry) getFetchURL() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.fetchURL != "" {
		return r.fetchURL
	}
	return DefaultRegistryURL
}

func (r *ModelRegistry) metaPath() string {
	return filepath.Join(filepath.Dir(r.cachePath), "meta.json")
}

func (r *ModelRegistry) readMetaLocked(metaPath string) (*MetaFile, error) {
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, err
	}
	var meta MetaFile
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

func (r *ModelRegistry) updateMetaTimestampLocked(metaPath string) {
	meta, err := r.readMetaLocked(metaPath)
	if err != nil || meta == nil {
		return
	}
	meta.FetchedAt = time.Now()
	data, _ := json.Marshal(meta)
	os.WriteFile(metaPath, data, 0644)
}

// parsePricingFromRegistry converts registry pricing ($/1M tokens) to per-token USD.
func parsePricingFromRegistry(p registryPricing) ModelPricing {
	out := ModelPricing{}
	if p.Input != nil {
		out.InputUSD = *p.Input / 1_000_000
	}
	if p.Output != nil {
		out.OutputUSD = *p.Output / 1_000_000
	}
	if p.CacheRead != nil {
		out.CacheReadUSD = *p.CacheRead / 1_000_000
	}
	if p.CacheCreation != nil {
		out.CacheCreationUSD = *p.CacheCreation / 1_000_000
	}
	return out
}

func copyStrings(src []string) []string {
	if src == nil {
		return nil
	}
	dst := make([]string, len(src))
	copy(dst, src)
	return dst
}

// ---------------------------------------------------------------------------
// Global registry instance
// ---------------------------------------------------------------------------

var (
	globalRegistry *ModelRegistry
	registryOnce   sync.Once
)

// InitGlobalRegistry initializes the global ModelRegistry singleton with the
// given cache path. It is safe to call multiple times; subsequent calls are
// no-ops (use ResetGlobalRegistry for testing).
func InitGlobalRegistry(cachePath string) {
	registryOnce.Do(func() {
		globalRegistry = NewModelRegistry(cachePath)
	})
}

// GlobalRegistry returns the global ModelRegistry instance, or nil if not
// initialized. Callers should always check for nil before using.
func GlobalRegistry() *ModelRegistry {
	return globalRegistry
}

// ResetGlobalRegistry clears the global registry singleton for testing.
func ResetGlobalRegistry() {
	registryOnce = sync.Once{}
	globalRegistry = nil
}
