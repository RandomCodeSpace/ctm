package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/RandomCodeSpace/ctm/internal/serve/api"
)

func newTeamsRequest(name string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/"+name+"/teams", nil)
	req.SetPathValue("name", name)
	return req
}

// TestTeams_WindowGroupsNearStarts pins the 2 s dispatch-window
// heuristic: three subagents whose starts fall inside the window
// collapse into one team; a later-arriving agent opens a second.
func TestTeams_WindowGroupsNearStarts(t *testing.T) {
	dir := t.TempDir()
	uuid := "bbbbbbbb-0000-0000-0000-000000000001"
	path := filepath.Join(dir, uuid+".jsonl")
	base := time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC)

	// Team 1 — agents a, b, c dispatched within 2 s.
	writeSubagentFixture(t, path, "a", "Explore", base, "Bash", "a", false)
	writeSubagentFixture(t, path, "b", "Explore", base.Add(500*time.Millisecond), "Bash", "b", false)
	writeSubagentFixture(t, path, "c", "Explore", base.Add(1500*time.Millisecond), "Bash", "c", false)
	// Team 2 — agent d dispatched 10 s later (way beyond window).
	writeSubagentFixture(t, path, "d", "Task", base.Add(15*time.Second), "Bash", "d", false)

	h := api.Teams(dir, subagentResolver{uuid: uuid, name: "alpha"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, newTeamsRequest("alpha"))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rr.Code, rr.Body.String())
	}
	var got struct {
		Teams []api.Team `json:"teams"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Teams) != 2 {
		t.Fatalf("len = %d, want 2; teams=%+v", len(got.Teams), got.Teams)
	}
	// Newest dispatch first → team d is [0], cluster abc is [1].
	if len(got.Teams[0].Members) != 1 || got.Teams[0].Members[0].SubagentID != "d" {
		t.Errorf("team[0] members = %+v, want [{d}]", got.Teams[0].Members)
	}
	if len(got.Teams[1].Members) != 3 {
		t.Errorf("team[1] len = %d, want 3; members=%+v", len(got.Teams[1].Members), got.Teams[1].Members)
	}
}

// TestTeams_StatusAggregation: mixed member statuses roll up per spec.
func TestTeams_StatusAggregation(t *testing.T) {
	dir := t.TempDir()
	uuid := "bbbbbbbb-0000-0000-0000-000000000002"
	path := filepath.Join(dir, uuid+".jsonl")
	base := time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC)

	// Team 1: one completed, one failed.
	writeSubagentFixture(t, path, "ok", "Explore", base, "Bash", "ok", false)
	writeSubagentFixture(t, path, "err", "Explore", base.Add(500*time.Millisecond), "Bash", "bad", true)
	// Team 2: one completed only — should be "completed".
	writeSubagentFixture(t, path, "clean", "Task", base.Add(30*time.Second), "Bash", "fine", false)

	h := api.Teams(dir, subagentResolver{uuid: uuid, name: "alpha"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, newTeamsRequest("alpha"))
	var got struct {
		Teams []api.Team `json:"teams"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	if len(got.Teams) != 2 {
		t.Fatalf("len = %d", len(got.Teams))
	}
	// Newest-first: team "clean" first, mixed team second.
	if got.Teams[0].Status != "completed" {
		t.Errorf("Teams[0].Status = %q, want completed", got.Teams[0].Status)
	}
	if got.Teams[1].Status != "failed" {
		t.Errorf("Teams[1].Status = %q, want failed (one member failed)", got.Teams[1].Status)
	}
}

// TestTeams_RunningMemberMakesTeamRunning: even with no failures, any
// currently-running member overrides "completed".
func TestTeams_RunningMemberMakesTeamRunning(t *testing.T) {
	dir := t.TempDir()
	uuid := "bbbbbbbb-0000-0000-0000-000000000003"
	path := filepath.Join(dir, uuid+".jsonl")

	// Old (completed) + fresh (running) in the same dispatch window.
	now := time.Now().UTC()
	writeSubagentFixture(t, path, "oldie", "Explore", now.Add(-1*time.Hour), "Bash", "done", false)
	writeSubagentFixture(t, path, "live", "Explore", now.Add(1*time.Second), "Bash", "still going", false)

	h := api.Teams(dir, subagentResolver{uuid: uuid, name: "alpha"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, newTeamsRequest("alpha"))
	var got struct {
		Teams []api.Team `json:"teams"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	// Two teams — the hour gap keeps them apart.
	if len(got.Teams) != 2 {
		t.Fatalf("len = %d, want 2", len(got.Teams))
	}
	// Newest team contains "live".
	if got.Teams[0].Status != "running" {
		t.Errorf("newest Status = %q, want running", got.Teams[0].Status)
	}
}

func TestTeams_404UnknownSession(t *testing.T) {
	dir := t.TempDir()
	h := api.Teams(dir, subagentResolver{uuid: "no-match", name: "other"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, newTeamsRequest("ghost"))
	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rr.Code)
	}
}

func TestTeams_405NonGET(t *testing.T) {
	dir := t.TempDir()
	h := api.Teams(dir, subagentResolver{uuid: "", name: ""})
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/alpha/teams", nil)
	req.SetPathValue("name", "alpha")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rr.Code)
	}
}

func TestTeams_EmptyLogReturnsEmptyArray(t *testing.T) {
	dir := t.TempDir()
	uuid := "bbbbbbbb-0000-0000-0000-000000000099"
	// Touch an empty log file so the resolver finds it.
	_ = os.WriteFile(filepath.Join(dir, uuid+".jsonl"), []byte{}, 0o600)
	h := api.Teams(dir, subagentResolver{uuid: uuid, name: "alpha"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, newTeamsRequest("alpha"))
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d", rr.Code)
	}
	var got struct {
		Teams []api.Team `json:"teams"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	if len(got.Teams) != 0 {
		t.Errorf("len = %d, want 0", len(got.Teams))
	}
}
