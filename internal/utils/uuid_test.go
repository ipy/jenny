package utils

import (
	"fmt"
	"testing"
	"time"
)

func TestNewUUID(t *testing.T) {
	s := NewUUID()
	if !IsValidUUID(s) {
		t.Errorf("NewUUID() = %q, want valid UUID v7", s)
	}
}

func TestNewUUIDUniqueness(t *testing.T) {
	seen := make(map[string]bool, 2000)
	for range 2000 {
		s := NewUUID()
		if seen[s] {
			t.Errorf("duplicate UUID: %s", s)
		}
		seen[s] = true
	}
}

func TestIsValidUUID(t *testing.T) {
	tests := []struct {
		name string
		s    string
		want bool
	}{
		{"valid v7", "019309af-6a74-75a3-b3c1-4a3e5f6a7b8c", true},
		{"v4 format", "019309af-6a74-4fff-b3c1-4a3e5f6a7b8c", false},
		{"empty", "", false},
		{"too short", "a-b-c", false},
		{"uppercase", "019309AF-6A74-75A3-B3C1-4A3E5F6A7B8C", false},
		{"wrong variant", "019309af-6a74-75a3-33c1-4a3e5f6a7b8c", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsValidUUID(tt.s); got != tt.want {
				t.Errorf("IsValidUUID(%q) = %v, want %v", tt.s, got, tt.want)
			}
		})
	}
}

func TestNewUUIDv7Timestamp(t *testing.T) {
	now := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	s := newUUIDv7(now)

	var hi, lo uint64
	if _, err := fmt.Sscanf(s[:13], "%08x-%04x", &hi, &lo); err != nil {
		t.Fatalf("failed to parse timestamp from %q: %v", s, err)
	}
	ts := (hi << 16) | lo
	expected := uint64(now.UnixMilli())
	if ts != expected {
		t.Errorf("timestamp = %d, want %d", ts, expected)
	}
}

func TestNewUUIDv7TimestampMonotonic(t *testing.T) {
	var lastTs uint64
	for range 100 {
		s := NewUUID()
		var hi, lo uint64
		if _, err := fmt.Sscanf(s[:13], "%08x-%04x", &hi, &lo); err != nil {
			t.Fatalf("failed to parse timestamp from %q: %v", s, err)
		}
		ts := (hi << 16) | lo
		if ts < lastTs {
			t.Errorf("timestamp went backwards: %d < %d", ts, lastTs)
		}
		lastTs = ts
	}
}
