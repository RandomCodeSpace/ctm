// Package store persists per-session token/cost history so the
// dashboard can render a cumulative-cost chart (V13) that survives
// daemon restarts.
//
// The store is a thin wrapper over SQLite (github.com/mattn/go-sqlite3,
// CGo — picked for raw retrieval speed; see project_v13_storage_decision).
// One table (cost_points) holds append-only samples; a compound index
// on (session, ts) keeps Range queries cheap regardless of how many
// sessions have landed rows.
//
// # Required server.go wiring (coordinator owns this)
//
// Open the DB after `hub := events.NewHub(0)`:
//
//	costDB, err := store.OpenCostStore(filepath.Join(config.Dir(), "ctm.db"))
//	if err != nil { return nil, fmt.Errorf("open cost db: %w", err) }
//
// Attach it to the Server struct (field `cost store.CostStore`) and
// close it in Run's shutdown path:
//
//	defer costDB.Close()
//
// Subscribe a goroutine to the hub that writes `quota_update` events
// carrying `session` + token triples into the store — see
// store.SubscribeQuotaWriter for the helper.
//
// Mount the handler in registerRoutes:
//
//	mux.Handle("GET /api/cost", authHF(api.Cost(s.cost)))
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// Point is a single persisted cost sample.
type Point struct {
	TS            time.Time
	Session       string
	InputTokens   int64
	OutputTokens  int64
	CacheTokens   int64
	CostUSDMicros int64 // USD * 1_000_000
}

// Totals is the aggregate returned by CostStore.Totals for a window.
type Totals struct {
	InputTokens   int64
	OutputTokens  int64
	CacheTokens   int64
	CostUSDMicros int64
}

// CostStore is the persistence seam. Handlers depend on the interface
// so tests can swap in an in-memory fake.
type CostStore interface {
	// Insert appends a batch of points in a single transaction. A nil
	// or empty slice is a no-op (returns nil).
	Insert(points []Point) error

	// Range returns every point for session (or all sessions if
	// session == "") with ts ∈ [since, until], sorted oldest-first.
	Range(session string, since, until time.Time) ([]Point, error)

	// Totals aggregates all points with ts >= since across every
	// session. The caller picks the time window; the store has no
	// opinion about what "total" means.
	Totals(since time.Time) (Totals, error)

	// Close releases the underlying DB handle. Idempotent.
	Close() error
}

// sqliteCostStore is the production CostStore backed by github.com/mattn/go-sqlite3.
type sqliteCostStore struct {
	db     *sql.DB
	closed bool
	mu     sync.Mutex
}

// OpenCostStore opens (or creates) the SQLite DB at path and applies
// the V13 schema. Callers should Close() on shutdown.
//
// WAL + NORMAL sync is used for write throughput; busy_timeout=5000ms
// keeps the handler-side Writer from erroring under light contention
// with the quota-subscriber goroutine.
func OpenCostStore(path string) (CostStore, error) {
	// DSN tuning: ?_busy_timeout=5000 waits out brief writer locks;
	// ?_journal=WAL enables concurrent readers; ?_sync=NORMAL pairs
	// with WAL for an acceptable durability-vs-speed trade.
	v := url.Values{}
	v.Set("_busy_timeout", "5000")
	v.Set("_journal", "WAL")
	v.Set("_sync", "NORMAL")
	dsn := fmt.Sprintf("file:%s?%s", path, v.Encode())

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("sql.Open: %w", err)
	}
	// SQLite doesn't benefit from a large pool; a single writer avoids
	// "database is locked" churn under WAL.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if err := applySchema(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &sqliteCostStore{db: db}, nil
}

const schemaSQL = `
CREATE TABLE IF NOT EXISTS cost_points(
  ts              INTEGER NOT NULL, -- unix millis
  session         TEXT NOT NULL,
  input_tokens    INTEGER NOT NULL,
  output_tokens   INTEGER NOT NULL,
  cache_tokens    INTEGER NOT NULL,
  cost_usd_micros INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS cost_points_session_ts ON cost_points(session, ts);
CREATE TABLE IF NOT EXISTS schema_meta(key TEXT PRIMARY KEY, value TEXT NOT NULL);
INSERT OR IGNORE INTO schema_meta(key, value) VALUES('version', '2');

-- V19 slice 3 (v0.3): FTS5 index over tool_call payloads.
-- Trigram tokenizer so queries like "needle" match inside
-- tokens like "has-needle-row" without needing explicit wildcards.
-- Wiped on every boot; the tailer's replay (offset starts at 0)
-- repopulates it via the tool_call hub subscriber.
CREATE VIRTUAL TABLE IF NOT EXISTS tool_calls_fts USING fts5(
  session, ts UNINDEXED, tool UNINDEXED, content,
  tokenize = 'trigram'
);
`

