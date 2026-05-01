// Package api — /api/sessions/{name}/feed/history (V6).
//
// Historical scroll past the in-memory 500-slot ring buffer. The hub's
// per-session ring is a cache; the on-disk JSONL log is the source of
// truth. This handler reads that log in reverse so a UI that has
// scrolled to the oldest ring entry can fetch older events on demand.
//
// Mount (wired in server.go alongside the other /feed routes):
//
//	mux.Handle("GET /api/sessions/{name}/feed/history",
//	    authHF(api.FeedHistory(s.logDir, logsUUIDResolver{proj: s.proj})))
//
// Shape (one response row per line):
//
//	{
//	  "events": [
//	    {"id":"<nano>-0", "session":"alpha", "type":"tool_call",
//	     "ts":"2026-04-21T14:33:09Z",
//	     "payload":{session,tool,input,summary,is_error,ts}},
//	    ...
//	  ],
//	  "has_more": true
//	}
//
// Contract notes:
//   - `before` query param is REQUIRED. The cursor is the opaque
//     `<unix-nano>-<seq>` ID the hub assigns at Publish time; the
//     client echoes back the oldest visible row's id.
//   - Derived IDs use seq=0: JSONL lines don't carry hub sequence
//     numbers, but monotonicity within this cursor window is preserved
//     because rows come out of a single file in append-order.
//   - Results are strictly less than `before` and returned newest-first
//     so the UI can append directly below the ring view.
//   - `has_more` is true when the backwards scan hit `limit` before
//     exhausting the file — the UI shows the "Load older" button again.
//   - Only `tool_call`-shaped lines are emitted (lines without a
//     `tool_name` field are dropped). The on-disk log only contains
//     tool_call payloads today, so this is effectively a schema guard.
package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	defaultFeedHistoryLimit = 100
	maxFeedHistoryLimit     = 500
	// reverseChunkSize is how many bytes we pull off the tail on each
	// seek step when walking the file backwards. 64 KB matches the
	// tailer's forward buffered-reader size and is big enough that a
	// typical 200-byte JSONL line doesn't thrash across many reads.
	reverseChunkSize = 64 << 10
	// historyInputMax mirrors the tailer's inputSummaryMax so summaries
	// look identical whether sourced live (SSE) or from history.
	historyInputMax = 200
	// jsonlExt is the per-session claude history file suffix.
	jsonlExt = ".jsonl"
)

// feedHistoryEvent mirrors events.Event but lives here for JSON shape
// control. Kept separate from the hub type so the wire contract is
// documented in a single place and doesn't accidentally drift with
// internal hub refactors.
type feedHistoryEvent struct {
	ID      string          `json:"id"`
	Session string          `json:"session"`
	Type    string          `json:"type"`
	TS      string          `json:"ts"`
	Payload json.RawMessage `json:"payload"`
}

type feedHistoryResponse struct {
	Events  []feedHistoryEvent `json:"events"`
	HasMore bool               `json:"has_more"`
}

// toolCallLinePayload is the on-the-wire payload nested inside an
// Event.Payload for a tool_call event. Matches ingest.ToolCallPayload
// — duplicated here so the history handler can construct the same
// shape without taking an import dependency on internal/serve/ingest.
type toolCallLinePayload struct {
	Session string `json:"session"`
	Tool    string `json:"tool"`
	Input   string `json:"input,omitempty"`
	Summary string `json:"summary,omitempty"`
	IsError bool   `json:"is_error"`
	TS      string `json:"ts"`
}

