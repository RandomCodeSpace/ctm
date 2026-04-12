package session

import (
	"crypto/rand"
	"fmt"
)

// newUUIDv4 generates a canonical RFC 4122 v4 UUID string using crypto/rand.
// Format: 8-4-4-4-12 lowercase hex digits with hyphens.
// Panics on crypto/rand failure — the OS RNG not being readable is unrecoverable.
func newUUIDv4() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("session: crypto/rand failed: %v", err))
	}
	// Version 4 (random): bits 12-15 of time_hi_and_version → 0100
	b[6] = (b[6] & 0x0f) | 0x40
	// Variant RFC 4122: bits 6-7 of clock_seq_hi_and_reserved → 10
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
