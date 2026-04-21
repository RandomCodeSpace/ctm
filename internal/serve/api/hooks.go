package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/RandomCodeSpace/ctm/internal/serve/events"
	"github.com/RandomCodeSpace/ctm/internal/serve/ingest"
)

// hookEvents enumerates the lifecycle event names that ctm's session-
// originating CLI commands POST to `/api/hooks/:event`. Anything else
// returns 404 to keep the surface area explicit.
var hookEvents = map[string]struct{}{
	"session_new":      {},
	"session_attached": {},
	"session_killed":   {},
	"on_yolo":          {},
}

// Hooks returns the handler for `POST /api/hooks/:event`. It accepts
// form-encoded payloads from `proc.PostEvent` (the in-process helper
// added in Step 7) co-located with each `fireHook` call site.
//
// The handler:
//
//   - Spawns a tailer on `session_new` (if `name` and `uuid` are present).
//   - Stops the tailer on `session_killed`.
//   - Republishes every event onto the hub so SSE clients see the
//     lifecycle in their feed.
func Hooks(mgr *ingest.TailerManager, hub *events.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		event := r.PathValue("event")
		if _, ok := hookEvents[event]; !ok {
			http.NotFound(w, r)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form payload", http.StatusBadRequest)
			return
		}

		name := r.PostForm.Get("name")
		uuid := r.PostForm.Get("uuid")
		mode := r.PostForm.Get("mode")
		workdir := r.PostForm.Get("workdir")

		switch event {
		case "session_new":
			if name != "" && uuid != "" {
				mgr.Start(r.Context(), name, uuid)
			}
		case "session_killed":
			if name != "" {
				mgr.Stop(name)
			}
		}

		body := map[string]any{}
		if name != "" {
			body["name"] = name
		}
		if uuid != "" {
			body["uuid"] = uuid
		}
		if mode != "" {
			body["mode"] = mode
		}
		if workdir != "" {
			body["workdir"] = workdir
		}
		if event == "session_attached" {
			body["at"] = time.Now().UTC().Format(time.RFC3339)
		}
		payload, _ := json.Marshal(body)

		hub.Publish(events.Event{
			Type:    event,
			Session: name,
			Payload: payload,
		})

		w.WriteHeader(http.StatusNoContent)
	}
}
