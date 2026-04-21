// Package api — /api/sessions/{name}/tool_calls/{id}/detail surfaces
// the full tool-call input (and, for Edit/MultiEdit/Write, a unified
// diff) on-demand so the Feed tab can expand any row without paying
// for a richer hub Event payload at ingest time.
//
// Wiring (paste into internal/serve/server.go registerRoutes, next to
// the other /api/sessions/{name}/... handlers):
//
//     mux.Handle(
//         "GET /api/sessions/{name}/tool_calls/{id}/detail",
//         authHF(api.ToolCallDetail(api.NewJSONLLogReader(s.logDir, s.proj))),
//     )
//
// `s.logDir` is the absolute path to ~/.config/ctm/logs (same value
// already passed to `api.LogsUsage` on line 463). `s.proj` is the
// ingest.Projection used to map human session name → Claude UUID.
package api

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/RandomCodeSpace/ctm/internal/serve/ingest"
)

// jsonlScanCap bounds how many bytes we'll read from a single session
// JSONL file while hunting for a matching ts. 5 MB at ~1 KB/line is
// ~5k tool calls — two orders of magnitude above the hub ring cap of
// 500, so any ID still referenced by the UI is comfortably reachable.
const jsonlScanCap = 5 << 20

// tsMatchTolerance is how far apart the RFC3339 `ts` inside the JSONL
// line and the unix-second prefix of the hub Event.ID may be while
// still matching. Tailer uses time.Now().UTC() when the hook payload
// omits ctm_timestamp, but hub.Publish also stamps its own monotonic
// clock — the two sources can disagree by a few hundred ms under
// load. ±1 s covers that safely without matching adjacent events.
const tsMatchTolerance = time.Second

// ErrDetailNotFound is the sentinel the LogReader returns when the
// requested id does not correspond to any line in the session's JSONL
// (either the file is missing, the id is too old to still be on disk,
// or the scan hit the 5 MB cap without a match).
var ErrDetailNotFound = errors.New("tool call detail not found")

// Detail is the JSON response shape for GET
// /api/sessions/{name}/tool_calls/{id}/detail.
//
// InputJSON is always the raw `tool_input` sub-object re-encoded as
// compact JSON so the UI can render it as a code block without having
// to re-marshal. Diff is only populated when Tool ∈ {Edit,MultiEdit,
// Write}; empty otherwise.
type Detail struct {
	Tool           string `json:"tool"`
	InputJSON      string `json:"input_json"`
	OutputExcerpt  string `json:"output_excerpt"`
	TS             string `json:"ts"`
	IsError        bool   `json:"is_error"`
	Diff           string `json:"diff,omitempty"`
}

// LogReader is the seam the handler talks to. Production wires
// JSONLLogReader; tests pass a fake. Keeping this narrow means the
// handler test doesn't need to touch the filesystem.
type LogReader interface {
	// ReadDetail returns the Detail for (sessionName, id). On a
	// clean miss it must return ErrDetailNotFound so the handler can
	// emit a 404 without logging a 5xx.
	ReadDetail(sessionName, id string) (Detail, error)
}

// ToolCallDetail returns the handler for
// GET /api/sessions/{name}/tool_calls/{id}/detail.
//
// reader is the seam described on LogReader. Responses are always
// application/json. Errors map as:
//
//   - 405 on non-GET
//   - 400 on empty name / id
//   - 404 on ErrDetailNotFound
//   - 500 on any other reader error
func ToolCallDetail(reader LogReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			w.Header().Set("Cache-Control", "no-store")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		name := r.PathValue("name")
		id := r.PathValue("id")
		if name == "" || id == "" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		detail, err := reader.ReadDetail(name, id)
		if err != nil {
			if errors.Is(err, ErrDetailNotFound) {
				http.NotFound(w, r)
				return
			}
			slog.Error("tool call detail lookup failed",
				"session", name, "id", id, "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(detail)
	}
}

// UUIDResolver maps a human session name to its Claude session UUID.
// Narrower than ingest.Projection so the reader stays testable.
type UUIDResolver interface {
	ResolveName(sessionName string) (uuid string, ok bool)
}

// projUUIDAdapter bridges ingest.Projection to UUIDResolver; used by
// the production wiring in server.go.
type projUUIDAdapter struct{ proj *ingest.Projection }

func (a projUUIDAdapter) ResolveName(name string) (string, bool) {
	s, ok := a.proj.Get(name)
	if !ok {
		return "", false
	}
	return s.UUID, s.UUID != ""
}

// JSONLLogReader is the production LogReader. It maps session name →
// Claude UUID, scans ~/.config/ctm/logs/<uuid>.jsonl from end-to-start
// matching on the hub Event.ID's nanosecond prefix against each
// line's `ctm_timestamp` (or, lacking that, by falling back to the
// newest line within the scan cap).
type JSONLLogReader struct {
	LogDir   string
	Resolver UUIDResolver
}

// NewJSONLLogReader wires a production reader against the tailer log
// directory and the ingest projection.
func NewJSONLLogReader(logDir string, proj *ingest.Projection) *JSONLLogReader {
	return &JSONLLogReader{
		LogDir:   logDir,
		Resolver: projUUIDAdapter{proj: proj},
	}
}

// ReadDetail implements LogReader. Errors other than ErrDetailNotFound
// are I/O-level and surface as 500.
func (r *JSONLLogReader) ReadDetail(sessionName, id string) (Detail, error) {
	if r == nil || r.Resolver == nil {
		return Detail{}, ErrDetailNotFound
	}
	uuid, ok := r.Resolver.ResolveName(sessionName)
	if !ok || uuid == "" {
		return Detail{}, ErrDetailNotFound
	}
	path := filepath.Join(r.LogDir, uuid+".jsonl")

	targetSec, hasTarget := idNanoSec(id)

	data, err := readTail(path, jsonlScanCap)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Detail{}, ErrDetailNotFound
		}
		return Detail{}, err
	}

	// Walk end → start. Each line is a hook payload with the same
	// shape the tailer parses.
	lines := splitLinesReverse(data)
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		var raw map[string]any
		if err := json.Unmarshal(line, &raw); err != nil {
			continue
		}
		if hasTarget {
			lineSec, ok := rawTimestampSec(raw)
			if !ok {
				continue
			}
			if absDiff(lineSec, targetSec) > int64(tsMatchTolerance/time.Second) {
				continue
			}
		}
		return buildDetail(raw), nil
	}

	return Detail{}, ErrDetailNotFound
}

