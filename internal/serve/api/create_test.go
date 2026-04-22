package api_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RandomCodeSpace/ctm/internal/serve/api"
	"github.com/RandomCodeSpace/ctm/internal/session"
)

// ---------- fakes ----------------------------------------------------------

type fakeCreateProj struct{ sess map[string]session.Session }

func (f *fakeCreateProj) Get(name string) (session.Session, bool) {
	s, ok := f.sess[name]
	return s, ok
}

// TmuxAlive on this fake satisfies InputSessionSource (the handler
// reuses that interface). Not exercised by CreateSession flow itself,
// but required by the interface contract.
func (f *fakeCreateProj) TmuxAlive(name string) bool { return true }

type fakeCreateSpawner struct {
	returnSess   session.Session
	err          error
	calledWith   struct{ name, workdir string }
	called       int
	initialCalls int
	initialArgs  struct{ name, text string }
}

func (f *fakeCreateSpawner) Spawn(name, workdir string) (session.Session, error) {
	f.called++
	f.calledWith.name = name
	f.calledWith.workdir = workdir
	if f.err != nil {
		return session.Session{}, f.err
	}
	s := f.returnSess
	s.Name = name
	s.Workdir = workdir
	return s, nil
}

func (f *fakeCreateSpawner) SendInitialPrompt(name, text string) {
	f.initialCalls++
	f.initialArgs.name = name
	f.initialArgs.text = text
}

type fakeLookPath struct{ ok bool }

func (f fakeLookPath) LookPath(file string) (string, error) {
	if f.ok {
		return "/usr/bin/" + file, nil
	}
	return "", errors.New("not found")
}

// ---------- helpers --------------------------------------------------------

func createReq(t *testing.T, body any) *http.Request {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	r := httptest.NewRequest(http.MethodPost, "/api/sessions", bytes.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	return r
}

func tempDir(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

// ---------- tests ----------------------------------------------------------

func TestCreate_HappyPath(t *testing.T) {
	dir := tempDir(t)
	proj := &fakeCreateProj{sess: map[string]session.Session{}}
	spawn := &fakeCreateSpawner{returnSess: session.Session{UUID: "u", Mode: "yolo"}}
	h := api.CreateSession(proj, spawn, fakeLookPath{ok: true})

	rec := httptest.NewRecorder()
	h(rec, createReq(t, map[string]string{"workdir": dir}))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d (%s), want 201", rec.Code, rec.Body.String())
	}
	var got session.Session
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if got.Name != filepath.Base(dir) {
		t.Fatalf("name = %q, want %q", got.Name, filepath.Base(dir))
	}
	if got.Workdir != dir {
		t.Fatalf("workdir = %q, want %q", got.Workdir, dir)
	}
	if got.Mode != "yolo" {
		t.Fatalf("mode = %q, want yolo", got.Mode)
	}
	if spawn.initialCalls != 0 {
		t.Fatalf("SendInitialPrompt called %d times without initial_prompt, want 0", spawn.initialCalls)
	}
}

func TestCreate_InitialPrompt_Fires(t *testing.T) {
	dir := tempDir(t)
	proj := &fakeCreateProj{sess: map[string]session.Session{}}
	spawn := &fakeCreateSpawner{returnSess: session.Session{UUID: "u", Mode: "yolo"}}
	h := api.CreateSession(proj, spawn, fakeLookPath{ok: true})

	rec := httptest.NewRecorder()
	h(rec, createReq(t, map[string]string{"workdir": dir, "initial_prompt": "review the diff"}))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d (%s), want 201", rec.Code, rec.Body.String())
	}
	if spawn.initialCalls != 1 {
		t.Fatalf("SendInitialPrompt called %d times, want 1", spawn.initialCalls)
	}
	if spawn.initialArgs.text != "review the diff" {
		t.Fatalf("prompt text = %q, want %q", spawn.initialArgs.text, "review the diff")
	}
	if spawn.initialArgs.name != filepath.Base(dir) {
		t.Fatalf("prompt name = %q, want %q", spawn.initialArgs.name, filepath.Base(dir))
	}
}

func TestCreate_InitialPrompt_Empty_Skips(t *testing.T) {
	dir := tempDir(t)
	spawn := &fakeCreateSpawner{returnSess: session.Session{UUID: "u", Mode: "yolo"}}
	h := api.CreateSession(&fakeCreateProj{sess: map[string]session.Session{}}, spawn, fakeLookPath{ok: true})

	rec := httptest.NewRecorder()
	h(rec, createReq(t, map[string]string{"workdir": dir, "initial_prompt": ""}))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
	if spawn.initialCalls != 0 {
		t.Fatalf("SendInitialPrompt called %d times for empty prompt, want 0", spawn.initialCalls)
	}
}

