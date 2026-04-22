// Package doctor implements the diagnostic check runner shared by the
// `ctm doctor` CLI and the `GET /api/doctor` HTTP endpoint.
//
// The CLI formats [ok/warn/err] Check slices as coloured lines; the
// HTTP endpoint JSON-encodes the same slice. Adding a new check means
// appending one function here — both surfaces pick it up without edits.
package doctor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/RandomCodeSpace/ctm/internal/config"
	"github.com/RandomCodeSpace/ctm/internal/session"
	"github.com/RandomCodeSpace/ctm/internal/tmux"
)

// Status enumerates the three check outcomes surfaced to both CLI and UI.
// Values are the wire form — do not rename without bumping the API.
const (
	StatusOK   = "ok"
	StatusWarn = "warn"
	StatusErr  = "err"
)

// Check is the single result row. JSON tags are the /api/doctor wire
// contract; Remediation is optional and omitted when empty.
type Check struct {
	Name        string `json:"name"`
	Status      string `json:"status"`
	Message     string `json:"message,omitempty"`
	Remediation string `json:"remediation,omitempty"`
}

// LookupBinary resolves a named binary on PATH. Exposed so the CLI
// formatter (cmd/doctor.go) can share the exact same probe logic
// rather than shelling out a second time.
func LookupBinary(name string) (path string, ok bool) {
	p, err := exec.LookPath(name)
	if err != nil {
		return "", false
	}
	return p, true
}

// TmuxVersion runs `tmux -V` under ctx and returns the trimmed output.
// ok=false means tmux is either missing or refused to print a version.
func TmuxVersion(ctx context.Context) (version string, ok bool) {
	if _, err := exec.LookPath("tmux"); err != nil {
		return "", false
	}
	b, err := exec.CommandContext(ctx, "tmux", "-V").Output()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(b)), true
}

// Run executes every diagnostic check and returns the results in
// display order. All checks respect ctx cancellation; Run returns
// immediately with whatever it has accumulated if ctx expires.
//
// cfg is the already-loaded user config. A zero-valued Config is
// legal: checks that depend on user-configurable lists (required_env,
// required_in_path) simply report "not configured".
func Run(ctx context.Context, cfg config.Config) []Check {
	checks := make([]Check, 0, 16)

	// Each checker is free to shell out; we gate the slow ones on
	// ctx.Err() so a caller with a 5-s deadline can still return a
	// partial list rather than blocking the HTTP response.
	for _, fn := range []func(context.Context, config.Config) []Check{
		checkDependencies,
		checkTmuxVersion,
		checkRequiredEnv,
		checkRequiredInPath,
		checkConfig,
		checkSessions,
	} {
		if ctx.Err() != nil {
			break
		}
		checks = append(checks, fn(ctx, cfg)...)
	}
	return checks
}

func checkDependencies(_ context.Context, _ config.Config) []Check {
	deps := []string{"tmux", "claude", "git"}
	out := make([]Check, 0, len(deps))
	for _, dep := range deps {
		c := Check{Name: "dep:" + dep}
		if path, err := exec.LookPath(dep); err == nil {
			c.Status = StatusOK
			c.Message = path
		} else {
			c.Status = StatusWarn
			c.Message = fmt.Sprintf("%s not found in PATH", dep)
			c.Remediation = fmt.Sprintf(
				"install %s and ensure it is on PATH (e.g. `apt install %s` on Debian)",
				dep, dep)
		}
		out = append(out, c)
	}
	return out
}

func checkTmuxVersion(ctx context.Context, _ config.Config) []Check {
	c := Check{Name: "tmux:version"}
	// Fail fast if tmux isn't even on PATH — the dep check already
	// reported the warning, no need to double-log.
	if _, err := exec.LookPath("tmux"); err != nil {
		c.Status = StatusWarn
		c.Message = "tmux not installed"
		c.Remediation = "install tmux first (see dep:tmux)"
		return []Check{c}
	}
	cmd := exec.CommandContext(ctx, "tmux", "-V")
	b, err := cmd.Output()
	if err != nil {
		c.Status = StatusWarn
		c.Message = fmt.Sprintf("could not read tmux -V: %v", err)
		c.Remediation = "verify tmux works: `tmux -V` in a shell"
		return []Check{c}
	}
	c.Status = StatusOK
	c.Message = strings.TrimSpace(string(b))
	return []Check{c}
}

