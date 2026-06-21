package utils

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"strings"
)

// EncodeString JSON-encodes a string. Equivalent to json.Marshal(s).
func EncodeString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// GenerateShortID generates a short random hex ID for synthetic message IDs.
func GenerateShortID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}

// EscapeShellArg escapes a string for use in a shell argument using single-quote wrapping.
func EscapeShellArg(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
