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
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/RandomCodeSpace/ctm/internal/config"
	"github.com/RandomCodeSpace/ctm/internal/serve/api"
	"github.com/RandomCodeSpace/ctm/internal/serve/attention"
	"github.com/RandomCodeSpace/ctm/internal/serve/auth"
	"github.com/RandomCodeSpace/ctm/internal/serve/events"
	"github.com/RandomCodeSpace/ctm/internal/serve/ingest"
	"github.com/RandomCodeSpace/ctm/internal/serve/store"
	"github.com/RandomCodeSpace/ctm/internal/serve/webhook"
	"github.com/RandomCodeSpace/ctm/internal/session"
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

	// Token, if non-empty, is pre-seeded into the in-memory session store
	// at startup so tests can authenticate without going through signup/
	// login. Production code leaves this empty.
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

	// runCancel, set by Run, cancels the root context driving all
	// background goroutines. Shutdown() triggers it so in-process
	// callers (e.g. PATCH /api/config) can bring the daemon down
	// without a signal.
	runCancel context.CancelFunc

	sessions   *auth.Store
	hub        *events.Hub
	proj       *ingest.Projection
	tailers    *ingest.TailerManager
	quota      *ingest.QuotaIngester
	cpCache    *api.CheckpointsCache
	attention  *attention.Engine
	webhook    *webhook.Dispatcher
	tmuxClient   *tmux.Client
	sessionStore *session.Store
	cost         store.CostStore
	logDir       string
}

// Shutdown cancels the daemon's root context so Run(ctx) returns and
// callers that want to respawn (e.g. config save → restart) can do so
// via proc.EnsureServeRunning. Safe to call more than once; no-op if
// Run hasn't started or has already returned.
func (s *Server) Shutdown(reason string) {
	slog.Info("ctm serve shutdown requested", "reason", reason)
	if s.runCancel != nil {
		s.runCancel()
	}
}

