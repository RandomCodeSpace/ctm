package cmd

import (
	"fmt"
	"sort"
	"time"

	"github.com/spf13/cobra"
	"github.com/RandomCodeSpace/ctm/internal/config"
	"github.com/RandomCodeSpace/ctm/internal/output"
	"github.com/RandomCodeSpace/ctm/internal/session"
	"github.com/RandomCodeSpace/ctm/internal/tmux"
)

// staleSessionThreshold is the idle duration after which a session is
// flagged as STALE in `ctm ls`. Sessions never attached and older than
// this are also considered stale.
const staleSessionThreshold = 7 * 24 * time.Hour

// isStale reports whether a session should be flagged STALE.
// A session is stale if its last-attach (or creation, if never attached)
// is older than staleSessionThreshold.
func isStale(sess *session.Session, now time.Time) bool {
	ref := sess.LastAttachedAt
	if ref.IsZero() {
		ref = sess.CreatedAt
	}
	if ref.IsZero() {
		return false
	}
	return now.Sub(ref) > staleSessionThreshold
}

// humanDuration formats a duration as a short, human-friendly string.
// Examples: "5s", "3m", "2h", "4d".
func humanDuration(d time.Duration) string {
	if d < 0 {
		return "—"
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

func init() {
	rootCmd.AddCommand(listCmd)
}

var listCmd = &cobra.Command{
	Use:     "ls",
	Aliases: []string{"list"},
	Short:   "List all sessions with mode and health",
	RunE:    runList,
}

func runList(cmd *cobra.Command, args []string) error {
	out := output.Stdout()
	store := session.NewStore(config.SessionsPath())
	tc := tmux.NewClient(config.TmuxConfPath())

	sessions, err := store.List()
	if err != nil {
		return fmt.Errorf("listing sessions: %w", err)
	}

	if len(sessions) == 0 {
		out.Dim("No active sessions")
		return nil
	}

	// Sort by name for stable, predictable output
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].Name < sessions[j].Name
	})

	now := time.Now().UTC()
	for _, sess := range sessions {
		// Mode tag
		var modeTag string
		if sess.Mode == "yolo" {
			modeTag = "\033[0;35m[YOLO]\033[0m"
		} else {
			modeTag = "\033[0;32m[SAFE]\033[0m"
		}

		// Status tag
		var statusTag string
		if tc.HasSession(sess.Name) {
			statusTag = "\033[0;32m[LIVE]\033[0m"
		} else {
			statusTag = "\033[0;31m[DEAD]\033[0m"
		}

		// Stale tag — yellow if idle > threshold
		staleTag := "      "
		if isStale(sess, now) {
			staleTag = "\033[0;33m[STALE]\033[0m"
		}

		// Age = since creation; Idle = since last attach
		age := humanDuration(now.Sub(sess.CreatedAt))
		idle := "—"
		if !sess.LastAttachedAt.IsZero() {
			idle = humanDuration(now.Sub(sess.LastAttachedAt))
		}
		ageIdle := fmt.Sprintf("\033[2mage=%s idle=%s\033[0m", age, idle)

		// Workdir dimmed
		workdir := fmt.Sprintf("\033[2m%s\033[0m", sess.Workdir)

		fmt.Printf("%-20s %s %s %s  %-22s %s\n", sess.Name, modeTag, statusTag, staleTag, ageIdle, workdir)
	}

	return nil
}
