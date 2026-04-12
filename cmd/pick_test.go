package cmd

import (
	"testing"

	"github.com/RandomCodeSpace/ctm/internal/session"
)

func TestFilterSessions(t *testing.T) {
	sessions := []*session.Session{
		{Name: "alpha"},
		{Name: "beta"},
		{Name: "ctm-dev"},
		{Name: "CTM-prod"},
		{Name: "gamma"},
	}

	tests := []struct {
		name   string
		filter string
		want   []string
	}{
		{"empty filter returns all", "", []string{"alpha", "beta", "ctm-dev", "CTM-prod", "gamma"}},
		{"exact match", "beta", []string{"beta"}},
		{"substring match", "ctm", []string{"ctm-dev", "CTM-prod"}},
		{"case-insensitive match", "CTM", []string{"ctm-dev", "CTM-prod"}},
		{"no match returns empty", "zzz", []string{}},
		{"single char match", "a", []string{"alpha", "beta", "gamma"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterSessions(sessions, tt.filter)
			if len(got) != len(tt.want) {
				t.Fatalf("len(got)=%d, want %d (got=%v)", len(got), len(tt.want), names(got))
			}
			for i, s := range got {
				if s.Name != tt.want[i] {
					t.Errorf("got[%d]=%q, want %q", i, s.Name, tt.want[i])
				}
			}
		})
	}
}

func names(sessions []*session.Session) []string {
	out := make([]string, len(sessions))
	for i, s := range sessions {
		out[i] = s.Name
	}
	return out
}