// FeedHistory returns the GET /api/sessions/{name}/feed/history handler.
// Reads the session's <uuid>.jsonl (uuid resolved via `resolver`) in
// reverse and returns up to `limit` tool_call events strictly older
// than `before`.
func FeedHistory(logDir string, resolver UUIDNameResolver) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			w.Header().Set("Cache-Control", "no-store")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")

		name := r.PathValue("name")
		if name == "" {
			writeJSON(w, http.StatusBadRequest, errorBody{Error: "session name required"})
			return
		}

		before := r.URL.Query().Get("before")
		if before == "" {
			writeJSON(w, http.StatusBadRequest, errorBody{Error: "before cursor required", Name: name})
			return
		}

		limit := defaultFeedHistoryLimit
		if q := r.URL.Query().Get("limit"); q != "" {
			if n, err := strconv.Atoi(q); err == nil && n > 0 {
				if n > maxFeedHistoryLimit {
					n = maxFeedHistoryLimit
				}
				limit = n
			}
		}

		// Resolve session name → log UUID. The logs_usage resolver
		// maps UUID → name; we need the reverse. Walk logDir asking
		// the resolver for each UUID — bounded by the existing
		// max-files gate (10_000) and only invoked on explicit user
		// click, so no cache is necessary.
		uuid, ok := resolveNameToUUID(resolver, logDir, name)
		if !ok {
			writeJSON(w, http.StatusNotFound, errorBody{Error: "session not found", Name: name})
			return
		}

		path := filepath.Join(logDir, uuid+jsonlExt)
		events, hasMore, err := readJSONLReverse(path, name, before, limit)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				// Log for the session doesn't exist yet (fresh session,
				// no tool calls recorded). Return an empty 200 rather
				// than 404 — the session is known to the projection.
				writeJSON(w, http.StatusOK, feedHistoryResponse{
					Events:  []feedHistoryEvent{},
					HasMore: false,
				})
				return
			}
			writeJSON(w, http.StatusInternalServerError, errorBody{Error: "read failed", Name: name})
			return
		}
		writeJSON(w, http.StatusOK, feedHistoryResponse{
			Events:  events,
			HasMore: hasMore,
		})
	}
}

// nameToUUIDResolver is the optional direct name→uuid lookup. When a
// UUIDNameResolver also implements this, resolveNameToUUID consults it
// first so the authoritative sessions.json mapping wins over the log-
// directory scan. Without this, sessions that had an older claude
// session_id (a dead log file still sitting in logDir) would race with
// the live one and could shadow it when filenames sort before the live
// UUID. See resolveNameToUUID below.
type nameToUUIDResolver interface {
	ResolveName(name string) (uuid string, ok bool)
}

// resolveNameToUUID returns the log UUID for a human session name.
//
// Order of resolution:
//  1. If resolver implements nameToUUIDResolver (production: the
//     projection-backed logsUUIDResolver), use that directly. This is
//     the authoritative path and handles the multi-historical-log-
//     file case where a session has cycled through several claude
//     session_ids.
//  2. Fallback: scan logDir for *.jsonl files and reverse-map each
//     via ResolveUUID. Preserves behaviour for orphan UUIDs whose
//     session isn't in the projection (tests, migration, manual
//     overrides).
func resolveNameToUUID(resolver UUIDNameResolver, logDir, name string) (string, bool) {
	if resolver == nil {
		return "", false
	}
	if nr, ok := resolver.(nameToUUIDResolver); ok {
		if uuid, ok := nr.ResolveName(name); ok {
			return uuid, true
		}
	}
	entries, err := os.ReadDir(logDir)
	if err != nil {
		return "", false
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		fn := e.Name()
		if !strings.HasSuffix(fn, jsonlExt) {
			continue
		}
		uuid := strings.TrimSuffix(fn, jsonlExt)
		if got, ok := resolver.ResolveUUID(uuid); ok && got == name {
			return uuid, true
		}
	}
	return "", false
}

