// Package serve runs the ctm web UI HTTP daemon (`ctm serve`).
//
// v0.1 scope: auth-gated REST + SSE endpoints on loopback:37778,
// backed by an in-memory hub, a read-through sessions projection
// over ~/.config/ctm/sessions.json, and per-workdir git checkpoint
// / revert handlers.
package serve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RandomCodeSpace/ctm/internal/config"
	"github.com/RandomCodeSpace/ctm/internal/serve/api"
	"github.com/RandomCodeSpace/ctm/internal/serve/attention"
	"github.com/RandomCodeSpace/ctm/internal/serve/auth"
	"github.com/RandomCodeSpace/ctm/internal/serve/events"
	"github.com/RandomCodeSpace/ctm/internal/serve/ingest"
	"github.com/RandomCodeSpace/ctm/internal/serve/webhook"
	"github.com/RandomCodeSpace/ctm/internal/tmux"
)

const (
	// DefaultPort is the loopback port ctm serve binds by default.
	DefaultPort = 37778

	// ServeVersionHeader is set on /healthz responses so a second
	// process can identify a live sibling daemon portably (no /proc).
	ServeVersionHeader = "X-Ctm-Serve"

	probeTimeout  = 200 * time.Millisecond
	shutdownGrace = 10 * time.Second
)

// ErrAlreadyRunning is returned by New when another `ctm serve` already
// owns the port (detected via the X-Ctm-Serve header on /healthz).
// Callers should treat it as silent success.
var ErrAlreadyRunning = errors.New("ctm serve already running on this port")

// Options configures a Server.
type Options struct {
	Port    int
	Version string

	// Token, if non-empty, is used for bearer auth directly; tests use
	// this seam to avoid touching ~/.config/ctm/serve.token. In
	// production leave it empty — serve loads auth.TokenPath().
	Token string

	// SessionsPath overrides the path serve watches for the sessions
	// projection. Empty means config.SessionsPath(). Primarily a test
	// seam.
	SessionsPath string

	// TmuxConfPath overrides the tmux client's conf path. Empty means
	// config.TmuxConfPath(). Primarily a test seam.
	TmuxConfPath string

	// LogDir overrides the directory the JSONL tailer manager watches.
	// Empty means filepath.Join(config.Dir(), "logs"). Test seam.
	LogDir string

	// StatuslineDumpDir overrides the directory the quota ingester
	// watches for `cmd statusline` per-session JSON dumps. Empty
	// means /tmp/ctm-statusline (per design spec §4 default).
	StatuslineDumpDir string

	// WebhookURL enables the webhook dispatcher. Empty → disabled.
	// HasWebhook in /api/bootstrap is derived from this.
	WebhookURL string

	// WebhookAuth, if non-empty, is sent verbatim in the Authorization
	// header on each POST (e.g. "Bearer abc123").
	WebhookAuth string

	// AttentionThresholds overrides the built-in defaults for the
	// attention engine's seven triggers. A zero-valued Thresholds falls
	// back to attention.Defaults().
	AttentionThresholds attention.Thresholds

	// Config is the already-loaded user config, threaded through so
	// the /api/doctor handler can report on required_env /
	// required_in_path without re-reading from disk. Zero value is
	// safe — the doctor runner treats unset fields as "not
	// configured" rather than failing.
	Config config.Config
}

// Server is the ctm serve HTTP daemon.
type Server struct {
	opts      Options
	listener  net.Listener
	http      *http.Server
	startedAt time.Time

	// requestCtx is the parent context every incoming HTTP request
	// inherits via http.Server.BaseContext. Cancelling it on shutdown
	// kicks long-lived SSE handlers out of their `<-r.Context().Done()`
	// select so http.Shutdown's grace deadline can complete cleanly.
	requestCtx    context.Context
	requestCancel context.CancelFunc

	token     string
	hub       *events.Hub
	proj      *ingest.Projection
	tailers   *ingest.TailerManager
	quota     *ingest.QuotaIngester
	cpCache   *api.CheckpointsCache
	attention *attention.Engine
	webhook   *webhook.Dispatcher
	logDir    string
}

