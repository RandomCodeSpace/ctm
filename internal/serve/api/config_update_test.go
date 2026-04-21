package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RandomCodeSpace/ctm/internal/config"
)

// validBody is a happy-path patch body that covers every allowlisted
// key. Reused across tests so a schema change breaks every case at
// once instead of letting old bodies silently pass.
func validBody() string {
	return `{
		"webhook_url": "https://hooks.example/ctm",
		"webhook_auth": "Bearer xyz",
		"attention": {
			"error_rate_pct": 25,
			"error_rate_window": 40,
			"idle_minutes": 10,
			"quota_pct": 90,
			"context_pct": 95,
			"yolo_unchecked_minutes": 45
		}
	}`
}

// seedConfig writes a default config.json to tmpDir and returns the
// full path. Load() auto-creates on missing, but seeding gives us a
// stable starting state so assertions can compare against known values.
func seedConfig(t *testing.T, dir string) string {
	t.Helper()
	cfgPath := filepath.Join(dir, "config.json")
	def := config.Default()
	// Give it a distinct webhook_url we can check was overwritten.
	def.Serve.WebhookURL = "https://old.example"
	data, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		t.Fatalf("marshal seed: %v", err)
	}
	if err := os.WriteFile(cfgPath, data, 0600); err != nil {
		t.Fatalf("write seed: %v", err)
	}
	return cfgPath
}

func TestConfigUpdate_HappyPath(t *testing.T) {
	dir := t.TempDir()
	cfgPath := seedConfig(t, dir)

	var shutdownCalls atomic.Int32
	var shutdownReason atomic.Value
	shutdown := func(reason string) {
		shutdownCalls.Add(1)
		shutdownReason.Store(reason)
	}

	h := ConfigUpdate(cfgPath, shutdown)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(validBody()))
	h(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode resp: %v", err)
	}
	if resp["status"] != "restarting" {
		t.Errorf("status field = %q", resp["status"])
	}

	// Config was written atomically.
	got, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.Serve.WebhookURL != "https://hooks.example/ctm" {
		t.Errorf("webhook_url = %q", got.Serve.WebhookURL)
	}
	if got.Serve.WebhookAuth != "Bearer xyz" {
		t.Errorf("webhook_auth = %q", got.Serve.WebhookAuth)
	}
	if got.Serve.Attention.QuotaPct != 90 {
		t.Errorf("quota_pct = %d", got.Serve.Attention.QuotaPct)
	}
	if got.Serve.Attention.YoloUncheckedMinutes != 45 {
		t.Errorf("yolo_unchecked_minutes = %d", got.Serve.Attention.YoloUncheckedMinutes)
	}

	// shutdown() runs in a goroutine after a 1s delay. Poll up to 3s so
	// slow CI workers don't flake; fail fast if it never fires.
	deadline := time.Now().Add(3 * time.Second)
	for shutdownCalls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if shutdownCalls.Load() != 1 {
		t.Errorf("shutdown called %d times, want 1", shutdownCalls.Load())
	}
	if got, _ := shutdownReason.Load().(string); got != "config change" {
		t.Errorf("shutdown reason = %q", got)
	}

	// Tmp sibling must not leak on success.
	if _, err := os.Stat(cfgPath + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("tmp file left behind: err=%v", err)
	}
}

