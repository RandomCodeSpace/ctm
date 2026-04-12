package cmd

import (
	"testing"
	"time"

	"github.com/RandomCodeSpace/ctm/internal/session"
)

func TestHealthCacheValid(t *testing.T) {
	tests := []struct {
		name     string
		sess     *session.Session
		expected bool
	}{
		{
			name:     "zero time = invalid",
			sess:     &session.Session{LastHealthAt: time.Time{}, LastHealthStatus: "ok"},
			expected: false,
		},
		{
			name:     "fresh ok = valid",
			sess:     &session.Session{LastHealthAt: time.Now().Add(-10 * time.Second), LastHealthStatus: "ok"},
			expected: true,
		},
		{
			name:     "fresh recovered = valid",
			sess:     &session.Session{LastHealthAt: time.Now().Add(-10 * time.Second), LastHealthStatus: "recovered"},
			expected: true,
		},
		{
			name:     "fresh recreated = valid",
			sess:     &session.Session{LastHealthAt: time.Now().Add(-10 * time.Second), LastHealthStatus: "recreated"},
			expected: true,
		},
		{
			name:     "stale ok = invalid",
			sess:     &session.Session{LastHealthAt: time.Now().Add(-2 * time.Minute), LastHealthStatus: "ok"},
			expected: false,
		},
		{
			name:     "fresh fail = invalid",
			sess:     &session.Session{LastHealthAt: time.Now().Add(-10 * time.Second), LastHealthStatus: "fail"},
			expected: false,
		},
		{
			name:     "fresh empty status = invalid",
			sess:     &session.Session{LastHealthAt: time.Now().Add(-10 * time.Second), LastHealthStatus: ""},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := healthCacheValid(tt.sess)
			if got != tt.expected {
				t.Errorf("healthCacheValid() = %v, want %v", got, tt.expected)
			}
		})
	}
}