// New binds the listener, loads the bearer token, constructs the hub
// and sessions projection, and wires routes. See DefaultPort, single-
// instance guard semantics in the package doc.
func New(opts Options) (*Server, error) {
	if opts.Port == 0 {
		opts.Port = DefaultPort
	}

	token := opts.Token
	if token == "" {
		t, err := auth.LoadToken(auth.TokenPath())
		if err != nil {
			return nil, fmt.Errorf(
				"load serve token: %w (expected at %s; run `ctm doctor` to recreate)",
				err, auth.TokenPath())
		}
		token = t
	}

	addr := fmt.Sprintf("127.0.0.1:%d", opts.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		if isAddrInUse(err) {
			if probeIsCtmServe(addr) {
				return nil, ErrAlreadyRunning
			}
			return nil, fmt.Errorf("port %d in use by a non-ctm-serve listener; refusing to bind", opts.Port)
		}
		return nil, fmt.Errorf("bind %s: %w", addr, err)
	}

	sessionsPath := opts.SessionsPath
	if sessionsPath == "" {
		sessionsPath = config.SessionsPath()
	}
	tmuxConf := opts.TmuxConfPath
	if tmuxConf == "" {
		tmuxConf = config.TmuxConfPath()
	}
	logDir := opts.LogDir
	if logDir == "" {
		logDir = filepath.Join(config.Dir(), "logs")
	}
	dumpDir := opts.StatuslineDumpDir
	if dumpDir == "" {
		dumpDir = "/tmp/ctm-statusline"
	}

	hub := events.NewHub(0)
	proj := ingest.New(sessionsPath, tmux.NewClient(tmuxConf))
	quota := ingest.NewQuotaIngester(dumpDir, proj, hub)
	cpCache := api.NewCheckpointsCache()

	// Attention engine: subscribes to hub, evaluates the seven triggers,
	// re-publishes attention_raised/_cleared on transitions. Wired into
	// quotaEnricher.Attention so /api/sessions surfaces the current
	// alert. Runs for the full daemon lifetime.
	thr := opts.AttentionThresholds
	if thr == (attention.Thresholds{}) {
		thr = attention.Defaults()
	}
	attEngine := attention.NewEngine(
		hub,
		quota,
		sessionSourceAdapter{proj: proj, cpCache: cpCache},
		thr,
		nil,
	)

	// Webhook dispatcher: POSTs attention_raised events to a user-
	// configured URL with 3× exponential retry and 60 s debounce per
	// (session, alert). URL empty → Run returns immediately without
	// subscribing (dispatcher disabled).
	disp := webhook.NewDispatcher(
		hub,
		sessionResolverAdapter{proj: proj},
		webhook.Config{
			URL:        opts.WebhookURL,
			AuthHeader: opts.WebhookAuth,
			UIBaseURL:  fmt.Sprintf("http://127.0.0.1:%d", opts.Port),
		},
		nil,
	)

	reqCtx, reqCancel := context.WithCancel(context.Background())
	s := &Server{
		opts:          opts,
		listener:      ln,
		startedAt:     time.Now(),
		requestCtx:    reqCtx,
		requestCancel: reqCancel,
		token:         token,
		hub:           hub,
		proj:          proj,
		tailers:       ingest.NewTailerManager(logDir, hub),
		quota:         quota,
		cpCache:       cpCache,
		attention:     attEngine,
		webhook:       disp,
		logDir:        logDir,
	}

	mux := http.NewServeMux()
	s.registerRoutes(mux)

	s.http = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return reqCtx },
	}
	return s, nil
}

// Addr returns the bound listener address.
func (s *Server) Addr() string { return s.listener.Addr().String() }

// Hub returns the server's event hub. Exposed for tests and, in later
// steps, for ingest layers that publish events from the same process.
func (s *Server) Hub() *events.Hub { return s.hub }

