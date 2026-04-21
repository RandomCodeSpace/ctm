package events

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// readSSEFrames parses raw SSE bytes off a reader into frame maps until
// `want` frames have been collected or the deadline expires.
func readSSEFrames(t *testing.T, r *bufio.Reader, want int, deadline time.Duration) []map[string]string {
	t.Helper()
	type result struct {
		frame map[string]string
		err   error
	}

	frames := make([]map[string]string, 0, want)
	cur := map[string]string{}
	dataLines := []string{}

	timer := time.NewTimer(deadline)
	defer timer.Stop()
	ch := make(chan result, 1)

	readOne := func() {
		line, err := r.ReadString('\n')
		ch <- result{frame: map[string]string{"_line": line}, err: err}
	}

	go readOne()
	for len(frames) < want {
		select {
		case res := <-ch:
			if res.err != nil {
				t.Fatalf("sse read: %v", res.err)
			}
			line := strings.TrimRight(res.frame["_line"], "\n")
			if line == "" {
				// Frame boundary. Only dispatch if a data line was seen
				// (per SSE spec a comment-only block dispatches nothing).
				if len(dataLines) > 0 {
					cur["data"] = strings.Join(dataLines, "\n")
					frames = append(frames, cur)
				}
				cur = map[string]string{}
				dataLines = nil
			} else if strings.HasPrefix(line, ":") {
				cur["_comment"] = strings.TrimSpace(line[1:])
			} else if i := strings.IndexByte(line, ':'); i > 0 {
				key := line[:i]
				val := strings.TrimPrefix(line[i+1:], " ")
				if key == "data" {
					dataLines = append(dataLines, val)
				} else {
					cur[key] = val
				}
			}
			if len(frames) < want {
				go readOne()
			}
		case <-timer.C:
			t.Fatalf("timed out reading %d sse frames (got %d)", want, len(frames))
		}
	}
	return frames
}

func newSSEServer(t *testing.T, h *Hub, filter string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/events", Handler(h, filter))
	return httptest.NewServer(mux)
}

func TestSSE_Handler_Basic(t *testing.T) {
	t.Parallel()
	h := NewHub(0)
	ts := newSSEServer(t, h, "")
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type=%q", got)
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control=%q", got)
	}
	if got := resp.Header.Get("X-Accel-Buffering"); got != "no" {
		t.Fatalf("X-Accel-Buffering=%q", got)
	}

	// Give handler a tick to register its sub before we publish.
	time.Sleep(20 * time.Millisecond)
	h.Publish(Event{Type: "tool_call", Payload: []byte(`{"k":"v"}`)})
	h.Publish(Event{Type: "quota_update", Payload: []byte(`{"weekly_pct":34}`)})

	br := bufio.NewReader(resp.Body)
	frames := readSSEFrames(t, br, 2, 2*time.Second)

	if frames[0]["event"] != "tool_call" {
		t.Fatalf("frame 0 event=%q", frames[0]["event"])
	}
	if frames[0]["data"] != `{"k":"v"}` {
		t.Fatalf("frame 0 data=%q", frames[0]["data"])
	}
	if frames[0]["id"] == "" {
		t.Fatal("frame 0 missing id")
	}
	if frames[1]["event"] != "quota_update" {
		t.Fatalf("frame 1 event=%q", frames[1]["event"])
	}
}

func TestSSE_LastEventIDResume(t *testing.T) {
	t.Parallel()
	h := NewHub(50)

	// Pre-publish a batch and capture IDs.
	for i := 0; i < 5; i++ {
		h.Publish(Event{Type: "x", Payload: []byte(`{}`)})
	}
	snap := h.rings[globalRing].snapshot()
	kthID := snap[2].ID

	ts := newSSEServer(t, h, "")
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/events", nil)
	req.Header.Set("Last-Event-ID", kthID)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	br := bufio.NewReader(resp.Body)
	// Expect 2 replayed frames (ids 3 and 4).
	frames := readSSEFrames(t, br, 2, 2*time.Second)
	if frames[0]["id"] != snap[3].ID {
		t.Fatalf("resume frame 0 id=%q want %q", frames[0]["id"], snap[3].ID)
	}
	if frames[1]["id"] != snap[4].ID {
		t.Fatalf("resume frame 1 id=%q want %q", frames[1]["id"], snap[4].ID)
	}
}

func TestSSE_MultilineDataSplit(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	tw := &testFlusher{w: &sb}
	e := Event{Type: "x", ID: "1-0", Payload: []byte("line1\nline2\nline3")}
	if err := writeEvent(tw, tw, e); err != nil {
		t.Fatal(err)
	}
	got := sb.String()
	want := "event: x\nid: 1-0\ndata: line1\ndata: line2\ndata: line3\n\n"
	if got != want {
		t.Fatalf("frame mismatch:\ngot  %q\nwant %q", got, want)
	}
}

type testFlusher struct{ w *strings.Builder }

func (t *testFlusher) Write(p []byte) (int, error) { return t.w.Write(p) }
func (t *testFlusher) Flush()                      {}