// idNanoSec extracts the unix-second prefix of a hub Event.ID of the
// form "<unix-nano>-<seq>". Returns ok=false for malformed input.
func idNanoSec(id string) (int64, bool) {
	left, _, ok := strings.Cut(id, "-")
	if !ok {
		return 0, false
	}
	ns, err := strconv.ParseInt(left, 10, 64)
	if err != nil {
		return 0, false
	}
	return ns / int64(time.Second), true
}

// rawTimestampSec parses a JSONL row's `ctm_timestamp` into a unix
// second, matching the ingest.parseTimestamp contract. Missing or
// malformed stamps fall back to the hook's top-level `ts` RFC3339
// string if present (some older payloads).
func rawTimestampSec(raw map[string]any) (int64, bool) {
	candidates := []string{"ctm_timestamp", "ts"}
	for _, k := range candidates {
		if s, ok := raw[k].(string); ok {
			if t, err := time.Parse(time.RFC3339, s); err == nil {
				return t.Unix(), true
			}
		}
	}
	return 0, false
}

func absDiff(a, b int64) int64 {
	if a > b {
		return a - b
	}
	return b - a
}

// buildDetail converts a raw hook payload into a Detail. Missing
// fields degrade gracefully per the tailer's lenient-ingest contract.
func buildDetail(raw map[string]any) Detail {
	tool := stringFromMap(raw, "tool_name")
	isErr := false
	if tr, ok := raw["tool_response"].(map[string]any); ok {
		if b, ok := tr["is_error"].(bool); ok {
			isErr = b
		}
	}
	inputJSON := ""
	if in, ok := raw["tool_input"].(map[string]any); ok {
		if b, err := json.Marshal(in); err == nil {
			inputJSON = string(b)
		}
	}
	output := ""
	if tr, ok := raw["tool_response"].(map[string]any); ok {
		if s, ok := tr["output"].(string); ok {
			output = truncateExcerpt(s, 4096)
		} else if s, ok := tr["error"].(string); ok {
			output = truncateExcerpt(s, 4096)
		}
	}
	ts := stringFromMap(raw, "ctm_timestamp")
	if ts == "" {
		ts = stringFromMap(raw, "ts")
	}

	d := Detail{
		Tool:          tool,
		InputJSON:     inputJSON,
		OutputExcerpt: output,
		TS:            ts,
		IsError:       isErr,
	}
	if diff := renderDiff(tool, raw); diff != "" {
		d.Diff = diff
	}
	return d
}

// renderDiff builds a unified-diff snippet for Edit/MultiEdit/Write.
// Returns "" for any other tool or when the payload is too malformed
// to diff (rather than faking an empty hunk).
func renderDiff(tool string, raw map[string]any) string {
	in, ok := raw["tool_input"].(map[string]any)
	if !ok {
		return ""
	}
	switch tool {
	case "Edit":
		return renderEditDiff(in)
	case "MultiEdit":
		return renderMultiEditDiff(in)
	case "Write":
		return renderWriteDiff(in)
	default:
		return ""
	}
}

