package hermes

import "testing"

func TestBuildCommand(t *testing.T) {
	tests := []struct {
		name           string
		agentSessionID string
		mode           string
		resume         bool
		envExports     string
		want           string
	}{
		{
			name: "fresh-safe",
			mode: "safe", resume: false,
			want: "hermes --tui",
		},
		{
			name: "fresh-yolo",
			mode: "yolo", resume: false,
			want: "hermes --tui --yolo",
		},
		{
			name:           "resume-with-id-safe",
			agentSessionID: "20260515_152727_9da209",
			mode:           "safe", resume: true,
			want: "hermes --tui --resume '20260515_152727_9da209' || hermes --tui",
		},
		{
			name:           "resume-with-id-yolo",
			agentSessionID: "20260515_152727_9da209",
			mode:           "yolo", resume: true,
			want: "hermes --tui --resume '20260515_152727_9da209' --yolo || hermes --tui --yolo",
		},
		{
			name: "resume-no-id-safe",
			mode: "safe", resume: true,
			want: "hermes --tui -c || hermes --tui",
		},
		{
			name: "resume-no-id-yolo",
			mode: "yolo", resume: true,
			want: "hermes --tui -c --yolo || hermes --tui --yolo",
		},
		{
			name: "env-prelude-fresh-safe",
			mode: "safe", resume: false,
			envExports: "export FOO='bar'",
			want:       "export FOO='bar'; hermes --tui",
		},
		{
			name:           "env-prelude-resume-yolo",
			agentSessionID: "id1",
			mode:           "yolo", resume: true,
			envExports: "export FOO='bar'",
			want:       "export FOO='bar'; hermes --tui --resume 'id1' --yolo || hermes --tui --yolo",
		},
		{
			name:           "shell-quote-escapes-single-quote",
			agentSessionID: `weird'id`,
			mode:           "safe", resume: true,
			want: `hermes --tui --resume 'weird'\''id' || hermes --tui`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildCommand(tt.agentSessionID, tt.mode, tt.resume, tt.envExports)
			if got != tt.want {
				t.Errorf("BuildCommand(%q, %q, %v, %q)\n got: %q\nwant: %q",
					tt.agentSessionID, tt.mode, tt.resume, tt.envExports, got, tt.want)
			}
		})
	}
}

func TestShellQuote(t *testing.T) {
	tests := []struct{ in, want string }{
		{"abc", "'abc'"},
		{"", "''"},
		{`a'b`, `'a'\''b'`},
		{`'`, `''\'''`},
	}
	for _, tt := range tests {
		if got := shellQuote(tt.in); got != tt.want {
			t.Errorf("shellQuote(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
