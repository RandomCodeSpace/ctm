// Package ingest — subagent event shapes (V15) + team aggregation
// primitives (V16).
//
// The Claude Code JSONL transcripts do not carry dedicated
// `subagent_start` / `subagent_stop` / `team_spawn` rows. Instead,
// each tool_call row optionally carries top-level `agent_id` +
// `agent_type` fields (appended by the PostToolUse hook when the tool
// call was dispatched via the Agent/Task tool). V15 treats the first
// occurrence of a given (session, agent_id) pair as the subagent's
// start and the last occurrence as its stop.
//
// To keep the ingest path additive — existing tool_call parsing must
// continue to work untouched per the V15/V16 brief — subagent
// metadata is surfaced three ways:
//
//  1. Tailer emits a sibling `subagent_start` hub event the first
//     time a given agent_id is seen on a session. The payload carries
//     enough context for the UI to wake up and refetch
//     /api/sessions/{name}/subagents (it does NOT try to be a
//     self-contained tree — computing the full tree from a stream of
//     live events would duplicate the replay logic and drift).
//  2. The /subagents REST handler (api.Subagents) replays the session
//     JSONL and groups rows by agent_id to produce the forest.
//  3. The /teams REST handler (api.Teams) groups subagents that start
//     within a bounded time window (see teamWindow below) into a
//     single "team" — the closest primitive the real-world JSONL
//     supports to the V16 brief's "agent_team" concept.
//
// Future work: if Claude Code starts emitting explicit
// `subagent_start` / `subagent_stop` rows (different `hook_event_name`
// or a dedicated field), update parseSubagentMeta to surface the
// lifecycle directly rather than infer it from first/last tool_call.
package ingest

import (
	"encoding/json"
	"time"

	"github.com/RandomCodeSpace/ctm/internal/serve/events"
)

// SubagentMeta is the minimal envelope a single JSONL tool_call row
// contributes to the subagent forest. All fields are best-effort;
// ok=false from parseSubagentMeta signals "this row does not belong
// to any subagent".
type SubagentMeta struct {
	AgentID   string    `json:"agent_id"`
	AgentType string    `json:"agent_type"`
	Tool      string    `json:"tool"`
	Input     string    `json:"input,omitempty"`
	IsError   bool      `json:"is_error"`
	TS        time.Time `json:"ts"`
}

// SubagentStartPayload is the hub-event payload emitted once per
// session+agent_id when the tailer first observes a new subagent. The
// UI treats it purely as a wake-up signal — the full tree is fetched
// via the REST endpoint.
type SubagentStartPayload struct {
	Session   string    `json:"session"`
	AgentID   string    `json:"agent_id"`
	AgentType string    `json:"agent_type"`
	TS        time.Time `json:"ts"`
}

// parseSubagentMeta reads just the subagent-relevant fields out of a
// raw JSONL line. Returns ok=false for any row that does not carry a
// non-empty `agent_id`.
//
// Duplicated field-pulling rather than reusing tailer_parse helpers
// so that (a) this path is cheap to run on every scanned line, and
// (b) future changes to the feed-row summariser can't accidentally
// change the subagent tree's `description` column.
func parseSubagentMeta(line []byte) (SubagentMeta, bool) {
	var raw map[string]any
	if err := json.Unmarshal(line, &raw); err != nil {
		return SubagentMeta{}, false
	}
	agentID, _ := raw["agent_id"].(string)
	if agentID == "" {
		return SubagentMeta{}, false
	}
	agentType, _ := raw["agent_type"].(string)
	tool, _ := raw["tool_name"].(string)
	return SubagentMeta{
		AgentID:   agentID,
		AgentType: agentType,
		Tool:      tool,
		Input:     summariseInput(raw),
		IsError:   boolAt(raw, "tool_response", "is_error"),
		TS:        parseTimestamp(raw),
	}, true
}

// buildSubagentStartEvent wraps the meta in a hub-ready Event. Kept
// separate from parseSubagentMeta so tests can exercise the two
// halves independently.
func buildSubagentStartEvent(sessionName string, meta SubagentMeta) (events.Event, error) {
	body, err := json.Marshal(SubagentStartPayload{
		Session:   sessionName,
		AgentID:   meta.AgentID,
		AgentType: meta.AgentType,
		TS:        meta.TS,
	})
	if err != nil {
		return events.Event{}, err
	}
	return events.Event{
		Type:    "subagent_start",
		Session: sessionName,
		Payload: body,
	}, nil
}
