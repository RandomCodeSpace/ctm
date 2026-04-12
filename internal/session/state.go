package session

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

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
	Sessions map[string]*Session `json:"sessions"`
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
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return &diskData{Sessions: make(map[string]*Session)}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read sessions file: %w", err)
	}
	var d diskData
	if err := json.Unmarshal(data, &d); err != nil {
		// Back up the corrupt file and start fresh rather than failing all ops.
		backupPath := fmt.Sprintf("%s.corrupt.%d", s.path, time.Now().UnixNano())
		if berr := os.Rename(s.path, backupPath); berr != nil {
			// If rename fails, try a copy.
			_ = os.WriteFile(backupPath, data, 0644)
			_ = os.Remove(s.path)
		}
		log.Printf("warning: sessions file was corrupt (JSON parse error: %v); backed up to %s and starting fresh", err, backupPath)
		return &diskData{Sessions: make(map[string]*Session)}, nil
	}
	if d.Sessions == nil {
		d.Sessions = make(map[string]*Session)
	}
	return &d, nil
}

func (s *Store) save(d *diskData) error {
	data, err := json.Marshal(d)
	if err != nil {
		return fmt.Errorf("marshal sessions: %w", err)
	}
	tmpPath := s.path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
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
	if err := os.WriteFile(backupPath, data, 0644); err != nil {
		return "", fmt.Errorf("write backup file: %w", err)
	}
	return backupPath, nil
}

// Save adds or updates a session.
func (s *Store) Save(sess *Session) error {
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
		return nil, fmt.Errorf("session %q not found", name)
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
		return fmt.Errorf("session %q not found", name)
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
		log.Printf("info: backed up sessions to %s before DeleteAll", backupPath)
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
		return fmt.Errorf("session %q not found", oldName)
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
		return fmt.Errorf("session %q not found", name)
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
		return fmt.Errorf("session %q not found", name)
	}
	sess.LastHealthStatus = status
	sess.LastHealthAt = time.Now().UTC()
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
		return fmt.Errorf("session %q not found", name)
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
