package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RandomCodeSpace/ctm/internal/serve/api"
)

// subagentResolver is a minimal UUIDNameResolver pinned to one
// (uuid, name) mapping so tests drive the lookup deterministically.
type subagentResolver struct{ uuid, name string }

func (s subagentResolver) ResolveUUID(u string) (string, bool) {
	if u == s.uuid {
		return s.name, true
	}
	return "", false
}

// writeSubagentFixture appends JSONL rows to <dir>/<uuid>.jsonl. Each
// row has a top-level agent_id / agent_type if `agentID` is non-empty.
// ctm_timestamp is taken from `ts`.
func writeSubagentFixture(t *testing.T, path, agentID, agentType string, ts time.Time, tool, cmd string, isError bool) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	row := map[string]any{
		"tool_name": tool,
		"tool_input": map[string]any{
			"command": cmd,
		},
		"tool_response": map[string]any{
			"is_error": isError,
			"output":   "ok",
		},
		"ctm_timestamp": ts.UTC().Format(time.RFC3339),
	}
	if agentID != "" {
		row["agent_id"] = agentID
		row["agent_type"] = agentType
	}
	body, _ := json.Marshal(row)
	if _, err := f.Write(append(body, '\n')); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func newSubagentRequest(name, query string) *http.Request {
	url := "/api/sessions/" + name + "/subagents"
	if query != "" {
		url += "?" + query
	}
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.SetPathValue("name", name)
	return req
}

