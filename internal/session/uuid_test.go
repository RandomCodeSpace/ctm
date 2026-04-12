package session

import (
	"regexp"
	"testing"
)

func TestNewUUIDv4Format(t *testing.T) {
	re := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	for i := 0; i < 200; i++ {
		id := newUUIDv4()
		if !re.MatchString(id) {
			t.Fatalf("uuid %q does not match RFC 4122 v4 format", id)
		}
	}
}

func TestNewUUIDv4Uniqueness(t *testing.T) {
	seen := make(map[string]struct{}, 1000)
	for i := 0; i < 1000; i++ {
		id := newUUIDv4()
		if _, ok := seen[id]; ok {
			t.Fatalf("duplicate uuid generated: %q", id)
		}
		seen[id] = struct{}{}
	}
}

func TestNewUUIDv4Version(t *testing.T) {
	// Character at position 14 (0-indexed) must always be '4' for v4.
	id := newUUIDv4()
	if id[14] != '4' {
		t.Errorf("uuid %q: expected version char '4' at position 14, got %q", id, string(id[14]))
	}
}

func TestNewUUIDv4Variant(t *testing.T) {
	// Character at position 19 (0-indexed) must be one of 8, 9, a, b for RFC 4122.
	id := newUUIDv4()
	variant := id[19]
	valid := variant == '8' || variant == '9' || variant == 'a' || variant == 'b'
	if !valid {
		t.Errorf("uuid %q: expected variant char [89ab] at position 19, got %q", id, string(variant))
	}
}
