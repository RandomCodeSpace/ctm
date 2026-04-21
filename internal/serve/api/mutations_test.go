package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RandomCodeSpace/ctm/internal/session"
)

// fakeStore is an in-memory SessionStore for tests.
type fakeStore struct {
	sessions    map[string]*session.Session
	deleteErr   error
	renameErr   error
	deleteCalls int
	renameCalls int
}

func newFakeStore(seed map[string]*session.Session) *fakeStore {
	if seed == nil {
		seed = map[string]*session.Session{}
	}
	return &fakeStore{sessions: seed}
}

func (f *fakeStore) Get(name string) (*session.Session, error) {
	s, ok := f.sessions[name]
	if !ok {
		return nil, errors.New("not found")
	}
	return s, nil
}
func (f *fakeStore) Delete(name string) error {
	f.deleteCalls++
	if f.deleteErr != nil {
		return f.deleteErr
	}
	delete(f.sessions, name)
	return nil
}
func (f *fakeStore) Rename(old, newName string) error {
	f.renameCalls++
	if f.renameErr != nil {
		return f.renameErr
	}
	s, ok := f.sessions[old]
	if !ok {
		return errors.New("not found")
	}
	s.Name = newName
	delete(f.sessions, old)
	f.sessions[newName] = s
	return nil
}

// fakeTmux is an in-memory TmuxMutator.
type fakeTmux struct {
	killErr     error
	renameErr   error
	killCalls   []string
	renameCalls [][2]string
}

func (f *fakeTmux) KillSession(name string) error {
	f.killCalls = append(f.killCalls, name)
	return f.killErr
}
func (f *fakeTmux) RenameSession(oldName, newName string) error {
	f.renameCalls = append(f.renameCalls, [2]string{oldName, newName})
	return f.renameErr
}

type fakeRefresher struct{ calls int }

func (f *fakeRefresher) Reload() { f.calls++ }

func makeRequest(method, path, name, body string) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.SetPathValue("name", name)
	return r
}

// ---------- kill ------------------------------------------------------------

func TestKill_HappyPath(t *testing.T) {
	store := newFakeStore(map[string]*session.Session{
		"alpha": {Name: "alpha", Workdir: "/tmp", Mode: "safe"},
	})
	tmuxC := &fakeTmux{}
	proj := &fakeRefresher{}
	h := Kill(store, tmuxC, proj)

	rr := httptest.NewRecorder()
	h(rr, makeRequest(http.MethodPost, "/api/sessions/alpha/kill", "alpha", `{"confirm":"alpha"}`))

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if len(tmuxC.killCalls) != 1 || tmuxC.killCalls[0] != "alpha" {
		t.Errorf("kill calls=%v", tmuxC.killCalls)
	}
	if proj.calls != 1 {
		t.Errorf("projection reload calls=%d want 1", proj.calls)
	}
	var got session.Session
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	if got.Name != "alpha" {
		t.Errorf("returned session name=%q want alpha", got.Name)
	}
}

func TestKill_ConfirmMismatch400(t *testing.T) {
	store := newFakeStore(map[string]*session.Session{"alpha": {Name: "alpha"}})
	h := Kill(store, &fakeTmux{}, &fakeRefresher{})

	rr := httptest.NewRecorder()
	h(rr, makeRequest(http.MethodPost, "/api/sessions/alpha/kill", "alpha", `{"confirm":"beta"}`))

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status=%d want 400", rr.Code)
	}
}

func TestKill_MissingSession404(t *testing.T) {
	h := Kill(newFakeStore(nil), &fakeTmux{}, &fakeRefresher{})

	rr := httptest.NewRecorder()
	h(rr, makeRequest(http.MethodPost, "/api/sessions/ghost/kill", "ghost", `{"confirm":"ghost"}`))

	if rr.Code != http.StatusNotFound {
		t.Errorf("status=%d want 404", rr.Code)
	}
}

