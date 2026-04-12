package cmd

import "testing"

func TestSanitizeSessionID(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"valid uuid", "f6489cb4-010f-4c96-940b-188014f746f0", "f6489cb4-010f-4c96-940b-188014f746f0"},
		{"simple alnum", "abc123", "abc123"},
		{"underscore ok", "a_b_c", "a_b_c"},
		{"dash ok", "a-b-c", "a-b-c"},
		{"path traversal attempt", "../../etc/passwd", "------etc-passwd"},
		{"absolute path", "/etc/passwd", "-etc-passwd"},
		{"dot", "a.b.c", "a-b-c"},
		{"spaces", "a b c", "a-b-c"},
		{"null byte", "abc\x00def", "abc-def"},
		{"empty string", "", "unknown"},
		{"only invalid chars", "////", "----"},
		{"really long", string(make([]byte, 200)), "unknown"}, // >128 after sanitize → fallback
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeSessionID(tt.in)
			if got != tt.want {
				t.Errorf("sanitizeSessionID(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestSanitizeSessionIDNoEmptyResult(t *testing.T) {
	// Sanity: never returns "" — either a clean id or "unknown".
	inputs := []string{"", "...", "/////", string(make([]byte, 500))}
	for _, in := range inputs {
		got := sanitizeSessionID(in)
		if got == "" {
			t.Errorf("sanitizeSessionID(%q) returned empty string", in)
		}
	}
}
