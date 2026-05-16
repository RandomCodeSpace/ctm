package cmd

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/RandomCodeSpace/ctm/internal/agent"
	"github.com/RandomCodeSpace/ctm/internal/config"
	"github.com/RandomCodeSpace/ctm/internal/health"
	"github.com/RandomCodeSpace/ctm/internal/output"
	"github.com/RandomCodeSpace/ctm/internal/session"
	"github.com/RandomCodeSpace/ctm/internal/tmux"
	"github.com/spf13/cobra"
)

// preflightCacheTTL is how long an "ok" health result is trusted before
// the slow checks (env, PATH, workdir) are re-run. This optimizes the
// reconnect path on flaky mobile networks where SSH drops repeatedly.
const preflightCacheTTL = 60 * time.Second

// Repeated message templates extracted to satisfy the no-duplicate-literal
// rule. The verb / format markers are intentional — these are passed to
// out.Warn / fmt.Errorf at every recreate / reattach branch.
const (
	warnUpdateAttached = "could not update attached timestamp: %v"
	warnUpdateHealth   = "could not update health status: %v"
	errHealthCheckFmt  = "health check failed: %s"
	errAttachingFmt    = "attaching to session %q: %w"
)

// healthCacheValid reports whether the session's last health check was
// successful and recent enough to skip the slow env/PATH/workdir checks.
func healthCacheValid(sess *session.Session) bool {
	if sess.LastHealthAt.IsZero() {
		return false
	}
	if sess.LastHealthStatus != "ok" && sess.LastHealthStatus != "recovered" && sess.LastHealthStatus != "recreated" {
		return false
	}
	return time.Since(sess.LastHealthAt) < preflightCacheTTL
}

func init() {
	rootCmd.RunE = runAttach
}

func runAttach(cmd *cobra.Command, args []string) error {
	name := session.DefaultAgent
	if len(args) > 0 {
		name = args[0]
	}
	if err := session.ValidateName(name); err != nil {
		return err
	}

	out := output.Stderr()
	cfgPtr, err := ensureSetup()
	if err != nil {
		return err
	}
	cfg := *cfgPtr

	store := session.NewStore(config.SessionsPath())
	tc := tmux.NewClient(config.TmuxConfPath())

	sess, err := store.Get(name)
	if err != nil {
		// Session doesn't exist — create new
		return createAndAttach(name, ".", cfg.DefaultMode, "", store, tc, out)
	}

	return preflight(sess, cfg, store, tc, out)
}

// createAndAttach creates a new session and attaches to it. agentName, when
// non-empty, overrides session.DefaultAgent for this spawn (must already be
// validated against the registry by the caller — see resolveAgent in yolo.go).
func createAndAttach(name, workdir, _ string, agentName string, store *session.Store, tc *tmux.Client, out *output.Printer) error {
	abs, err := filepath.Abs(workdir)
	if err != nil {
		return fmt.Errorf("resolving workdir: %w", err)
	}

	out.Info("No session %q found — creating in %s", name, abs)

	sess, err := session.Yolo(session.SpawnOpts{
		Name:    name,
		Workdir: abs,
		Agent:   agentName,
		Tmux:    tc,
		Store:   store,
	})
	if err != nil {
		return fmt.Errorf("createAndAttach spawn: %w", err)
	}

	if err := store.UpdateAttached(name); err != nil {
		out.Warn(warnUpdateAttached, err)
	}

	out.Success("created session %q", name)
	fireHook("on_new", &sess)
	return tc.Go(name)
}