// Run blocks until ctx is cancelled, then gracefully shuts down.
// Returns nil on clean shutdown; propagates non-ErrServerClosed errors.
func (s *Server) Run(ctx context.Context) error {
	// Synchronous initial projection load before the polling goroutine
	// starts; the tailer-spawn loop below depends on All() being
	// populated, otherwise it sees an empty list and no tailers fire
	// for sessions that were already running when serve booted.
	s.proj.Reload()

	projCtx, projCancel := context.WithCancel(ctx)
	defer projCancel()
	projDone := make(chan error, 1)
	go func() { projDone <- s.proj.Run(projCtx) }()

	quotaCtx, quotaCancel := context.WithCancel(ctx)
	defer quotaCancel()
	quotaDone := make(chan error, 1)
	go func() { quotaDone <- s.quota.Run(quotaCtx) }()

	attCtx, attCancel := context.WithCancel(ctx)
	defer attCancel()
	attDone := make(chan error, 1)
	go func() { attDone <- s.attention.Run(attCtx) }()

	whCtx, whCancel := context.WithCancel(ctx)
	defer whCancel()
	whDone := make(chan error, 1)
	go func() { whDone <- s.webhook.Run(whCtx) }()

	// Tailer adoption: scan the JSONL log directory and spawn a tailer
	// for every UUID we find a log file for. The log files are the
	// ground truth (claude writes them via the log-tool-use hook
	// regardless of what sessions.json says); the sessions projection
	// is just metadata. Resolving UUID → human session name from the
	// projection is best-effort — when no match exists we use a
	// "uuid:<short>" placeholder so the UI still surfaces the activity
	// rather than silently dropping it.
	tailerCtx, tailerCancel := context.WithCancel(ctx)
	defer tailerCancel()
	uuidToName := make(map[string]string, len(s.proj.All()))
	// claudeDirToName maps Claude's project-directory naming convention
	// (`/home/dev/projects/ctm` → `-home-dev-projects-ctm`) back to a
	// session name, so orphan UUIDs from previous claude sessions that
	// ran in the same workdir still get routed to the right session's
	// ring. Without this, each new claude session for the same tmux
	// session starts a fresh UUID whose prior transcripts disappear
	// into `uuid:<short>` rings the UI never surfaces.
	claudeDirToName := make(map[string]string, len(s.proj.All()))
	for _, sess := range s.proj.All() {
		if sess.UUID != "" {
			uuidToName[sess.UUID] = sess.Name
		}
		if sess.Workdir != "" {
			claudeDirToName[strings.ReplaceAll(sess.Workdir, "/", "-")] = sess.Name
		}
	}
	claudeProjectsRoot := ""
	if home, err := os.UserHomeDir(); err == nil {
		claudeProjectsRoot = filepath.Join(home, ".claude", "projects")
	}
	adopted := 0
	adoptedViaWorkdir := 0
	orphans := 0
	if entries, err := os.ReadDir(s.logDir); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
				continue
			}
			uuid := strings.TrimSuffix(e.Name(), ".jsonl")
			name, ok := uuidToName[uuid]
			if !ok && claudeProjectsRoot != "" {
				// Fall back: walk `~/.claude/projects/*/` for a
				// transcript with this UUID; its parent directory
				// name encodes the workdir.
				if matches, _ := filepath.Glob(filepath.Join(claudeProjectsRoot, "*", uuid+".jsonl")); len(matches) == 1 {
					if mapped, ok2 := claudeDirToName[filepath.Base(filepath.Dir(matches[0]))]; ok2 {
						name = mapped
						ok = true
						adoptedViaWorkdir++
					}
				}
			}
			if !ok {
				short := uuid
				if len(short) > 8 {
					short = short[:8]
				}
				name = "uuid:" + short
				orphans++
			}
			s.tailers.Start(tailerCtx, name, uuid)
			adopted++
		}
	}
	slog.Info("ctm serve tailers started",
		"sessions_in_projection", len(s.proj.All()),
		"tailers_started", adopted,
		"adopted_via_workdir", adoptedViaWorkdir,
		"orphan_uuids", orphans)

	serveDone := make(chan error, 1)
	go func() { serveDone <- s.http.Serve(s.listener) }()

	slog.Info("ctm serve started",
		"addr", s.listener.Addr().String(),
		"version", s.opts.Version)

	select {
	case err := <-serveDone:
		projCancel()
		<-projDone
		quotaCancel()
		<-quotaDone
		attCancel()
		<-attDone
		whCancel()
		<-whDone
		s.tailers.StopAll()
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		slog.Info("ctm serve shutting down")
		// Cancel BaseContext first so SSE handlers' r.Context().Done()
		// fires; otherwise their long-lived goroutines keep the
		// connection in StateActive and Shutdown blocks until the
		// grace deadline.
		s.requestCancel()
		shutCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer cancel()
		shutErr := s.http.Shutdown(shutCtx)
		projCancel()
		<-projDone
		quotaCancel()
		<-quotaDone
		attCancel()
		<-attDone
		whCancel()
		<-whDone
		s.tailers.StopAll()
		err := <-serveDone
		if shutErr != nil {
			return shutErr
		}
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func (s *Server) registerRoutes(mux *http.ServeMux) {
	authHF := func(h http.HandlerFunc) http.Handler {
		return auth.Required(s.token, h)
	}

	// Unauthenticated: liveness only. Registered without a method
	// prefix so the handler's own method check (returns 405 for non-
	// GET/HEAD) sees the request rather than letting non-GET fall
	// through to the / catch-all and accidentally serve 200 HTML.
	mux.HandleFunc("/healthz", api.Healthz(s.opts.Version, ServeVersionHeader, s.startedAt))

	// Authenticated JSON endpoints.
	mux.Handle("GET /health", authHF(api.Health(s.opts.Version, ServeVersionHeader, s.startedAt, hubStatsAdapter{s.hub})))
	mux.Handle("GET /api/bootstrap", authHF(api.Bootstrap(s.opts.Version, s.opts.Port, s.opts.WebhookURL != "")))
	// Diagnostics (V20) — mirrors `ctm doctor` CLI over JSON. Handler
	// enforces its own 5 s timeout on the runner so a pathological box
	// can't hang the response.
	mux.Handle("GET /api/doctor", authHF(api.Doctor(api.DefaultDoctorRunner(s.opts.Config))))

	enricher := quotaEnricher{quota: s.quota, attention: s.attention}
	mux.Handle("GET /api/sessions", authHF(api.List(s.proj, enricher)))
	mux.Handle("GET /api/sessions/{name}", authHF(api.Get(s.proj, enricher)))
	// Quota REST fallback so global rate-limit bars render on first
	// paint without waiting for the next SSE-delivered value change
	// (hub.Subscribe returns no replay when Last-Event-ID is empty).
	mux.Handle("GET /api/quota", authHF(api.Quota(quotaSourceAdapter{s.quota})))

	resolveWorkdir := func(name string) (string, bool) {
		sess, ok := s.proj.Get(name)
		if !ok {
			return "", false
		}
		return sess.Workdir, true
	}
	// Shared 5 s checkpoint cache: the /checkpoints handler and the
	// revert SHA-allowlist check both read through the same cache,
	// preventing a rapid-revert client from spinning up unbounded
	// `git log` subprocesses. Also consumed by the attention engine
	// (trigger G yolo_unchecked).
	allowedSHA := func(name, sha string) bool {
		wd, ok := resolveWorkdir(name)
		if !ok {
			return false
		}
		// Full SHA equality only; abbreviated SHAs are intentionally
		// rejected (see api.CheckpointsCache.IsCheckpoint comment).
		return s.cpCache.IsCheckpoint(wd, name, sha)
	}
	mux.Handle("GET /api/sessions/{name}/checkpoints", authHF(api.Checkpoints(resolveWorkdir, s.cpCache)))
	// V18 standalone diff viewer: unified diff for a single checkpoint
	// SHA, guarded by the same 5 s cache + full-SHA allowlist as
	// /revert. Response is text/plain so the UI can render it in a
	// <pre> without JSON-envelope overhead.
	mux.Handle("GET /api/sessions/{name}/checkpoints/{sha}/diff", authHF(api.Diff(resolveWorkdir, s.cpCache)))
	mux.Handle("POST /api/sessions/{name}/revert", authHF(api.Revert(resolveWorkdir, allowedSHA)))
	// Feed REST seed — parallel to /api/quota. Global ring and per-
	// session variant; both emit tool_call payloads only. See
	// api/feed.go for shape.
	mux.Handle("GET /api/feed", authHF(api.Feed(s.hub, "")))
	mux.Handle("GET /api/sessions/{name}/feed", authHF(api.Feed(s.hub, "")))

	// Hook intake from `proc.PostEvent` (Step 7); spawns / stops
	// tailers as a side-effect of session_new / session_killed.
	mux.Handle("POST /api/hooks/{event}", authHF(api.Hooks(s.tailers, s.hub)))

	// V21 log disk usage. Walks the JSONL log dir and reports bytes per
	// session + total so users can notice when it's time to prune.
	// Read-only — no deletion verbs on this endpoint.
	mux.Handle("GET /api/logs/usage", authHF(api.LogsUsage(s.logDir, logsUUIDResolver{proj: s.proj})))

	// Debug: hub counters + subscriber count. Gated on auth; useful
	// from curl to check whether publishes are flowing and whether
	// the browser is actually subscribed.
	mux.Handle("GET /debug/hub", authHF(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(s.hub.Stats())
	}))

	// SSE.
	mux.Handle("GET /events/all", authHF(events.Handler(s.hub, "")))
	mux.Handle("GET /events/session/{name}", authHF(func(w http.ResponseWriter, r *http.Request) {
		events.Handler(s.hub, r.PathValue("name"))(w, r)
	}))

	// Placeholder UI at / ; returns 404 for unknown /api/ and /events/
	// paths so future routes can claim those prefixes cleanly.
	mux.Handle("/", assetHandler())
}