func TestSubagents_ReplaysForestNewestFirst(t *testing.T) {
	dir := t.TempDir()
	uuid := "aaaaaaaa-0000-0000-0000-000000000010"
	path := filepath.Join(dir, uuid+".jsonl")

	base := time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC)
	// Three subagents with distinct agent_ids, interleaved tool calls.
	writeSubagentFixture(t, path, "agent-a", "Explore", base, "Bash", "echo a1", false)
	writeSubagentFixture(t, path, "agent-b", "Task", base.Add(10*time.Second), "Bash", "echo b1", false)
	writeSubagentFixture(t, path, "agent-a", "Explore", base.Add(20*time.Second), "Read", "/tmp/x", false)
	writeSubagentFixture(t, path, "agent-c", "Explore", base.Add(30*time.Second), "Bash", "echo c1", false)
	writeSubagentFixture(t, path, "agent-b", "Task", base.Add(40*time.Second), "Bash", "echo b2", false)
	// A no-agent row should not corrupt counts.
	writeSubagentFixture(t, path, "", "", base.Add(50*time.Second), "Bash", "echo ignored", false)

	h := api.Subagents(dir, subagentResolver{uuid: uuid, name: "alpha"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, newSubagentRequest("alpha", ""))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var got struct {
		Subagents []api.SubagentNode `json:"subagents"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Subagents) != 3 {
		t.Fatalf("len = %d, want 3; got=%+v", len(got.Subagents), got.Subagents)
	}
	// Newest start first → c (t+30s), b (t+10s), a (t+0s).
	wantIDs := []string{"agent-c", "agent-b", "agent-a"}
	for i, want := range wantIDs {
		if got.Subagents[i].ID != want {
			t.Errorf("Subagents[%d].ID = %q, want %q", i, got.Subagents[i].ID, want)
		}
	}
	// Per-agent tool_call counts.
	byID := map[string]api.SubagentNode{}
	for _, n := range got.Subagents {
		byID[n.ID] = n
	}
	if byID["agent-a"].ToolCalls != 2 {
		t.Errorf("agent-a ToolCalls = %d, want 2", byID["agent-a"].ToolCalls)
	}
	if byID["agent-b"].ToolCalls != 2 {
		t.Errorf("agent-b ToolCalls = %d, want 2", byID["agent-b"].ToolCalls)
	}
	if byID["agent-c"].ToolCalls != 1 {
		t.Errorf("agent-c ToolCalls = %d, want 1", byID["agent-c"].ToolCalls)
	}
	// parent_id always null today.
	for _, n := range got.Subagents {
		if n.ParentID != nil {
			t.Errorf("%s ParentID = %v, want nil", n.ID, *n.ParentID)
		}
	}
}

func TestSubagents_DuplicateAgentIDCoalesces(t *testing.T) {
	dir := t.TempDir()
	uuid := "aaaaaaaa-0000-0000-0000-000000000011"
	path := filepath.Join(dir, uuid+".jsonl")

	base := time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC)
	// Same agent_id across 5 rows — should produce exactly one node
	// with tool_calls=5 and the earliest ts as started_at.
	for i := 0; i < 5; i++ {
		writeSubagentFixture(t, path, "same-agent", "Explore",
			base.Add(time.Duration(i)*time.Second), "Bash",
			"echo "+strings.Repeat("a", i+1), false)
	}

	h := api.Subagents(dir, subagentResolver{uuid: uuid, name: "alpha"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, newSubagentRequest("alpha", ""))

	var got struct {
		Subagents []api.SubagentNode `json:"subagents"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	if len(got.Subagents) != 1 {
		t.Fatalf("len = %d, want 1", len(got.Subagents))
	}
	n := got.Subagents[0]
	if n.ToolCalls != 5 {
		t.Errorf("ToolCalls = %d, want 5", n.ToolCalls)
	}
	if !n.StartedAt.Equal(base) {
		t.Errorf("StartedAt = %v, want %v", n.StartedAt, base)
	}
}

func TestSubagents_StatusFailedWhenAnyToolErrored(t *testing.T) {
	dir := t.TempDir()
	uuid := "aaaaaaaa-0000-0000-0000-000000000012"
	path := filepath.Join(dir, uuid+".jsonl")

	base := time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC)
	writeSubagentFixture(t, path, "agent-a", "Explore", base, "Bash", "ok", false)
	writeSubagentFixture(t, path, "agent-a", "Explore", base.Add(1*time.Second), "Bash", "boom", true)

	h := api.Subagents(dir, subagentResolver{uuid: uuid, name: "alpha"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, newSubagentRequest("alpha", ""))

	var got struct {
		Subagents []api.SubagentNode `json:"subagents"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	if len(got.Subagents) != 1 {
		t.Fatalf("len = %d, want 1", len(got.Subagents))
	}
	if got.Subagents[0].Status != "failed" {
		t.Errorf("Status = %q, want failed", got.Subagents[0].Status)
	}
}

func TestSubagents_SinceFiltersOlder(t *testing.T) {
	dir := t.TempDir()
	uuid := "aaaaaaaa-0000-0000-0000-000000000013"
	path := filepath.Join(dir, uuid+".jsonl")

	base := time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC)
	writeSubagentFixture(t, path, "old-agent", "Explore", base, "Bash", "old", false)
	writeSubagentFixture(t, path, "new-agent", "Explore", base.Add(1*time.Hour), "Bash", "new", false)

	since := base.Add(30 * time.Minute).Format(time.RFC3339)
	h := api.Subagents(dir, subagentResolver{uuid: uuid, name: "alpha"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, newSubagentRequest("alpha", "since="+since))

	var got struct {
		Subagents []api.SubagentNode `json:"subagents"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	if len(got.Subagents) != 1 || got.Subagents[0].ID != "new-agent" {
		t.Errorf("got = %+v, want [new-agent]", got.Subagents)
	}
}

func TestSubagents_404UnknownSession(t *testing.T) {
	dir := t.TempDir()
	h := api.Subagents(dir, subagentResolver{uuid: "no-match", name: "other"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, newSubagentRequest("ghost", ""))
	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rr.Code)
	}
}

func TestSubagents_405NonGET(t *testing.T) {
	dir := t.TempDir()
	h := api.Subagents(dir, subagentResolver{uuid: "", name: ""})
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/alpha/subagents", nil)
	req.SetPathValue("name", "alpha")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rr.Code)
	}
}

