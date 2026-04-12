package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/RandomCodeSpace/ctm/internal/config"
	"github.com/RandomCodeSpace/ctm/internal/health"
	"github.com/RandomCodeSpace/ctm/internal/output"
	"github.com/RandomCodeSpace/ctm/internal/session"
	"github.com/RandomCodeSpace/ctm/internal/shell"
	"github.com/RandomCodeSpace/ctm/internal/tmux"
)

func init() {
	rootCmd.AddCommand(checkCmd)
}

var checkCmd = &cobra.Command{
	Use:               "check [name]",
	Short:             "Run pre-flight checks for a session without attaching",
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: shell.SessionNameCompletion(),
	RunE:              runCheck,
}

func runCheck(cmd *cobra.Command, args []string) error {
	name := "claude"
	if len(args) > 0 {
		name = args[0]
	}

	out := output.Stdout()
	cfg, err := config.Load(config.ConfigPath())
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	store := session.NewStore(config.SessionsPath())
	tc := tmux.NewClient(config.TmuxConfPath())

	sess, sessErr := store.Get(name)

	out.Bold("Pre-flight checks for session %q", name)

	allPassed := true

	// ENV vars
	envResult := health.CheckEnvVars(cfg.RequiredEnv)
	printCheckResult(out, envResult)
	if !envResult.Passed() {
		allPassed = false
	}

	// PATH entries
	pathResult := health.CheckPathEntries(cfg.RequiredInPath)
	printCheckResult(out, pathResult)
	if !pathResult.Passed() {
		allPassed = false
	}

	// Working directory
	if sessErr != nil {
		wdResult := health.CheckResult{
			Name:    "workdir",
			Status:  health.StatusFail,
			Message: fmt.Sprintf("session %q not found in store", name),
			Fix:     fmt.Sprintf("run: ctm new %s", name),
		}
		printCheckResult(out, wdResult)
		allPassed = false
	} else {
		wdResult := health.CheckWorkdir(sess.Workdir)
		printCheckResult(out, wdResult)
		if !wdResult.Passed() {
			allPassed = false
		}
	}

	// Tmux session
	tmuxResult := health.CheckTmuxSession(tc, name)
	printCheckResult(out, tmuxResult)
	if !tmuxResult.Passed() {
		allPassed = false
	}

	// Claude process
	claudeResult := health.CheckClaudeProcess(tc, name)
	printCheckResult(out, claudeResult)
	if !claudeResult.Passed() {
		allPassed = false
	}

	// Summary
	fmt.Println()
	if allPassed {
		out.Success("All checks passed")
	} else {
		out.Warn("Some checks failed")
		return fmt.Errorf("pre-flight checks failed")
	}

	return nil
}

func printCheckResult(out *output.Printer, r health.CheckResult) {
	status := "[PASS]"
	if !r.Passed() {
		status = "[FAIL]"
	}
	if r.Passed() {
		out.Success("%s %s: %s", status, r.Name, r.Message)
	} else {
		out.Warn("%s %s: %s", status, r.Name, r.Message)
		if r.Fix != "" {
			out.Dim("       fix: %s", r.Fix)
		}
	}
}