// hubStatsAdapter exposes the events.Hub's Stats() to api.Health
// without forcing api/health.go to import internal/serve/events.
type hubStatsAdapter struct{ hub *events.Hub }

func (a hubStatsAdapter) Stats() any { return a.hub.Stats() }

// quotaEnricher adapts the QuotaIngester (in `internal/serve/ingest`)
// and the attention engine to the api.SessionEnricher interface so
// per-session `context_pct`, tokens, and current attention alert show
// up on /api/sessions responses without coupling the api package to
// ingest or attention internals.
type quotaEnricher struct {
	quota     *ingest.QuotaIngester
	attention *attention.Engine
}

func (e quotaEnricher) ContextPct(name string) (int, bool) {
	if e.quota == nil {
		return 0, false
	}
	return e.quota.ContextPct(name)
}

func (e quotaEnricher) LastToolCallAt(name string) (time.Time, bool) {
	if e.attention == nil {
		return time.Time{}, false
	}
	return e.attention.LastToolCallAt(name)
}

func (e quotaEnricher) Attention(name string) (api.Attention, bool) {
	if e.attention == nil {
		return api.Attention{}, false
	}
	snap, ok := e.attention.Snapshot(name)
	if !ok {
		return api.Attention{}, false
	}
	return api.Attention{
		State:   snap.State,
		Since:   snap.Since,
		Details: snap.Details,
	}, true
}

