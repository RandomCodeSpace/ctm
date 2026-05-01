package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeQuotaSrc is a tiny in-memory QuotaSource so the Quota handler can
// be exercised without touching ingest.
type fakeQuotaSrc struct{ snap QuotaSnapshot }

func (f fakeQuotaSrc) Snapshot() QuotaSnapshot { return f.snap }

func TestQuota_HappyPath(t *testing.T) {
	weeklyReset := time.Date(2026, 4, 22, 13, 0, 0, 0, time.UTC)
	fiveReset := time.Date(2026, 4, 21, 18, 0, 0, 0, time.UTC)
	src := fakeQuotaSrc{snap: QuotaSnapshot{
		WeeklyPct:       46,
		FiveHourPct:     3,
		WeeklyResetsAt:  weeklyReset,
		FiveHourResetAt: fiveReset,
		Known:           true,
	}}
	h := Quota(src)
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/api/quota", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}

	var body struct {
		WeeklyPct      int    `json:"weekly_pct"`
		FiveHrPct      int    `json:"five_hr_pct"`
		WeeklyResetsAt string `json:"weekly_resets_at"`
		FiveHrResetsAt string `json:"five_hr_resets_at"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.WeeklyPct != 46 || body.FiveHrPct != 3 {
		t.Errorf("body pcts = (%d,%d), want (46,3)", body.WeeklyPct, body.FiveHrPct)
	}
	if body.WeeklyResetsAt != weeklyReset.Format(time.RFC3339) {
		t.Errorf("weekly_resets_at = %q, want %q", body.WeeklyResetsAt, weeklyReset.Format(time.RFC3339))
	}
	if body.FiveHrResetsAt != fiveReset.Format(time.RFC3339) {
		t.Errorf("five_hr_resets_at = %q, want %q", body.FiveHrResetsAt, fiveReset.Format(time.RFC3339))
	}
}

func TestQuota_UnknownReturns204(t *testing.T) {
	src := fakeQuotaSrc{snap: QuotaSnapshot{Known: false}}
	h := Quota(src)
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/api/quota", nil))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("body should be empty, got %q", rec.Body.String())
	}
}

func TestQuota_MethodNotAllowed(t *testing.T) {
	h := Quota(fakeQuotaSrc{snap: QuotaSnapshot{Known: true}})
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		rec := httptest.NewRecorder()
		h(rec, httptest.NewRequest(m, "/api/quota", strings.NewReader("")))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s status = %d, want 405", m, rec.Code)
		}
		if got := rec.Header().Get("Allow"); got != http.MethodGet {
			t.Errorf("%s Allow = %q, want GET", m, got)
		}
	}
}

func TestQuota_ZeroResetTimesEmitEmptyStrings(t *testing.T) {
	src := fakeQuotaSrc{snap: QuotaSnapshot{
		WeeklyPct:   12,
		FiveHourPct: 7,
		Known:       true,
		// Reset times left as zero — handler must serialize as "".
	}}
	h := Quota(src)
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/api/quota", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got, _ := body["weekly_resets_at"].(string); got != "" {
		t.Errorf("weekly_resets_at = %q, want empty string", got)
	}
	if got, _ := body["five_hr_resets_at"].(string); got != "" {
		t.Errorf("five_hr_resets_at = %q, want empty string", got)
	}
}

func TestRfc3339OrEmpty(t *testing.T) {
	if got := rfc3339OrEmpty(time.Time{}); got != "" {
		t.Errorf("rfc3339OrEmpty(zero) = %q, want empty", got)
	}
	// Non-UTC input must be normalized to UTC in the output.
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	in := time.Date(2026, 1, 2, 3, 4, 5, 0, loc)
	got := rfc3339OrEmpty(in)
	want := in.UTC().Format(time.RFC3339)
	if got != want {
		t.Errorf("rfc3339OrEmpty(NY time) = %q, want %q", got, want)
	}
	if !strings.HasSuffix(got, "Z") {
		t.Errorf("rfc3339OrEmpty output %q should end with Z (UTC)", got)
	}
}
