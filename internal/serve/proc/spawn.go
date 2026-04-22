// Package proc provides the CLI-side glue between session-creating
// commands (`ctm attach`, `ctm new`, `ctm yolo`, etc.) and the local
// `ctm serve` daemon: a fire-and-forget spawner that ensures serve is
// up, and a tiny HTTP client that POSTs lifecycle events to its
// /api/hooks/:event endpoint.
//
// Both helpers are best-effort. Serve is observability — failures
// here log at WARN/DEBUG and never block the user-visible CLI flow.
package proc

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

const (
	// serveAddr is the loopback address `ctm serve` binds. Mirrors
	// `internal/serve.DefaultPort`; not imported to avoid the larger
	// dep cone (proc must stay light — it's hot-pathed by the CLI).
	serveAddr = "127.0.0.1:37778"

	probeTimeout = 200 * time.Millisecond
	spawnDeadline = 2 * time.Second
	postTimeout  = 1 * time.Second
)

// EnsureServeRunning probes /healthz; if no `ctm serve` listens, it
// spawns one as a detached child via setsid and waits up to 2 s for
// readiness, then returns. The caller never blocks past 2 s — if
// serve isn't up by then, subsequent PostEvent calls degrade silently.
func EnsureServeRunning(ctx context.Context) {
	if probeServe() {
		return
	}
	if err := spawnDetached(); err != nil {
		slog.Warn("failed to spawn ctm serve", "err", err)
		return
	}
	deadline := time.Now().Add(spawnDeadline)
	for time.Now().Before(deadline) {
		if probeServe() {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(75 * time.Millisecond):
		}
	}
	slog.Debug("ctm serve not ready within readiness window; continuing without it")
}

// PostEvent fires a hook event to the local serve daemon. event must
// match one of the names whitelisted in `internal/serve/api.Hooks`
// (session_new / session_attached / session_killed / on_yolo). The
// form is sent as `application/x-www-form-urlencoded`. No bearer token
// is needed — /api/hooks/* is unauthed (daemon binds 127.0.0.1 only).
func PostEvent(event string, form url.Values) {
	body := strings.NewReader(form.Encode())
	req, err := http.NewRequest(http.MethodPost,
		"http://"+serveAddr+"/api/hooks/"+event, body)
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: postTimeout}
	resp, err := client.Do(req)
	if err != nil {
		slog.Debug("PostEvent failed", "event", event, "err", err)
		return
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	if resp.StatusCode >= 400 {
		slog.Debug("PostEvent non-2xx", "event", event, "status", resp.StatusCode)
	}
}

// serveVersionHeader mirrors internal/serve.ServeVersionHeader.
// Inlined here to avoid importing the larger internal/serve package
// (proc must stay light — it's hot-pathed by every CLI invocation).
const serveVersionHeader = "X-Ctm-Serve"

// probeServe verifies that the listener on serveAddr is a real ctm
// serve daemon, NOT just any process returning 200 on /healthz. The
// X-Ctm-Serve header check defends against a local-uid impostor
// binding 127.0.0.1:37778 before the real daemon starts.
func probeServe() bool {
	client := &http.Client{Timeout: probeTimeout}
	resp, err := client.Get("http://" + serveAddr + "/healthz")
	if err != nil {
		return false
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	return resp.Header.Get(serveVersionHeader) != ""
}

// spawnDetached launches `ctm serve` as a detached child. stdout/stderr
// are routed to the same descriptors the parent had — `serve.log`
// rotation is the daemon's job (handled inside `cmd serve` if/when
// wired). For now nil descriptors mean inherited (typically /dev/tty
// or a pipe); a future polish pass can swap for a logrotate sink.
func spawnDetached() error {
	bin, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(bin, "serve")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	// Detach stdio so the parent's terminal isn't disturbed by serve's
	// slog output. /dev/null on POSIX.
	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err == nil {
		cmd.Stdin = devNull
		cmd.Stdout = devNull
		cmd.Stderr = devNull
	}
	return cmd.Start()
}

