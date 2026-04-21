package ingest

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// inputSummaryMax bounds the length of best-effort string summaries
// emitted for the UI feed. Longer payloads are visible in detail
// expansions (later UI step), not the row.
const inputSummaryMax = 200

// stringField returns m[key] as a string, or "" if missing / not a
// string. JSON unmarshal stores everything as `any`, so this is the
// cheapest accessor that survives schema drift.
func stringField(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// boolAt walks a nested path of map keys and returns the leaf bool.
// Missing intermediate keys, non-map values, or non-bool leaves all
// yield false — matching the spec's lenient ingest contract.
func boolAt(m map[string]any, path ...string) bool {
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
	if b, ok := cur.(bool); ok {
		return b
	}
	return false
}

// summariseInput produces a short, human-readable summary of a hook
// payload's `tool_input` for the feed row. Per-tool conventions favour
// the field most users associate with the tool (Bash → command, Edit
// → file_path, etc.); unknown tools fall back to a JSON-encoded prefix.
func summariseInput(raw map[string]any) string {
	tool := stringField(raw, "tool_name")
	in, ok := raw["tool_input"].(map[string]any)
	if !ok {
		return ""
	}
	switch tool {
	case "Bash":
		return truncate(stringField(in, "command"))
	case "Edit", "Write", "Read", "MultiEdit", "NotebookEdit":
		return truncate(stringField(in, "file_path"))
	case "Glob":
		return truncate(stringField(in, "pattern"))
	case "Grep":
		return truncate(stringField(in, "pattern"))
	case "WebFetch":
		return truncate(stringField(in, "url"))
	case "Task":
		return truncate(stringField(in, "description"))
	}
	body, err := json.Marshal(in)
	if err != nil {
		return ""
	}
	return truncate(string(body))
}

// summariseResponse extracts a one-line summary from `tool_response`.
// Bash gets the exit-code line; for tools where the response is a
// string we truncate it; everything else returns "".
func summariseResponse(raw map[string]any) string {
	resp, ok := raw["tool_response"]
	if !ok {
		return ""
	}
	switch r := resp.(type) {
	case string:
		return truncate(r)
	case map[string]any:
		if v, ok := r["output"].(string); ok {
			return truncate(firstLine(v))
		}
		if isErr, _ := r["is_error"].(bool); isErr {
			if msg, ok := r["error"].(string); ok {
				return truncate(msg)
			}
			return "error"
		}
		// Fall through to a generic key list so the UI shows *something*.
		keys := make([]string, 0, len(r))
		for k := range r {
			keys = append(keys, k)
		}
		if len(keys) == 0 {
			return ""
		}
		return truncate(fmt.Sprintf("%v", keys))
	}
	return ""
}

// parseTimestamp prefers ctm's appended `ctm_timestamp` (always
// RFC3339, written by `cmd/log-tool-use`); falls back to time.Now()
// when absent or unparseable.
func parseTimestamp(raw map[string]any) time.Time {
	if s, ok := raw["ctm_timestamp"].(string); ok {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t
		}
	}
	return time.Now().UTC()
}

func truncate(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= inputSummaryMax {
		return s
	}
	// "…" is 3 bytes (U+2026), so trim to inputSummaryMax-3 to keep the
	// total byte length at exactly inputSummaryMax.
	return s[:inputSummaryMax-3] + "…"
}

func firstLine(s string) string {
	if before, _, ok := strings.Cut(s, "\n"); ok {
		return before
	}
	return s
}
