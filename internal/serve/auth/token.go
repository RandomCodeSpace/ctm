// Package auth handles bearer-token issuance and validation for the
// ctm serve HTTP daemon. The token lives in ~/.config/ctm/serve.token
// (mode 0600) and is generated on first run by the bootstrap path.
package auth

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/RandomCodeSpace/ctm/internal/config"
)

// tokenBytes is the entropy of a freshly minted token. 32 bytes of
// crypto/rand → 43 url-safe base64 chars (no padding).
const tokenBytes = 32

// TokenPath returns the on-disk location of the serve bearer token.
func TokenPath() string {
	return filepath.Join(config.Dir(), "serve.token")
}

// EnsureToken returns the existing token at path or generates a new
// one. When the file is present its trimmed contents must be non-empty,
// otherwise the file is treated as corrupt and an error is returned —
// callers (bootstrap) can decide whether to abort or rotate.
//
// When the file is missing, 32 bytes of crypto/rand are encoded with
// base64.RawURLEncoding (no padding) and written with mode 0600. The
// parent directory is assumed to exist; surface the error if not.
func EnsureToken(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		tok := strings.TrimSpace(string(data))
		if tok == "" {
			return "", fmt.Errorf("serve token at %s is empty after trim", path)
		}
		return tok, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("reading serve token: %w", err)
	}

	buf := make([]byte, tokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generating serve token: %w", err)
	}
	tok := base64.RawURLEncoding.EncodeToString(buf)
	if err := os.WriteFile(path, []byte(tok), 0600); err != nil {
		return "", fmt.Errorf("writing serve token to %s: %w", path, err)
	}
	return tok, nil
}

// LoadToken is the read-only counterpart used by `ctm serve` startup.
// A missing file returns a wrapped error so callers can show the user
// the remediation (run any session-creating command to seed the token).
// Empty-after-trim is also rejected.
func LoadToken(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("serve token not found at %s: run `ctm` once to seed it: %w", path, err)
		}
		return "", fmt.Errorf("reading serve token: %w", err)
	}
	tok := strings.TrimSpace(string(data))
	if tok == "" {
		return "", fmt.Errorf("serve token at %s is empty after trim", path)
	}
	return tok, nil
}
