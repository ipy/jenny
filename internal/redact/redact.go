package redact

import (
	"fmt"
	"math"
	"os"
	"regexp"
	"strings"
	"sync"
)

// entropyThreshold is the minimum Shannon entropy (bits/char) for a token run to be
// considered a candidate secret. Matches gitleaks' per-rule default. See
// docs/arch/secret-redaction.md.
const entropyThreshold = 4.5

// entropyTokenRegex matches runs of base64/hex-like characters long enough to be
// potential secrets. Alphanumeric plus URL-safe base64 padding/separator chars.
var entropyTokenRegex = regexp.MustCompile(`[A-Za-z0-9+/=_\-]{20,}`)

// shannonEntropy computes the Shannon entropy (bits per character) of data.
// Copied verbatim from github.com/zricethezav/gitleaks/v8/detect/utils.go
// (gitleaks keeps this helper unexported; we mirror it for entropy-based detection).
// Kept in sync with upstream so behavior matches gitleaks' own scoring.
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

// containsDigit reports whether s has at least one ASCII digit (0-9).
// Mirrors the digit-requirement gitleaks applies to "generic" rules
// (see gitleaks/v8/detect/utils.go: containsDigit). Used by the entropy
// layer to avoid redacting long alphabetic runs that have high entropy
// but are not secrets (e.g. prose, identifiers, repeated patterns).
func containsDigit(s string) bool {
	for i := 0; i < len(s); i++ {
		if c := s[i]; c >= '0' && c <= '9' {
			return true
		}
	}
	return false
}

// containsLetter reports whether s has at least one ASCII letter (a-zA-Z).
// Paired with containsDigit to require the candidate token to mix character
// classes — most real secrets do, while long base64-style or hex-only runs
// of a single class are common in non-secret data (paths, hashes, ids).
func containsLetter(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			return true
		}
	}
	return false
}

// secretPattern represents a regex pattern for detecting secrets.
type secretPattern struct {
	pattern *regexp.Regexp
}

// Built-in regex patterns for common secret types not covered by gitleaks default.
var additionalPatterns = []secretPattern{
	// OpenAI API key - sk- prefix followed by base64-like characters
	{pattern: regexp.MustCompile(`\b(sk-[A-Za-z0-9]{20,})\b`)},
	// GitHub Personal Access Token (classic) - ghp_ prefix followed by 36-40 alphanumeric chars
	{pattern: regexp.MustCompile(`\b(ghp_[A-Za-z0-9]{36,40})\b`)},
	// GitHub OAuth - gho_ prefix followed by 36-40 alphanumeric chars
	{pattern: regexp.MustCompile(`\b(gho_[A-Za-z0-9]{36,40})\b`)},
	// GitHub fine-grained PAT - github_pat_ prefix
	{pattern: regexp.MustCompile(`\b(github_pat_[A-Za-z0-9_]{50,})\b`)},
	// GitHub refresh token - ghr_ prefix followed by 36-40 chars
	{pattern: regexp.MustCompile(`\b(ghr_[A-Za-z0-9]{36,40})\b`)},
	// AWS Access Key ID - AKIA prefix followed by 16 alphanumeric chars
	{pattern: regexp.MustCompile(`\b(AKIA[A-Za-z0-9]{16})\b`)},
	// AWS Secret Access Key - typically 40 chars of mixed alphanumeric/special
	{pattern: regexp.MustCompile(`\b(AWS|[Aa]ws|aws)[^"\']{0,20}([A-Za-z0-9/+=]{40})\b`)},
	// Slack token - xox[baprs]- prefix
	{pattern: regexp.MustCompile(`\b(xox[baprs]-[A-Za-z0-9-]{10,48})\b`)},
	// NPM token - npm_ prefix followed by various lengths
	{pattern: regexp.MustCompile(`\b(npm_[A-Za-z0-9]{30,})\b`)},
	// PyPI token - pypi_ prefix
	{pattern: regexp.MustCompile(`\b(pypi_[A-Za-z0-9_]{50,})\b`)},
	// SSH private keys - PEM format with BEGIN/END markers
	// Matches OPENSSH, RSA, DSA, EC, and other PEM private key formats
	{pattern: regexp.MustCompile(`-----BEGIN[ A-Z]*PRIVATE KEY-----[\s\S]*?-----END[ A-Z]*PRIVATE KEY-----`)},
}

// SecretRedactor detects and redacts secrets in tool results.
type SecretRedactor struct {
	mu           sync.Mutex
	enabled      bool
	counter      int
	replacements map[string]string // placeholder -> original
	secretToID   map[string]string // secret -> placeholder ID (for deduplication)
}

