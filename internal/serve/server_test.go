package serve

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// testToken is the bearer token injected into every Server spun up by
// startServer. Tests that exercise authenticated endpoints prepend
// `Authorization: Bearer <testToken>` on their requests.
const testToken = "test-bearer-token"

// authedGet issues a GET with the test bearer header set.
func authedGet(t *testing.T, url string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do %s: %v", url, err)
	}
	return resp
}

// pickFreePort asks the kernel for a free loopback port, then releases
// it so the Server under test can bind. A race between release and
// re-bind is theoretically possible but vanishingly rare on a single
// test box; acceptable for unit tests.
func pickFreePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	if err := l.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return port
}

// startServer brings up a Server on the given port and waits until
// /healthz is live. Returns a stop function that cancels Run and
// blocks until the server goroutine exits.
func startServer(t *testing.T, version string, port int) func() {
	t.Helper()
	// Isolate the projection away from the user's real ~/.config/ctm.
	sessionsPath := filepath.Join(t.TempDir(), "sessions.json")
	srv, err := New(Options{
		Port:              port,
		Version:           version,
		Token:             testToken,
		SessionsPath:      sessionsPath,
		TmuxConfPath:      filepath.Join(t.TempDir(), "tmux.conf"),
		LogDir:            filepath.Join(t.TempDir(), "logs"),
		StatuslineDumpDir: filepath.Join(t.TempDir(), "statusline"),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = srv.Run(ctx)
		close(done)
	}()

	stop := func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Errorf("server did not shut down within 5s")
		}
	}

	addr := "http://127.0.0.1:" + strconv.Itoa(port)
	deadline := time.Now().Add(2 * time.Second)
	for {
		resp, err := http.Get(addr + "/healthz")
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return stop
			}
		}
		if time.Now().After(deadline) {
			stop()
			t.Fatal("server did not become healthy in 2s")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestHealthzReportsVersionHeader(t *testing.T) {
	port := pickFreePort(t)
	t.Cleanup(startServer(t, "test-v1", port))

	resp, err := http.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/healthz")
	if err != nil {
		t.Fatalf("get /healthz: %v", err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get(ServeVersionHeader); got != "test-v1" {
		t.Errorf("X-Ctm-Serve = %q, want %q", got, "test-v1")
	}
	if got := resp.Header.Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", got)
	}

	var body struct {
		Status        string  `json:"status"`
		UptimeSeconds float64 `json:"uptime_seconds"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Status != "ok" {
		t.Errorf("status = %q, want ok", body.Status)
	}
	if body.UptimeSeconds < 0 {
		t.Errorf("uptime_seconds = %v, want >= 0", body.UptimeSeconds)
	}
}

func TestHealthIncludesComponents(t *testing.T) {
	port := pickFreePort(t)
	t.Cleanup(startServer(t, "vX", port))

	resp := authedGet(t, "http://127.0.0.1:"+strconv.Itoa(port)+"/health")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var body struct {
		Status     string            `json:"status"`
		Version    string            `json:"version"`
		Components map[string]string `json:"components"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Version != "vX" {
		t.Errorf("version = %q, want vX", body.Version)
	}
	if body.Components["http"] != "ok" {
		t.Errorf("components.http = %q, want ok", body.Components["http"])
	}
}

func TestAuthRejectsUnauthenticated(t *testing.T) {
	port := pickFreePort(t)
	t.Cleanup(startServer(t, "vA", port))

	// /healthz is public; every other documented endpoint requires the
	// bearer token.
	authed := []string{
		"/health",
		"/api/bootstrap",
		"/api/sessions",
		"/events/all",
	}
	for _, path := range authed {
		resp, err := http.Get("http://127.0.0.1:" + strconv.Itoa(port) + path)
		if err != nil {
			t.Fatalf("get %s: %v", path, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("GET %s without token: status = %d, want 401", path, resp.StatusCode)
		}
	}
}

func TestSessionsListEmptyByDefault(t *testing.T) {
	port := pickFreePort(t)
	t.Cleanup(startServer(t, "v1", port))

	resp := authedGet(t, "http://127.0.0.1:"+strconv.Itoa(port)+"/api/sessions")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	// Empty sessions.json → empty JSON array.
	if got := strings.TrimSpace(string(body)); got != "[]" {
		t.Errorf("body = %q, want '[]'", got)
	}
}

func TestBootstrapReflectsServerVersion(t *testing.T) {
	port := pickFreePort(t)
	t.Cleanup(startServer(t, "vBoot", port))

	resp := authedGet(t, "http://127.0.0.1:"+strconv.Itoa(port)+"/api/bootstrap")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Version    string `json:"version"`
		Port       int    `json:"port"`
		HasWebhook bool   `json:"has_webhook"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Version != "vBoot" || body.Port != port {
		t.Errorf("body = %+v, want version=vBoot port=%d", body, port)
	}
}

func TestSingleInstanceGuardDetectsSibling(t *testing.T) {
	port := pickFreePort(t)
	t.Cleanup(startServer(t, "first", port))

	_, err := New(Options{Port: port, Version: "second", Token: testToken})
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Errorf("err = %v, want ErrAlreadyRunning", err)
	}
}

func TestSingleInstanceGuardRefusesForeignListener(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()
	port := l.Addr().(*net.TCPAddr).Port

	_, err = New(Options{Port: port, Version: "x", Token: testToken})
	if err == nil {
		t.Fatal("New succeeded on foreign-occupied port; want error")
	}
	if errors.Is(err, ErrAlreadyRunning) {
		t.Errorf("err = %v, want non-ErrAlreadyRunning", err)
	}
	if !strings.Contains(err.Error(), "non-ctm-serve") {
		t.Errorf("err = %q, want mention of non-ctm-serve", err.Error())
	}
}

func TestRootServesEmbeddedIndex(t *testing.T) {
	port := pickFreePort(t)
	t.Cleanup(startServer(t, "v1", port))

	resp, err := http.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/")
	if err != nil {
		t.Fatalf("get /: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	// Vite builds emit `<!doctype html>` (lowercase). Anything HTML-ish
	// proves the embed is wired; aesthetic checks happen in the UI.
	if !strings.Contains(strings.ToLower(string(body)), "<!doctype html>") {
		t.Errorf("body missing <!doctype html>: %q", string(body)[:min(200, len(body))])
	}
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", got)
	}
}

func TestUnknownAPIReturns404(t *testing.T) {
	port := pickFreePort(t)
	t.Cleanup(startServer(t, "v1", port))

	resp, err := http.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/api/does-not-exist")
	if err != nil {
		t.Fatalf("get /api: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestUnknownEventsReturns404(t *testing.T) {
	port := pickFreePort(t)
	t.Cleanup(startServer(t, "v1", port))

	// /events/all and /events/session/{name} are registered; anything
	// else under /events/ should hit the placeholder catch-all and 404.
	resp, err := http.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/events/does-not-exist")
	if err != nil {
		t.Fatalf("get /events: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestHealthzRejectsPost(t *testing.T) {
	port := pickFreePort(t)
	t.Cleanup(startServer(t, "v1", port))

	resp, err := http.Post("http://127.0.0.1:"+strconv.Itoa(port)+"/healthz",
		"text/plain", strings.NewReader("nope"))
	if err != nil {
		t.Fatalf("post /healthz: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.StatusCode)
	}
}
