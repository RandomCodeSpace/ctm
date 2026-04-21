// Package webhook dispatches attention_raised events to a user-configured
// HTTP endpoint. Retries with exponential backoff and debounces duplicate
// (session, alert) pairs within a configurable window to prevent flapping.
package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/RandomCodeSpace/ctm/internal/serve/events"
)

const (
	defaultTimeout     = 10 * time.Second
	defaultDebounce    = 60 * time.Second
	attentionRaisedKey = "attention_raised"
	maxInflight        = 8
)

var defaultRetryDelays = []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second}

// Config configures the dispatcher. Zero-value fields fall back to
// sensible defaults; an empty URL disables dispatch entirely.
type Config struct {
	URL         string
	AuthHeader  string
	UIBaseURL   string
	Timeout     time.Duration
	DebounceFor time.Duration
	// RetryDelays lets tests inject short delays. Nil → {1s, 2s, 4s}.
	RetryDelays []time.Duration
}

// SessionResolver returns metadata for a session name. Zero values are
// acceptable when unknown — the dispatcher emits whatever is provided.
type SessionResolver interface {
	Resolve(name string) (uuid string, workdir string, mode string, ok bool)
}

// Payload matches spec §6 exactly. Time is serialized as RFC 3339.
type Payload struct {
	Alert       string    `json:"alert"`
	Session     string    `json:"session"`
	SessionUUID string    `json:"session_uuid,omitempty"`
	Workdir     string    `json:"workdir,omitempty"`
	Mode        string    `json:"mode,omitempty"`
	Details     string    `json:"details,omitempty"`
	TS          time.Time `json:"ts"`
	UIURL       string    `json:"ui_url,omitempty"`
}

// attentionPayload mirrors the JSON shape the attention engine publishes
// on the hub under Event.Payload for attention_raised.
type attentionPayload struct {
	State   string `json:"state"`
	Details string `json:"details"`
	// TS is optional; the attention engine may or may not set it.
	TS time.Time `json:"ts"`
}

// Dispatcher subscribes to the hub and dispatches attention_raised
// events via HTTP POST, retrying on failure and debouncing duplicates.
type Dispatcher struct {
	hub      *events.Hub
	resolver SessionResolver
	cfg      Config
	client   *http.Client

	mu          sync.Mutex
	lastSent    map[string]time.Time
	debounceFor time.Duration
	retryDelays []time.Duration

	sem chan struct{}
	wg  sync.WaitGroup
}

// NewDispatcher constructs a Dispatcher. httpClient may be nil; a client
// with the configured per-attempt Timeout is built on demand.
func NewDispatcher(hub *events.Hub, resolver SessionResolver, cfg Config, httpClient *http.Client) *Dispatcher {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: timeout}
	}
	debounce := cfg.DebounceFor
	if debounce <= 0 {
		debounce = defaultDebounce
	}
	delays := cfg.RetryDelays
	if delays == nil {
		delays = defaultRetryDelays
	}
	return &Dispatcher{
		hub:         hub,
		resolver:    resolver,
		cfg:         cfg,
		client:      httpClient,
		lastSent:    make(map[string]time.Time),
		debounceFor: debounce,
		retryDelays: delays,
		sem:         make(chan struct{}, maxInflight),
	}
}

// Run subscribes to the hub's global stream, dispatching attention_raised
// events until ctx is done. When cfg.URL is empty, it logs and returns
// immediately. Active POST goroutines honour ctx cancellation; Run waits
// for them before returning.
func (d *Dispatcher) Run(ctx context.Context) error {
	if d.cfg.URL == "" {
		slog.Debug("webhook dispatcher disabled")
		return nil
	}

	sub, _ := d.hub.Subscribe("", "")
	defer sub.Close()

	slog.Info("webhook dispatcher started", "url", d.cfg.URL)

	for {
		select {
		case <-ctx.Done():
			d.wg.Wait()
			return nil
		case e, ok := <-sub.Events():
			if !ok {
				d.wg.Wait()
				return nil
			}
			if e.Type != attentionRaisedKey {
				continue
			}
			d.handle(ctx, e)
		}
	}
}

func (d *Dispatcher) handle(ctx context.Context, e events.Event) {
	var ap attentionPayload
	if len(e.Payload) > 0 {
		_ = json.Unmarshal(e.Payload, &ap)
	}
	alert := ap.State
	if alert == "" {
		// No alert label → nothing actionable to send.
		slog.Debug("webhook: attention_raised missing state, skipping", "session", e.Session)
		return
	}

	key := e.Session + "|" + alert
	now := time.Now()

	d.mu.Lock()
	last, hadPrev := d.lastSent[key]
	if hadPrev && now.Sub(last) < d.debounceFor {
		d.mu.Unlock()
		slog.Debug("webhook: debounced", "session", e.Session, "alert", alert)
		return
	}
	d.lastSent[key] = now
	d.mu.Unlock()

	ts := ap.TS
	if ts.IsZero() {
		ts = now
	}

	p := Payload{
		Alert:   alert,
		Session: e.Session,
		Details: ap.Details,
		TS:      ts,
	}
	if d.resolver != nil {
		if uuid, workdir, mode, ok := d.resolver.Resolve(e.Session); ok {
			p.SessionUUID = uuid
			p.Workdir = workdir
			p.Mode = mode
		}
	}
	if d.cfg.UIBaseURL != "" && e.Session != "" {
		p.UIURL = d.cfg.UIBaseURL + "/s/" + e.Session
	}

	// Bounded concurrency: if the pool is full, wait or abort on ctx.
	select {
	case d.sem <- struct{}{}:
	case <-ctx.Done():
		return
	}

	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		defer func() { <-d.sem }()
		d.send(ctx, p)
	}()
}

func (d *Dispatcher) send(ctx context.Context, p Payload) {
	body, err := json.Marshal(p)
	if err != nil {
		slog.Warn("webhook: marshal failed", "err", err, "session", p.Session, "alert", p.Alert)
		return
	}

	var lastErr error
	// attempts = initial try + len(retryDelays). After attempt i (0-indexed),
	// if it failed and i < len(retryDelays), sleep retryDelays[i] then retry.
	for i := 0; i <= len(d.retryDelays); i++ {
		if ctx.Err() != nil {
			return
		}
		lastErr = d.postOnce(ctx, body)
		if lastErr == nil {
			if i > 0 {
				slog.Info("webhook: delivered after retry", "session", p.Session, "alert", p.Alert, "attempts", i+1)
			} else {
				slog.Info("webhook: delivered", "session", p.Session, "alert", p.Alert)
			}
			return
		}
		if i == len(d.retryDelays) {
			break
		}
		delay := d.retryDelays[i]
		t := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
	}
	slog.Warn("webhook: delivery failed after retries",
		"session", p.Session,
		"alert", p.Alert,
		"attempts", len(d.retryDelays)+1,
		"err", lastErr)
}

func (d *Dispatcher) postOnce(ctx context.Context, body []byte) error {
	// Per-attempt timeout: Config.Timeout. The http.Client.Timeout covers
	// this when client is constructed by us; but when a caller injects
	// their own client we still want a bound, so use a per-request ctx.
	timeout := d.cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, d.cfg.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if d.cfg.AuthHeader != "" {
		req.Header.Set("Authorization", d.cfg.AuthHeader)
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return fmt.Errorf("webhook: non-2xx status %d", resp.StatusCode)
}

// ErrDisabled is kept for potential future callers that want to check
// whether a dispatcher was disabled. Currently Run just returns nil.
var ErrDisabled = errors.New("webhook dispatcher disabled")
