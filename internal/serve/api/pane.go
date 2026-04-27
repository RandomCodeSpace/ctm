// Package api — pane.go: V24 live tmux pane capture SSE stream.
//
// Route wiring (owned by coordinator in server.go — do NOT edit here):
//
//	mux.Handle("GET /events/session/{name}/pane", authHF(api.PaneStream(s.tmux)))
package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"
)

// paneTick is the capture cadence. 1 Hz matches the design brief —
// a shell prompt feels live without hammering tmux / the browser.
const paneTick = 1 * time.Second

// Default and upper bound for the scrollback window captured above
// the visible pane area. Detached tmux sessions often collapse to a
// small geometry (e.g. 55×28); without scrollback the viewer shows
// only the last ~28 rows no matter how much output has been
// produced. 500 lines is a generous debugging window; 10 000 caps a
// pathological `?history=` query from shipping megabytes per tick.
const (
	defaultPaneScrollback = 500
	maxPaneScrollback     = 10_000
)

// TmuxPaneCapturer is the narrow slice of *tmux.Client this handler
// needs. A package-local interface keeps the api package decoupled
// from internal/tmux (which would otherwise pull os/exec into every
// api test binary) and makes the handler trivially faked.
type TmuxPaneCapturer interface {
	// CapturePaneHistory returns the raw output of
	//   tmux capture-pane -e -p -J -t <name> -S -<scrollback>
	// scrollback lines above the visible pane, with -e preserving
	// SGR, -p writing to stdout, and -J joining wrapped lines.
	CapturePaneHistory(name string, scrollback int) (string, error)
}

// PaneStream returns a GET /events/session/{name}/pane handler that
// streams a live capture of the named tmux pane as SSE.
//
// Behaviour:
//   - Emits one `event: pane` frame per tick (1 Hz) whose `data` is a
//     JSON-encoded string containing the raw capture (escape sequences
//     preserved).
//   - Debounces identical payloads — a tick whose capture matches the
//     last emitted payload is skipped. Keeps the stream quiet when the
//     pane is idle.
//   - Emits a single initial frame on connect so the UI has something
//     to render immediately (no 1s blank state).
//   - Exits cleanly when the client disconnects (r.Context().Done())
//     or when the pane disappears (CapturePaneHistory returns an
//     error twice in a row — we tolerate one transient miss).
//   - `?history=<N>` query param overrides the default scrollback
//     window. Clamped to [0, maxPaneScrollback]. 0 = visible-only.
func PaneStream(tmux TmuxPaneCapturer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if name == "" {
			http.Error(w, "missing session name", http.StatusBadRequest)
			return
		}

		scrollback := defaultPaneScrollback
		if raw := r.URL.Query().Get("history"); raw != "" {
			if n, err := strconv.Atoi(raw); err == nil && n >= 0 {
				if n > maxPaneScrollback {
					n = maxPaneScrollback
				}
				scrollback = n
			}
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}

		hdr := w.Header()
		hdr.Set("Content-Type", "text/event-stream")
		hdr.Set("Cache-Control", "no-store")
		hdr.Set("Connection", "keep-alive")
		hdr.Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)

		// Initial comment so fetch-event-source's onopen fires without
		// waiting for the first real frame — matches events/sse.go.
		if _, err := io.WriteString(w, ": ok\n\n"); err != nil {
			return
		}
		flusher.Flush()

		ctx := r.Context()
		ticker := time.NewTicker(paneTick)
		defer ticker.Stop()

		var last string
		var hadOne bool
		var consecutiveErrs int

		emit := func(payload string) bool {
			if hadOne && payload == last {
				return true // debounce
			}
			b, err := json.Marshal(payload)
			if err != nil {
				return false
			}
			if _, err := io.WriteString(w, "event: pane\ndata: "); err != nil {
				return false
			}
			if _, err := w.Write(b); err != nil {
				return false
			}
			if _, err := io.WriteString(w, "\n\n"); err != nil {
				return false
			}
			flusher.Flush()
			last = payload
			hadOne = true
			return true
		}

		// Initial capture + emission — so the UI has a first frame
		// without waiting 1s.
		if out, err := tmux.CapturePaneHistory(name, scrollback); err == nil {
			if !emit(out) {
				return
			}
		} else {
			consecutiveErrs++
		}

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				out, err := tmux.CapturePaneHistory(name, scrollback)
				if err != nil {
					consecutiveErrs++
					if consecutiveErrs >= 2 {
						// Pane is gone — signal end politely.
						_, _ = io.WriteString(w, "event: pane_end\ndata: \"\"\n\n")
						flusher.Flush()
						return
					}
					continue
				}
				consecutiveErrs = 0
				if !emit(out) {
					return
				}
			}
		}
	}
}
