package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/RandomCodeSpace/ctm/internal/config"
)

// User is the single-user record persisted at userPath().
type User struct {
	Username  string    `json:"username"`
	Password  Encoded   `json:"-"`
	CreatedAt time.Time `json:"created_at"`
}

// userPersisted is the on-disk shape — Encoded fields flattened
// into the top level for readability.
type userPersisted struct {
	Username  string    `json:"username"`
	Algo      string    `json:"algo"`
	Params    Params    `json:"params"`
	SaltB64   string    `json:"salt_b64"`
	HashB64   string    `json:"hash_b64"`
	CreatedAt time.Time `json:"created_at"`
}

// UserPath returns the absolute path to user.json.
func UserPath() string {
	return filepath.Join(config.Dir(), "user.json")
}

// Exists reports whether user.json is present.
func Exists() bool {
	_, err := os.Stat(UserPath())
	return err == nil
}

// Save writes u to UserPath() atomically (tmp-file + rename) with
// 0600 perms, creating the config directory if needed.
func Save(u User) error {
	if err := os.MkdirAll(config.Dir(), 0o700); err != nil {
		return fmt.Errorf("auth: mkdir: %w", err)
	}
	if u.CreatedAt.IsZero() {
		u.CreatedAt = time.Now().UTC()
	}
	persisted := userPersisted{
		Username:  u.Username,
		Algo:      u.Password.Algo,
		Params:    u.Password.Params,
		SaltB64:   u.Password.SaltB64,
		HashB64:   u.Password.HashB64,
		CreatedAt: u.CreatedAt,
	}
	blob, err := json.MarshalIndent(persisted, "", "  ")
	if err != nil {
		return fmt.Errorf("auth: marshal: %w", err)
	}
	tmp := UserPath() + ".tmp"
	if err := os.WriteFile(tmp, blob, 0o600); err != nil {
		return fmt.Errorf("auth: write tmp: %w", err)
	}
	if err := os.Rename(tmp, UserPath()); err != nil {
		return fmt.Errorf("auth: rename: %w", err)
	}
	return nil
}

// Load reads + parses user.json. Returns fs.ErrNotExist if the file
// is absent.
func Load() (User, error) {
	blob, err := os.ReadFile(UserPath())
	if err != nil {
		return User{}, err
	}
	var p userPersisted
	if err := json.Unmarshal(blob, &p); err != nil {
		return User{}, fmt.Errorf("auth: unmarshal: %w", err)
	}
	return User{
		Username: p.Username,
		Password: Encoded{
			Algo:    p.Algo,
			Params:  p.Params,
			SaltB64: p.SaltB64,
			HashB64: p.HashB64,
		},
		CreatedAt: p.CreatedAt,
	}, nil
}

// Delete removes user.json. Returns nil if the file was already
// absent (idempotent by design so ctm auth reset can be run twice
// without confusion).
func Delete() error {
	err := os.Remove(UserPath())
	if err == nil {
		return nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return fmt.Errorf("auth: delete: %w", err)
}