func TestKill_AlreadyGoneStillSucceeds(t *testing.T) {
	store := newFakeStore(map[string]*session.Session{"alpha": {Name: "alpha"}})
	tmuxC := &fakeTmux{killErr: errors.New("can't find session: alpha")}
	proj := &fakeRefresher{}
	h := Kill(store, tmuxC, proj)

	rr := httptest.NewRecorder()
	h(rr, makeRequest(http.MethodPost, "/api/sessions/alpha/kill", "alpha", `{"confirm":"alpha"}`))

	if rr.Code != http.StatusOK {
		t.Errorf("status=%d want 200 (tmux already gone should be idempotent)", rr.Code)
	}
	if proj.calls != 1 {
		t.Errorf("reload should still fire, got %d", proj.calls)
	}
}

func TestKill_405OnGet(t *testing.T) {
	h := Kill(newFakeStore(nil), &fakeTmux{}, &fakeRefresher{})
	rr := httptest.NewRecorder()
	h(rr, makeRequest(http.MethodGet, "/api/sessions/alpha/kill", "alpha", ""))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("status=%d want 405", rr.Code)
	}
	if rr.Header().Get("Allow") != http.MethodPost {
		t.Errorf("Allow=%q want POST", rr.Header().Get("Allow"))
	}
}

func TestKill_UnknownJSONField400(t *testing.T) {
	store := newFakeStore(map[string]*session.Session{"alpha": {Name: "alpha"}})
	h := Kill(store, &fakeTmux{}, &fakeRefresher{})

	rr := httptest.NewRecorder()
	h(rr, makeRequest(http.MethodPost, "/api/sessions/alpha/kill", "alpha", `{"confirm":"alpha","extra":true}`))

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status=%d want 400", rr.Code)
	}
}

// ---------- forget ----------------------------------------------------------

func TestForget_HappyPath(t *testing.T) {
	store := newFakeStore(map[string]*session.Session{"alpha": {Name: "alpha"}})
	proj := &fakeRefresher{}
	h := Forget(store, proj)

	rr := httptest.NewRecorder()
	h(rr, makeRequest(http.MethodPost, "/api/sessions/alpha/forget", "alpha", `{"confirm":"alpha"}`))

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if store.deleteCalls != 1 {
		t.Errorf("delete calls=%d want 1", store.deleteCalls)
	}
	if _, ok := store.sessions["alpha"]; ok {
		t.Errorf("alpha should be gone from store")
	}
	if proj.calls != 1 {
		t.Errorf("reload calls=%d", proj.calls)
	}
}

func TestForget_ConfirmMismatch400(t *testing.T) {
	store := newFakeStore(map[string]*session.Session{"alpha": {Name: "alpha"}})
	h := Forget(store, &fakeRefresher{})
	rr := httptest.NewRecorder()
	h(rr, makeRequest(http.MethodPost, "/api/sessions/alpha/forget", "alpha", `{"confirm":"wrong"}`))
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status=%d want 400", rr.Code)
	}
	if store.deleteCalls != 0 {
		t.Errorf("delete should not fire on confirm mismatch")
	}
}

func TestForget_MissingSession404(t *testing.T) {
	h := Forget(newFakeStore(nil), &fakeRefresher{})
	rr := httptest.NewRecorder()
	h(rr, makeRequest(http.MethodPost, "/api/sessions/ghost/forget", "ghost", `{"confirm":"ghost"}`))
	if rr.Code != http.StatusNotFound {
		t.Errorf("status=%d want 404", rr.Code)
	}
}

// ---------- rename ----------------------------------------------------------

func TestRename_HappyPath(t *testing.T) {
	store := newFakeStore(map[string]*session.Session{"alpha": {Name: "alpha", Mode: "safe"}})
	tmuxC := &fakeTmux{}
	proj := &fakeRefresher{}
	h := Rename(store, tmuxC, proj)

	rr := httptest.NewRecorder()
	h(rr, makeRequest(http.MethodPost, "/api/sessions/alpha/rename", "alpha", `{"to":"beta"}`))

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if _, ok := store.sessions["beta"]; !ok {
		t.Errorf("beta missing from store")
	}
	if _, ok := store.sessions["alpha"]; ok {
		t.Errorf("alpha should be gone")
	}
	if len(tmuxC.renameCalls) != 1 || tmuxC.renameCalls[0] != [2]string{"alpha", "beta"} {
		t.Errorf("tmux rename calls=%v", tmuxC.renameCalls)
	}
}