// New binds the listener, loads the bearer token, constructs the hub
// and sessions projection, and wires routes. See DefaultPort, single-
// instance guard semantics in the package doc.
func New(opts Options) (*Server, error) {
	if opts.Port == 0 {
		opts.Port = DefaultPort
	}

	sessions := auth.NewStore()
	if opts.Token != "" {
		// Test seam: pre-seed the session store so tests can authenticate
		// without going through the signup/login flow.
		sessions.Seed(opts.Token, "test")
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
	tmuxClient := tmux.NewClient(tmuxConf)
	sessionStore := session.NewStore(sessionsPath)

	// V13 cost store: persists per-session token/cost history so the
	// dashboard chart survives daemon restarts. WAL mode + batched tx
	// inserts keep the write path off the hub's hot loop.
	costDB, err := store.OpenCostStore(filepath.Join(config.Dir(), "ctm.db"))
	if err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("open cost db: %w", err)
	}

	proj := ingest.New(sessionsPath, tmuxClient)
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
		sessions:      sessions,
		hub:           hub,
		proj:          proj,
		tailers:       ingest.NewTailerManager(logDir, hub),
		quota:         quota,
		cpCache:       cpCache,
		attention:    attEngine,
		webhook:      disp,
		tmuxClient:   tmuxClient,
		sessionStore: sessionStore,
		cost:         costDB,
		logDir:       logDir,
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
	// Wrap the caller's ctx so Shutdown() can trigger the same
	// cascading-cancel path that a parent SIGINT would.
	ctx, runCancel := context.WithCancel(ctx)
	s.runCancel = runCancel
	defer runCancel()

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

	// V13 cost subscriber: writes per-session token/cost rows every
	// time the quota ingester publishes a fresh triple. The goroutine
	// exits when costCtx cancels; we wait on costDone in the shutdown
	// sequence so the final batch lands before the DB closes.
	costCtx, costCancel := context.WithCancel(ctx)
	defer costCancel()
	costDone := make(chan struct{})
	go func() {
		defer close(costDone)
		store.SubscribeQuotaWriter(costCtx, s.hub, s.cost, nil)
	}()

	// V19 slice 3 FTS subscriber: indexes every tool_call payload into
	// the SQLite FTS5 table. OpenCostStore wipes the index on boot, so
	// the tailer's offset-0 replay repopulates it cleanly.
	ftsCtx, ftsCancel := context.WithCancel(ctx)
	defer ftsCancel()
	ftsDone := make(chan struct{})
	go func() {
		defer close(ftsDone)
		if idx, ok := s.cost.(store.ToolCallIndexer); ok {
			store.SubscribeToolCallWriter(ftsCtx, s.hub, idx, nil)
		}
	}()

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
		costCancel()
		<-costDone
		ftsCancel()
		<-ftsDone
		_ = s.cost.Close()
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
		costCancel()
		<-costDone
		ftsCancel()
		<-ftsDone
		_ = s.cost.Close()
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
	// authHF wraps h so that every request carries a valid session
	// token (V27). Existing mux.Handle(..., authHF(h)) callsites
	// don't need changes.
	authHF := func(h http.HandlerFunc) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tok := api.BearerFromRequest(r)
			if tok == "" {
				writeJSONAuthErr(w, http.StatusUnauthorized, "missing_token")
				return
			}
			user, ok := s.sessions.Lookup(tok)
			if !ok {
				writeJSONAuthErr(w, http.StatusUnauthorized, "invalid_token")
				return
			}
			r = r.WithContext(auth.WithUser(r.Context(), user))
			h(w, r)
		})
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

	// V19 slice 3 (v0.3): FTS5-backed full-text search over indexed
	// tool_call content. Rebuilt on each boot from the tailer's
	// offset-0 replay; live rows appended via the tool_call hub
	// subscriber. Min query length bumps to 3 chars (trigram
	// tokenizer).
	mux.Handle("GET /api/search", authHF(api.Search(
		searchSourceAdapter{s.cost},
		sessionNameResolver{proj: s.proj},
	)))

	// V13 cumulative cost chart. Pulls from the SQLite cost store; adapter
	// below copies store.CostPoint → api.CostPoint to keep the api package
	// free of the store dependency.
	mux.Handle("GET /api/cost", authHF(api.Cost(costSourceAdapter{s.cost})))

	// V15/V16 — subagent tree + agent teams. Both replay the session's
	// JSONL to infer lifecycle (parseSubagentMeta → start; last-tool-
	// call-ts → stop/running; is_error → failed). Teams are a 2 s
	// dispatch-window heuristic until Claude Code emits explicit
	// team events.
	mux.Handle("GET /api/sessions/{name}/subagents",
		authHF(api.Subagents(s.logDir, logsUUIDResolver{proj: s.proj})))
	mux.Handle("GET /api/sessions/{name}/teams",
		authHF(api.Teams(s.logDir, logsUUIDResolver{proj: s.proj})))

	// V6 historical feed scroll. Returns tool_call rows older than a
	// cursor by reading backwards over the session's JSONL log, so the
	// UI's Load-older button can walk past the 500-slot hub ring.
	mux.Handle(
		"GET /api/sessions/{name}/feed/history",
		authHF(api.FeedHistory(s.logDir, logsUUIDResolver{proj: s.proj})),
	)

	// V9 inline Edit/Write diff viewer — looks up a single tool_call
	// row by its hub event ID and returns a rendered unified diff when
	// the tool is Edit/MultiEdit/Write.
	mux.Handle(
		"GET /api/sessions/{name}/tool_calls/{id}/detail",
		authHF(api.ToolCallDetail(api.NewJSONLLogReader(s.logDir, s.proj))),
	)

	// V24 live tmux pane capture. 1 Hz SSE of `tmux capture-pane -e -p`
	// output; frames are debounced on identical payloads so idle panes
	// stay quiet. The handler exits when the client disconnects.
	mux.Handle("GET /events/session/{name}/pane", authHF(api.PaneStream(s.tmuxClient)))

	// Settings drawer (webhook URL + attention thresholds). GET returns
	// the current config; PATCH applies a subset and triggers a daemon
	// restart so the new config takes effect on the next user action.
	mux.Handle("GET /api/config", authHF(api.ConfigGet(config.ConfigPath())))
	mux.Handle("PATCH /api/config", authHF(api.ConfigUpdate(config.ConfigPath(), s.Shutdown)))

	// V23 mutation endpoints: bearer + Origin-allowlist + type-to-confirm
	// (for destructive ones) per docs/v02/V23-mutation-auth.md (A+B+D).
	// Extra origins for reverse-proxy / tunnel deployments are sourced
	// from (a) CTM_ALLOWED_ORIGINS env var (comma-separated, useful for
	// one-off tests) and (b) ~/.config/ctm/allowed_origins file (one per
	// line, blank/`#` lines ignored — persists across reloads). The
	// loopback pair from DefaultAllowedOrigins is always included.
	allowedOrigins := api.DefaultAllowedOrigins(s.opts.Port)
	if extra := os.Getenv("CTM_ALLOWED_ORIGINS"); extra != "" {
		for _, o := range strings.Split(extra, ",") {
			if o = strings.TrimSpace(o); o != "" {
				allowedOrigins = append(allowedOrigins, o)
			}
		}
	}
	if raw, err := os.ReadFile(config.AllowedOriginsPath()); err == nil {
		for _, line := range strings.Split(string(raw), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			allowedOrigins = append(allowedOrigins, line)
		}
	}
	// V27 auth routes. /api/auth/status is intentionally unauthenticated
	// so the UI can probe it from any context. Signup/login are Origin-
	// gated to prevent CSRF. Logout requires a valid session.
	mux.Handle("GET /api/auth/status", api.AuthStatus(s.sessions))
	mux.Handle("POST /api/auth/signup", api.RequireOriginFunc(allowedOrigins, api.AuthSignup(s.sessions)))
	mux.Handle("POST /api/auth/login", api.RequireOriginFunc(allowedOrigins, api.AuthLogin(s.sessions)))
	mux.Handle("POST /api/auth/logout", authHF(api.AuthLogout(s.sessions)))

	mux.Handle("POST /api/sessions/{name}/kill",
		authHF(api.RequireOriginFunc(allowedOrigins, api.Kill(s.sessionStore, s.tmuxClient, s.proj))))
	mux.Handle("POST /api/sessions/{name}/forget",
		authHF(api.RequireOriginFunc(allowedOrigins, api.Forget(s.sessionStore, s.proj))))
	mux.Handle("POST /api/sessions/{name}/rename",
		authHF(api.RequireOriginFunc(allowedOrigins, api.Rename(s.sessionStore, s.tmuxClient, s.proj))))
	mux.Handle("GET /api/sessions/{name}/attach-url",
		authHF(api.RequireOriginFunc(allowedOrigins, api.AttachURL())))
	mux.Handle("POST /api/sessions/{name}/input",
		authHF(api.RequireOriginFunc(allowedOrigins,
			api.Input(inputSessionSource{proj: s.proj}, s.tmuxClient))))
	mux.Handle("POST /api/sessions",
		authHF(api.RequireOriginFunc(allowedOrigins,
			api.CreateSession(
				inputSessionSource{proj: s.proj},
				createSpawner{store: s.sessionStore, tmux: s.tmuxClient},
				execLookPath{}))))

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

