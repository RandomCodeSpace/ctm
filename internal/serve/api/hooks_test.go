package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/RandomCodeSpace/ctm/internal/serve/events"
	"github.com/RandomCodeSpace/ctm/internal/serve/ingest"
)

const hooksTestUUID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

func newHookServer(t *testing.T, hub *events.Hub) (*httptest.Server, *ingest.TailerManager) {
	t.Helper()
	mgr := ingest.NewTailerManager(t.TempDir(), hub)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/hooks/{event}", Hooks(mgr, hub))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	t.Cleanup(mgr.StopAll)
	return srv, mgr
}

func postHook(t *testing.T, srv *httptest.Server, event string, form url.Values) *http.Response {
	t.Helper()
	resp, err := http.PostForm(srv.URL+"/api/hooks/"+event, form)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	return resp
}

func TestHooks_SessionNewSpawnsTailerAndPublishes(t *testing.T) {
	hub := events.NewHub(0)
	sub, _ := hub.Subscribe("", "")
	defer sub.Close()

	srv, mgr := newHookServer(t, hub)

	resp := postHook(t, srv, "session_new", url.Values{
		"name":    {"alpha"},
		"uuid":    {hooksTestUUID},
		"mode":    {"yolo"},
		"workdir": {"/tmp/work"},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}

	select {
	case ev := <-sub.Events():
		if ev.Type != "session_new" {
			t.Errorf("ev.Type = %q, want session_new", ev.Type)
		}
		if ev.Session != "alpha" {
			t.Errorf("ev.Session = %q, want alpha", ev.Session)
		}
		var body map[string]any
		_ = json.Unmarshal(ev.Payload, &body)
		if body["mode"] != "yolo" || body["workdir"] != "/tmp/work" || body["uuid"] != hooksTestUUID {
			t.Errorf("payload = %v, want mode=yolo workdir=/tmp/work uuid=%s", body, hooksTestUUID)
		}
	case <-time.After(time.Second):
		t.Fatal("no session_new event published")
	}

	if got := mgr.Active(); len(got) != 1 || got[0] != "alpha" {
		t.Errorf("Active = %v, want [alpha]", got)
	}
}

func TestHooks_SessionKilledStopsTailer(t *testing.T) {
	hub := events.NewHub(0)
	srv, mgr := newHookServer(t, hub)

	postHook(t, srv, "session_new", url.Values{
		"name": {"beta"},
		"uuid": {hooksTestUUID},
	}).Body.Close()
	if got := mgr.Active(); len(got) != 1 {
		t.Fatalf("expected 1 tailer, got %v", got)
	}

	resp := postHook(t, srv, "session_killed", url.Values{"name": {"beta"}})
	resp.Body.Close()

	if got := mgr.Active(); len(got) != 0 {
		t.Errorf("Active after kill = %v, want []", got)
	}
}

func TestHooks_UnknownEventReturns404(t *testing.T) {
	hub := events.NewHub(0)
	srv, _ := newHookServer(t, hub)

	resp := postHook(t, srv, "session_oops", url.Values{"name": {"x"}})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestHooks_GetReturns405(t *testing.T) {
	hub := events.NewHub(0)
	srv, _ := newHookServer(t, hub)
	resp, err := http.Get(srv.URL + "/api/hooks/session_new")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.StatusCode)
	}
	if got := resp.Header.Get("Allow"); !strings.Contains(got, "POST") {
		t.Errorf("Allow = %q, want contains POST", got)
	}
}

func TestHooks_AttachedStampsTimestamp(t *testing.T) {
	hub := events.NewHub(0)
	sub, _ := hub.Subscribe("", "")
	defer sub.Close()
	srv, _ := newHookServer(t, hub)

	resp := postHook(t, srv, "session_attached", url.Values{"name": {"gamma"}})
	resp.Body.Close()

	select {
	case ev := <-sub.Events():
		var body map[string]any
		_ = json.Unmarshal(ev.Payload, &body)
		at, ok := body["at"].(string)
		if !ok || at == "" {
			t.Errorf("missing `at` timestamp; payload = %v", body)
		}
		if _, err := time.Parse(time.RFC3339, at); err != nil {
			t.Errorf("at = %q, not RFC3339: %v", at, err)
		}
	case <-time.After(time.Second):
		t.Fatal("no session_attached event")
	}
}
