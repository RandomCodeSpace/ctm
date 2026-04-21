package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/RandomCodeSpace/ctm/internal/serve/events"
)

func openTest(t *testing.T) CostStore {
	t.Helper()
	s, err := OpenCostStore(filepath.Join(t.TempDir(), "ctm.db"))
	if err != nil {
		t.Fatalf("OpenCostStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestCostStore_InsertRangeRoundTrip(t *testing.T) {
	t.Parallel()
	s := openTest(t)

	base := time.Now().UTC().Truncate(time.Millisecond)
	pts := []Point{
		{TS: base.Add(-2 * time.Minute), Session: "alpha", InputTokens: 100, OutputTokens: 50, CacheTokens: 10, CostUSDMicros: 1234},
		{TS: base.Add(-1 * time.Minute), Session: "alpha", InputTokens: 200, OutputTokens: 80, CacheTokens: 20, CostUSDMicros: 2468},
		{TS: base, Session: "beta", InputTokens: 50, OutputTokens: 25, CacheTokens: 5, CostUSDMicros: 500},
	}
	if err := s.Insert(pts); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	got, err := s.Range("", base.Add(-5*time.Minute), base.Add(time.Minute))
	if err != nil {
		t.Fatalf("Range all: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len(all) = %d, want 3", len(got))
	}
	for i := 1; i < len(got); i++ {
		if got[i].TS.Before(got[i-1].TS) {
			t.Errorf("Range not sorted asc: %v before %v", got[i].TS, got[i-1].TS)
		}
	}
}

func TestCostStore_RangeSessionFilter(t *testing.T) {
	t.Parallel()
	s := openTest(t)

	base := time.Now().UTC().Truncate(time.Millisecond)
	_ = s.Insert([]Point{
		{TS: base, Session: "alpha", InputTokens: 100},
		{TS: base, Session: "beta", InputTokens: 200},
	})

	alpha, err := s.Range("alpha", base.Add(-time.Hour), base.Add(time.Hour))
	if err != nil {
		t.Fatalf("Range alpha: %v", err)
	}
	if len(alpha) != 1 || alpha[0].Session != "alpha" {
		t.Fatalf("alpha = %+v, want one alpha row", alpha)
	}
}

func TestCostStore_RangeTimeBounded(t *testing.T) {
	t.Parallel()
	s := openTest(t)

	now := time.Now().UTC().Truncate(time.Millisecond)
	_ = s.Insert([]Point{
		{TS: now.Add(-2 * time.Hour), Session: "alpha", InputTokens: 10},
		{TS: now, Session: "alpha", InputTokens: 20},
	})

	// Only the recent row falls within a 1h window.
	got, err := s.Range("alpha", now.Add(-time.Hour), now.Add(time.Second))
	if err != nil {
		t.Fatalf("Range: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].InputTokens != 20 {
		t.Errorf("got[0].InputTokens = %d, want 20", got[0].InputTokens)
	}
}

func TestCostStore_RangeEmpty(t *testing.T) {
	t.Parallel()
	s := openTest(t)
	got, err := s.Range("nobody", time.Now().Add(-time.Hour), time.Now())
	if err != nil {
		t.Fatalf("Range empty: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("len = %d, want 0", len(got))
	}
}

func TestCostStore_Totals(t *testing.T) {
	t.Parallel()
	s := openTest(t)

	now := time.Now().UTC().Truncate(time.Millisecond)
	// Two sessions, two samples each. Totals picks the latest per
	// session and sums across sessions — matches the handler shape.
	_ = s.Insert([]Point{
		{TS: now.Add(-time.Minute), Session: "a", InputTokens: 10, OutputTokens: 5, CacheTokens: 1, CostUSDMicros: 100},
		{TS: now, Session: "a", InputTokens: 20, OutputTokens: 10, CacheTokens: 2, CostUSDMicros: 200},
		{TS: now, Session: "b", InputTokens: 50, OutputTokens: 25, CacheTokens: 5, CostUSDMicros: 500},
	})

	tot, err := s.Totals(now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("Totals: %v", err)
	}
	// Latest-per-session: a=20/10/2/200, b=50/25/5/500 → sum.
	if tot.InputTokens != 70 {
		t.Errorf("InputTokens = %d, want 70", tot.InputTokens)
	}
	if tot.OutputTokens != 35 {
		t.Errorf("OutputTokens = %d, want 35", tot.OutputTokens)
	}
	if tot.CacheTokens != 7 {
		t.Errorf("CacheTokens = %d, want 7", tot.CacheTokens)
	}
	if tot.CostUSDMicros != 700 {
		t.Errorf("CostUSDMicros = %d, want 700", tot.CostUSDMicros)
	}
}

func TestCostStore_BatchInsertTxSemantics(t *testing.T) {
	t.Parallel()
	// Correctness check: a 500-row batch lands atomically. Not a
	// benchmark — just verifies the BEGIN/COMMIT wrap works.
	s := openTest(t)

	batch := make([]Point, 500)
	base := time.Now().UTC().Truncate(time.Millisecond)
	for i := range batch {
		batch[i] = Point{
			TS:          base.Add(time.Duration(i) * time.Millisecond),
			Session:     "bulk",
			InputTokens: int64(i),
		}
	}
	if err := s.Insert(batch); err != nil {
		t.Fatalf("Insert batch: %v", err)
	}
	got, err := s.Range("bulk", base.Add(-time.Second), base.Add(time.Hour))
	if err != nil {
		t.Fatalf("Range: %v", err)
	}
	if len(got) != 500 {
		t.Fatalf("len = %d, want 500", len(got))
	}
}

func TestCostStore_ConcurrentWrites(t *testing.T) {
	t.Parallel()
	s := openTest(t)

	const writers = 4
	const perWriter = 50
	base := time.Now().UTC().Truncate(time.Millisecond)

	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				_ = s.Insert([]Point{{
					TS:          base.Add(time.Duration(w*perWriter+i) * time.Millisecond),
					Session:     "shared",
					InputTokens: int64(w*100 + i),
				}})
			}
		}(w)
	}
	wg.Wait()

	got, err := s.Range("shared", base.Add(-time.Second), base.Add(time.Hour))
	if err != nil {
		t.Fatalf("Range: %v", err)
	}
	if len(got) != writers*perWriter {
		t.Fatalf("len = %d, want %d", len(got), writers*perWriter)
	}
}

func TestCostStore_CloseIdempotent(t *testing.T) {
	t.Parallel()
	s, err := OpenCostStore(filepath.Join(t.TempDir(), "ctm.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	// Second call must not panic or error.
	if err := s.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
	// Operations after Close return an error, not a panic.
	if err := s.Insert([]Point{{Session: "x", TS: time.Now()}}); err == nil {
		t.Errorf("Insert after Close: want error, got nil")
	}
}

func TestSubscribeQuotaWriter_PersistsPerSessionUpdates(t *testing.T) {
	t.Parallel()
	s := openTest(t)
	hub := events.NewHub(0)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	ready := make(chan struct{})
	go func() {
		defer close(done)
		SubscribeQuotaWriter(ctx, hub, s, ready)
	}()
	<-ready

	payload, _ := json.Marshal(map[string]any{
		"session":       "alpha",
		"input_tokens":  1000,
		"output_tokens": 500,
		"cache_tokens":  100,
	})
	hub.Publish(events.Event{Type: "quota_update", Session: "alpha", Payload: payload})

	// Poll for the write to land (async subscriber).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		pts, err := s.Range("alpha", time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
		if err == nil && len(pts) == 1 {
			if pts[0].InputTokens != 1000 {
				t.Errorf("InputTokens = %d, want 1000", pts[0].InputTokens)
			}
			if pts[0].CostUSDMicros == 0 {
				t.Errorf("cost not computed")
			}
			cancel()
			<-done
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
	t.Fatalf("no point persisted within deadline")
}

func TestSubscribeQuotaWriter_IgnoresGlobalAndEmpty(t *testing.T) {
	t.Parallel()
	s := openTest(t)
	hub := events.NewHub(0)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	ready := make(chan struct{})
	go func() {
		defer close(done)
		SubscribeQuotaWriter(ctx, hub, s, ready)
	}()
	<-ready

	// Global rate-limit update: no session, no tokens. Must be ignored.
	gbl, _ := json.Marshal(map[string]any{"weekly_pct": 10})
	hub.Publish(events.Event{Type: "quota_update", Payload: gbl})

	// Empty per-session — all zeros, should also be ignored (noise).
	zero, _ := json.Marshal(map[string]any{"session": "a", "input_tokens": 0})
	hub.Publish(events.Event{Type: "quota_update", Session: "a", Payload: zero})

	// Let the goroutine process.
	time.Sleep(100 * time.Millisecond)
	cancel()
	<-done

	pts, _ := s.Range("", time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	if len(pts) != 0 {
		t.Errorf("got %d points, want 0", len(pts))
	}
}

func TestComputeCostMicros(t *testing.T) {
	t.Parallel()
	// 1M input tokens → $3 → 3_000_000 micros.
	got := ComputeCostMicros(1_000_000, 0, 0)
	if got != 3_000_000 {
		t.Errorf("input 1M: got %d, want 3_000_000", got)
	}
	// 1M output → $15 → 15_000_000 micros.
	got = ComputeCostMicros(0, 1_000_000, 0)
	if got != 15_000_000 {
		t.Errorf("output 1M: got %d, want 15_000_000", got)
	}
	// 1M cache → $0.30 → 300_000 micros.
	got = ComputeCostMicros(0, 0, 1_000_000)
	if got != 300_000 {
		t.Errorf("cache 1M: got %d, want 300_000", got)
	}
}