func writeJSONAuthErr(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
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
// searchSourceAdapter implements api.SearchSource on top of
// store.SearchStore (sqliteCostStore satisfies both CostStore and
// SearchStore via the shared DB handle).
type searchSourceAdapter struct{ s store.CostStore }

func (a searchSourceAdapter) SearchFTS(q, sessionFilter string, limit int) ([]api.SearchHit, bool, error) {
	ss, ok := a.s.(store.SearchStore)
	if !ok {
		return nil, false, nil
	}
	hits, truncated, err := ss.SearchFTS(q, sessionFilter, limit)
	if err != nil {
		return nil, false, err
	}
	out := make([]api.SearchHit, len(hits))
	for i, h := range hits {
		out[i] = api.SearchHit(h)
	}
	return out, truncated, nil
}

// sessionNameResolver reverse-maps session name → log UUID for the
// V19 slice-3 search handler. logsUUIDResolver already does the
// forward direction; this type walks the projection's session list
// for the reverse (O(n), n ≤ ~100 in practice).
type sessionNameResolver struct{ proj *ingest.Projection }

func (r sessionNameResolver) ResolveSessionName(name string) (string, bool) {
	if r.proj == nil || name == "" {
		return "", false
	}
	sess, ok := r.proj.Get(name)
	if !ok {
		return "", false
	}
	return sess.UUID, sess.UUID != ""
}

// costSourceAdapter implements api.CostSource on top of store.CostStore.
// The structs have identical field shapes so the conversion is direct.
type costSourceAdapter struct{ s store.CostStore }

func (a costSourceAdapter) Range(session string, since, until time.Time) ([]api.CostPoint, error) {
	pts, err := a.s.Range(session, since, until)
	if err != nil {
		return nil, err
	}
	out := make([]api.CostPoint, len(pts))
	for i, p := range pts {
		out[i] = api.CostPoint(p)
	}
	return out, nil
}

func (a costSourceAdapter) Totals(since time.Time) (api.CostTotals, error) {
	t, err := a.s.Totals(since)
	if err != nil {
		return api.CostTotals{}, err
	}
	return api.CostTotals(t), nil
}

// createSpawner adapts session.Yolo to api.CreateSpawner.
type createSpawner struct {
	store *session.Store
	tmux  *tmux.Client
}

func (c createSpawner) Spawn(name, workdir string) (session.Session, error) {
	return session.Yolo(session.SpawnOpts{
		Name:    name,
		Workdir: workdir,
		Tmux:    c.tmux,
		Store:   c.store,
	})
}

// SendInitialPrompt fires `text` into the new session's pane after a
// short delay so claude has time to boot and show its prompt. Runs
// in a goroutine — fire-and-forget; errors are logged, not returned.
func (c createSpawner) SendInitialPrompt(name, text string) {
	go func() {
		time.Sleep(3 * time.Second)
		target := name + ":0.0"
		if err := c.tmux.SendKeys(target, text); err != nil {
			slog.Warn("initial prompt send failed", "session", name, "err", err.Error())
			return
		}
		if err := c.tmux.SendEnter(target); err != nil {
			slog.Warn("initial prompt enter failed", "session", name, "err", err.Error())
		}
	}()
}

// execLookPath is a tiny adapter so api.CreateLookPath can be
// satisfied by the free function os/exec.LookPath.
type execLookPath struct{}

func (execLookPath) LookPath(file string) (string, error) { return exec.LookPath(file) }

// inputSessionSource adapts *ingest.Projection to api.InputSessionSource.
// Both Get and TmuxAlive are implemented directly on *ingest.Projection.
type inputSessionSource struct{ proj *ingest.Projection }

func (a inputSessionSource) Get(name string) (session.Session, bool) {
	return a.proj.Get(name)
}

func (a inputSessionSource) TmuxAlive(name string) bool {
	return a.proj.TmuxAlive(name)
}

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
