package webhook

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RandomCodeSpace/ctm/internal/serve/events"
)

type stubResolver struct {
	uuid, workdir, mode string
	ok                  bool
}

func (s stubResolver) Resolve(string) (string, string, string, bool) {
	return s.uuid, s.workdir, s.mode, s.ok
}

// fastCfg returns a Config with tiny retry delays / debounce for tests.
func fastCfg(url string) Config {
	return Config{
		URL:         url,
		UIBaseURL:   "http://localhost:37778",
		Timeout:     2 * time.Second,
		DebounceFor: 50 * time.Millisecond,
		RetryDelays: []time.Duration{1 * time.Millisecond, 1 * time.Millisecond, 1 * time.Millisecond},
	}
}

// publishAttention publishes an attention_raised event onto the hub and
// returns immediately. Callers synchronize via the test server callback.
func publishAttention(t *testing.T, hub *events.Hub, session, state, details string) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"state":   state,
		"details": details,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	hub.Publish(events.Event{
		Type:    "attention_raised",
		Session: session,
		Payload: payload,
	})
}

// waitFor spins until cond() returns true or the timeout elapses.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("condition not met within %v", timeout)
}

func TestDispatcher_EmptyURL_ReturnsImmediately(t *testing.T) {
	hub := events.NewHub(0)
	d := NewDispatcher(hub, nil, Config{}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if err := d.Run(ctx); err != nil {
		t.Fatalf("expected nil err, got %v", err)
	}
}

func TestDispatcher_PostsCorrectPayload(t *testing.T) {
	var (
		mu      sync.Mutex
		gotBody []byte
		gotAuth string
	)
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		gotBody = body
		gotAuth = r.Header.Get("Authorization")
		mu.Unlock()
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		w.WriteHeader(http.StatusNoContent)
		select {
		case <-done:
		default:
			close(done)
		}
	}))
	defer srv.Close()

	hub := events.NewHub(0)
	cfg := fastCfg(srv.URL)
	cfg.AuthHeader = "Bearer secret-xyz"

	d := NewDispatcher(hub, stubResolver{uuid: "uuid-1", workdir: "/tmp/work", mode: "yolo", ok: true}, cfg, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runDone := make(chan struct{})
	go func() { _ = d.Run(ctx); close(runDone) }()

	// Let the subscription register before publishing.
	time.Sleep(10 * time.Millisecond)
	publishAttention(t, hub, "sess-a", "error_burst", "3/5 errors")

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("server did not receive request")
	}

	mu.Lock()
	body := gotBody
	auth := gotAuth
	mu.Unlock()

	var p Payload
	if err := json.Unmarshal(body, &p); err != nil {
		t.Fatalf("unmarshal: %v  body=%s", err, string(body))
	}
	if p.Alert != "error_burst" {
		t.Errorf("Alert = %q, want error_burst", p.Alert)
	}
	if p.Session != "sess-a" {
		t.Errorf("Session = %q, want sess-a", p.Session)
	}
	if p.SessionUUID != "uuid-1" {
		t.Errorf("SessionUUID = %q, want uuid-1", p.SessionUUID)
	}
	if p.Workdir != "/tmp/work" {
		t.Errorf("Workdir = %q, want /tmp/work", p.Workdir)
	}
	if p.Mode != "yolo" {
		t.Errorf("Mode = %q, want yolo", p.Mode)
	}
	if p.Details != "3/5 errors" {
		t.Errorf("Details = %q, want 3/5 errors", p.Details)
	}
	if p.UIURL != "http://localhost:37778/s/sess-a" {
		t.Errorf("UIURL = %q", p.UIURL)
	}
	if p.TS.IsZero() {
		t.Error("TS should be set")
	}
	if auth != "Bearer secret-xyz" {
		t.Errorf("Authorization = %q", auth)
	}

	cancel()
	<-runDone
}