// preflight runs health checks then attaches to an existing session.
func preflight(sess *session.Session, cfg config.Config, store *session.Store, tc *tmux.Client, out *output.Printer) error {
	// Skip slow checks (env vars, PATH) if cached health is still fresh.
	// This optimizes the reconnect path on flaky mobile networks.
	// Workdir check is always run — it's a single os.Stat and is the
	// check most likely to flip between attaches (e.g., user deletes the dir).
	if healthCacheValid(sess) {
		out.Debug(Verbose, "preflight cache hit (last health %s, age %s) — skipping env/PATH checks",
			sess.LastHealthStatus, time.Since(sess.LastHealthAt).Round(time.Second))
	} else {
		// 1. Env var and PATH checks
		out.Debug(Verbose, "running env var check...")
		envResult := health.CheckEnvVars(cfg.RequiredEnv)
		if !envResult.Passed() {
			out.Error("environment check failed", envResult.Message, envResult.Fix)
			return fmt.Errorf(errHealthCheckFmt, envResult.Name)
		}

		out.Debug(Verbose, "running PATH check...")
		pathResult := health.CheckPathEntries(cfg.RequiredInPath)
		if !pathResult.Passed() {
			out.Error("PATH check failed", pathResult.Message, pathResult.Fix)
			return fmt.Errorf(errHealthCheckFmt, pathResult.Name)
		}
	}

	// 2. Workdir check — always run, never cached
	out.Debug(Verbose, "checking workdir: %s", sess.Workdir)
	wdResult := health.CheckWorkdir(sess.Workdir)
	if !wdResult.Passed() {
		out.Error("workdir check failed", wdResult.Message, wdResult.Fix)
		return fmt.Errorf(errHealthCheckFmt, wdResult.Name)
	}

	a, ok := agent.For(sess.NormalizeAgent())
	if !ok {
		return fmt.Errorf("session %q references unregistered agent %q", sess.Name, sess.NormalizeAgent())
	}
	resumeSpec := agent.SpawnSpec{
		UUID:           sess.UUID,
		AgentSessionID: sess.AgentSessionID,
		Mode:           sess.Mode,
		Resume:         true,
	}

	// 3. Tmux session check — if missing, recreate with resume semantics
	out.Debug(Verbose, "checking tmux session: %s", sess.Name)
	tmuxResult := health.CheckTmuxSession(tc, sess.Name)
	if !tmuxResult.Passed() {
		out.Warn("tmux session %q missing — recreating", sess.Name)
		shellCmd := a.BuildCommand(resumeSpec)
		if err := tc.NewSession(sess.Name, sess.Workdir, shellCmd); err != nil {
			return fmt.Errorf("recreating tmux session: %w", err)
		}
		if err := store.UpdateHealth(sess.Name, "recreated"); err != nil {
			out.Warn(warnUpdateHealth, err)
		}
		if err := store.UpdateAttached(sess.Name); err != nil {
			out.Warn(warnUpdateAttached, err)
		}
		fireHook("on_attach", sess)
		if err := tc.Go(sess.Name); err != nil {
			return fmt.Errorf(errAttachingFmt, sess.Name, err)
		}
		return nil
	}

	// 4. Agent process check — if dead, respawn with resume semantics
	out.Debug(Verbose, "checking %s process in session: %s", a.Name(), sess.Name)
	agentResult := health.CheckAgentProcess(tc, sess.Name, a.ProcessName())
	if !agentResult.Passed() {
		out.Debug(Verbose, "%s not running, restarting with resume", a.Name())
		out.Warn("%s process dead — respawning", a.Name())
		shellCmd := a.BuildCommand(resumeSpec)
		if err := tc.RespawnPane(sess.Name, shellCmd); err != nil {
			return fmt.Errorf("respawning pane: %w", err)
		}
		if err := store.UpdateHealth(sess.Name, "recovered"); err != nil {
			out.Warn(warnUpdateHealth, err)
		}
		if err := store.UpdateAttached(sess.Name); err != nil {
			out.Warn(warnUpdateAttached, err)
		}
		fireHook("on_attach", sess)
		if err := tc.Go(sess.Name); err != nil {
			return fmt.Errorf(errAttachingFmt, sess.Name, err)
		}
		return nil
	}

	// 5. All checks passed
	out.Debug(Verbose, "all pre-flight checks passed")
	if err := store.UpdateHealth(sess.Name, "ok"); err != nil {
		out.Warn(warnUpdateHealth, err)
	}
	if err := store.UpdateAttached(sess.Name); err != nil {
		out.Warn(warnUpdateAttached, err)
	}

	fireHook("on_attach", sess)
	if err := tc.Go(sess.Name); err != nil {
		return fmt.Errorf(errAttachingFmt, sess.Name, err)
	}
	return nil
}
