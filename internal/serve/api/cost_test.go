package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeCostSource is an in-memory CostSource for handler tests.
type fakeCostSource struct {
	points []CostPoint
	totals CostTotals
	err    error
}

func (f fakeCostSource) Range(session string, since, until time.Time) ([]CostPoint, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := make([]CostPoint, 0, len(f.points))
	for _, p := range f.points {
		if session != "" && p.Session != session {
			continue
		}
		if p.TS.Before(since) || p.TS.After(until) {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

func (f fakeCostSource) Totals(since time.Time) (CostTotals, error) {
	if f.err != nil {
		return CostTotals{}, f.err
	}
	return f.totals, nil
}

func TestCost_HappyPath(t *testing.T) {
	now := time.Now().UTC()
	src := fakeCostSource{
		points: []CostPoint{
			{TS: now.Add(-10 * time.Minute), Session: "alpha", InputTokens: 100, OutputTokens: 50, CacheTokens: 10, CostUSDMicros: 1200},
			{TS: now.Add(-5 * time.Minute), Session: "alpha", InputTokens: 200, OutputTokens: 100, CacheTokens: 20, CostUSDMicros: 2400},
		},
		totals: CostTotals{InputTokens: 200, OutputTokens: 100, CacheTokens: 20, CostUSDMicros: 2400},
	}

	h := Cost(src)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/cost?window=hour&session=alpha", nil)
	h(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body costResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Window != "hour" {
		t.Errorf("window = %q, want hour", body.Window)
	}
	if len(body.Points) != 2 {
		t.Fatalf("points len = %d, want 2", len(body.Points))
	}
	if body.Points[0].Session != "alpha" || body.Points[0].InputTokens != 100 {
		t.Errorf("points[0] = %+v", body.Points[0])
	}
	if body.Totals.Input != 200 || body.Totals.CostUSDMicros != 2400 {
		t.Errorf("totals = %+v", body.Totals)
	}
}

func TestCost_UnknownWindow(t *testing.T) {
	h := Cost(fakeCostSource{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/cost?window=forever", nil)
	h(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["error"] != "unknown_window" {
		t.Errorf("error = %v, want unknown_window", body["error"])
	}
}

func TestCost_DefaultWindowIsDay(t *testing.T) {
	h := Cost(fakeCostSource{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/cost", nil)
	h(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body costResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Window != "day" {
		t.Errorf("window = %q, want day", body.Window)
	}
}

func TestCost_MissingSessionAggregatesAcross(t *testing.T) {
	now := time.Now().UTC()
	src := fakeCostSource{
		points: []CostPoint{
			{TS: now.Add(-1 * time.Minute), Session: "alpha", InputTokens: 10},
			{TS: now.Add(-1 * time.Minute), Session: "beta", InputTokens: 20},
		},
	}
	h := Cost(src)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/cost?window=day", nil)
	h(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body costResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if len(body.Points) != 2 {
		t.Fatalf("points len = %d, want 2 (both sessions)", len(body.Points))
	}
}

func TestCost_EmptyStore(t *testing.T) {
	h := Cost(fakeCostSource{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/cost?window=day", nil)
	h(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body costResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Points) != 0 {
		t.Errorf("points = %+v, want []", body.Points)
	}
	if body.Totals.Input != 0 || body.Totals.CostUSDMicros != 0 {
		t.Errorf("totals = %+v, want zero", body.Totals)
	}
	// Shape check: points is explicitly [], not null. JS code path
	// assumes an array; null would break .map() without extra guards.
	if !strings.Contains(rec.Body.String(), `"points":[]`) {
		t.Errorf("body missing points:[] literal — got %s", rec.Body.String())
	}
}

func TestCost_405OnPost(t *testing.T) {
	h := Cost(fakeCostSource{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/cost", nil)
	h(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
	if got := rec.Header().Get("Allow"); !strings.Contains(got, "GET") {
		t.Errorf("Allow header = %q, want GET", got)
	}
}
