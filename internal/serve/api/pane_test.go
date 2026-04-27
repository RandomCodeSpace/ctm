package api

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeCapturer returns scripted outputs on successive calls. Once the
// scripted slice is exhausted, the last entry is repeated so the loop
// has a stable "idle" value to debounce against. lastScrollback
// records the most recent CapturePaneHistory argument so tests can
// assert the handler threaded `?history=` through correctly.
type fakeCapturer struct {
	mu             sync.Mutex
	outputs        []string
	errs           []error
	calls          int32
	lastScrollback int
}

func (f *fakeCapturer) CapturePaneHistory(_ string, scrollback int) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastScrollback = scrollback
	i := int(atomic.AddInt32(&f.calls, 1)) - 1
	var out string
	var err error
	if i < len(f.outputs) {
		out = f.outputs[i]
	} else if len(f.outputs) > 0 {
		out = f.outputs[len(f.outputs)-1]
	}
	if i < len(f.errs) {
		err = f.errs[i]
	}
	return out, err
}

// readPaneFrames scans an SSE body for `event: pane` frames and
// returns their JSON-encoded data lines in order. Stops when the
// reader errors (body closed) or after `want` frames.
func readPaneFrames(t *testing.T, body io.Reader, want int, timeout time.Duration) []string {
	t.Helper()
	r := bufio.NewReader(body)
	got := make([]string, 0, want)
	done := make(chan struct{})
	var mu sync.Mutex
	go func() {
		defer close(done)
		var eventType string
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\r\n")
			switch {
			case strings.HasPrefix(line, "event: "):
				eventType = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				if eventType == "pane" {
					mu.Lock()
					got = append(got, strings.TrimPrefix(line, "data: "))
					n := len(got)
					mu.Unlock()
					if n >= want {
						return
					}
				}
			case line == "":
				eventType = ""
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(timeout):
	}
	mu.Lock()
	defer mu.Unlock()
	return append([]string(nil), got...)
}

func TestPaneStream_EmitsFramesWithCorrectFormat(t *testing.T) {
	fake := &fakeCapturer{outputs: []string{"alpha\n", "bravo\n", "charlie\n"}}

	h := PaneStream(fake)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.SetPathValue("name", "alpha")
		h(w, r)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("content-type = %q, want text/event-stream", ct)
	}

	frames := readPaneFrames(t, resp.Body, 3, 2800*time.Millisecond)
	if len(frames) < 3 {
		t.Fatalf("frames = %d (%v), want >=3", len(frames), frames)
	}
	wants := []string{`"alpha\n"`, `"bravo\n"`, `"charlie\n"`}
	for i, w := range wants {
		if frames[i] != w {
			t.Errorf("frame[%d] = %q, want %q", i, frames[i], w)
		}
	}
}

func TestPaneStream_DebouncesIdenticalPayloads(t *testing.T) {
	fake := &fakeCapturer{outputs: []string{"same\n"}}

	h := PaneStream(fake)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.SetPathValue("name", "alpha")
		h(w, r)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	frames := readPaneFrames(t, resp.Body, 3, 2300*time.Millisecond)
	if len(frames) != 1 {
		t.Errorf("frames = %d (%v), want 1 (identical payloads debounced)", len(frames), frames)
	}
}

func TestPaneStream_ExitsOnClientDisconnect(t *testing.T) {
	fake := &fakeCapturer{outputs: []string{"one\n"}}

	done := make(chan struct{})
	h := PaneStream(fake)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.SetPathValue("name", "alpha")
		h(w, r)
		close(done)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}

	_ = readPaneFrames(t, resp.Body, 1, 1500*time.Millisecond)
	resp.Body.Close()
	cancel()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("handler did not exit within 3s of client disconnect")
	}
}

func TestPaneStream_ErrorTwiceEndsStream(t *testing.T) {
	fake := &fakeCapturer{
		outputs: []string{"ok\n", "", ""},
		errs:    []error{nil, errors.New("gone"), errors.New("gone")},
	}
	h := PaneStream(fake)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.SetPathValue("name", "alpha")
		h(w, r)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "event: pane\ndata: \"ok\\n\"") {
		t.Errorf("expected initial pane frame, got:\n%s", string(body))
	}
	if !strings.Contains(string(body), "event: pane_end") {
		t.Errorf("expected pane_end frame after consecutive errors, got:\n%s", string(body))
	}
}

func TestPaneStream_DefaultScrollbackAndOverride(t *testing.T) {
	cases := []struct {
		name    string
		query   string
		want    int
	}{
		{name: "default when unset", query: "", want: defaultPaneScrollback},
		{name: "honours ?history=250", query: "history=250", want: 250},
		{name: "caps at maxPaneScrollback", query: "history=99999", want: maxPaneScrollback},
		{name: "zero disables scrollback", query: "history=0", want: 0},
		{name: "negative falls back to default", query: "history=-5", want: defaultPaneScrollback},
		{name: "garbage falls back to default", query: "history=notanumber", want: defaultPaneScrollback},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeCapturer{outputs: []string{"x\n"}}
			h := PaneStream(fake)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				r.SetPathValue("name", "alpha")
				h(w, r)
			}))
			defer srv.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
			defer cancel()
			url := srv.URL
			if tc.query != "" {
				url += "?" + tc.query
			}
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("GET: %v", err)
			}
			defer resp.Body.Close()
			_ = readPaneFrames(t, resp.Body, 1, 900*time.Millisecond)

			fake.mu.Lock()
			got := fake.lastScrollback
			fake.mu.Unlock()
			if got != tc.want {
				t.Errorf("lastScrollback = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestPaneStream_MissingNameReturns400(t *testing.T) {
	fake := &fakeCapturer{outputs: []string{"x"}}
	h := PaneStream(fake)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events/session//pane", nil)
	h(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}