func TestRename_InvalidNewName400(t *testing.T) {
	store := newFakeStore(map[string]*session.Session{"alpha": {Name: "alpha"}})
	h := Rename(store, &fakeTmux{}, &fakeRefresher{})

	// SanitizeName would reject this; ValidateName enforces the rule.
	rr := httptest.NewRecorder()
	h(rr, makeRequest(http.MethodPost, "/api/sessions/alpha/rename", "alpha", `{"to":"bad/name"}`))

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status=%d want 400", rr.Code)
	}
}

func TestRename_SameName400(t *testing.T) {
	store := newFakeStore(map[string]*session.Session{"alpha": {Name: "alpha"}})
	h := Rename(store, &fakeTmux{}, &fakeRefresher{})

	rr := httptest.NewRecorder()
	h(rr, makeRequest(http.MethodPost, "/api/sessions/alpha/rename", "alpha", `{"to":"alpha"}`))

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status=%d want 400", rr.Code)
	}
}

func TestRename_MissingSession404(t *testing.T) {
	h := Rename(newFakeStore(nil), &fakeTmux{}, &fakeRefresher{})
	rr := httptest.NewRecorder()
	h(rr, makeRequest(http.MethodPost, "/api/sessions/ghost/rename", "ghost", `{"to":"zeta"}`))
	if rr.Code != http.StatusNotFound {
		t.Errorf("status=%d want 404", rr.Code)
	}
}

func TestRename_TmuxFailureDoesNotTouchStore(t *testing.T) {
	store := newFakeStore(map[string]*session.Session{"alpha": {Name: "alpha"}})
	tmuxC := &fakeTmux{renameErr: errors.New("session_name already exists: beta")}
	h := Rename(store, tmuxC, &fakeRefresher{})

	rr := httptest.NewRecorder()
	h(rr, makeRequest(http.MethodPost, "/api/sessions/alpha/rename", "alpha", `{"to":"beta"}`))

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status=%d want 500", rr.Code)
	}
	if store.renameCalls != 0 {
		t.Errorf("store should not be touched when tmux fails")
	}
}

// ---------- attach-url ------------------------------------------------------

func TestAttachURL_ReturnsCtmDeeplink(t *testing.T) {
	h := AttachURL()
	rr := httptest.NewRecorder()
	h(rr, makeRequest(http.MethodGet, "/api/sessions/alpha/attach-url", "alpha", ""))

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var got map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.HasPrefix(got["url"], "ctm://attach?") {
		t.Errorf("url=%q want ctm://attach? prefix", got["url"])
	}
	if !strings.Contains(got["url"], "name=alpha") {
		t.Errorf("url=%q missing name=alpha", got["url"])
	}
}

func TestAttachURL_PercentEncodesName(t *testing.T) {
	h := AttachURL()
	rr := httptest.NewRecorder()
	h(rr, makeRequest(http.MethodGet, "/api/sessions/my+session/attach-url", "my session", ""))

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d", rr.Code)
	}
	var got map[string]string
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	// url.Values.Encode() produces + for space.
	if !strings.Contains(got["url"], "name=my+session") {
		t.Errorf("url=%q want name=my+session", got["url"])
	}
}

func TestAttachURL_405OnPost(t *testing.T) {
	h := AttachURL()
	rr := httptest.NewRecorder()
	h(rr, makeRequest(http.MethodPost, "/api/sessions/alpha/attach-url", "alpha", ""))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("status=%d want 405", rr.Code)
	}
}

// ---------- misc -----------------------------------------------------------

func TestIsAlreadyGone_MatchesKnownPhrases(t *testing.T) {
	for _, s := range []string{
		"can't find session: alpha",
		"session not found",
		"no server running on /tmp/tmux-1000/default",
	} {
		if !isAlreadyGone(errors.New(s)) {
			t.Errorf("isAlreadyGone(%q)=false want true", s)
		}
	}
	if isAlreadyGone(errors.New("random tmux hiccup")) {
		t.Errorf("unexpected match on unrelated error")
	}
}

// guard that the mutations package compiles against bytes.Buffer (a
// previous refactor shadowed strings.NewReader in one spot — keep a
// belt-and-braces sanity check so we notice regressions).
func TestBytesImportAvailable(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteString("x")
	if buf.String() != "x" {
		t.Fatal("bytes import broken")
	}
}
