package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RandomCodeSpace/ctm/internal/serve/api"
	"github.com/RandomCodeSpace/ctm/internal/serve/auth"
)

// ---------- helpers --------------------------------------------------------

func authTempHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	old := os.Getenv("HOME")
	t.Cleanup(func() { _ = os.Setenv("HOME", old) })
	_ = os.Setenv("HOME", home)
	return home
}

func authJSONReq(t *testing.T, method, path string, body any) *http.Request {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	return r
}

// ---------- status ---------------------------------------------------------

func TestStatus_Unregistered(t *testing.T) {
	authTempHome(t)
	store := auth.NewStore()
	h := api.AuthStatus(store)
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/api/auth/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct{ Registered, Authenticated bool }
	_ = json.NewDecoder(rec.Body).Decode(&body)
	if body.Registered || body.Authenticated {
		t.Fatalf("got %+v, want both false", body)
	}
}

func TestStatus_RegisteredButAnonymous(t *testing.T) {
	authTempHome(t)
	enc, _ := auth.Hash("pw")
	_ = auth.Save(auth.User{Username: "alice", Password: enc})
	store := auth.NewStore()
	h := api.AuthStatus(store)
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/api/auth/status", nil))
	var body struct{ Registered, Authenticated bool }
	_ = json.NewDecoder(rec.Body).Decode(&body)
	if !body.Registered || body.Authenticated {
		t.Fatalf("got %+v, want registered=true authenticated=false", body)
	}
}

func TestStatus_Authenticated(t *testing.T) {
	authTempHome(t)
	enc, _ := auth.Hash("pw")
	_ = auth.Save(auth.User{Username: "alice", Password: enc})
	store := auth.NewStore()
	tok, _ := store.Create("alice")
	h := api.AuthStatus(store)
	req := httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	h(rec, req)
	var body struct{ Registered, Authenticated bool }
	_ = json.NewDecoder(rec.Body).Decode(&body)
	if !body.Registered || !body.Authenticated {
		t.Fatalf("got %+v, want both true", body)
	}
}

// ---------- signup ---------------------------------------------------------

func TestSignup_HappyPath(t *testing.T) {
	authTempHome(t)
	store := auth.NewStore()
	h := api.AuthSignup(store)
	rec := httptest.NewRecorder()
	h(rec, authJSONReq(t, http.MethodPost, "/api/auth/signup",
		map[string]string{"username": "alice", "password": "password123"}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d (%s), want 201", rec.Code, rec.Body.String())
	}
	var body struct{ Token, Username string }
	_ = json.NewDecoder(rec.Body).Decode(&body)
	if body.Token == "" || body.Username != "alice" {
		t.Fatalf("body = %+v", body)
	}
	if _, ok := store.Lookup(body.Token); !ok {
		t.Fatal("token not in session store")
	}
	if !auth.Exists() {
		t.Fatal("user.json was not created")
	}
}

func TestSignup_AlreadyRegistered(t *testing.T) {
	authTempHome(t)
	enc, _ := auth.Hash("pw")
	_ = auth.Save(auth.User{Username: "bob", Password: enc})
	store := auth.NewStore()
	h := api.AuthSignup(store)
	rec := httptest.NewRecorder()
	h(rec, authJSONReq(t, http.MethodPost, "/api/auth/signup",
		map[string]string{"username": "alice", "password": "password123"}))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "already_registered") {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestSignup_BadBody(t *testing.T) {
	authTempHome(t)
	store := auth.NewStore()
	h := api.AuthSignup(store)
	cases := []map[string]string{
		{},
		{"username": "ab", "password": "password123"},
		{"username": "alice", "password": "short"},
		{"username": "has space", "password": "password123"},
		{"username": "alice", "password": "        "},
	}
	for i, c := range cases {
		rec := httptest.NewRecorder()
		h(rec, authJSONReq(t, http.MethodPost, "/api/auth/signup", c))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("case %d status = %d, want 400 (body=%s)", i, rec.Code, rec.Body.String())
		}
	}
}

// ---------- login ----------------------------------------------------------

func TestLogin_HappyPath(t *testing.T) {
	authTempHome(t)
	enc, _ := auth.Hash("password123")
	_ = auth.Save(auth.User{Username: "alice", Password: enc})
	store := auth.NewStore()
	h := api.AuthLogin(store)
	rec := httptest.NewRecorder()
	h(rec, authJSONReq(t, http.MethodPost, "/api/auth/login",
		map[string]string{"username": "alice", "password": "password123"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	var body struct{ Token, Username string }
	_ = json.NewDecoder(rec.Body).Decode(&body)
	if body.Token == "" || body.Username != "alice" {
		t.Fatalf("body = %+v", body)
	}
}

func TestLogin_BadPassword(t *testing.T) {
	authTempHome(t)
	enc, _ := auth.Hash("password123")
	_ = auth.Save(auth.User{Username: "alice", Password: enc})
	store := auth.NewStore()
	h := api.AuthLogin(store)
	rec := httptest.NewRecorder()
	h(rec, authJSONReq(t, http.MethodPost, "/api/auth/login",
		map[string]string{"username": "alice", "password": "wrong"}))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "invalid_credentials") {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestLogin_UnknownUsername(t *testing.T) {
	authTempHome(t)
	enc, _ := auth.Hash("password123")
	_ = auth.Save(auth.User{Username: "alice", Password: enc})
	store := auth.NewStore()
	h := api.AuthLogin(store)
	rec := httptest.NewRecorder()
	h(rec, authJSONReq(t, http.MethodPost, "/api/auth/login",
		map[string]string{"username": "mallory", "password": "password123"}))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestLogin_NotRegistered(t *testing.T) {
	authTempHome(t)
	store := auth.NewStore()
	h := api.AuthLogin(store)
	rec := httptest.NewRecorder()
	h(rec, authJSONReq(t, http.MethodPost, "/api/auth/login",
		map[string]string{"username": "alice", "password": "password123"}))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "not_registered") {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

// ---------- logout ---------------------------------------------------------

func TestLogout_RevokesToken(t *testing.T) {
	authTempHome(t)
	enc, _ := auth.Hash("pw")
	_ = auth.Save(auth.User{Username: "alice", Password: enc})
	store := auth.NewStore()
	tok, _ := store.Create("alice")
	h := api.AuthLogout(store)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	if _, ok := store.Lookup(tok); ok {
		t.Fatal("token still present after logout")
	}
}

func TestLogout_NoToken(t *testing.T) {
	authTempHome(t)
	store := auth.NewStore()
	h := api.AuthLogout(store)
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// ---------- file-based paths sanity check ---------------------------------

func TestUserPath_UsesHome(t *testing.T) {
	home := authTempHome(t)
	want := filepath.Join(home, ".config", "ctm", "user.json")
	if got := auth.UserPath(); got != want {
		t.Fatalf("UserPath = %q, want %q", got, want)
	}
}
