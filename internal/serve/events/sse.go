package events

import (
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	sseKeepalive = 15 * time.Second
)

// Handler returns an http.HandlerFunc that streams hub events as an SSE
// response. filter == "" streams the global feed; otherwise only events
// whose Session matches filter are delivered.
//
// Honours the Last-Event-ID request header for ring replay. Sends a
// keepalive comment every sseKeepalive so corporate proxies don't time
// the stream out. Exits when the client disconnects.
func Handler(h *Hub, filter string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
		flusher.Flush()

		// Initial comment frame: gives proxies (Caddy, nginx, CF) and the
		// browser an immediate body byte so the response is committed and
		// EventSource.onopen fires without waiting for the first 15s
		// keepalive. Without this, fetch-event-source can sit idle and
		// some intermediaries hold headers until first chunk.
		if _, err := io.WriteString(w, ": ok\n\n"); err != nil {
			return
		}
		flusher.Flush()

		since := r.Header.Get("Last-Event-ID")
		sub, replay, lost := h.subscribe(filter, since)
		defer sub.Close()

		if lost {
			if _, err := io.WriteString(w, ": lost\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}

		for _, e := range replay {
			if err := writeEvent(w, flusher, e); err != nil {
				return
			}
		}

		ticker := time.NewTicker(sseKeepalive)
		defer ticker.Stop()

		ctx := r.Context()
		for {
			select {
			case <-ctx.Done():
				return
			case e, open := <-sub.Events():
				if !open {
					return
				}
				if err := writeEvent(w, flusher, e); err != nil {
					return
				}
			case <-ticker.C:
				if _, err := io.WriteString(w, ": keepalive\n\n"); err != nil {
					return
				}
				flusher.Flush()
			}
		}
	}
}

// writeEvent serialises one Event to an SSE frame.
//
// Multi-line payloads must be split on newline per the SSE spec — each
// data line is prefixed with "data: ".
func writeEvent(w io.Writer, f http.Flusher, e Event) error {
	var b strings.Builder
	if e.Type != "" {
		b.WriteString("event: ")
		b.WriteString(e.Type)
		b.WriteByte('\n')
	}
	if e.ID != "" {
		b.WriteString("id: ")
		b.WriteString(e.ID)
		b.WriteByte('\n')
	}
	payload := string(e.Payload)
	if payload == "" {
		b.WriteString("data: \n")
	} else {
		for line := range strings.SplitSeq(payload, "\n") {
			b.WriteString("data: ")
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	b.WriteByte('\n')

	if _, err := io.WriteString(w, b.String()); err != nil {
		return err
	}
	f.Flush()
	return nil
}
