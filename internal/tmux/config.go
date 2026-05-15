package tmux

import (
	"fmt"
	"os"
	"path/filepath"
)

// GenerateConfig writes a mobile-optimized tmux.conf to path.
func GenerateConfig(path string, scrollbackLines int) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	content := fmt.Sprintf(`# --- CTM Managed Config - Do Not Edit Manually ---
set -g mouse on
set -g history-limit %d
set -g status-position top
set -g status-style "bg=default,fg=white"
set -g status-left "[#S] "
set -g status-right "#{?client_prefix,CMD,}"
set -g status-left-length 20
set -g status-right-length 10

# --- Mobile-friendly keybindings (Termius & WebSSH on iOS) ---
# Ctrl is buried on iOS keyboards; Alt (Option) is easy to reach.
# Alt-a as a second prefix (Ctrl-b still works for desktop muscle memory).
set -g prefix2 M-a
# Alt-[ enters copy mode with no prefix — pair with a Snippet for one-tap scroll.
bind -n M-[ copy-mode
# Alt-d detaches the client (already used).
bind -n M-d detach-client

set -g default-terminal "tmux-256color"
set -ga terminal-overrides ",*256col*:Tc,xterm*:Tc"
set -g allow-rename off
set -sg escape-time 10
set -g monitor-activity on
set -g visual-activity off
# Focus events: lets apps inside tmux (codex, vim) see focus-in/out events,
# which improves redraw behavior and avoids stale cursor state on reattach.
set -g focus-events on
# OSC52: sync tmux copy-mode selections to system clipboard.
set -g set-clipboard on
`, scrollbackLines)

	return os.WriteFile(path, []byte(content), 0644)
}