func TestDispatcher_RetriesOn500ThenSucceeds(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	hub := events.NewHub(0)
	d := NewDispatcher(hub, nil, fastCfg(srv.URL), nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runDone := make(chan struct{})
	go func() { _ = d.Run(ctx); close(runDone) }()

	time.Sleep(10 * time.Millisecond)
	publishAttention(t, hub, "sess-b", "stuck", "")

	waitFor(t, 2*time.Second, func() bool { return calls.Load() >= 3 })
	// Give the dispatcher a moment to record "success".
	time.Sleep(20 * time.Millisecond)
	if got := calls.Load(); got != 3 {
		t.Errorf("expected 3 calls, got %d", got)
	}

	cancel()
	<-runDone
}

func TestDispatcher_StopsAfterMaxRetries(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	hub := events.NewHub(0)
	d := NewDispatcher(hub, nil, fastCfg(srv.URL), nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runDone := make(chan struct{})
	go func() { _ = d.Run(ctx); close(runDone) }()

	time.Sleep(10 * time.Millisecond)
	publishAttention(t, hub, "sess-c", "tmux_dead", "")

	// 1 initial + 3 retries = 4 attempts.
	waitFor(t, 2*time.Second, func() bool { return calls.Load() >= 4 })
	time.Sleep(30 * time.Millisecond)
	if got := calls.Load(); got != 4 {
		t.Errorf("expected 4 attempts, got %d", got)
	}

	cancel()
	<-runDone
}

func TestDispatcher_Debounce(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	hub := events.NewHub(0)
	cfg := fastCfg(srv.URL)
	cfg.DebounceFor = 80 * time.Millisecond
	d := NewDispatcher(hub, nil, cfg, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runDone := make(chan struct{})
	go func() { _ = d.Run(ctx); close(runDone) }()

	time.Sleep(10 * time.Millisecond)
	publishAttention(t, hub, "sess-d", "error_burst", "")
	waitFor(t, 1*time.Second, func() bool { return calls.Load() == 1 })

	// Second event within the debounce window: dropped.
	publishAttention(t, hub, "sess-d", "error_burst", "")
	time.Sleep(30 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Errorf("expected 1 call (2nd debounced), got %d", got)
	}

	// Different alert: should go through.
	publishAttention(t, hub, "sess-d", "stuck", "")
	waitFor(t, 1*time.Second, func() bool { return calls.Load() == 2 })

	// After debounce window passes, the same (session, alert) fires again.
	time.Sleep(100 * time.Millisecond)
	publishAttention(t, hub, "sess-d", "error_burst", "")
	waitFor(t, 1*time.Second, func() bool { return calls.Load() == 3 })

	cancel()
	<-runDone
}

func TestDispatcher_IgnoresOtherEventTypes(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	hub := events.NewHub(0)
	d := NewDispatcher(hub, nil, fastCfg(srv.URL), nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runDone := make(chan struct{})
	go func() { _ = d.Run(ctx); close(runDone) }()

	time.Sleep(10 * time.Millisecond)
	hub.Publish(events.Event{Type: "tool_call", Session: "x", Payload: json.RawMessage(`{}`)})
	hub.Publish(events.Event{Type: "quota_update", Payload: json.RawMessage(`{}`)})
	hub.Publish(events.Event{Type: "attention_cleared", Session: "x", Payload: json.RawMessage(`{}`)})
	time.Sleep(50 * time.Millisecond)

	if got := calls.Load(); got != 0 {
		t.Errorf("expected 0 calls, got %d", got)
	}

	cancel()
	<-runDone
}

func TestDispatcher_CtxCancel_NoGoroutineLeak(t *testing.T) {
	// Block indefinitely on the server so a POST is in-flight when we cancel.
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	defer close(release)

	hub := events.NewHub(0)
	cfg := fastCfg(srv.URL)
	cfg.Timeout = 5 * time.Second
	d := NewDispatcher(hub, nil, cfg, nil)

	baseline := runtime.NumGoroutine()

	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan struct{})
	go func() { _ = d.Run(ctx); close(runDone) }()

	time.Sleep(10 * time.Millisecond)
	publishAttention(t, hub, "sess-e", "error_burst", "")
	time.Sleep(20 * time.Millisecond)

	cancel()
	select {
	case <-runDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after ctx cancel")
	}

	// Allow Go runtime to reap goroutines.
	waitFor(t, 2*time.Second, func() bool {
		return runtime.NumGoroutine() <= baseline+1
	})
}
