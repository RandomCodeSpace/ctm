// Package api — V15: /api/sessions/{name}/subagents.
//
// Returns the forest of subagents for a session, computed by replaying
// the session's JSONL log and grouping tool_call rows by their
// top-level `agent_id` field. Each unique (session, agent_id) pair
// produces one tree node; the `parent_id` field is reserved for a
// future Claude Code schema change (the current JSONL shape doesn't
// carry a parent pointer, so every node is a root today).
//
// Mount (wired in server.go alongside the other /api/sessions/{name}
// routes — the coordinator pastes these lines into registerRoutes):
//
//	mux.Handle("GET /api/sessions/{name}/subagents",
//	    authHF(api.Subagents(s.logDir, logsUUIDResolver{proj: s.proj})))
//
// Shape:
//
//	{
//	  "subagents": [
//	    {
//	      "id": "ada78973e092dae52",
//	      "parent_id": null,
//	      "type": "Explore",
//	      "description": "cat README.md",
//	      "started_at": "2026-04-21T12:00:00Z",
//	      "stopped_at": "2026-04-21T12:02:10Z",
//	      "tool_calls": 7,
//	      "status": "completed"
//	    },
//	    ...
//	  ]
//	}
//
// Newest root first; children newest-first (see orderNodes below —
// trees are flat today so this is a simple top-level sort). Cap at
// maxSubagentsPerSession (500).
//
// `since` query param (RFC3339) — when present, rows with
// `started_at <= since` are elided server-side to reduce the payload
// on re-fetch after an SSE wake-up.
//
// Completion status is inferred: a subagent is "running" when its
// last observed tool_call is within runningGrace (5 s) of now AND
// no tool_response error bit is set; "failed" when any of its tool
// calls returned an error; otherwise "completed".
package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// maxSubagentsPerSession caps the response at the same 500 as the
// hub's per-session ring — keeps the response predictable on long
// transcripts.
const maxSubagentsPerSession = 500

// runningGrace is the staleness window past the most recent tool_call
// before we flip a subagent's status from "running" to "completed".
// Matches the attention engine's "stalled" trigger ballpark so live
// agents and "just-finished" agents don't bounce between the two.
const runningGrace = 5 * time.Second

// SubagentNode is a single row in the /subagents response. ParentID
// is a pointer so JSON emits `null` when absent (rather than the zero
// string "").
type SubagentNode struct {
	ID          string     `json:"id"`
	ParentID    *string    `json:"parent_id"`
	Type        string     `json:"type"`
	Description string     `json:"description"`
	StartedAt   time.Time  `json:"started_at"`
	StoppedAt   *time.Time `json:"stopped_at,omitempty"`
	ToolCalls   int        `json:"tool_calls"`
	Status      string     `json:"status"`
}

type subagentsResponse struct {
	Subagents []SubagentNode `json:"subagents"`
}

// Subagents returns the GET handler for /api/sessions/{name}/subagents.
// logDir is the JSONL tailer directory; resolver maps session name →
// log UUID via the same `claudeDirToName` fallback used elsewhere in
// the api package.
func Subagents(logDir string, resolver UUIDNameResolver) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name, ok := requireSessionPreamble(w, r)
		if !ok {
			return
		}

		var since time.Time
		if raw := r.URL.Query().Get("since"); raw != "" {
			t, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, errorBody{Error: "since must be RFC3339", Name: name})
				return
			}
			since = t.UTC()
		}

		uuid, ok := resolveNameToUUID(resolver, logDir, name)
		if !ok {
			writeJSON(w, http.StatusNotFound, errorBody{Error: "session not found", Name: name})
			return
		}

		path := filepath.Join(logDir, uuid+".jsonl")
		nodes, err := buildSubagentForest(path, time.Now().UTC(), since)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				writeJSON(w, http.StatusOK, subagentsResponse{Subagents: []SubagentNode{}})
				return
			}
			writeJSON(w, http.StatusInternalServerError, errorBody{Error: "read failed", Name: name})
			return
		}

		if len(nodes) > maxSubagentsPerSession {
			nodes = nodes[:maxSubagentsPerSession]
		}

		writeJSON(w, http.StatusOK, subagentsResponse{Subagents: nodes})
	}
}

// buildSubagentForest replays the JSONL at path, grouping by
// agent_id. `now` drives the running/completed cutoff;
// `since` (zero-value = include all) filters out any subagent whose
// started_at is <= since, which is how the `since=` query param
// trims re-fetch payloads.
//
// Exported for reuse by teams.go.
func buildSubagentForest(path string, now, since time.Time) ([]SubagentNode, error) {
	return buildSubagentForestFromReader(path, nil, now, since)
}

