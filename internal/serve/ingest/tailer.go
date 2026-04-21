package ingest

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/RandomCodeSpace/ctm/internal/serve/events"
)

// Tailer watches the JSONL log file for a single Claude session and
// publishes `tool_call` events to the hub for each appended line.
//
// Per the design spec (§4 Tailers), log files are keyed on Claude's
// session UUID, not the human session name. The human name is stamped
// into outgoing events so the UI can route by a stable, user-friendly
// key while the file on disk follows Claude's identifier.
type Tailer struct {
	SessionName string
	SessionUUID string
	LogPath     string
	Hub         *events.Hub
}

// NewTailer constructs a tailer for the given session. The log file is
// assumed to live at `<logDir>/<sessionUUID>.jsonl`.
func NewTailer(sessionName, sessionUUID, logDir string, hub *events.Hub) *Tailer {
	return &Tailer{
		SessionName: sessionName,
		SessionUUID: sessionUUID,
		LogPath:     filepath.Join(logDir, sessionUUID+".jsonl"),
		Hub:         hub,
	}
}

// Run blocks until ctx is cancelled or a fatal fsnotify error occurs.
// On startup it scans the file to EOF (if it already exists), then
// waits for WRITE / CREATE / RENAME / REMOVE events on the parent
// directory and reacts per spec §7 "Error handling per layer":
//
//   - WRITE: re-scan from last offset (not just "tail new bytes") —
//     fsnotify can coalesce writes, so always catch up to EOF.
//   - RENAME / REMOVE: close fd; wait for CREATE to reopen.
//   - CREATE (after rotation or first appearance): reopen at offset 0.
//
// Parse errors on individual lines are logged and skipped — never fatal.
func (t *Tailer) Run(ctx context.Context) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer w.Close()

	parent := filepath.Dir(t.LogPath)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return err
	}
	if err := w.Add(parent); err != nil {
		return err
	}

	var (
		fh     *os.File
		offset int64
	)

	// subagentSeen tracks which agent_ids we've already fired
	// `subagent_start` for on this tailer — used to keep the SSE
	// signal idempotent when the tailer re-scans the same byte range
	// after a transient fsnotify hiccup.
	subagentSeen := make(map[string]bool)

	openAndScan := func() {
		f, err := os.Open(t.LogPath)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				slog.Warn("tailer open failed",
					"session", t.SessionName, "path", t.LogPath, "err", err)
			}
			return
		}
		fh = f
		offset = 0
		scan(fh, &offset, t.SessionName, t.Hub, subagentSeen)
	}

	closeFile := func() {
		if fh != nil {
			_ = fh.Close()
			fh = nil
			offset = 0
		}
	}

	openAndScan() // initial catch-up
	defer closeFile()

	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-w.Events:
			if !ok {
				return nil
			}
			if ev.Name != t.LogPath {
				continue
			}
			switch {
			case ev.Op&fsnotify.Write == fsnotify.Write:
				if fh == nil {
					openAndScan()
				} else {
					scan(fh, &offset, t.SessionName, t.Hub, subagentSeen)
				}
			case ev.Op&fsnotify.Create == fsnotify.Create:
				closeFile()
				openAndScan()
			case ev.Op&fsnotify.Rename == fsnotify.Rename,
				ev.Op&fsnotify.Remove == fsnotify.Remove:
				closeFile()
			}
		case err, ok := <-w.Errors:
			if !ok {
				return nil
			}
			slog.Warn("tailer fsnotify error", "session", t.SessionName, "err", err)
		}
	}
}

// scan reads from *offset to EOF, parses each JSONL line, and publishes
// a tool_call event per line. Advances *offset by bytes consumed.
//
// subagentSeen tracks already-announced agent_ids: whenever we see a
// tool_call whose raw line carries an `agent_id` we haven't observed
// yet for this session, we emit a sibling `subagent_start` event so
// the UI's Subagents tab and Teams tab can wake up and refetch. Stop
// events are not emitted live — completion is inferred server-side
// from "no tool calls for N seconds" when the JSONL is replayed (see
// api.Subagents).
func scan(fh *os.File, offset *int64, sessionName string, hub *events.Hub, subagentSeen map[string]bool) {
	if _, err := fh.Seek(*offset, io.SeekStart); err != nil {
		slog.Warn("tailer seek failed", "session", sessionName, "err", err)
		return
	}
	br := bufio.NewReaderSize(fh, 64<<10)
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			*offset += int64(len(line))
			trimmed := line
			if trimmed[len(trimmed)-1] == '\n' {
				trimmed = trimmed[:len(trimmed)-1]
			}
			if len(trimmed) == 0 {
				continue
			}
			ev, perr := parseToolCallLine(sessionName, trimmed)
			if perr != nil {
				slog.Debug("tailer skipped malformed line",
					"session", sessionName, "err", perr)
				continue
			}
			hub.Publish(ev)

			// Best-effort subagent_start detection. parseSubagentMeta
			// decodes just the agent_id/agent_type/ctm_timestamp
			// fields from the same raw line; if we've already seen
			// this agent_id we skip the notification. Parse errors
			// here are non-fatal — the upstream tool_call event was
			// already published.
			if subagentSeen != nil {
				if meta, ok := parseSubagentMeta(trimmed); ok {
					if !subagentSeen[meta.AgentID] {
						subagentSeen[meta.AgentID] = true
						if startEv, serr := buildSubagentStartEvent(sessionName, meta); serr == nil {
							hub.Publish(startEv)
						}
					}
				}
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				slog.Warn("tailer read error", "session", sessionName, "err", err)
			}
			return
		}
	}
}

// ToolCallPayload is the JSON envelope published on the hub for each
// tool invocation, matching §6 of the design spec exactly.
type ToolCallPayload struct {
	Session string    `json:"session"`
	Tool    string    `json:"tool"`
	Input   string    `json:"input,omitempty"`
	Summary string    `json:"summary,omitempty"`
	IsError bool      `json:"is_error"`
	TS      time.Time `json:"ts"`
}

// parseToolCallLine turns a raw hook-payload JSON line into a hub Event.
//
// Hook payloads from `cmd/log-tool-use` are permissive (`map[string]any`),
// so we tolerate missing/renamed fields rather than fail. Input and
// Summary strings are best-effort summaries used by the UI's row
// rendering; later steps can refine per-tool formatting.
func parseToolCallLine(sessionName string, line []byte) (events.Event, error) {
	var raw map[string]any
	if err := json.Unmarshal(line, &raw); err != nil {
		return events.Event{}, err
	}
	p := ToolCallPayload{
		Session: sessionName,
		Tool:    stringField(raw, "tool_name"),
		IsError: boolAt(raw, "tool_response", "is_error"),
		Input:   summariseInput(raw),
		Summary: summariseResponse(raw),
		TS:      parseTimestamp(raw),
	}
	body, err := json.Marshal(p)
	if err != nil {
		return events.Event{}, err
	}
	return events.Event{
		Type:    "tool_call",
		Session: sessionName,
		Payload: body,
	}, nil
}
