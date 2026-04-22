package api

// V19 slice 3: /api/search is now backed by the FTS5 index maintained
// in internal/serve/store. The handler no longer walks *.jsonl files
// — queries hit the index directly. Trigram tokenization means the
// minimum query length bumps from 2 to 3 chars.
//
// Wiring (central, server.go):
//   mux.Handle("GET /api/search", authHF(api.Search(searchSourceAdapter{s.cost}, logsUUIDResolver{proj: s.proj})))

import (
	"errors"
	"net/http"
	"time"
)

// SessionNameResolver reverse-maps session name → log UUID. Satisfied
// by serve.logsUUIDResolver. Kept separate from UUIDNameResolver so
// callers can depend on only the direction they need.
type SessionNameResolver interface {
	ResolveSessionName(name string) (uuid string, ok bool)
}

var errNotPositiveInt = errors.New("not a positive int")

// SearchMatch is one hit in the search response.
type SearchMatch struct {
	Session string `json:"session"`
	UUID    string `json:"uuid"`
	TS      string `json:"ts,omitempty"`
	Tool    string `json:"tool,omitempty"`
	Snippet string `json:"snippet"`
}

// SearchResponse wraps matches with scan stats for the UI.
type SearchResponse struct {
	Query     string        `json:"query"`
	Matches   []SearchMatch `json:"matches"`
	Truncated bool          `json:"truncated"`
}

// SearchSource is the persistence seam — in production it's the FTS5
// store; tests fake it with an in-memory slice.
type SearchSource interface {
	SearchFTS(q, sessionFilter string, limit int) ([]SearchHit, bool, error)
}

// SearchHit mirrors store.SearchMatch in wire-neutral form so the api
// package doesn't depend on the store package.
type SearchHit struct {
	Session string
	TS      time.Time
	Tool    string
	Snippet string
}

const (
	searchMinLen   = 3
	searchMaxLen   = 256
	searchMaxLimit = 500
	searchDefLimit = 100
)

// Search returns a handler that queries the FTS5 index for substring
// hits against q. Response shape matches V19 slice 1 — UUID is
// resolved from the session name at query time.
func Search(src SearchSource, resolver SessionNameResolver) http.HandlerFunc {
	// Build an inverse name → uuid map once per request (via a
	// resolver-scan closure) so Search can stamp the UUID field.
	// Resolver walks via (uuid → name); we keep the call site cheap
	// by doing an O(n) scan on cache miss. For v0.3 the session
	// count is small (<100) so this is fine.
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		q := r.URL.Query().Get("q")
		if len(q) < searchMinLen || len(q) > searchMaxLen {
			http.Error(w, "q must be 3..256 chars", http.StatusBadRequest)
			return
		}
		sessionFilter := r.URL.Query().Get("session")
		limit := searchDefLimit
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := parseSearchPositiveInt(v); err == nil {
				limit = n
			}
		}
		if limit > searchMaxLimit {
			limit = searchMaxLimit
		}

		hits, truncated, err := src.SearchFTS(q, sessionFilter, limit)
		if err != nil {
			http.Error(w, "search: "+err.Error(), http.StatusInternalServerError)
			return
		}

		matches := make([]SearchMatch, 0, len(hits))
		uuidOf := map[string]string{}
		for _, h := range hits {
			uuid, ok := uuidOf[h.Session]
			if !ok && resolver != nil {
				uuid, _ = resolver.ResolveSessionName(h.Session)
				uuidOf[h.Session] = uuid
			}
			m := SearchMatch{
				Session: h.Session,
				UUID:    uuid,
				Tool:    h.Tool,
				Snippet: h.Snippet,
			}
			if !h.TS.IsZero() {
				m.TS = h.TS.Format(time.RFC3339Nano)
			}
			matches = append(matches, m)
		}

		writeJSON(w, http.StatusOK, SearchResponse{
			Query:     q,
			Matches:   matches,
			Truncated: truncated,
		})
	}
}

func parseSearchPositiveInt(s string) (int, error) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, errNotPositiveInt
		}
		n = n*10 + int(c-'0')
		if n > 1<<20 {
			return n, nil
		}
	}
	if n <= 0 {
		return 0, errNotPositiveInt
	}
	return n, nil
}
