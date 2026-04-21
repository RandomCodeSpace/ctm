package api

// Wiring (central, server.go):
//   mux.Handle("GET /api/search", authHF(api.Search(s.logDir, logsUUIDResolver{proj: s.proj})))

import (
	"bufio"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// UUIDNameResolver is already defined in logs_usage.go (V21) —
// satisfied by serve.logsUUIDResolver. Reused here.

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
	Query        string        `json:"query"`
	Matches      []SearchMatch `json:"matches"`
	ScannedFiles int           `json:"scanned_files"`
	Truncated    bool          `json:"truncated"`
}

const (
	searchMinLen   = 2
	searchMaxLen   = 256
	searchMaxLimit = 500
	searchDefLimit = 100
	snippetHalf    = 30
	maxLineBytes   = 1 << 20 // 1 MiB, matches tailer
)

// Search returns a handler that scans *.jsonl files under logDir for
// substring hits against q. Slice 1 of V19 — regex and FTS5 come later.
func Search(logDir string, resolver UUIDNameResolver) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		q := r.URL.Query().Get("q")
		if len(q) < searchMinLen || len(q) > searchMaxLen {
			http.Error(w, "q must be 2..256 chars", http.StatusBadRequest)
			return
		}
		sessionFilter := r.URL.Query().Get("session")
		limit := searchDefLimit
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := parsePositiveInt(v); err == nil {
				limit = n
			}
		}
		if limit > searchMaxLimit {
			limit = searchMaxLimit
		}

		entries, err := os.ReadDir(logDir)
		if err != nil {
			// Missing dir is treated as empty — the UI shouldn't blow up
			// before the first session ever runs.
			if os.IsNotExist(err) {
				writeJSON(w, http.StatusOK, SearchResponse{Query: q, Matches: []SearchMatch{}})
				return
			}
			http.Error(w, "read log dir", http.StatusInternalServerError)
			return
		}

		// Pre-resolve file list, with session-filter short-circuit when set.
		type job struct {
			path string
			uuid string
			name string
		}
		var jobs []job
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
				continue
			}
			uuid := strings.TrimSuffix(e.Name(), ".jsonl")
			name := ""
			if resolver != nil {
				name, _ = resolver.ResolveUUID(uuid)
			}
			if sessionFilter != "" && name != sessionFilter {
				continue
			}
			jobs = append(jobs, job{
				path: filepath.Join(logDir, e.Name()),
				uuid: uuid,
				name: name,
			})
		}

		// Worker pool over files. Slice 1: single pass, no early-abort via
		// ctx — handlers inherit request ctx and the OS cancels on client
		// disconnect via the file close path.
		workers := runtime.GOMAXPROCS(0)
		if workers > len(jobs) {
			workers = len(jobs)
		}
		if workers < 1 {
			workers = 1
		}
		jobCh := make(chan job)
		resCh := make(chan SearchMatch, 256)
		var wg sync.WaitGroup
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := range jobCh {
					scanFile(j.path, j.uuid, j.name, q, resCh)
				}
			}()
		}
		go func() {
			for _, j := range jobs {
				jobCh <- j
			}
			close(jobCh)
			wg.Wait()
			close(resCh)
		}()

		matches := make([]SearchMatch, 0, limit)
		truncated := false
		for m := range resCh {
			if len(matches) >= limit {
				truncated = true
				// Keep draining so workers finish, but stop appending.
				continue
			}
			matches = append(matches, m)
		}

		writeJSON(w, http.StatusOK, SearchResponse{
			Query:        q,
			Matches:      matches,
			ScannedFiles: len(jobs),
			Truncated:    truncated,
		})
	}
}

// scanFile emits one match per matching line. Slice 1 behaviour: plain
// substring match with a 60-char snippet centered on the hit.
func scanFile(path, uuid, session, q string, out chan<- SearchMatch) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	for sc.Scan() {
		line := sc.Text()
		idx := strings.Index(line, q)
		if idx < 0 {
			continue
		}
		// Parse just enough JSON for ts/tool — best-effort only. Rows
		// that don't parse still count as hits (the snippet is what the
		// user wants to see).
		var row struct {
			TS   string `json:"ts"`
			Tool string `json:"tool"`
		}
		_ = json.Unmarshal([]byte(line), &row)

		start := idx - snippetHalf
		if start < 0 {
			start = 0
		}
		end := idx + len(q) + snippetHalf
		if end > len(line) {
			end = len(line)
		}
		out <- SearchMatch{
			Session: session,
			UUID:    uuid,
			TS:      row.TS,
			Tool:    row.Tool,
			Snippet: line[start:end],
		}
	}
}

func parsePositiveInt(s string) (int, error) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, errNotInt
		}
		n = n*10 + int(c-'0')
		if n > 1<<20 {
			return n, nil
		}
	}
	if n <= 0 {
		return 0, errNotInt
	}
	return n, nil
}

var errNotInt = &apiError{msg: "not a positive int"}

type apiError struct{ msg string }

func (e *apiError) Error() string { return e.msg }