func TestSubagents_OrphanParentIsRoot(t *testing.T) {
	// Today the JSONL doesn't carry parent_id, so every node is a
	// root. This test pins the contract: even if a hypothetical
	// parent reference showed up as a free-form "parent_id" field
	// (which parseSubagentLine doesn't consume today), the node
	// must still appear in the output as a root.
	dir := t.TempDir()
	uuid := "aaaaaaaa-0000-0000-0000-000000000014"
	path := filepath.Join(dir, uuid+".jsonl")
	// Hand-craft a row with an (unused-today) parent_id field.
	f, _ := os.Create(path)
	line := map[string]any{
		"agent_id":      "orphan",
		"agent_type":    "Explore",
		"parent_id":     "does-not-exist",
		"tool_name":     "Bash",
		"tool_input":    map[string]any{"command": "echo hi"},
		"tool_response": map[string]any{"is_error": false},
		"ctm_timestamp": "2026-04-21T12:00:00Z",
	}
	body, _ := json.Marshal(line)
	_, _ = f.Write(append(body, '\n'))
	_ = f.Close()

	h := api.Subagents(dir, subagentResolver{uuid: uuid, name: "alpha"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, newSubagentRequest("alpha", ""))
	var got struct {
		Subagents []api.SubagentNode `json:"subagents"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	if len(got.Subagents) != 1 {
		t.Fatalf("len = %d", len(got.Subagents))
	}
	if got.Subagents[0].ParentID != nil {
		t.Errorf("ParentID = %v, want nil (orphan promoted to root)", *got.Subagents[0].ParentID)
	}
}

// TestSubagents_RunningWhenRecent uses a fixture dated "a few ms ago"
// so runningGrace hasn't elapsed — the node should report status
// "running" with no stopped_at.
func TestSubagents_RunningWhenRecent(t *testing.T) {
	dir := t.TempDir()
	uuid := "aaaaaaaa-0000-0000-0000-000000000015"
	path := filepath.Join(dir, uuid+".jsonl")

	// Stamp "now" via the fixture so the test doesn't flake on a
	// slow CI box. buildSubagentForest uses time.Now() directly —
	// we approximate by writing a ts one second in the future.
	fmtTS := time.Now().UTC().Add(1 * time.Second)
	writeSubagentFixture(t, path, "fresh", "Explore", fmtTS, "Bash", "echo live", false)

	h := api.Subagents(dir, subagentResolver{uuid: uuid, name: "alpha"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, newSubagentRequest("alpha", ""))
	var got struct {
		Subagents []api.SubagentNode `json:"subagents"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	if len(got.Subagents) != 1 {
		t.Fatalf("len = %d; body=%s", len(got.Subagents), rr.Body.String())
	}
	if got.Subagents[0].Status != "running" {
		t.Errorf("Status = %q, want running", got.Subagents[0].Status)
	}
	if got.Subagents[0].StoppedAt != nil {
		t.Errorf("StoppedAt = %v, want nil", *got.Subagents[0].StoppedAt)
	}
}

// Sanity check: the response is a well-formed JSON object even when
// the log file is empty (fresh session whose JSONL has been opened by
// the tailer but not yet written to). The file must exist for the
// session-name → UUID resolver to find it — that's the same contract
// as feed_history. When the file doesn't exist at all the resolver
// returns 404.
func TestSubagents_EmptyLogOK(t *testing.T) {
	dir := t.TempDir()
	uuid := "aaaaaaaa-0000-0000-0000-000000000016"
	// Touch an empty log so the resolver can find it.
	path := filepath.Join(dir, uuid+".jsonl")
	_ = os.WriteFile(path, []byte{}, 0o600)

	h := api.Subagents(dir, subagentResolver{uuid: uuid, name: "alpha"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, newSubagentRequest("alpha", ""))
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	var got struct {
		Subagents []api.SubagentNode `json:"subagents"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Subagents) != 0 {
		t.Errorf("len = %d, want 0", len(got.Subagents))
	}
}

// Nested-tree smoke: even if a child arrives before a grand-child
// the agent_id grouping keeps both as roots today. Locks the shape
// so a future parent_id implementation can change it deliberately.
func TestSubagents_NestedFamilyStaysFlat(t *testing.T) {
	dir := t.TempDir()
	uuid := "aaaaaaaa-0000-0000-0000-000000000017"
	path := filepath.Join(dir, uuid+".jsonl")
	base := time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC)
	for i, id := range []string{"root", "child-1", "child-2", "grandchild"} {
		writeSubagentFixture(t, path, id, "Explore", base.Add(time.Duration(i)*time.Second), "Bash", fmt.Sprintf("step %d", i), false)
	}
	h := api.Subagents(dir, subagentResolver{uuid: uuid, name: "alpha"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, newSubagentRequest("alpha", ""))
	var got struct {
		Subagents []api.SubagentNode `json:"subagents"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	if len(got.Subagents) != 4 {
		t.Fatalf("len = %d, want 4", len(got.Subagents))
	}
	for _, n := range got.Subagents {
		if n.ParentID != nil {
			t.Errorf("%s ParentID = %v, want nil", n.ID, *n.ParentID)
		}
	}
}