// readJSONLReverse seeks to EOF and reads `reverseChunkSize` bytes at
// a time going backwards, splitting on '\n' and parsing each complete
// line as a tool_call. Returns up to `limit` events strictly less
// than `before`, newest-first. hasMore is true when the scan stopped
// because it hit `limit` (i.e. older rows may still exist below the
// returned window).
func readJSONLReverse(path, sessionName, before string, limit int) ([]feedHistoryEvent, bool, error) {
	fh, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer fh.Close()

	info, err := fh.Stat()
	if err != nil {
		return nil, false, err
	}
	size := info.Size()

	out := make([]feedHistoryEvent, 0, limit)
	// `tail` is the partial leading fragment carried over from the
	// previous (newer) chunk — its bytes belong to a line whose '\n'
	// terminator hasn't been seen yet. Gets reset every time we find
	// a newline that splits a chunk cleanly.
	var tail []byte
	offset := size
	// hasMore is flipped true as soon as we would have appended the
	// (limit+1)-th event. Keeps the signal precise: we definitively
	// skipped at least one older eligible row, and the caller should
	// show the "Load older" button again.
	hasMore := false

	for offset > 0 && len(out) < limit {
		readSize := int64(reverseChunkSize)
		if readSize > offset {
			readSize = offset
		}
		offset -= readSize
		buf := make([]byte, readSize)
		if _, err := fh.ReadAt(buf, offset); err != nil && !errors.Is(err, io.EOF) {
			return nil, false, err
		}

		// Combine this chunk with any partial head carried over. The
		// partial head originally sat in the newer chunk but was
		// missing its leading '\n'; now that we've pulled the older
		// bytes, we can stitch them together.
		combined := make([]byte, 0, len(buf)+len(tail))
		combined = append(combined, buf...)
		combined = append(combined, tail...)

		// If we're not at file-start, the first byte of `combined` is
		// mid-line (the line's start lies further back, in an older
		// chunk we haven't read yet). Stash everything up to the first
		// '\n' as the new tail; emit lines strictly after that first
		// '\n'. At file-start we emit from byte 0.
		var emitFrom int
		if offset > 0 {
			nl := bytes.IndexByte(combined, '\n')
			if nl < 0 {
				// No newline in this chunk — entire chunk is a partial
				// line. Keep walking backwards with the accumulated
				// fragment so the caller can see it once we reach a
				// '\n' (or the start of file).
				tail = combined
				continue
			}
			tail = combined[:nl]
			emitFrom = nl + 1
		} else {
			tail = nil
			emitFrom = 0
		}

		// Split emittable region on '\n' and walk newest → oldest.
		region := combined[emitFrom:]
		lines := bytes.Split(region, []byte{'\n'})
		for i := len(lines) - 1; i >= 0; i-- {
			line := bytes.TrimRight(lines[i], "\r")
			if len(line) == 0 {
				continue
			}
			ev, ok := synthEvent(sessionName, line)
			if !ok {
				continue
			}
			if !idLessThanExt(ev.ID, before) {
				continue
			}
			if len(out) >= limit {
				// We have an (limit+1)-th eligible row in hand — the
				// caller needs to know older content exists so it can
				// render "Load older" again. Bail out of both loops.
				hasMore = true
				break
			}
			out = append(out, ev)
		}
		if hasMore {
			break
		}
	}

	// At offset == 0 the residual `tail` holds the file's first line
	// (or is empty if the file begins with '\n'). Emit if there's
	// still budget; otherwise consult it for the has_more signal.
	if !hasMore && len(tail) > 0 {
		line := bytes.TrimRight(tail, "\r")
		if len(line) > 0 {
			if ev, ok := synthEvent(sessionName, line); ok && idLessThanExt(ev.ID, before) {
				if len(out) < limit {
					out = append(out, ev)
				} else {
					hasMore = true
				}
			}
		}
	}

	return out, hasMore, nil
}

