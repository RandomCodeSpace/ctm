package cmd

import (
	"testing"
	"time"

	"github.com/RandomCodeSpace/ctm/internal/session"
)

func TestIsStale(t *testing.T) {
	now := time.Date(2026, 4, 12, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		sess *session.Session
		want bool
	}{
		{
			name: "fresh attached",
			sess: &session.Session{LastAttachedAt: now.Add(-1 * time.Hour)},
			want: false,
		},
		{
			name: "attached just under threshold",
			sess: &session.Session{LastAttachedAt: now.Add(-6 * 24 * time.Hour)},
			want: false,
		},
		{
			name: "attached just over threshold",
			sess: &session.Session{LastAttachedAt: now.Add(-8 * 24 * time.Hour)},
			want: true,
		},
		{
			name: "never attached, fresh creation",
			sess: &session.Session{CreatedAt: now.Add(-1 * time.Hour)},
			want: false,
		},
		{
			name: "never attached, old creation",
			sess: &session.Session{CreatedAt: now.Add(-30 * 24 * time.Hour)},
			want: true,
		},
		{
			name: "no timestamps at all",
			sess: &session.Session{},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isStale(tt.sess, now)
			if got != tt.want {
				t.Errorf("isStale() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHumanDuration(t *testing.T) {
	tests := []struct {
		name string
		d    time.Duration
		want string
	}{
		{"negative", -1 * time.Second, "—"},
		{"zero", 0, "0s"},
		{"seconds", 45 * time.Second, "45s"},
		{"sub-minute boundary", 59 * time.Second, "59s"},
		{"one minute", 1 * time.Minute, "1m"},
		{"minutes", 30 * time.Minute, "30m"},
		{"sub-hour boundary", 59 * time.Minute, "59m"},
		{"one hour", 1 * time.Hour, "1h"},
		{"hours", 12 * time.Hour, "12h"},
		{"sub-day boundary", 23 * time.Hour, "23h"},
		{"one day", 24 * time.Hour, "1d"},
		{"days", 7 * 24 * time.Hour, "7d"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := humanDuration(tt.d)
			if got != tt.want {
				t.Errorf("humanDuration(%v) = %q, want %q", tt.d, got, tt.want)
			}
		})
	}
}