func (e quotaEnricher) Tokens(name string) (api.TokenUsage, bool) {
	if e.quota == nil {
		return api.TokenUsage{}, false
	}
	s, ok := e.quota.PerSessionSnapshot(name)
	if !ok {
		return api.TokenUsage{}, false
	}
	return api.TokenUsage{
		InputTokens:  s.InputTokens,
		OutputTokens: s.OutputTokens,
		CacheTokens:  s.CacheTokens,
	}, true
}

// sessionSourceAdapter satisfies attention.SessionSource by reading
// through the live projection and the checkpoint cache. Projection
// already owns its own tmux client for TmuxAlive; checkpoint freshness
// (for trigger G) comes from a bounded-limit cache Get that avoids
// unbounded `git log` calls per tick.
type sessionSourceAdapter struct {
	proj    *ingest.Projection
	cpCache *api.CheckpointsCache
}

func (a sessionSourceAdapter) Names() []string {
	all := a.proj.All()
	out := make([]string, 0, len(all))
	for _, s := range all {
		out = append(out, s.Name)
	}
	return out
}

func (a sessionSourceAdapter) Mode(name string) string {
	s, ok := a.proj.Get(name)
	if !ok {
		return ""
	}
	return s.Mode
}

func (a sessionSourceAdapter) TmuxAlive(name string) bool {
	return a.proj.TmuxAlive(name)
}

