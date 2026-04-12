package shell

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RandomCodeSpace/ctm/internal/session"
)

// MigrateFromCC reads sessions from old ~/.claude/cc-sessions/ directory
// and imports into ctm session store. Returns migrated names.
func MigrateFromCC(ccSessionsDir, sessionsPath string) ([]string, error) {
	entries, err := os.ReadDir(ccSessionsDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read cc sessions dir: %w", err)
	}

	store := session.NewStore(sessionsPath)

	var migrated []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		// Strip extension for session name
		sessionName := strings.TrimSuffix(name, filepath.Ext(name))
		if sessionName == "" {
			continue
		}

		// Read UUID from file
		data, err := os.ReadFile(filepath.Join(ccSessionsDir, name))
		if err != nil {
			continue
		}
		uuid := strings.TrimSpace(string(data))
		if uuid == "" {
			continue
		}

		// Skip if already in store
		if _, err := store.Get(sessionName); err == nil {
			continue
		}

		sess := &session.Session{
			Name:      sessionName,
			UUID:      uuid,
			Mode:      "safe",
			Workdir:   "",
			CreatedAt: time.Now().UTC(),
		}

		if err := store.Save(sess); err != nil {
			return migrated, fmt.Errorf("save session %q: %w", sessionName, err)
		}
		migrated = append(migrated, sessionName)
	}

	return migrated, nil
}