func TestCreate_InitialPrompt_WhitespaceOnly_Skips(t *testing.T) {
	dir := tempDir(t)
	spawn := &fakeCreateSpawner{returnSess: session.Session{UUID: "u", Mode: "yolo"}}
	h := api.CreateSession(&fakeCreateProj{sess: map[string]session.Session{}}, spawn, fakeLookPath{ok: true})

	rec := httptest.NewRecorder()
	h(rec, createReq(t, map[string]string{"workdir": dir, "initial_prompt": "  \n\t  "}))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
	if spawn.initialCalls != 0 {
		t.Fatalf("SendInitialPrompt called %d times for whitespace prompt, want 0", spawn.initialCalls)
	}
}

func TestCreate_NameOverride(t *testing.T) {
	dir := tempDir(t)
	proj := &fakeCreateProj{sess: map[string]session.Session{}}
	spawn := &fakeCreateSpawner{returnSess: session.Session{UUID: "u", Mode: "yolo"}}
	h := api.CreateSession(proj, spawn, fakeLookPath{ok: true})

	rec := httptest.NewRecorder()
	h(rec, createReq(t, map[string]string{"workdir": dir, "name": "explicit"}))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	var got session.Session
	_ = json.NewDecoder(rec.Body).Decode(&got)
	if got.Name != "explicit" {
		t.Fatalf("name = %q, want explicit", got.Name)
	}
}

func TestCreate_RelativeWorkdir(t *testing.T) {
	h := api.CreateSession(&fakeCreateProj{}, &fakeCreateSpawner{}, fakeLookPath{ok: true})
	rec := httptest.NewRecorder()
	h(rec, createReq(t, map[string]string{"workdir": "relative/path"}))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "workdir_not_absolute") {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestCreate_MissingWorkdir(t *testing.T) {
	h := api.CreateSession(&fakeCreateProj{}, &fakeCreateSpawner{}, fakeLookPath{ok: true})
	rec := httptest.NewRecorder()
	h(rec, createReq(t, map[string]string{"workdir": "/definitely/not/here/xyz"}))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "bad_workdir") {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestCreate_FileInsteadOfDir(t *testing.T) {
	dir := tempDir(t)
	f := filepath.Join(dir, "file")
	_ = os.WriteFile(f, []byte("x"), 0o600)
	h := api.CreateSession(&fakeCreateProj{}, &fakeCreateSpawner{}, fakeLookPath{ok: true})
	rec := httptest.NewRecorder()
	h(rec, createReq(t, map[string]string{"workdir": f}))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "workdir_not_dir") {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestCreate_NoClaude(t *testing.T) {
	dir := tempDir(t)
	h := api.CreateSession(&fakeCreateProj{}, &fakeCreateSpawner{}, fakeLookPath{ok: false})
	rec := httptest.NewRecorder()
	h(rec, createReq(t, map[string]string{"workdir": dir}))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "no_claude") {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestCreate_Collision(t *testing.T) {
	dir := tempDir(t)
	proj := &fakeCreateProj{sess: map[string]session.Session{
		filepath.Base(dir): {Name: filepath.Base(dir), Mode: "yolo"},
	}}
	spawn := &fakeCreateSpawner{}
	h := api.CreateSession(proj, spawn, fakeLookPath{ok: true})

	rec := httptest.NewRecorder()
	h(rec, createReq(t, map[string]string{"workdir": dir}))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
	var body struct {
		Error   string          `json:"error"`
		Session session.Session `json:"session"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error != "name_exists" {
		t.Fatalf("error = %q", body.Error)
	}
	if body.Session.Name != filepath.Base(dir) {
		t.Fatalf("existing session not surfaced: %+v", body.Session)
	}
	if spawn.called != 0 {
		t.Fatalf("Spawn should NOT be called on collision, was called %d times", spawn.called)
	}
}

func TestCreate_EmptyBody(t *testing.T) {
	h := api.CreateSession(&fakeCreateProj{}, &fakeCreateSpawner{}, fakeLookPath{ok: true})
	rec := httptest.NewRecorder()
	h(rec, createReq(t, map[string]string{}))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestCreate_BadName(t *testing.T) {
	dir := tempDir(t)
	h := api.CreateSession(&fakeCreateProj{}, &fakeCreateSpawner{}, fakeLookPath{ok: true})
	rec := httptest.NewRecorder()
	h(rec, createReq(t, map[string]string{"workdir": dir, "name": "has space"}))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "bad_name") {
		t.Fatalf("body = %q", rec.Body.String())
	}
}
