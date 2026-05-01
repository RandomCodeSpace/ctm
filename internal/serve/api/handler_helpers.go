package api

import (
	"net/http"
)

// requireSessionPreamble runs the boilerplate every /api/sessions/{name}/...
// JSON GET handler needs: enforce GET/HEAD only, set the standard
// Content-Type + Cache-Control headers, extract the {name} path param, and
// reject an empty name. Returns the session name on success; on failure it
// has already written the error response and the caller should return.
//
// Sonar previously flagged subagents.go, teams.go, etc. as duplicating this
// 16-line block — lifting it removes the copy-paste while keeping each
// handler's mode-specific logic local.
func requireSessionPreamble(w http.ResponseWriter, r *http.Request) (name string, ok bool) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		w.Header().Set("Cache-Control", "no-store")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return "", false
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")

	name = r.PathValue("name")
	if name == "" {
		writeJSON(w, http.StatusBadRequest, errorBody{Error: "session name required"})
		return "", false
	}
	return name, true
}

// truncateToolInputField returns the well-known "primary input" string for
// the named tool — Bash → command, Edit/Write/etc. → file_path, Glob/Grep
// → pattern, WebFetch → url, Task → description. Returns ok=false for tools
// the switch doesn't know about so the caller can pick its own fallback
// (the feed-history summariser JSON-marshals the whole input map; the
// subagent label scans description/prompt/query).
//
// Both shortestSubagentInputLabel (subagents.go) and summariseHistoryInput
// (feed_history.go) used to inline the same get-closure + switch — Sonar
// reported the duplicate, this consolidates the per-tool routing.
func truncateToolInputField(tool string, in map[string]any) (string, bool) {
	get := func(k string) string {
		if v, ok := in[k].(string); ok {
			return v
		}
		return ""
	}
	switch tool {
	case "Bash":
		return truncateHistory(get("command")), true
	case "Edit", "Write", "Read", "MultiEdit", "NotebookEdit":
		return truncateHistory(get("file_path")), true
	case "Glob", "Grep":
		return truncateHistory(get("pattern")), true
	case "WebFetch":
		return truncateHistory(get("url")), true
	case "Task":
		return truncateHistory(get("description")), true
	}
	return "", false
}