// NewSecretRedactor creates a new SecretRedactor.
// Enabled by default unless JENNY_REDACT_DISABLE=1 is set.
func NewSecretRedactor() *SecretRedactor {
	enabled := os.Getenv("JENNY_REDACT_DISABLE") == ""
	return &SecretRedactor{
		enabled:      enabled,
		replacements: make(map[string]string),
		secretToID:   make(map[string]string),
	}
}

// Enabled returns whether redaction is active.
func (r *SecretRedactor) Enabled() bool {
	return r.enabled
}

// Redact scans content for secrets and replaces them with placeholders.
// Returns the content with all detected secrets replaced.
//
// Detection layers (in order):
//  1. Shannon entropy — catches high-entropy tokens with no known prefix
//     (gitleaks' algorithm; see shannonEntropy for attribution).
//  2. Regex patterns — catches structured secrets (OpenAI, GitHub, AWS, etc.)
//     that entropy may flag inconsistently.
func (r *SecretRedactor) Redact(content string) string {
	if !r.enabled {
		return content
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	result := content

	// Layer 1: entropy-based detection. Runs first so prefix-less high-entropy
	// tokens (custom API keys, random passwords) are caught even when no regex
	// matches.
	result = r.replaceWithEntropy(result)

	// Layer 2: regex patterns for structured secret formats.
	for _, sp := range additionalPatterns {
		result = r.replaceWithPattern(result, sp.pattern)
	}

	return result
}

// replaceWithEntropy scans content for high-entropy token runs and replaces each
// candidate with a placeholder. A run is a candidate when ALL of the following hold:
//
//  1. It is at least 20 characters long (entropyTokenRegex).
//  2. Its Shannon entropy exceeds entropyThreshold (4.5 bits/char).
//  3. It contains at least one ASCII digit AND at least one ASCII letter
//     (the "digit+alpha gate"). This mirrors gitleaks' treatment of
//     "generic" rules and avoids redacting long alphabetic or numeric
//     runs that have high entropy but are not secrets — e.g. prose,
//     identifiers, repeated patterns, hex-only hashes.
//
// All three gates must pass. This makes the layer stable against common
// non-secret content (long identifiers, repeated-char padding) while still
// catching the typical custom-API-key / random-password case.
func (r *SecretRedactor) replaceWithEntropy(content string) string {
	matches := entropyTokenRegex.FindAllStringIndex(content, -1)
	if len(matches) == 0 {
		return content
	}

	result := content
	// Process matches in reverse order to preserve positions.
	for i := len(matches) - 1; i >= 0; i-- {
		match := matches[i]
		if len(match) < 2 {
			continue
		}
		token := content[match[0]:match[1]]
		if shannonEntropy(token) <= entropyThreshold {
			continue
		}
		if !containsDigit(token) || !containsLetter(token) {
			continue
		}

		var placeholder string
		if existingID, ok := r.secretToID[token]; ok {
			placeholder = existingID
		} else {
			r.counter++
			placeholder = fmt.Sprintf("[REDACTED:ID_%05d]", r.counter)
			r.secretToID[token] = placeholder
			r.replacements[placeholder] = token
		}
		result = result[:match[0]] + placeholder + result[match[1]:]
	}
	return result
}

// replaceWithPattern finds all matches of the pattern and replaces them with placeholders.
func (r *SecretRedactor) replaceWithPattern(content string, pattern *regexp.Regexp) string {
	matches := pattern.FindAllStringSubmatchIndex(content, -1)
	if len(matches) == 0 {
		return content
	}

	result := content
	// Process matches in reverse order to preserve positions
	// Precondition fix: len(match) must be >= 2 to access match[0] and match[1]
	// (match[0]=start, match[1]=end of full match; groups start at match[2])
	for i := len(matches) - 1; i >= 0; i-- {
		match := matches[i]
		if len(match) < 2 {
			continue
		}
		// Extract the full match (not just a subgroup)
		secret := content[match[0]:match[1]]

		// Generate placeholder
		var placeholder string
		if existingID, ok := r.secretToID[secret]; ok {
			placeholder = existingID
		} else {
			r.counter++
			placeholder = fmt.Sprintf("[REDACTED:ID_%05d]", r.counter)
			r.secretToID[secret] = placeholder
			r.replacements[placeholder] = secret
		}

		// Replace in result
		result = result[:match[0]] + placeholder + result[match[1]:]
	}

	return result
}

// Recover replaces placeholders with their original values.
// Unknown placeholders are left unchanged.
func (r *SecretRedactor) Recover(content string) string {
	if !r.enabled {
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

// Reset clears all stored mappings and resets the counter.
func (r *SecretRedactor) Reset() {
	if !r.enabled {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.replacements = make(map[string]string)
	r.secretToID = make(map[string]string)
	r.counter = 0
}