// synthEvent parses one raw JSONL hook line and synthesises an Event
// envelope equivalent to what the hub would emit live. The derived id
// (<unix-nano>-0) uses the line's `ctm_timestamp` for monotonicity
// within the cursor window.
func synthEvent(sessionName string, line []byte) (feedHistoryEvent, bool) {
	var raw map[string]any
	if err := json.Unmarshal(line, &raw); err != nil {
		return feedHistoryEvent{}, false
	}

	tool, _ := raw["tool_name"].(string)
	if tool == "" {
		// Not a tool_call line (e.g. future event types sharing the
		// log would be filtered out here).
		return feedHistoryEvent{}, false
	}

	ts := extractTS(raw)
	nanos := ts.UnixNano()

	p := toolCallLinePayload{
		Session: sessionName,
		Tool:    tool,
		Input:   summariseHistoryInput(raw, tool),
		Summary: summariseHistoryResponse(raw),
		IsError: nestedBool(raw, "tool_response", "is_error"),
		TS:      ts.Format(time.RFC3339),
	}
	body, err := json.Marshal(p)
	if err != nil {
		return feedHistoryEvent{}, false
	}
	return feedHistoryEvent{
		ID:      strconv.FormatInt(nanos, 10) + "-0",
		Session: sessionName,
		Type:    "tool_call",
		TS:      p.TS,
		Payload: body,
	}, true
}

// extractTS prefers the `ctm_timestamp` field (RFC3339) already written
// by cmd/log-tool-use; falls back to zero time (id becomes 0-0) when
// absent. Equivalent to tailer_parse.parseTimestamp minus the now()
// fallback — for history, now() would produce a monotonically
// increasing id that breaks cursor ordering.
func extractTS(raw map[string]any) time.Time {
	if s, ok := raw["ctm_timestamp"].(string); ok {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t.UTC()
		}
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

func nestedBool(m map[string]any, path ...string) bool {
	cur := any(m)
	for _, k := range path {
		mp, ok := cur.(map[string]any)
		if !ok {
			return false
		}
		cur, ok = mp[k]
		if !ok {
			return false
		}
	}
	b, _ := cur.(bool)
	return b
}

// summariseHistoryInput mirrors tailer_parse.summariseInput — kept as
// a duplicate here to avoid importing internal/serve/ingest (would add
// a new direction to the dep graph).
func summariseHistoryInput(raw map[string]any, tool string) string {
	in, ok := raw["tool_input"].(map[string]any)
	if !ok {
		return ""
	}
	if v, ok := truncateToolInputField(tool, in); ok {
		return v
	}
	body, err := json.Marshal(in)
	if err != nil {
		return ""
	}
	return truncateHistory(string(body))
}

// summariseHistoryResponse mirrors tailer_parse.summariseResponse.
func summariseHistoryResponse(raw map[string]any) string {
	resp, ok := raw["tool_response"]
	if !ok {
		return ""
	}
	switch r := resp.(type) {
	case string:
		return truncateHistory(r)
	case map[string]any:
		if v, ok := r["output"].(string); ok {
			if i := strings.IndexByte(v, '\n'); i >= 0 {
				v = v[:i]
			}
			return truncateHistory(v)
		}
		if isErr, _ := r["is_error"].(bool); isErr {
			if msg, ok := r["error"].(string); ok {
				return truncateHistory(msg)
			}
			return "error"
		}
		keys := make([]string, 0, len(r))
		for k := range r {
			keys = append(keys, k)
		}
		if len(keys) == 0 {
			return ""
		}
		return truncateHistory("[" + strings.Join(keys, " ") + "]")
	}
	return ""
}

func truncateHistory(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= historyInputMax {
		return s
	}
	return s[:historyInputMax-3] + "…"
}

// idLessThanExt is a duplicate of events.idLessThan, inlined here so
// the api package stays event-agnostic (importing events would couple
// the http layer to the in-process pub-sub). If the hub's id scheme
// ever changes, both copies need to move — flagged in the hub.go
// comment already.
func idLessThanExt(a, b string) bool {
	an, as := splitIDExt(a)
	bn, bs := splitIDExt(b)
	if an != bn {
		return an < bn
	}
	return as < bs
}

func splitIDExt(id string) (int64, uint64) {
	for i := 0; i < len(id); i++ {
		if id[i] == '-' {
			ns, _ := strconv.ParseInt(id[:i], 10, 64)
			seq, _ := strconv.ParseUint(id[i+1:], 10, 64)
			return ns, seq
		}
	}
	return 0, 0
}
