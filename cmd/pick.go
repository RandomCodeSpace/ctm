package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/RandomCodeSpace/ctm/internal/config"
	"github.com/RandomCodeSpace/ctm/internal/output"
	"github.com/RandomCodeSpace/ctm/internal/session"
	"github.com/RandomCodeSpace/ctm/internal/tmux"
)

func init() {
	rootCmd.AddCommand(pickCmd)
}

var pickCmd = &cobra.Command{
	Use:     "pick [filter]",
	Aliases: []string{"p"},
	Short:   "Interactive session picker (mobile-friendly)",
	Long: "Show a numbered list of sessions and attach to the selected one. " +
		"Inside tmux, uses tmux choose-session. An optional filter substring " +
		"narrows the list; a single match auto-attaches.",
	Args: cobra.MaximumNArgs(1),
	RunE: runPick,
}

// filterSessions returns sessions whose name contains the (case-insensitive)
// filter substring. An empty filter returns the input unchanged.
//
// ASCII-only: session names are constrained by session.ValidateName to
// ^[a-zA-Z0-9][a-zA-Z0-9_-]+$, so strings.ToLower is sufficient and Unicode
// case-folding is unnecessary.
func filterSessions(sessions []*session.Session, filter string) []*session.Session {
	if filter == "" {
		return sessions
	}
	needle := strings.ToLower(filter)
	out := make([]*session.Session, 0, len(sessions))
	for _, s := range sessions {
		if strings.Contains(strings.ToLower(s.Name), needle) {
			out = append(out, s)
		}
	}
	return out
}

func runPick(cmd *cobra.Command, args []string) error {
	tc := tmux.NewClient(config.TmuxConfPath())

	// Inside tmux with no filter, use the native chooser — best UX.
	// With a filter, fall through to our list so the filter applies.
	if tmux.IsInsideTmux() && len(args) == 0 {
		return tc.ChooseSession()
	}

	store := session.NewStore(config.SessionsPath())
	sessions, err := store.List()
	if err != nil {
		return fmt.Errorf("listing sessions: %w", err)
	}

	if len(sessions) == 0 {
		out := output.Stderr()
		out.Dim("No sessions yet. Create one with: ctm new <name>")
		return nil
	}

	// Apply optional filter
	var filter string
	if len(args) > 0 {
		filter = args[0]
	}
	sessions = filterSessions(sessions, filter)
	if len(sessions) == 0 {
		out := output.Stderr()
		out.Dim("No sessions match %q", filter)
		return nil
	}

	// Single match → auto-attach, no prompt (huge mobile win)
	if len(sessions) == 1 {
		return runAttach(cmd, []string{sessions[0].Name})
	}

	// Stable order: by name
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].Name < sessions[j].Name
	})

	// Print numbered list to stderr so stdout stays clean
	out := output.Stderr()
	for i, sess := range sessions {
		live := "DEAD"
		if tc.HasSession(sess.Name) {
			live = "LIVE"
		}
		fmt.Fprintf(os.Stderr, "  %d) %-20s [%s] %s\n", i+1, sess.Name, sess.Mode, live)
	}
	out.Dim("Pick a number (or q to quit): ")

	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	// EOF is fine — accept whatever was read before it (e.g. `echo -n 2 | ctm pick`).
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("reading selection: %w", err)
	}
	line = strings.TrimSpace(line)
	if line == "" || line == "q" || line == "Q" {
		return nil
	}

	idx, err := strconv.Atoi(line)
	if err != nil || idx < 1 || idx > len(sessions) {
		return fmt.Errorf("invalid selection: %q", line)
	}

	// Re-fetch from store to guard against TOCTOU — another ctm invocation
	// may have deleted the session between List and now.
	chosen := sessions[idx-1]
	if _, err := store.Get(chosen.Name); err != nil {
		return fmt.Errorf("session %q no longer exists", chosen.Name)
	}
	return runAttach(cmd, []string{chosen.Name})
}