func applySchema(db *sql.DB) error {
	// PRAGMAs must run outside a transaction.
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL;",
		"PRAGMA synchronous=NORMAL;",
	} {
		if _, err := db.Exec(pragma); err != nil {
			return fmt.Errorf("%s: %w", pragma, err)
		}
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		return fmt.Errorf("schema DDL: %w", err)
	}
	// V19 slice 3: the FTS index is rebuilt on each boot by the
	// tailer's offset-0 replay feeding the tool_call subscriber, so
	// we start from an empty table to avoid accumulating duplicates
	// across restarts.
	if _, err := db.Exec("DELETE FROM tool_calls_fts;"); err != nil {
		return fmt.Errorf("wipe fts: %w", err)
	}
	return nil
}

func (s *sqliteCostStore) Insert(points []Point) error {
	if len(points) == 0 {
		return nil
	}
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return errors.New("cost store closed")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	// Rollback on any error path — safe to call after Commit.
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.Prepare(`
INSERT INTO cost_points(ts, session, input_tokens, output_tokens, cache_tokens, cost_usd_micros)
VALUES(?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	for _, p := range points {
		if _, err := stmt.Exec(
			p.TS.UnixMilli(),
			p.Session,
			p.InputTokens,
			p.OutputTokens,
			p.CacheTokens,
			p.CostUSDMicros,
		); err != nil {
			return fmt.Errorf("exec: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

func (s *sqliteCostStore) Range(session string, since, until time.Time) ([]Point, error) {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return nil, errors.New("cost store closed")
	}
	sinceMs := since.UnixMilli()
	untilMs := until.UnixMilli()

	var (
		rows *sql.Rows
		err  error
	)
	if session == "" {
		rows, err = s.db.Query(`
SELECT ts, session, input_tokens, output_tokens, cache_tokens, cost_usd_micros
FROM cost_points
WHERE ts >= ? AND ts <= ?
ORDER BY ts ASC`, sinceMs, untilMs)
	} else {
		rows, err = s.db.Query(`
SELECT ts, session, input_tokens, output_tokens, cache_tokens, cost_usd_micros
FROM cost_points
WHERE session = ? AND ts >= ? AND ts <= ?
ORDER BY ts ASC`, session, sinceMs, untilMs)
	}
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]Point, 0, 64)
	for rows.Next() {
		var (
			tsMs int64
			p    Point
		)
		if err := rows.Scan(
			&tsMs,
			&p.Session,
			&p.InputTokens,
			&p.OutputTokens,
			&p.CacheTokens,
			&p.CostUSDMicros,
		); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		p.TS = time.UnixMilli(tsMs).UTC()
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows.Err: %w", err)
	}
	return out, nil
}

func (s *sqliteCostStore) Totals(since time.Time) (Totals, error) {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return Totals{}, errors.New("cost store closed")
	}
	// Cost is an append-only cumulative-delta series: every row is a
	// token snapshot at a point in time. The totals view the handler
	// wants is "latest counts per session, summed". Use a correlated
	// MAX(ts) sub-select to avoid pulling every row into memory.
	row := s.db.QueryRow(`
WITH latest AS (
  SELECT cp.session,
         cp.input_tokens,
         cp.output_tokens,
         cp.cache_tokens,
         cp.cost_usd_micros
  FROM cost_points cp
  JOIN (
    SELECT session, MAX(ts) AS max_ts
    FROM cost_points
    WHERE ts >= ?
    GROUP BY session
  ) m ON m.session = cp.session AND m.max_ts = cp.ts
)
SELECT
  COALESCE(SUM(input_tokens),   0),
  COALESCE(SUM(output_tokens),  0),
  COALESCE(SUM(cache_tokens),   0),
  COALESCE(SUM(cost_usd_micros), 0)
FROM latest`, since.UnixMilli())

	var t Totals
	if err := row.Scan(&t.InputTokens, &t.OutputTokens, &t.CacheTokens, &t.CostUSDMicros); err != nil {
		return Totals{}, fmt.Errorf("scan: %w", err)
	}
	return t, nil
}

func (s *sqliteCostStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}
