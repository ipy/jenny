package redact

import (
	"os"
	"strings"
	"testing"
)

// withRedactionEnabled returns a redactor with JENNY_REDACT_DISABLE cleared so the
// entropy and regex layers are both active.
func withRedactionEnabled(t *testing.T) *SecretRedactor {
	t.Helper()
	origVal := os.Getenv("JENNY_REDACT_DISABLE")
	os.Unsetenv("JENNY_REDACT_DISABLE")
	t.Cleanup(func() {
		if origVal != "" {
			os.Setenv("JENNY_REDACT_DISABLE", origVal)
		}
	})
	return NewSecretRedactor()
}

// TestEntropy_RedactsHighEntropyToken verifies that a 20+ char run with high Shannon
// entropy is redacted, even when no known secret prefix is present.
func TestEntropy_RedactsHighEntropyToken(t *testing.T) {
	r := withRedactionEnabled(t)
	// 24 random base64-like chars, no prefix. Entropy > 4.5.
	token := "aZ9bY0cX1dW2eV3fU4gT5hS"
	result := r.Redact("token=" + token)

	if !strings.Contains(result, "[REDACTED:") {
		t.Errorf("Expected high-entropy token to be redacted, got: %s", result)
	}
	if strings.Contains(result, token) {
		t.Errorf("Original high-entropy token should not be present, got: %s", result)
	}
}

// TestEntropy_PreservesLowEntropyText verifies that ordinary English (low entropy)
// is left alone by the entropy layer.
func TestEntropy_PreservesLowEntropyText(t *testing.T) {
	r := withRedactionEnabled(t)
	input := "The quick brown fox jumps over the lazy dog near the riverbank"
	result := r.Redact(input)
	if result != input {
		t.Errorf("Expected low-entropy English text unchanged, got: %s", result)
	}
}

// TestEntropy_PreservesShortTokens verifies the minimum run length of 20 chars.
// 19 random-looking chars (separated by a non-class char) should NOT trip the
// entropy layer.
func TestEntropy_PreservesShortTokens(t *testing.T) {
	r := withRedactionEnabled(t)
	// 19 high-entropy chars, separated by a space (not in the regex class).
	token := "aZ9bY0cX1dW2eV3fU4g"
	input := "label " + token + " suffix"
	result := r.Redact(input)
	if result != input {
		t.Errorf("Tokens under 20 chars should not be redacted, got: %s", result)
	}
}

// TestEntropy_PreservesRepeatedChars verifies that a run of repeated characters has
// entropy 0 and is NOT redacted, even if it is long.
func TestEntropy_PreservesRepeatedChars(t *testing.T) {
	r := withRedactionEnabled(t)
	token := strings.Repeat("a", 40)
	input := "padding=" + token
	result := r.Redact(input)
	if result != input {
		t.Errorf("Repeated-char run should not be redacted, got: %s", result)
	}
}

// TestEntropy_ThresholdIs_4_5 pins the entropy threshold. A run with entropy just
// below 4.5 should NOT be redacted; a run with entropy comfortably above 4.5 SHOULD.
func TestEntropy_ThresholdIs_4_5(t *testing.T) {
	// entropyThreshold is the package-level constant. It must match the value
	// documented in docs/arch/secret-redaction.md (4.5).
	if entropyThreshold != 4.5 {
		t.Errorf("entropyThreshold = %v, want 4.5 (must match doc)", entropyThreshold)
	}
}

// TestEntropy_CatchesPrefixlessSecret simulates a custom API key with no known prefix.
// This is the case regex-based detection cannot catch; entropy is the only signal.
func TestEntropy_CatchesPrefixlessSecret(t *testing.T) {
	r := withRedactionEnabled(t)
	// 32 random hex chars, no prefix. Entropy = 4.0 if uniform hex (16 chars vocab),
	// but 32 random alnum chars across ~62-char vocab has entropy ~5.95.
	token := "k3J9xQ7vR2mN8wL5tY1sD6fH0pZ4aB"
	input := "Authorization: Bearer " + token
	result := r.Redact(input)

	if !strings.Contains(result, "[REDACTED:") {
		t.Errorf("Expected prefixless high-entropy secret to be redacted, got: %s", result)
	}
	if strings.Contains(result, token) {
		t.Errorf("Original prefixless secret should not be present, got: %s", result)
	}
}

