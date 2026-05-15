package session

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/RandomCodeSpace/ctm/internal/jsonstrict"
	"github.com/RandomCodeSpace/ctm/internal/migrate"
)

// SchemaVersion is the current on-disk schema version of sessions.json.
// Bump this and append a Step to the Plan returned by MigrationPlan()
// whenever the shape of diskData or Session changes in a non-additive way.
//
// v3: codex is the only supported agent. Any v2 rows with agent="claude"
// are migrated to agent="codex" — the claude implementation has been
// removed and continuing to reference it would fail at agent.For lookup.
const SchemaVersion = 3

// DefaultAgent is the registry key used when a session row has no agent
// field set. Exposed as a constant so cmd/* code branching on the
// default doesn't drift from the migration / Save / NormalizeAgent
// codepaths.
const DefaultAgent = "codex"

// errFmtNotFound is the consistent shape returned by Get/Set/Delete/etc.
// when a session name is unknown. Callers that distinguish "not found"
// from other errors do so by string-matching this prefix; a typed
// sentinel would be a behaviour change.
const errFmtNotFound = "session %q not found"

// MigrationPlan returns the migrate.Plan for sessions.json.
//
//   - v0 → v1: stamp only (initial schema_version introduction).
//   - v1 → v2: backfill agent="claude" on rows missing the field
//     (historical — claude was the only agent at the time).
//   - v2 → v3: rewrite any agent="claude" rows to agent="codex" after
//     claude support was removed.
func MigrationPlan() migrate.Plan {
	return migrate.Plan{
		Name:           "sessions.json",
		CurrentVersion: SchemaVersion,
		Steps: []migrate.Step{
			nil,                 // v0 → v1: stamp only
			stampAgentClaude,    // v1 → v2: backfill agent
			rewriteClaudeToCodex, // v2 → v3: claude → codex
		},
	}
}

// stampAgentClaude walks obj["sessions"] and sets agent="claude" on
// rows missing the field. Idempotent — rows that already have an
// agent value are left untouched.
//
// obj["sessions"] is the JSON map keyed by session name, so the
// values are themselves objects. The step decodes lazily to keep
// per-row diffs minimal.
//
// Historical: at v2 claude was the only supported agent. The follow-on
// v2→v3 step (rewriteClaudeToCodex) rewrites the value once claude
// was removed.
func stampAgentClaude(obj map[string]json.RawMessage) error {
	raw, ok := obj["sessions"]
	if !ok || len(raw) == 0 {
		return nil
	}
	var byName map[string]map[string]json.RawMessage
	if err := json.Unmarshal(raw, &byName); err != nil {
		return fmt.Errorf("stampAgentClaude: parse sessions: %w", err)
	}
	for _, row := range byName {
		if _, present := row["agent"]; !present {
			row["agent"] = json.RawMessage(`"claude"`)
		}
	}
	out, err := json.Marshal(byName)
	if err != nil {
		return fmt.Errorf("stampAgentClaude: marshal sessions: %w", err)
	}
	obj["sessions"] = out
	return nil
}

// rewriteClaudeToCodex rewrites any agent="claude" row to agent="codex"
// during the v2 → v3 migration. The claude Agent implementation was
// removed; leaving the value as "claude" would surface as an
// agent.For miss at session resume time.
//
// Idempotent — rows already at "codex" or any other (future) agent
// pass through unchanged.
func rewriteClaudeToCodex(obj map[string]json.RawMessage) error {
	raw, ok := obj["sessions"]
	if !ok || len(raw) == 0 {
		return nil
	}
	var byName map[string]map[string]json.RawMessage
	if err := json.Unmarshal(raw, &byName); err != nil {
		return fmt.Errorf("rewriteClaudeToCodex: parse sessions: %w", err)
	}
	for _, row := range byName {
		if a, present := row["agent"]; present && string(a) == `"claude"` {
			row["agent"] = json.RawMessage(`"codex"`)
		}
	}
	out, err := json.Marshal(byName)
	if err != nil {
		return fmt.Errorf("rewriteClaudeToCodex: marshal sessions: %w", err)
	}
	obj["sessions"] = out
	return nil
}

var nameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,99}$`)
var sanitizeRe = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

// SanitizeName converts a string into a valid session name by replacing
// invalid characters with dashes and trimming.
func SanitizeName(name string) string {
	name = sanitizeRe.ReplaceAllString(name, "-")
	name = strings.TrimLeft(name, "-_")
	if name == "" {
		name = "session"
	}
	if len(name) > 100 {
		name = name[:100]
	}
	return name
}

// Session holds metadata for a managed tmux session.
type Session struct {
	Name             string    `json:"name"`
	UUID             string    `json:"uuid"`
	Mode             string    `json:"mode"`
	Workdir          string    `json:"workdir"`
	CreatedAt        time.Time `json:"created_at"`
	LastAttachedAt   time.Time `json:"last_attached_at,omitempty"`
	LastHealthStatus string    `json:"last_health_status,omitempty"`
	LastHealthAt     time.Time `json:"last_health_at,omitempty"`

	// Agent identifies the CLI driving this session. Set at creation
	// and never mutated. Empty value on read = legacy → normalized to
	// "claude" by Save and NormalizeAgent. Migration v1→v2 backfills
	// the on-disk value.
	Agent string `json:"agent,omitempty"`

	// AgentSessionID is the agent-backend session/thread identifier.
	// For claude this equals UUID (`claude --session-id <uuid>`). For
	// codex it is the thread UUID discovered post-spawn from the
	// rollout file; empty until the first discovery succeeds.
	AgentSessionID string `json:"agent_session_id,omitempty"`
}

// NormalizeAgent returns DefaultAgent ("codex") when s.Agent is empty,
// else s.Agent verbatim. Cheap idempotent guard used by read paths
// that handle pre-migration in-memory values without touching disk.
//
// Legacy "claude" values that escaped the v2→v3 migration are also
// remapped to "codex" so a stale in-memory Session never surfaces as
// an agent.For miss at the call site.
func (s *Session) NormalizeAgent() string {
	if s.Agent == "" || s.Agent == "claude" {
		return DefaultAgent
	}
	return s.Agent
}

// ValidateName returns an error if name is not a valid session name.
func ValidateName(name string) error {
	if name == "" {
		return fmt.Errorf("session name must not be empty")
	}
	if len(name) > 100 {
		return fmt.Errorf("session name must not exceed 100 characters")
	}
	if !nameRe.MatchString(name) {
		return fmt.Errorf("session name %q is invalid: must match ^[a-zA-Z0-9][a-zA-Z0-9_-]{0,99}$", name)
	}
	return nil
}

// New creates a new Session with a generated UUID and current timestamp.
func New(name, workdir, mode string) *Session {
	return &Session{
		Name:      name,
		UUID:      newUUIDv4(),
		Mode:      mode,
		Workdir:   workdir,
		CreatedAt: time.Now().UTC(),
	}
}

// diskData is the JSON structure persisted to disk.
type diskData struct {
	// SchemaVersion is stamped onto sessions.json by the migrate runner on
	// startup. save() force-sets it before every write so the file always
	// round-trips through the migrator cleanly.
	SchemaVersion int                 `json:"schema_version"`
	Sessions      map[string]*Session `json:"sessions"`
}

// Store manages session persistence via a JSON file with flock-based locking.
type Store struct {
	path string
}

// NewStore creates a Store for the given file path.
func NewStore(path string) *Store {
	return &Store{path: path}
}

func (s *Store) lock() (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return nil, fmt.Errorf("creating state dir: %w", err)
	}
	f, err := os.OpenFile(s.path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, fmt.Errorf("flock: %w", err)
	}
	return f, nil
}

func (s *Store) unlock(f *os.File) {
	syscall.Flock(int(f.Fd()), syscall.LOCK_UN) //nolint:errcheck
	f.Close()
}

func (s *Store) load() (*diskData, error) {
	var d diskData
	err := jsonstrict.Decode(s.path, &d)
	if os.IsNotExist(err) {
		return &diskData{Sessions: make(map[string]*Session)}, nil
	}
	if err != nil {
		// Malformed JSON or type mismatch. Back up the corrupt file and
		// start fresh rather than failing every subsequent Store op.
		// (Unknown-field errors do not reach this branch — jsonstrict
		// strips-and-rewrites and returns nil for that case; see
		// internal/jsonstrict for the mitigation contract.)
		data, _ := os.ReadFile(s.path) // best-effort for the copy fallback
		backupPath := fmt.Sprintf("%s.corrupt.%d", s.path, time.Now().UnixNano())
		if berr := os.Rename(s.path, backupPath); berr != nil {
			_ = os.WriteFile(backupPath, data, 0600)
			_ = os.Remove(s.path)
		}
		slog.Warn("sessions file was corrupt; backed up and starting fresh",
			"parse_error", err,
			"backup_path", backupPath,
			"source_path", s.path)
		return &diskData{Sessions: make(map[string]*Session)}, nil
	}
	if d.Sessions == nil {
		d.Sessions = make(map[string]*Session)
	}
	return &d, nil
}

func (s *Store) save(d *diskData) error {
	d.SchemaVersion = SchemaVersion
	data, err := json.Marshal(d)
	if err != nil {
		return fmt.Errorf("marshal sessions: %w", err)
	}
	tmpPath := s.path + ".tmp"
	// 0600: sessions.json contains session UUIDs, workdirs, and mode — not
	// secrets per se, but personal state that doesn't need to be world- or
	// group-readable even on shared hosts.
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return fmt.Errorf("write sessions tmp file: %w", err)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("atomic rename sessions file: %w", err)
	}
	return nil
}

// Backup copies sessions.json to sessions.json.bak.<timestamp> and returns the backup path.
// It acquires the store lock for the duration of the read.
func (s *Store) Backup() (string, error) {
	lf, err := s.lock()
	if err != nil {
		return "", err
	}
	defer s.unlock(lf)
	return s.backupLocked()
}

// backupLocked performs a backup assuming the lock is already held by the caller.
func (s *Store) backupLocked() (string, error) {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return "", nil // nothing to back up
	}
	if err != nil {
		return "", fmt.Errorf("read sessions for backup: %w", err)
	}
	backupPath := fmt.Sprintf("%s.bak.%d", s.path, time.Now().UnixNano())
	if err := os.WriteFile(backupPath, data, 0600); err != nil {
		return "", fmt.Errorf("write backup file: %w", err)
	}
	return backupPath, nil
}

// Save adds or updates a session. Empty sess.Agent is normalized to
// DefaultAgent ("codex"). Legacy "claude" values are also rewritten —
// the claude implementation was removed and a stray "claude" row
// would fail at spawn-time agent.For lookup.
func (s *Store) Save(sess *Session) error {
	if sess.Agent == "" || sess.Agent == "claude" {
		sess.Agent = DefaultAgent
	}

	lf, err := s.lock()
	if err != nil {
		return err
	}
	defer s.unlock(lf)

	d, err := s.load()
	if err != nil {
		return err
	}
	d.Sessions[sess.Name] = sess
	return s.save(d)
}

// Get retrieves a session by name, returning an error if not found.
func (s *Store) Get(name string) (*Session, error) {
	lf, err := s.lock()
	if err != nil {
		return nil, err
	}
	defer s.unlock(lf)

	d, err := s.load()
	if err != nil {
		return nil, err
	}
	sess, ok := d.Sessions[name]
	if !ok {
		return nil, fmt.Errorf(errFmtNotFound, name)
	}
	return sess, nil
}

// List returns all sessions.
func (s *Store) List() ([]*Session, error) {
	lf, err := s.lock()
	if err != nil {
		return nil, err
	}
	defer s.unlock(lf)

	d, err := s.load()
	if err != nil {
		return nil, err
	}
	list := make([]*Session, 0, len(d.Sessions))
	for _, sess := range d.Sessions {
		list = append(list, sess)
	}
	return list, nil
}

// Delete removes a session by name.
func (s *Store) Delete(name string) error {
	lf, err := s.lock()
	if err != nil {
		return err
	}
	defer s.unlock(lf)

	d, err := s.load()
	if err != nil {
		return err
	}
	if _, ok := d.Sessions[name]; !ok {
		return fmt.Errorf(errFmtNotFound, name)
	}
	delete(d.Sessions, name)
	return s.save(d)
}

// DeleteAll removes all sessions. It automatically creates a backup first so
// the user can recover from an accidental killall.
func (s *Store) DeleteAll() error {
	lf, err := s.lock()
	if err != nil {
		return err
	}
	defer s.unlock(lf)

	if backupPath, err := s.backupLocked(); err == nil && backupPath != "" {
		slog.Info("backed up sessions before DeleteAll",
			"backup_path", backupPath)
	}

	return s.save(&diskData{Sessions: make(map[string]*Session)})
}

// Rename renames a session from oldName to newName.
func (s *Store) Rename(oldName, newName string) error {
	if err := ValidateName(newName); err != nil {
		return err
	}

	lf, err := s.lock()
	if err != nil {
		return err
	}
	defer s.unlock(lf)

	d, err := s.load()
	if err != nil {
		return err
	}
	sess, ok := d.Sessions[oldName]
	if !ok {
		return fmt.Errorf(errFmtNotFound, oldName)
	}
	if _, exists := d.Sessions[newName]; exists {
		return fmt.Errorf("session %q already exists", newName)
	}
	sess.Name = newName
	d.Sessions[newName] = sess
	delete(d.Sessions, oldName)
	return s.save(d)
}

// UpdateMode changes the mode of a session.
func (s *Store) UpdateMode(name, mode string) error {
	lf, err := s.lock()
	if err != nil {
		return err
	}
	defer s.unlock(lf)

	d, err := s.load()
	if err != nil {
		return err
	}
	sess, ok := d.Sessions[name]
	if !ok {
		return fmt.Errorf(errFmtNotFound, name)
	}
	sess.Mode = mode
	return s.save(d)
}

// UpdateHealth updates the health status and timestamp of a session.
func (s *Store) UpdateHealth(name, status string) error {
	lf, err := s.lock()
	if err != nil {
		return err
	}
	defer s.unlock(lf)

	d, err := s.load()
	if err != nil {
		return err
	}
	sess, ok := d.Sessions[name]
	if !ok {
		return fmt.Errorf(errFmtNotFound, name)
	}
	sess.LastHealthStatus = status
	sess.LastHealthAt = time.Now().UTC()
	return s.save(d)
}

// UpdateAgentSessionID stamps the agent-backend thread/session
// identifier on the named session. Idempotent — supplying the same id
// twice is a no-op on disk apart from the rewrite. Returns the
// "not found" error if name has no store entry.
func (s *Store) UpdateAgentSessionID(name, id string) error {
	lf, err := s.lock()
	if err != nil {
		return err
	}
	defer s.unlock(lf)

	d, err := s.load()
	if err != nil {
		return err
	}
	sess, ok := d.Sessions[name]
	if !ok {
		return fmt.Errorf(errFmtNotFound, name)
	}
	if sess.AgentSessionID == id {
		return nil
	}
	sess.AgentSessionID = id
	return s.save(d)
}

// UpdateAttached updates the last attached timestamp of a session.
func (s *Store) UpdateAttached(name string) error {
	lf, err := s.lock()
	if err != nil {
		return err
	}
	defer s.unlock(lf)

	d, err := s.load()
	if err != nil {
		return err
	}
	sess, ok := d.Sessions[name]
	if !ok {
		return fmt.Errorf(errFmtNotFound, name)
	}
	sess.LastAttachedAt = time.Now().UTC()
	return s.save(d)
}

// Names returns all session names (useful for completions).
func (s *Store) Names() ([]string, error) {
	lf, err := s.lock()
	if err != nil {
		return nil, err
	}
	defer s.unlock(lf)

	d, err := s.load()
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(d.Sessions))
	for name := range d.Sessions {
		names = append(names, name)
	}
	return names, nil
}
