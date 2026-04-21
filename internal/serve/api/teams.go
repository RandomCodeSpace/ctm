// Package api — V16: /api/sessions/{name}/teams.
//
// A "team" is a group of subagents dispatched within a tight time
// window (teamWindow). The Claude Code JSONL doesn't carry an
// explicit `team_name` or `team_spawn` row today, so the team shape
// is inferred from the same replay as V15: any pair of subagents
// whose `started_at` timestamps fall inside teamWindow get merged
// into a single team. When the schema grows a dedicated team_name
// field, extend parseSubagentLine to surface it and switch the group
// key here.
//
// Mount (coordinator pastes into registerRoutes in server.go):
//
//	mux.Handle("GET /api/sessions/{name}/teams",
//	    authHF(api.Teams(s.logDir, logsUUIDResolver{proj: s.proj})))
//
// Shape:
//
//	{
//	  "teams": [
//	    {
//	      "id": "team-<earliest-agent-id>",
//	      "name": "Explore · 3 agents",
//	      "dispatched_at": "2026-04-21T12:00:00Z",
//	      "status": "completed",
//	      "summary": null,
//	      "members": [
//	        {"subagent_id":"abc","description":"...","status":"completed"},
//	        ...
//	      ]
//	    }
//	  ]
//	}
//
// Status roll-up:
//   - any member running → team is "running"
//   - any member failed  → team is "failed"
//   - else                → team is "completed"
//
// Summary is currently always null (no `team_summary` event exists in
// the JSONL yet); the field stays in the contract so the UI can
// render a blockquote when one lands later.
package api

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// teamWindow is the maximum delta between two subagent start times
// before we consider them to be part of separate teams. 2s matches
// the rough cadence of a parallel dispatch from the Task/Agent tool —
// tests can override via the exported TeamWindowForTest setter below.
const teamWindow = 2 * time.Second

// maxTeamsPerSession caps the response at the same 500-ish ceiling as
// V15, for the same reason (predictable payload on long transcripts).
const maxTeamsPerSession = 500

// TeamMember mirrors a single row in team.members.
type TeamMember struct {
	SubagentID  string `json:"subagent_id"`
	Description string `json:"description"`
	Status      string `json:"status"`
}

// Team is a single row in the /teams response.
type Team struct {
	ID           string       `json:"id"`
	Name         string       `json:"name"`
	DispatchedAt time.Time    `json:"dispatched_at"`
	Status       string       `json:"status"`
	Summary      *string      `json:"summary,omitempty"`
	Members      []TeamMember `json:"members"`
}

type teamsResponse struct {
	Teams []Team `json:"teams"`
}

// Teams returns GET /api/sessions/{name}/teams.
func Teams(logDir string, resolver UUIDNameResolver) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
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

		uuid, ok := resolveNameToUUID(resolver, logDir, name)
		if !ok {
			writeJSON(w, http.StatusNotFound, errorBody{Error: "session not found", Name: name})
			return
		}

		path := filepath.Join(logDir, uuid+".jsonl")
		teams, err := buildTeamsFromForest(path, time.Now().UTC())
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				writeJSON(w, http.StatusOK, teamsResponse{Teams: []Team{}})
				return
			}
			writeJSON(w, http.StatusInternalServerError, errorBody{Error: "read failed", Name: name})
			return
		}

		if len(teams) > maxTeamsPerSession {
			teams = teams[:maxTeamsPerSession]
		}
		writeJSON(w, http.StatusOK, teamsResponse{Teams: teams})
	}
}

// buildTeamsFromForest computes the team list for a session by first
// walking the JSONL to produce the subagent forest, then bucketing
// subagents into teams whose start times cluster within teamWindow.
//
// `now` drives the underlying forest builder's running/completed
// cutoff — propagating it here keeps teams.status roll-up consistent
// with /subagents at the same instant.
//
// Public via the Teams handler only; kept unexported to leave room
// for evolving the team shape without breaking callers.
func buildTeamsFromForest(path string, now time.Time) ([]Team, error) {
	nodes, err := buildSubagentForest(path, now, time.Time{})
	if err != nil {
		return nil, err
	}
	if len(nodes) == 0 {
		return []Team{}, nil
	}

	// Sort by started_at ASC so we can sweep left→right and open a
	// new team whenever the delta exceeds teamWindow. (The forest
	// builder returns newest-first; we reverse for sweeping and
	// reverse the final teams slice to preserve newest-first output.)
	asc := make([]SubagentNode, len(nodes))
	copy(asc, nodes)
	sort.Slice(asc, func(i, j int) bool {
		if !asc[i].StartedAt.Equal(asc[j].StartedAt) {
			return asc[i].StartedAt.Before(asc[j].StartedAt)
		}
		return asc[i].ID < asc[j].ID
	})

	type bucket struct {
		first time.Time
		last  time.Time
		ids   []SubagentNode
	}
	var buckets []*bucket
	for _, n := range asc {
		if len(buckets) == 0 {
			buckets = append(buckets, &bucket{
				first: n.StartedAt,
				last:  n.StartedAt,
				ids:   []SubagentNode{n},
			})
			continue
		}
		cur := buckets[len(buckets)-1]
		// Gap relative to the cluster's last start, so a rolling
		// burst of dispatches (e.g. 4 agents at t, t+1s, t+2s, t+3s)
		// still collapses into a single team.
		if n.StartedAt.Sub(cur.last) <= teamWindow {
			cur.ids = append(cur.ids, n)
			cur.last = n.StartedAt
			continue
		}
		buckets = append(buckets, &bucket{
			first: n.StartedAt,
			last:  n.StartedAt,
			ids:   []SubagentNode{n},
		})
	}

	out := make([]Team, 0, len(buckets))
	for _, b := range buckets {
		if len(b.ids) == 0 {
			continue
		}
		members := make([]TeamMember, 0, len(b.ids))
		runningCount, failedCount := 0, 0
		// Capture the dominant agent_type for the team's display name
		// (first member wins — same stability reasoning as
		// SubagentNode.Description).
		primaryType := b.ids[0].Type
		for _, n := range b.ids {
			if n.Status == "running" {
				runningCount++
			}
			if n.Status == "failed" {
				failedCount++
			}
			members = append(members, TeamMember{
				SubagentID:  n.ID,
				Description: n.Description,
				Status:      n.Status,
			})
		}
		status := "completed"
		switch {
		case runningCount > 0:
			status = "running"
		case failedCount > 0:
			status = "failed"
		}
		name := fmt.Sprintf("%s · %d agents", primaryType, len(b.ids))
		if primaryType == "" {
			name = fmt.Sprintf("%d agents", len(b.ids))
		}
		out = append(out, Team{
			ID:           "team-" + b.ids[0].ID,
			Name:         name,
			DispatchedAt: b.first,
			Status:       status,
			Summary:      nil,
			Members:      members,
		})
	}

	// Newest dispatch first.
	sort.Slice(out, func(i, j int) bool {
		if !out[i].DispatchedAt.Equal(out[j].DispatchedAt) {
			return out[i].DispatchedAt.After(out[j].DispatchedAt)
		}
		return out[i].ID > out[j].ID
	})
	return out, nil
}