func renderEditDiff(in map[string]any) string {
	path := stringFromMap(in, "file_path")
	oldS := stringFromMap(in, "old_string")
	newS := stringFromMap(in, "new_string")
	if path == "" {
		return ""
	}
	var b strings.Builder
	writeDiffHeader(&b, path)
	writeHunk(&b, oldS, newS)
	return b.String()
}

func renderMultiEditDiff(in map[string]any) string {
	path := stringFromMap(in, "file_path")
	edits, ok := in["edits"].([]any)
	if path == "" || !ok || len(edits) == 0 {
		return ""
	}
	var b strings.Builder
	writeDiffHeader(&b, path)
	for _, e := range edits {
		em, ok := e.(map[string]any)
		if !ok {
			continue
		}
		writeHunk(&b, stringFromMap(em, "old_string"), stringFromMap(em, "new_string"))
	}
	return b.String()
}

func renderWriteDiff(in map[string]any) string {
	path := stringFromMap(in, "file_path")
	content := stringFromMap(in, "content")
	if path == "" {
		return ""
	}
	var b strings.Builder
	writeDiffHeader(&b, path)
	// Write replaces the entire file — render as a single all-added
	// hunk. The "old" side is empty; line counts reflect that.
	newLines := splitForDiff(content)
	fmt.Fprintf(&b, "@@ -0,0 +1,%d @@\n", len(newLines))
	for _, l := range newLines {
		b.WriteString("+")
		b.WriteString(l)
		b.WriteString("\n")
	}
	return b.String()
}

func writeDiffHeader(b *strings.Builder, path string) {
	fmt.Fprintf(b, "--- a/%s\n", path)
	fmt.Fprintf(b, "+++ b/%s\n", path)
}

// writeHunk emits a minimal `@@ ... @@` block for a single Edit-style
// old/new pair. Line numbers start at 1 because we don't track where
// in the file the hunk lives — the Edit tool's old_string match is
// unique within the file, so the UI presents this purely as a
// before/after snippet rather than a locatable patch.
func writeHunk(b *strings.Builder, oldS, newS string) {
	oldLines := splitForDiff(oldS)
	newLines := splitForDiff(newS)
	oldCount := len(oldLines)
	newCount := len(newLines)
	if oldS == "" {
		oldCount = 0
	}
	if newS == "" {
		newCount = 0
	}
	oldStart := 1
	if oldCount == 0 {
		oldStart = 0
	}
	newStart := 1
	if newCount == 0 {
		newStart = 0
	}
	fmt.Fprintf(b, "@@ -%d,%d +%d,%d @@\n", oldStart, oldCount, newStart, newCount)
	if oldS != "" {
		for _, l := range oldLines {
			b.WriteString("-")
			b.WriteString(l)
			b.WriteString("\n")
		}
	}
	if newS != "" {
		for _, l := range newLines {
			b.WriteString("+")
			b.WriteString(l)
			b.WriteString("\n")
		}
	}
}

// splitForDiff splits on "\n" without dropping a trailing empty line
// that represents a real final newline in the source — callers only
// render it when the original string was non-empty so this is safe.
func splitForDiff(s string) []string {
	if s == "" {
		return nil
	}
	out := strings.Split(s, "\n")
	// If the string ends with '\n', Split produces a trailing "" —
	// drop it so we don't emit a spurious "+" line.
	if len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

// stringFromMap is a local helper — we can't import ingest's
// unexported stringField. Keeping the code duplicated here is cheaper
// than exporting ingest internals just for this.
func stringFromMap(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func truncateExcerpt(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

// readTail reads the last `cap` bytes of the file at path. Returns
// the full content when the file is smaller than cap. os.ErrNotExist
// is propagated so the caller can emit a 404.
func readTail(path string, cap int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := st.Size()
	readFrom := int64(0)
	if size > int64(cap) {
		readFrom = size - int64(cap)
	}
	if _, err := f.Seek(readFrom, io.SeekStart); err != nil {
		return nil, err
	}
	// If we skipped a partial line at the tail-start boundary, drop
	// the first partial line so we don't hand bufio a malformed row.
	br := bufio.NewReader(f)
	if readFrom > 0 {
		if _, err := br.ReadBytes('\n'); err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
	}
	data, err := io.ReadAll(br)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return data, nil
}

// splitLinesReverse splits `data` on "\n" and returns the lines in
// reverse (newest-first) order. Empty trailing line (from a terminal
// "\n") is dropped.
func splitLinesReverse(data []byte) [][]byte {
	// Walk backwards so we don't allocate a forward slice first.
	out := make([][]byte, 0, 32)
	end := len(data)
	for i := len(data) - 1; i >= 0; i-- {
		if data[i] == '\n' {
			if i+1 < end {
				out = append(out, data[i+1:end])
			}
			end = i
		}
	}
	if end > 0 {
		out = append(out, data[:end])
	}
	return out
}
