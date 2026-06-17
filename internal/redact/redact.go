package redact

import (
	"crypto/rand"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
)

// entropyThreshold is the minimum Shannon entropy (bits/char) used by
// defaultRules()'s entropy-gated rules. Most rules (generic-api-key, JWT,
// AWS secret) declare their own Entropy; this constant is the fallback used
// by code that wants to call shannonEntropy directly. See
// docs/arch/secret-redaction.md.
const entropyThreshold = 3.5

// shannonEntropy computes the Shannon entropy (bits per character) of data.
// Copied verbatim from github.com/zricethezav/gitleaks/v8/detect/utils.go
// (gitleaks keeps this helper unexported; we mirror it for entropy-based
// detection). Kept in sync with upstream so behavior matches gitleaks'
// own scoring.
func shannonEntropy(data string) float64 {
	if data == "" {
		return 0
	}
	charCounts := make(map[rune]int)
	for _, char := range data {
		charCounts[char]++
	}
	invLength := 1.0 / float64(len(data))
	var entropy float64
	for _, count := range charCounts {
		freq := float64(count) * invLength
		entropy -= freq * math.Log2(freq)
	}
	return entropy
}

// SecretRedactor detects and redacts secrets in tool results.
type SecretRedactor struct {
	mu           sync.Mutex
	mode         RedactMode
	replacements map[string]string // placeholder -> original
	secretToID   map[string]string // secret -> placeholder ID (for deduplication)
	detector     *Detector         // rule-based detector
}

// RedactMode defines the behavior of secret redaction.
type RedactMode string

const (
	// ModeDisabled disables secret redaction entirely.
	ModeDisabled RedactMode = "disabled"
	// ModeRedact enables one-way secret redaction (cannot be recovered; default).
	ModeRedact RedactMode = "redact"
	// ModeRecover enables secret redaction with recovery.
	ModeRecover RedactMode = "recover"
)

// ParseRedactMode parses a string into a RedactMode.
func ParseRedactMode(s string) RedactMode {
	switch strings.ToLower(s) {
	case "disabled", "0", "false":
		return ModeDisabled
	case "1", "true", "redact":
		return ModeRedact
	case "recover":
		return ModeRecover
	default:
		return ModeRedact
	}
}

// NewSecretRedactor creates a new SecretRedactor backed by the default
// gitleaks-aligned rule set.
func NewSecretRedactor(mode RedactMode) *SecretRedactor {
	if mode == "" {
		mode = ModeRedact
	}
	return &SecretRedactor{
		mode:         mode,
		replacements: make(map[string]string),
		secretToID:   make(map[string]string),
		detector:     DefaultDetector(),
	}
}

// Enabled returns whether redaction is active.
func (r *SecretRedactor) Enabled() bool {
	return r.mode != ModeDisabled
}

// Redact scans content for secrets and replaces them with placeholders.
// Detection is rule-based: the configured Detector (default: gitleaks-aligned
// rules with keyword prefilter, per-rule entropy, allowlist, and stop words)
// produces Findings, each of which is converted to a `[REDACTED:ID_XXXXX]`
// placeholder. Same-secret deduplication: identical secret text gets the
// same placeholder ID across calls.
func (r *SecretRedactor) Redact(content string) string {
	if r.mode == ModeDisabled {
		return content
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	findings := r.detector.Detect(content)
	if len(findings) == 0 {
		return content
	}

	// Sort by start position so we can apply placeholders left-to-right
	// without breaking offsets. Stable sort preserves rule-firing order
	// for ties.
	sort.SliceStable(findings, func(i, j int) bool {
		return findings[i].Start < findings[j].Start
	})

	// Build the result by walking findings in order. Use a cursor so we
	// can skip any part of the content that was already replaced by an
	// earlier (longer / earlier) match.
	var b strings.Builder
	cursor := 0
	for _, f := range findings {
		if f.Start < cursor {
			// Overlapping match — earlier finding already covered this range.
			// Skip to avoid double-redaction and double-counting.
			continue
		}
		b.WriteString(content[cursor:f.Start])
		b.WriteString(r.placeholderFor(f.Secret))
		cursor = f.End
	}
	b.WriteString(content[cursor:])
	return b.String()
}

// placeholderFor returns the existing placeholder for secret, or creates
// a new one and records the mapping. Caller must hold r.mu.
func (r *SecretRedactor) placeholderFor(secret string) string {
	if existingID, ok := r.secretToID[secret]; ok {
		return existingID
	}
	id := randomHex(8)
	placeholder := fmt.Sprintf("[REDACTED:%s]", id)
	r.secretToID[secret] = placeholder
	if r.mode == ModeRecover {
		r.replacements[placeholder] = secret
	}
	return placeholder
}

// randomHex generates a random hex string of n bytes.
func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}

// Recover replaces placeholders with their original values.
// Unknown placeholders are left unchanged.
func (r *SecretRedactor) Recover(content string) string {
	if r.mode != ModeRecover {
		return content
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	result := content
	for placeholder, original := range r.replacements {
		result = strings.ReplaceAll(result, placeholder, original)
	}
	return result
}

// Reset clears all stored mappings.
func (r *SecretRedactor) Reset() {
	if r.mode == ModeDisabled {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.replacements = make(map[string]string)
	r.secretToID = make(map[string]string)
}