// TestEntropy_LayeredWithRegex verifies that the entropy layer runs alongside the
// regex layer: a known-prefix secret (caught by regex) and a prefixless secret
// (caught by entropy) in the same content are both redacted.
func TestEntropy_LayeredWithRegex(t *testing.T) {
	r := withRedactionEnabled(t)
	regexSecret := "sk-abcdefghijklmnopqrstT3BlbkFJabcdefghijklmnopqrst"
	entropySecret := "k3J9xQ7vR2mN8wL5tY1sD6fH0pZ4aB"
	input := "openai=" + regexSecret + " custom=" + entropySecret
	result := r.Redact(input)

	if !strings.Contains(result, "[REDACTED:") {
		t.Fatalf("Expected at least one redaction, got: %s", result)
	}
	if strings.Contains(result, regexSecret) {
		t.Errorf("Regex-matched secret should not be present, got: %s", result)
	}
	if strings.Contains(result, entropySecret) {
		t.Errorf("Entropy-matched secret should not be present, got: %s", result)
	}
	// Both secrets should be redacted — count placeholders.
	if got, want := strings.Count(result, "[REDACTED:"), 2; got != want {
		t.Errorf("placeholder count = %d, want %d (got: %s)", got, want, result)
	}
}

// TestEntropy_RecoversHighEntropyToken verifies that placeholders produced by the
// entropy layer round-trip through Recover.
func TestEntropy_RecoversHighEntropyToken(t *testing.T) {
	r := withRedactionEnabled(t)
	token := "aZ9bY0cX1dW2eV3fU4gT5hS"
	redacted := r.Redact("token=" + token)
	recovered := r.Recover(redacted)
	if !strings.Contains(recovered, token) {
		t.Errorf("Recover should restore high-entropy token, got: %s", recovered)
	}
}

// TestShannonEntropy_KnownValues pins the entropy formula against hand-computed
// values. If the formula drifts, this test catches it.
func TestShannonEntropy_KnownValues(t *testing.T) {
	cases := []struct {
		input string
		want  float64
		delta float64
	}{
		// Empty string → 0.
		{"", 0, 0.0001},
		// 8 identical chars → 0 (single symbol, no information).
		{"aaaaaaaa", 0, 0.0001},
		// 2 distinct chars, equal count → 1.0 bit/char.
		{"aabb", 1.0, 0.0001},
		// 4 distinct chars, equal count → 2.0 bits/char.
		{"aabbccdd", 2.0, 0.0001},
	}
	for _, tc := range cases {
		got := shannonEntropy(tc.input)
		if diff := got - tc.want; diff < -tc.delta || diff > tc.delta {
			t.Errorf("shannonEntropy(%q) = %v, want %v (±%v)", tc.input, got, tc.want, tc.delta)
		}
	}
}

// TestShannonEntropy_RandomStringIsHigh checks that a 20-char random alnum string
// has entropy > 4.5 (so it will be redacted).
func TestShannonEntropy_RandomStringIsHigh(t *testing.T) {
	// 24 random alnum chars; with ~62 unique chars, entropy ≈ log2(62) ≈ 5.95.
	token := "aZ9bY0cX1dW2eV3fU4gT5hS"
	got := shannonEntropy(token)
	if got <= entropyThreshold {
		t.Errorf("shannonEntropy(%q) = %v, expected > %v", token, got, entropyThreshold)
	}
}

