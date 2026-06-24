// Package config provides external model registry fetching, caching, and lookup.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/ipy/jenny/internal/log"
	"github.com/knadh/koanf/v2"
)

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
	FetchedAt     string `json:"fetchedAt"`
	ETag          string `json:"etag"`
	SchemaVersion int    `json:"schemaVersion"`
	ModelsVersion string `json:"modelsVersion,omitempty"`
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

// PricingOverride allows partial field-level overrides from settings.json.
// JSON tags match the upstream registry format (camelCase), consistent with
// how the models key in settings.json mirrors the registry schema directly.
type PricingOverride struct {
	InputUSD         *float64 `json:"input"`
	OutputUSD        *float64 `json:"output"`
	CacheReadUSD     *float64 `json:"cacheRead"`
	CacheCreationUSD *float64 `json:"cacheCreation"`
}

// ModelOverride defines user-configurable overrides for a single model.
// JSON tags match the upstream registry format (camelCase), not snake_case,
// so that settings.json models blocks mirror the registry schema directly.
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
// Resolution order: user settings.json override > upstream snapshot > (caller's bundled defaults).
// The cache file is maintained by the update package (CDN download + SHA verification).
type ModelRegistry struct {
	mu       sync.RWMutex
	reloadMu sync.Mutex // serializes Reload calls

	// Configuration
	cachePath string
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
		models:     make(map[string]*modelSnapshot),
		overrides:  make(map[string]*ModelOverride),
		loadCalled: false,
	}
}

// SetOffline enables or disables offline mode.
func (r *ModelRegistry) SetOffline(offline bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.offline = offline
}

// IsOffline returns whether offline mode is active.
func (r *ModelRegistry) IsOffline() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.offline
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
// User override (settings.json models key)
// ---------------------------------------------------------------------------

// ParseConfigModels parses the "models" key from settings.json.
// Returns a map of model ID -> override, or an error if the JSON is malformed.
// A missing or unexpected "models" type returns a nil error with nil map.
func (r *ModelRegistry) ParseConfigModels(configData []byte) (map[string]*ModelOverride, error) {
	var raw struct {
		Models json.RawMessage `json:"models"`
	}
	if err := json.Unmarshal(configData, &raw); err != nil {
		log.Warn("registry: failed to parse settings.json", "error", err)
		return nil, err
	}

	if len(raw.Models) == 0 {
		return nil, nil
	}

	var models map[string]*ModelOverride
	if err := json.Unmarshal(raw.Models, &models); err != nil {
		log.Warn("registry: malformed models block in settings.json, treating as empty", "error", err)
		return nil, err
	}
	return models, nil
}

// ParseConfigModelsFromKoanf extracts the "models" key from a koanf instance
// that has already loaded settings.json. It produces the same result as
// ParseConfigModels would for the same JSON content.
// Returns (nil, nil) when the "models" key is absent or not a valid object.
func (r *ModelRegistry) ParseConfigModelsFromKoanf(k *koanf.Koanf) (map[string]*ModelOverride, error) {
	if k == nil {
		return nil, nil
	}

	raw := k.Get("models")
	if raw == nil {
		return nil, nil
	}

	// koanf parses JSON objects as map[string]interface{}.
	modelsMap, ok := raw.(map[string]interface{})
	if !ok {
		log.Warn("registry: models key in settings.json is not a JSON object, treating as empty")
		return nil, nil
	}

	// Marshal the raw value back to JSON, then unmarshal into the target type.
	// This round-trip ensures identical semantics to ParseConfigModels (JSON -> bytes -> struct).
	data, err := json.Marshal(modelsMap)
	if err != nil {
		log.Warn("registry: failed to marshal models from koanf", "error", err)
		return nil, err
	}

	var models map[string]*ModelOverride
	if err := json.Unmarshal(data, &models); err != nil {
		log.Warn("registry: malformed models block in settings.json, treating as empty", "error", err)
		return nil, nil
	}
	return models, nil
}

// ApplyUserOverrides applies model overrides from settings.json.
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
	r.applyOverridesLocked()
	r.loadCalled = true
}

// Reload re-reads the cache file from disk and refreshes in-memory state.
// Called by the update package after it writes a new models.json to disk.
func (r *ModelRegistry) Reload() error {
	r.reloadMu.Lock()
	defer r.reloadMu.Unlock()

	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.loadFromCacheLocked(); err != nil {
		return err
	}
	r.applyOverridesLocked()
	return nil
}

// MetaPath returns the path to the meta.json file for external consumers.
func (r *ModelRegistry) MetaPath() string {
	return r.metaPath()
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
		log.Warn("registry: corrupt cache file, renaming to .broken", "path", r.cachePath, "error", err)
		brokenPath := r.cachePath + ".broken"
		if renameErr := os.Rename(r.cachePath, brokenPath); renameErr != nil {
			log.Warn("registry: failed to rename broken cache", "path", r.cachePath, "error", renameErr)
		}
		r.models = make(map[string]*modelSnapshot)
		return err
	}

	// Read meta
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

// ParseFromBytes parses registry JSON from bytes. Exported for cross-package testing.
func (r *ModelRegistry) ParseFromBytes(data []byte) error {
	return r.parseFromBytes(data)
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

// parseBodyToModels parses registry JSON from bytes and returns the model map.
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

// SetGlobalRegistryForTest injects a pre-built registry for testing.
func SetGlobalRegistryForTest(r *ModelRegistry) {
	registryOnce = sync.Once{}
	globalRegistry = r
}