func (a sessionSourceAdapter) LastCheckpointAt(name string) (time.Time, bool) {
	s, ok := a.proj.Get(name)
	if !ok || s.Workdir == "" {
		return time.Time{}, false
	}
	cps, err := a.cpCache.Get(s.Workdir, name, 1)
	if err != nil || len(cps) == 0 {
		return time.Time{}, false
	}
	t, perr := time.Parse(time.RFC3339, cps[0].TS)
	if perr != nil {
		return time.Time{}, false
	}
	return t, true
}

// sessionResolverAdapter satisfies webhook.SessionResolver so webhook
// payloads carry session_uuid / workdir / mode alongside the alert.
type sessionResolverAdapter struct{ proj *ingest.Projection }

func (a sessionResolverAdapter) Resolve(name string) (uuid, workdir, mode string, ok bool) {
	s, found := a.proj.Get(name)
	if !found {
		return "", "", "", false
	}
	return s.UUID, s.Workdir, s.Mode, true
}

// quotaSourceAdapter wraps the ingester's GlobalSnapshot return into
// the api-package's private quotaSnapshot, keeping the ingest →
// api dependency one-way (api stays ignorant of ingest internals).
type quotaSourceAdapter struct{ quota *ingest.QuotaIngester }

func (a quotaSourceAdapter) Snapshot() api.QuotaSnapshot {
	if a.quota == nil {
		return api.QuotaSnapshot{}
	}
	s := a.quota.Snapshot()
	return api.QuotaSnapshot{
		WeeklyPct:       s.WeeklyPct,
		FiveHourPct:     s.FiveHourPct,
		WeeklyResetsAt:  s.WeeklyResetsAt,
		FiveHourResetAt: s.FiveHourResetAt,
		Known:           s.Known,
	}
}

// logsUUIDResolver maps a log-file UUID to a human session name for
// /api/logs/usage. It mirrors the orphan-adoption lookup done in
// Server.Run: direct UUID match first, then the claudeDirToName fall-
// back via ~/.claude/projects/*/<uuid>.jsonl so transcripts from
// previous claude sessions in the same workdir still surface with
// their tmux session name.
type logsUUIDResolver struct{ proj *ingest.Projection }

func (r logsUUIDResolver) ResolveUUID(uuid string) (string, bool) {
	if r.proj == nil || uuid == "" {
		return "", false
	}
	// Build the direct + workdir maps on every call. Projection.All()
	// is RWMutex-guarded and copies defensively; the caller (handler)
	// only fires on user-triggered refresh (TanStack 30 s staleTime),
	// so the cost is negligible and we avoid stale caching for
	// sessions that have been renamed.
	all := r.proj.All()
	for _, sess := range all {
		if sess.UUID == uuid {
			return sess.Name, true
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false
	}
	matches, _ := filepath.Glob(filepath.Join(home, ".claude", "projects", "*", uuid+".jsonl"))
	if len(matches) != 1 {
		return "", false
	}
	dirName := filepath.Base(filepath.Dir(matches[0]))
	for _, sess := range all {
		if sess.Workdir != "" && strings.ReplaceAll(sess.Workdir, "/", "-") == dirName {
			return sess.Name, true
		}
	}
	return "", false
}