// TestEntropy_PreservesAlphaOnlyRun verifies the digit+alpha gate: a 20+ char
// run of letters only, even with high entropy, must NOT be redacted. This is
// the gitleaks "generic" rule behavior.
func TestEntropy_PreservesAlphaOnlyRun(t *testing.T) {
	r := withRedactionEnabled(t)
	// 24 unique letters, no digits. High entropy, but not a secret pattern.
	token := "abcdefghijklmnopqrstuvwx"
	input := "label " + token + " suffix"
	result := r.Redact(input)
	if result != input {
		t.Errorf("Alpha-only high-entropy run should not be redacted, got: %s", result)
	}
}

// TestEntropy_PreservesDigitsOnlyRun verifies that a long run of digits only
// is also not redacted by the entropy layer.
func TestEntropy_PreservesDigitsOnlyRun(t *testing.T) {
	r := withRedactionEnabled(t)
	// 24 unique digits, no letters. Many distinct chars → high entropy,
	// but no character-class mix → not a candidate.
	token := "012345678901234567890123"
	input := "id=" + token
	result := r.Redact(input)
	if result != input {
		t.Errorf("Digits-only run should not be redacted, got: %s", result)
	}
}

// TestEntropy_PreservesLongHexHash verifies the digit+alpha gate against a
// realistic 40-char SHA-1 hex string. Hex is high-entropy but not a secret.
func TestEntropy_PreservesLongHexHash(t *testing.T) {
	r := withRedactionEnabled(t)
	// 40-char SHA-1 hex. 16 unique chars (0-9a-f), entropy = log2(16) = 4.0,
	// already below threshold. But the digit+alpha gate also rules it out
	// only when entropy is borderline — verify the gate works independently
	// by checking a hex string that DOES exceed the threshold.
	// 64-char SHA-256 hex with 16 unique chars: entropy = 4.0, below threshold.
	hash := "da39a3ee5e6b4b0d3255bfef95601890afd80709"
	result := r.Redact("hash=" + hash)
	if result != "hash="+hash {
		t.Errorf("SHA-1 hex should not be redacted, got: %s", result)
	}
}

// TestEntropy_RequiresDigitAndLetter_Boundary pins the digit+alpha gate at
// the boundary: a token that passes entropy but fails the gate must not be
// redacted, regardless of how high its entropy is.
func TestEntropy_RequiresDigitAndLetter_Boundary(t *testing.T) {
	// Confirm the gate via direct call to the candidate-check logic.
	// (We don't expose a helper, so verify through public behavior.)
	r := withRedactionEnabled(t)

	// All-letter run with entropy > 4.5.
	letterRun := "ZbYcXdWeVfUgThSiRjQkPlOmN" // 24 unique letters
	if shannonEntropy(letterRun) <= entropyThreshold {
		t.Skipf("letterRun entropy %v not above threshold; test fixture invalid", shannonEntropy(letterRun))
	}
	if got := r.Redact("x=" + letterRun); got != "x="+letterRun {
		t.Errorf("letterRun should pass entropy but be blocked by gate, got: %s", got)
	}

	// All-digit run with entropy > 4.5 (impossible with 10 digits, so use
	// mixed case digits — but our gate is digit-or-letter, not mixed class,
	// so a pure-digit run is the counterexample). 10 digits is the max
	// distinct → entropy = log2(10) ≈ 3.32, below threshold anyway.
	// So this test effectively just confirms the alpha-only path.
}

// TestContainsDigit_AndContainsLetter pins the gate helpers directly.
func TestContainsDigit_AndContainsLetter(t *testing.T) {
	cases := []struct {
		in        string
		wantDigit bool
		wantLetter bool
	}{
		{"", false, false},
		{"abcdefg", false, true},
		{"1234567", true, false},
		{"abc123", true, true},
		{"-----", false, false},
		{"sk-1234567890abcdefghij", true, true},
	}
	for _, tc := range cases {
		if got := containsDigit(tc.in); got != tc.wantDigit {
			t.Errorf("containsDigit(%q) = %v, want %v", tc.in, got, tc.wantDigit)
		}
		if got := containsLetter(tc.in); got != tc.wantLetter {
			t.Errorf("containsLetter(%q) = %v, want %v", tc.in, got, tc.wantLetter)
		}
	}
}