func TestConfigUpdate_UnknownKey(t *testing.T) {
	dir := t.TempDir()
	cfgPath := seedConfig(t, dir)
	h := ConfigUpdate(cfgPath, func(string) {})
	body := `{"bogus_key": 1}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	h(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if !strings.Contains(resp["error"], "unknown key") {
		t.Errorf("error = %q, want contains 'unknown key'", resp["error"])
	}
}

func TestConfigUpdate_InvalidRange(t *testing.T) {
	dir := t.TempDir()
	cfgPath := seedConfig(t, dir)
	h := ConfigUpdate(cfgPath, func(string) {})

	cases := []struct {
		name string
		body string
		want string // substring the error message must contain
	}{
		{
			"pct over 100",
			`{"attention":{"error_rate_pct":150,"error_rate_window":10,"idle_minutes":5,"quota_pct":80,"context_pct":90,"yolo_unchecked_minutes":10}}`,
			"error_rate_pct",
		},
		{
			"minutes over 1440",
			`{"attention":{"error_rate_pct":10,"error_rate_window":10,"idle_minutes":9999,"quota_pct":80,"context_pct":90,"yolo_unchecked_minutes":10}}`,
			"idle_minutes",
		},
		{
			"zero rejected",
			`{"attention":{"error_rate_pct":10,"error_rate_window":10,"idle_minutes":5,"quota_pct":0,"context_pct":90,"yolo_unchecked_minutes":10}}`,
			"quota_pct",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(tc.body))
			h(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
			}
			var resp map[string]string
			_ = json.Unmarshal(rec.Body.Bytes(), &resp)
			if !strings.Contains(resp["error"], tc.want) {
				t.Errorf("error = %q, want contains %q", resp["error"], tc.want)
			}
		})
	}
}

func TestConfigUpdate_MethodNotAllowed(t *testing.T) {
	dir := t.TempDir()
	cfgPath := seedConfig(t, dir)
	h := ConfigUpdate(cfgPath, func(string) {})
	for _, m := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete} {
		t.Run(m, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(m, "/api/config", strings.NewReader(validBody()))
			h(rec, req)
			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("status = %d, want 405", rec.Code)
			}
			if got := rec.Header().Get("Allow"); got != http.MethodPatch {
				t.Errorf("Allow = %q, want PATCH", got)
			}
		})
	}
}

func TestConfigUpdate_WriteFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod-based read-only dir test is POSIX-only")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses directory permissions")
	}
	dir := t.TempDir()
	cfgPath := seedConfig(t, dir)
	// Make the parent directory read-only so the atomic rename's
	// WriteFile on the .tmp sibling fails. Restore in cleanup so the
	// test runner can tear the temp dir down.
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatalf("chmod dir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0700) })

	var shutdownCalls atomic.Int32
	h := ConfigUpdate(cfgPath, func(string) { shutdownCalls.Add(1) })
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(validBody()))
	h(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", rec.Code, rec.Body.String())
	}
	// The daemon must NOT restart when the write failed — otherwise
	// users lose the running daemon state for a change that was never
	// persisted.
	time.Sleep(50 * time.Millisecond)
	if shutdownCalls.Load() != 0 {
		t.Errorf("shutdown called on write failure (count=%d); must not restart", shutdownCalls.Load())
	}
}

func TestConfigUpdate_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	cfgPath := seedConfig(t, dir)
	h := ConfigUpdate(cfgPath, func(string) {})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader("{not json"))
	h(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestConfigGet_HappyPath(t *testing.T) {
	dir := t.TempDir()
	cfgPath := seedConfig(t, dir)

	h := ConfigGet(cfgPath)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	h(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		WebhookURL  string `json:"webhook_url"`
		WebhookAuth string `json:"webhook_auth"`
		Attention   struct {
			ErrorRatePct         int `json:"error_rate_pct"`
			QuotaPct             int `json:"quota_pct"`
			YoloUncheckedMinutes int `json:"yolo_unchecked_minutes"`
		} `json:"attention"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.WebhookURL != "https://old.example" {
		t.Errorf("webhook_url = %q", got.WebhookURL)
	}
	// Resolved() fills zeros with defaults.
	if got.Attention.ErrorRatePct == 0 {
		t.Errorf("error_rate_pct = 0, want resolved default")
	}
	if got.Attention.QuotaPct == 0 {
		t.Errorf("quota_pct = 0, want resolved default")
	}
}

func TestConfigGet_MethodNotAllowed(t *testing.T) {
	dir := t.TempDir()
	cfgPath := seedConfig(t, dir)
	h := ConfigGet(cfgPath)
	for _, m := range []string{http.MethodPost, http.MethodPatch, http.MethodDelete} {
		t.Run(m, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(m, "/api/config", nil)
			h(rec, req)
			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("status = %d, want 405", rec.Code)
			}
		})
	}
}
