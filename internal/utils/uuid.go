// Package utils provides shared utility functions.
package utils

import (
	"crypto/rand"
	"fmt"
	"regexp"
	"time"
)

// v7Re matches a lowercase UUID v7 string.
var v7Re = regexp.MustCompile(
	`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`,
)

// NewUUID generates a UUID v7 string (lowercase), per RFC 9562.
func NewUUID() string {
	return newUUIDv7(time.Now())
}

// IsValidUUID reports whether s is a valid UUID v7 string.
func IsValidUUID(s string) bool {
	return v7Re.MatchString(s)
}

// newUUIDv7 generates a UUID v7 with the given timestamp.
func newUUIDv7(now time.Time) string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)

	ms := uint64(now.UnixMilli())
	b[0] = byte(ms >> 40)
	b[1] = byte(ms >> 32)
	b[2] = byte(ms >> 24)
	b[3] = byte(ms >> 16)
	b[4] = byte(ms >> 8)
	b[5] = byte(ms)

	b[6] = (b[6] & 0x0f) | 0x70
	b[8] = (b[8] & 0x3f) | 0x80

	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