// buildSubagentForestFromReader is the test seam. When openOverride
// is non-nil it returns the fixture reader instead of opening `path`;
// production passes nil and we fall through to os.Open.
func buildSubagentForestFromReader(
	path string,
	openOverride func(string) (io.ReadCloser, error),
	now, since time.Time,
) ([]SubagentNode, error) {
	var (
		rc  io.ReadCloser
		err error
	)
	if openOverride != nil {
		rc, err = openOverride(path)
	} else {
		rc, err = os.Open(path)
	}
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	all, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}

	// Group by agent_id. A map preserves O(n) replay; the ordering
	// step at the end re-sorts by started_at descending so the forest
	// is newest-first.
	type accumulator struct {
		node     SubagentNode
		anyError bool
		lastTS   time.Time
	}
	byID := make(map[string]*accumulator)
	order := make([]string, 0)

	for _, rawLine := range bytes.Split(all, []byte{'\n'}) {
		line := bytes.TrimRight(rawLine, "\r")
		if len(line) == 0 {
			continue
		}
		meta, ok := parseSubagentLine(line)
		if !ok {
			continue
		}
		// De-duplicate the agent_id → tree-node bookkeeping.
		acc, exists := byID[meta.AgentID]
		if !exists {
			acc = &accumulator{
				node: SubagentNode{
					ID:          meta.AgentID,
					Type:        meta.AgentType,
					Description: meta.Input,
					StartedAt:   meta.TS,
				},
			}
			byID[meta.AgentID] = acc
			order = append(order, meta.AgentID)
		}
		acc.node.ToolCalls++
		if meta.IsError {
			acc.anyError = true
		}
		// Keep the highest ts as the "stopped_at" candidate; track
		// it in a local so we can later decide running vs completed.
		if meta.TS.After(acc.lastTS) {
			acc.lastTS = meta.TS
		}
		// If a later row has a better description, keep the first
		// non-empty one for stability — users get jumpy when row
		// descriptions swap mid-stream.
		if acc.node.Description == "" && meta.Input != "" {
			acc.node.Description = meta.Input
		}
	}

	out := make([]SubagentNode, 0, len(byID))
	for _, id := range order {
		acc := byID[id]
		node := acc.node
		if !acc.lastTS.IsZero() {
			last := acc.lastTS
			if now.Sub(last) > runningGrace {
				// Subagent went quiet — treat last-seen ts as the
				// stopped_at marker.
				ls := last
				node.StoppedAt = &ls
			}
		}
		switch {
		case acc.anyError:
			node.Status = "failed"
		case node.StoppedAt == nil:
			node.Status = "running"
		default:
			node.Status = "completed"
		}
		if !since.IsZero() && !node.StartedAt.After(since) {
			continue
		}
		out = append(out, node)
	}

	// Newest root first. Deterministic tie-break on id so tests that
	// stamp the same timestamp on multiple subagents don't flake.
	sort.Slice(out, func(i, j int) bool {
		if !out[i].StartedAt.Equal(out[j].StartedAt) {
			return out[i].StartedAt.After(out[j].StartedAt)
		}
		return out[i].ID > out[j].ID
	})
	return out, nil
}

// parseSubagentLine is a local duplicate of ingest.parseSubagentMeta
// kept here so the api package does not import internal/serve/ingest
// (would add a new direction to the dep graph). The shape is trivial;
// both copies stay in sync via the tests.
type subagentLineMeta struct {
	AgentID   string
	AgentType string
	Input     string
	IsError   bool
	TS        time.Time
}

func parseSubagentLine(line []byte) (subagentLineMeta, bool) {
	var raw map[string]any
	if err := json.Unmarshal(line, &raw); err != nil {
		return subagentLineMeta{}, false
	}
	agentID, _ := raw["agent_id"].(string)
	if agentID == "" {
		return subagentLineMeta{}, false
	}
	agentType, _ := raw["agent_type"].(string)
	tool, _ := raw["tool_name"].(string)
	input := ""
	if in, ok := raw["tool_input"].(map[string]any); ok {
		input = shortestSubagentInputLabel(tool, in)
	}
	ts := extractTS(raw)
	if ts.IsZero() {
		// Fall back to zero — handler re-fetches clamp this to
		// "unknown" and still produce a stable id ordering via
		// agent_id lexical sort when timestamps collide.
	}
	return subagentLineMeta{
		AgentID:   agentID,
		AgentType: agentType,
		Input:     input,
		IsError:   nestedBool(raw, "tool_response", "is_error"),
		TS:        ts,
	}, true
}

// shortestSubagentInputLabel picks the most human-readable single
// field from a tool_input map for the subagent row's `description`.
// Mirrors the feed-row summariser's per-tool conventions so a
// subagent expanded in the UI matches the feed rows shown below it.
func shortestSubagentInputLabel(tool string, in map[string]any) string {
	if v, ok := truncateToolInputField(tool, in); ok {
		return v
	}
	// Fallback: any "description"-ish key.
	for _, k := range []string{"description", "prompt", "query"} {
		if v, ok := in[k].(string); ok && v != "" {
			return truncateHistory(v)
		}
	}
	return ""
}
