// Package api — /api/logs/usage surfaces disk usage of the JSONL
// tailer directory so users can notice when it's time to prune.
//
// Walk is bounded (maxFilesLimit) to keep the handler cheap even on
// very old installs that have accumulated thousands of transcripts.
// Resolution of uuid → session name reuses the same "claudeDirToName"
// fallback already used for orphan UUID adoption in serve.Server.Run
// (see server.go for background).
package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// maxFilesLimit caps how many *.jsonl entries logsUsage will stat in a
// single request. Past this bound we return 507 Insufficient Storage
// with a hint so the UI can surface "too many log files — prune" rather
// than hang on an uncached dir walk.
const maxFilesLimit = 10_000

// UUIDNameResolver returns the human session name for a given log UUID.
// Implementations should encapsulate the uuidToName + claudeDirToName
// fallback lookup so the handler stays decoupled from ingest internals.
//
// ok=false signals "unknown UUID"; the handler falls back to a
// "uuid:<short>" placeholder identical to the orphan-adoption path.
type UUIDNameResolver interface {
	ResolveUUID(uuid string) (name string, ok bool)
}

// logsUsageFile is a single *.jsonl entry in the response.
type logsUsageFile struct {
	UUID    string `json:"uuid"`
	Session string `json:"session"`
	Bytes   int64  `json:"bytes"`
	// Mtime is RFC3339 in UTC for easy JS Date parsing. Empty string
	// when stat returned a zero time (shouldn't happen on a real fs).
	Mtime string `json:"mtime"`
}

// logsUsageResponse is the shape returned by GET /api/logs/usage.
type logsUsageResponse struct {
	Dir        string          `json:"dir"`
	TotalBytes int64           `json:"total_bytes"`
	Files      []logsUsageFile `json:"files"`
}

// LogsUsage returns the GET /api/logs/usage handler. logDir is the
// absolute path of the directory the tailer watches (normally
// ~/.config/ctm/logs). resolver maps log UUID → human session name;
// pass nil to disable resolution (every row falls back to the
// "uuid:<short>" placeholder).
func LogsUsage(logDir string, resolver UUIDNameResolver) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			w.Header().Set("Cache-Control", "no-store")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")

		entries, err := os.ReadDir(logDir)
		if err != nil {
			if os.IsNotExist(err) {
				// Directory doesn't exist yet (fresh install). Return
				// an empty but well-formed response rather than 5xx so
				// the UI can render "no logs yet" instead of an error.
				_ = json.NewEncoder(w).Encode(logsUsageResponse{
					Dir:   logDir,
					Files: []logsUsageFile{},
				})
				return
			}
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": "readdir_failed",
			})
			return
		}

		// Pre-count the *.jsonl entries so we can bail early with 507
		// without stat()-ing anything. A non-jsonl file in logDir is
		// unexpected but not fatal; ignore it entirely.
		jsonls := 0
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), jsonlExt) {
				jsonls++
			}
		}
		if jsonls > maxFilesLimit {
			w.WriteHeader(http.StatusInsufficientStorage)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error":     "too_many_log_files",
				"count":     jsonls,
				"limit":     maxFilesLimit,
				"hint":      "prune old *.jsonl files in " + logDir,
				"directory": logDir,
			})
			return
		}

		files := make([]logsUsageFile, 0, jsonls)
		var total int64
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), jsonlExt) {
				continue
			}
			uuid := strings.TrimSuffix(e.Name(), jsonlExt)
			full := filepath.Join(logDir, e.Name())
			info, err := os.Stat(full)
			if err != nil {
				// Race: the file was removed between ReadDir and Stat,
				// or we lost permission. Skip it silently rather than
				// failing the whole response.
				continue
			}
			name := resolveSessionName(resolver, uuid)
			mtime := ""
			if t := info.ModTime(); !t.IsZero() {
				mtime = t.UTC().Format(time.RFC3339)
			}
			files = append(files, logsUsageFile{
				UUID:    uuid,
				Session: name,
				Bytes:   info.Size(),
				Mtime:   mtime,
			})
			total += info.Size()
		}

		// Sort by bytes desc, then UUID asc for determinism on ties.
		sort.Slice(files, func(i, j int) bool {
			if files[i].Bytes != files[j].Bytes {
				return files[i].Bytes > files[j].Bytes
			}
			return files[i].UUID < files[j].UUID
		})

		_ = json.NewEncoder(w).Encode(logsUsageResponse{
			Dir:        logDir,
			TotalBytes: total,
			Files:      files,
		})
	}
}

// resolveSessionName asks the resolver for a human name; on miss or
// nil resolver it falls back to the same "uuid:<short>" placeholder
// that the orphan-adoption path in serve.Server.Run uses, so the UI
// sees a consistent identifier for unmapped UUIDs.
func resolveSessionName(resolver UUIDNameResolver, uuid string) string {
	if resolver != nil {
		if n, ok := resolver.ResolveUUID(uuid); ok && n != "" {
			return n
		}
	}
	short := uuid
	if len(short) > 8 {
		short = short[:8]
	}
	return "uuid:" + short
}
