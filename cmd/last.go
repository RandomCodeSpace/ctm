package cmd

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"
	"github.com/RandomCodeSpace/ctm/internal/config"
	"github.com/RandomCodeSpace/ctm/internal/output"
	"github.com/RandomCodeSpace/ctm/internal/session"
	"github.com/RandomCodeSpace/ctm/internal/tmux"
)

func init() {
	rootCmd.AddCommand(lastCmd)
}

var lastCmd = &cobra.Command{
	Use:     "last",
	Aliases: []string{"l"},
	Short:   "Attach to the most recently used session",
	Long:    "Attach to the session with the highest LastAttachedAt timestamp. Mobile-friendly one-word reconnect.",
	RunE:    runLast,
}

// sortByMostRecent sorts sessions by LastAttachedAt descending, with name
// as a tie-breaker. This makes the never-attached fallback deterministic.
func sortByMostRecent(sessions []*session.Session) {
	sort.SliceStable(sessions, func(i, j int) bool {
		if !sessions[i].LastAttachedAt.Equal(sessions[j].LastAttachedAt) {
			return sessions[i].LastAttachedAt.After(sessions[j].LastAttachedAt)
		}
		return sessions[i].Name < sessions[j].Name
	})
}

func runLast(cmd *cobra.Command, args []string) error {
	out := output.Stderr()
	store := session.NewStore(config.SessionsPath())
	tc := tmux.NewClient(config.TmuxConfPath())

	sessions, err := store.List()
	if err != nil {
		return fmt.Errorf("listing sessions: %w", err)
	}

	if len(sessions) == 0 {
		out.Dim("No sessions yet. Create one with: ctm new <name>")
		return nil
	}

	sortByMostRecent(sessions)

	// Prefer the most recent LIVE session. If none are live, fall back to
	// the most recent metadata entry (runAttach will recreate the tmux side).
	var chosen *session.Session
	for _, s := range sessions {
		if tc.HasSession(s.Name) {
			chosen = s
			break
		}
	}
	if chosen == nil {
		chosen = sessions[0]
		out.Dim("No live sessions — falling back to %q (most recent metadata)", chosen.Name)
	}

	if chosen.LastAttachedAt.IsZero() {
		out.Dim("No session has been attached yet — picking %q", chosen.Name)
	}

	return runAttach(cmd, []string{chosen.Name})
}
