package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RandomCodeSpace/ctm/internal/serve/api"
	"github.com/RandomCodeSpace/ctm/internal/session"
)

// ---------- fakes ----------------------------------------------------------

type fakeInputProj struct {
	sess      map[string]session.Session
	tmuxAlive map[string]bool
}

func (f *fakeInputProj) Get(name string) (session.Session, bool) {
	s, ok := f.sess[name]
	return s, ok
}

func (f *fakeInputProj) TmuxAlive(name string) bool {
	return f.tmuxAlive[name]
}

type fakeInputTmux struct {
	lastTarget  string
	lastKeys    string
	sendCalls   int
	enterCalls  int
	err         error
}

func (f *fakeInputTmux) SendKeys(target, keys string) error {
	f.lastTarget = target
	f.lastKeys = keys
	f.sendCalls++
	return f.err
}

func (f *fakeInputTmux) SendEnter(target string) error {
	f.lastTarget = target
	f.enterCalls++
	return f.err
}

// ---------- helpers --------------------------------------------------------

func inputReq(t *testing.T, name string, body any) *http.Request {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	r := httptest.NewRequest(http.MethodPost, "/api/sessions/"+name+"/input", bytes.NewReader(b))
	r.SetPathValue("name", name)
	r.Header.Set("Content-Type", "application/json")
	return r
}

func newYoloProj() *fakeInputProj {
	return &fakeInputProj{
		sess: map[string]session.Session{
			"alpha": {Name: "alpha", UUID: "u-1", Mode: "yolo"},
			"safe":  {Name: "safe", UUID: "u-2", Mode: "safe"},
			"dead":  {Name: "dead", UUID: "u-3", Mode: "yolo"},
		},
		tmuxAlive: map[string]bool{
			"alpha": true,
			"safe":  true,
			"dead":  false,
		},
	}
}

// ---------- tests ----------------------------------------------------------

func TestInput_Preset_Yes(t *testing.T) {
	tmux := &fakeInputTmux{}
	h := api.Input(newYoloProj(), tmux)
	rec := httptest.NewRecorder()
	h(rec, inputReq(t, "alpha", map[string]string{"preset": "yes"}))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if tmux.lastTarget != "alpha:0.0" {
		t.Fatalf("tmux target = %q, want %q", tmux.lastTarget, "alpha:0.0")
	}
	if tmux.lastKeys != "Approve" {
		t.Fatalf("tmux literal = %q, want %q", tmux.lastKeys, "Approve")
	}
	if tmux.enterCalls != 1 {
		t.Fatalf("SendEnter called %d times, want 1", tmux.enterCalls)
	}
}

func TestInput_Preset_No(t *testing.T) {
	tmux := &fakeInputTmux{}
	h := api.Input(newYoloProj(), tmux)
	rec := httptest.NewRecorder()
	h(rec, inputReq(t, "alpha", map[string]string{"preset": "no"}))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if tmux.lastKeys != "Deny" || tmux.enterCalls != 1 {
		t.Fatalf("tmux literal=%q enters=%d, want %q + 1", tmux.lastKeys, tmux.enterCalls, "Deny")
	}
}

func TestInput_Preset_Continue(t *testing.T) {
	tmux := &fakeInputTmux{}
	h := api.Input(newYoloProj(), tmux)
	rec := httptest.NewRecorder()
	h(rec, inputReq(t, "alpha", map[string]string{"preset": "continue"}))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if tmux.sendCalls != 0 {
		t.Fatalf("SendKeys called %d times for 'continue', want 0 (Enter-only)", tmux.sendCalls)
	}
	if tmux.enterCalls != 1 {
		t.Fatalf("SendEnter called %d times, want 1", tmux.enterCalls)
	}
}

func TestInput_FreeText(t *testing.T) {
	tmux := &fakeInputTmux{}
	h := api.Input(newYoloProj(), tmux)
	rec := httptest.NewRecorder()
	h(rec, inputReq(t, "alpha", map[string]string{"text": "approve"}))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if tmux.lastKeys != "approve" || tmux.enterCalls != 1 {
		t.Fatalf("tmux literal=%q enters=%d, want %q + 1", tmux.lastKeys, tmux.enterCalls, "approve")
	}
}

func TestInput_NotYolo(t *testing.T) {
	tmux := &fakeInputTmux{}
	h := api.Input(newYoloProj(), tmux)
	rec := httptest.NewRecorder()
	h(rec, inputReq(t, "safe", map[string]string{"preset": "yes"}))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "not_yolo") {
		t.Fatalf("body = %q, want substring %q", rec.Body.String(), "not_yolo")
	}
	if tmux.lastTarget != "" {
		t.Fatalf("tmux was called with target %q — expected no call", tmux.lastTarget)
	}
}

func TestInput_TmuxDead(t *testing.T) {
	tmux := &fakeInputTmux{}
	h := api.Input(newYoloProj(), tmux)
	rec := httptest.NewRecorder()
	h(rec, inputReq(t, "dead", map[string]string{"preset": "yes"}))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "tmux_dead") {
		t.Fatalf("body = %q, want substring %q", rec.Body.String(), "tmux_dead")
	}
}

func TestInput_NotFound(t *testing.T) {
	tmux := &fakeInputTmux{}
	h := api.Input(newYoloProj(), tmux)
	rec := httptest.NewRecorder()
	h(rec, inputReq(t, "nope", map[string]string{"preset": "yes"}))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestInput_BothTextAndPreset(t *testing.T) {
	tmux := &fakeInputTmux{}
	h := api.Input(newYoloProj(), tmux)
	rec := httptest.NewRecorder()
	h(rec, inputReq(t, "alpha", map[string]string{"text": "hi", "preset": "yes"}))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "invalid_body") {
		t.Fatalf("body = %q, want substring %q", rec.Body.String(), "invalid_body")
	}
}

func TestInput_TextTooLong(t *testing.T) {
	tmux := &fakeInputTmux{}
	h := api.Input(newYoloProj(), tmux)
	rec := httptest.NewRecorder()
	h(rec, inputReq(t, "alpha", map[string]string{"text": strings.Repeat("x", 257)}))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "invalid_text") {
		t.Fatalf("body = %q, want substring %q", rec.Body.String(), "invalid_text")
	}
}

func TestInput_TextContainsNewline(t *testing.T) {
	tmux := &fakeInputTmux{}
	h := api.Input(newYoloProj(), tmux)
	rec := httptest.NewRecorder()
	h(rec, inputReq(t, "alpha", map[string]string{"text": "line1\nline2"}))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestInput_UnknownPreset(t *testing.T) {
	tmux := &fakeInputTmux{}
	h := api.Input(newYoloProj(), tmux)
	rec := httptest.NewRecorder()
	h(rec, inputReq(t, "alpha", map[string]string{"preset": "maybe"}))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "invalid_preset") {
		t.Fatalf("body = %q, want substring %q", rec.Body.String(), "invalid_preset")
	}
}

func TestInput_EmptyBody(t *testing.T) {
	tmux := &fakeInputTmux{}
	h := api.Input(newYoloProj(), tmux)
	rec := httptest.NewRecorder()
	h(rec, inputReq(t, "alpha", map[string]string{}))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestInput_WrongMethod(t *testing.T) {
	tmux := &fakeInputTmux{}
	h := api.Input(newYoloProj(), tmux)
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/sessions/alpha/input", nil)
	r.SetPathValue("name", "alpha")
	h(rec, r)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}
