package store

// Package-level docs in cost_store.go. This file adds the V19 slice 3
// FTS5-backed full-text search layer, sharing the same SQLite handle
// already opened by OpenCostStore.

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// SearchMatch mirrors api.SearchMatch in wire form. Exported so the
// server.go adapter can type-assert cleanly (shared field shape keeps
// the api package free of the store dependency).
type SearchMatch struct {
	Session string
	TS      time.Time
	Tool    string
	Snippet string
}

// SearchStore is the persistence seam for the FTS5 index. sqliteCostStore
// implements both CostStore and SearchStore so a single *sql.DB handle
// backs every V13/V19 write path.
type SearchStore interface {
	// IndexToolCall appends one searchable row. Idempotency is the
	// caller's problem — OpenCostStore wipes the FTS table on boot so
	// the tailer's offset-0 replay repopulates it fresh.
	IndexToolCall(session, tool, content string, ts time.Time) error

	// SearchFTS returns at most limit matches for q, optionally filtered
	// by session. The boolean return reports truncation.
	SearchFTS(q, sessionFilter string, limit int) ([]SearchMatch, bool, error)
}

// IndexToolCall writes one row to the FTS virtual table. Empty content
// is skipped — nothing to search.
func (s *sqliteCostStore) IndexToolCall(session, tool, content string, ts time.Time) error {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}

	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return errors.New("search store closed")
	}

	_, err := s.db.Exec(
		`INSERT INTO tool_calls_fts(session, ts, tool, content) VALUES(?, ?, ?, ?)`,
		session, ts.UnixMilli(), tool, content,
	)
	if err != nil {
		return fmt.Errorf("fts insert: %w", err)
	}
	return nil
}

// SearchFTS runs an FTS5 MATCH query. q is wrapped in double quotes so
// shell-level punctuation (slashes, dots, underscores in file paths)
// is treated as a literal phrase under the trigram tokenizer. Limit is
// clamped to >=1 by the caller; we over-fetch by one row to detect
// truncation.
func (s *sqliteCostStore) SearchFTS(q, sessionFilter string, limit int) ([]SearchMatch, bool, error) {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return nil, false, errors.New("search store closed")
	}
	if limit < 1 {
		limit = 1
	}

	// Trigram tokenizer expects a phrase expression; the double-quoted
	// form matches a literal substring of length >= 3. The caller
	// enforces the minimum length at the API boundary.
	phrase := fts5QuotePhrase(q)

	var (
		rows *sql.Rows
		err  error
	)
	if sessionFilter == "" {
		rows, err = s.db.Query(`
SELECT session, ts, tool, content
FROM tool_calls_fts
WHERE tool_calls_fts MATCH ?
ORDER BY rowid DESC
LIMIT ?`, phrase, limit+1)
	} else {
		rows, err = s.db.Query(`
SELECT session, ts, tool, content
FROM tool_calls_fts
WHERE tool_calls_fts MATCH ? AND session = ?
ORDER BY rowid DESC
LIMIT ?`, phrase, sessionFilter, limit+1)
	}
	if err != nil {
		return nil, false, fmt.Errorf("fts query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]SearchMatch, 0, limit)
	truncated := false
	for rows.Next() {
		if len(out) >= limit {
			truncated = true
			break
		}
		var (
			session, tool, content string
			tsMs                   int64
		)
		if err := rows.Scan(&session, &tsMs, &tool, &content); err != nil {
			return nil, false, fmt.Errorf("fts scan: %w", err)
		}
		out = append(out, SearchMatch{
			Session: session,
			TS:      time.UnixMilli(tsMs).UTC(),
			Tool:    tool,
			Snippet: snippet(content, q),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("fts rows: %w", err)
	}
	return out, truncated, nil
}

// fts5QuotePhrase escapes the caller's query so FTS5 treats it as a
// literal phrase: any internal double-quote is doubled per the FTS5
// quoting rules, and the whole string is wrapped in double quotes.
// This is intentionally narrow — we do not support the full MATCH
// grammar (AND/OR/NEAR) in slice 3; power users can add the v0.4
// advanced-query-syntax mode later.
func fts5QuotePhrase(q string) string {
	var b strings.Builder
	b.Grow(len(q) + 2)
	b.WriteByte('"')
	for _, r := range q {
		if r == '"' {
			b.WriteString(`""`)
			continue
		}
		b.WriteRune(r)
	}
	b.WriteByte('"')
	return b.String()
}

// snippet extracts a 60-char window around the first case-insensitive
// occurrence of q in content. Mirrors the slice-1 snippet shape so UI
// code stays unchanged.
func snippet(content, q string) string {
	const half = 30
	lc := strings.ToLower(content)
	lq := strings.ToLower(q)
	idx := strings.Index(lc, lq)
	if idx < 0 {
		// Shouldn't happen — FTS MATCH said there was a hit. Fall
		// back to a head-of-content prefix so we still return
		// something intelligible.
		if len(content) > 2*half+len(q) {
			return content[:2*half+len(q)]
		}
		return content
	}
	start := idx - half
	if start < 0 {
		start = 0
	}
	end := idx + len(q) + half
	if end > len(content) {
		end = len(content)
	}
	return content[start:end]
}