func checkRequiredEnv(_ context.Context, cfg config.Config) []Check {
	if len(cfg.RequiredEnv) == 0 {
		return nil
	}
	out := make([]Check, 0, len(cfg.RequiredEnv))
	for _, name := range cfg.RequiredEnv {
		c := Check{Name: "env:" + name}
		if v, ok := os.LookupEnv(name); ok && v != "" {
			c.Status = StatusOK
			c.Message = "set"
		} else {
			c.Status = StatusErr
			c.Message = fmt.Sprintf("%s is not set", name)
			c.Remediation = fmt.Sprintf("export %s=... in your shell rc or the session's env file", name)
		}
		out = append(out, c)
	}
	return out
}

func checkRequiredInPath(_ context.Context, cfg config.Config) []Check {
	if len(cfg.RequiredInPath) == 0 {
		return nil
	}
	out := make([]Check, 0, len(cfg.RequiredInPath))
	for _, bin := range cfg.RequiredInPath {
		c := Check{Name: "path:" + bin}
		if path, err := exec.LookPath(bin); err == nil {
			c.Status = StatusOK
			c.Message = path
		} else {
			c.Status = StatusWarn
			c.Message = fmt.Sprintf("%s not found on PATH", bin)
			c.Remediation = fmt.Sprintf("install %s or add its directory to PATH", bin)
		}
		out = append(out, c)
	}
	return out
}

func checkConfig(_ context.Context, cfg config.Config) []Check {
	c := Check{Name: "config:load"}
	cfgPath := config.ConfigPath()
	if _, err := os.Stat(cfgPath); err != nil {
		c.Status = StatusWarn
		c.Message = fmt.Sprintf("%s not present", cfgPath)
		c.Remediation = "run any `ctm` command once to seed defaults"
		return []Check{c}
	}
	// cfg is passed in already loaded; we check that it has *something*
	// populated to distinguish "zero value" (caller forgot to load)
	// from "file exists and parsed".
	if cfg.DefaultMode == "" && len(cfg.RequiredEnv) == 0 && cfg.ScrollbackLines == 0 {
		c.Status = StatusWarn
		c.Message = fmt.Sprintf("%s is present but appears empty", cfgPath)
		c.Remediation = "delete the file and re-run any `ctm` command to re-seed defaults"
		return []Check{c}
	}
	c.Status = StatusOK
	c.Message = fmt.Sprintf("loaded (default_mode=%s, scrollback=%d)",
		cfg.DefaultMode, cfg.ScrollbackLines)
	return []Check{c}
}

func checkSessions(_ context.Context, _ config.Config) []Check {
	c := Check{Name: "sessions:store"}
	path := config.SessionsPath()
	store := session.NewStore(path)
	sessions, err := store.List()
	if err != nil {
		c.Status = StatusWarn
		c.Message = fmt.Sprintf("could not list sessions: %v", err)
		c.Remediation = fmt.Sprintf("inspect %s; if corrupt, delete it (sessions will be rebuilt from tmux on next attach)", path)
		return []Check{c}
	}
	// Count tmux liveness so the panel tells users whether the store
	// agrees with reality without them opening another panel.
	tc := tmux.NewClient(config.TmuxConfPath())
	alive := 0
	for _, s := range sessions {
		if tc.HasSession(s.Name) {
			alive++
		}
	}
	c.Status = StatusOK
	if len(sessions) == 0 {
		c.Message = "no sessions on record"
	} else {
		c.Message = fmt.Sprintf("%d session(s), %d tmux-alive", len(sessions), alive)
	}
	return []Check{c}
}
