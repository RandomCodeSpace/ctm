package cmd

import (
	"testing"

	"github.com/RandomCodeSpace/ctm/internal/session"
)

func TestShouldResumeExisting(t *testing.T) {
	tests := []struct {
		name          string
		sess          *session.Session
		requestedMode string
		want          bool
	}{
		{
			name:          "nil session never resumes",
			sess:          nil,
			requestedMode: "yolo",
			want:          false,
		},
		{
			name:          "yolo session + yolo request = resume",
			sess:          &session.Session{Mode: "yolo"},
			requestedMode: "yolo",
			want:          true,
		},
		{
			name:          "safe session + safe request = resume",
			sess:          &session.Session{Mode: "safe"},
			requestedMode: "safe",
			want:          true,
		},
		{
			name:          "safe session + yolo request = recreate",
			sess:          &session.Session{Mode: "safe"},
			requestedMode: "yolo",
			want:          false,
		},
		{
			name:          "yolo session + safe request = recreate",
			sess:          &session.Session{Mode: "yolo"},
			requestedMode: "safe",
			want:          false,
		},
		{
			// Regression: previously this case required tc.HasSession(name) to
			// also be true, which caused `ctm yolo <name>` after the agent exited
			// (tmux dies with the agent) to drop the stored UUID and start fresh.
			name:          "yolo session whose tmux died still resumes",
			sess:          &session.Session{Mode: "yolo"},
			requestedMode: "yolo",
			want:          true,
		},
		{
			name:          "empty mode never matches",
			sess:          &session.Session{Mode: ""},
			requestedMode: "yolo",
			want:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldResumeExisting(tt.sess, tt.requestedMode)
			if got != tt.want {
				t.Errorf("shouldResumeExisting(%+v, %q) = %v, want %v",
					tt.sess, tt.requestedMode, got, tt.want)
			}
		})
	}
}
