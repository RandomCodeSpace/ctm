// Package auth owns the ctm serve password hashing, user credentials
// file, and in-memory session store (V27 single-user auth).
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"

	"golang.org/x/crypto/argon2"
)

// Params holds the argon2id cost parameters. Defaults follow
// current OWASP guidance for modest-power servers.
type Params struct {
	M       uint32 `json:"m"`
	T       uint32 `json:"t"`
	P       uint8  `json:"p"`
	SaltLen uint32 `json:"salt_len"`
	HashLen uint32 `json:"hash_len"`
}

// DefaultParams is the canonical set of argon2id params used by Hash.
// Stored inside Encoded so a future bump does not invalidate old hashes.
var DefaultParams = Params{
	M:       64 * 1024, // 64 MiB
	T:       3,
	P:       2,
	SaltLen: 16,
	HashLen: 32,
}

// Encoded is the on-disk representation of a hashed password.
// Everything needed to verify a password against this hash is
// contained here; no external key material is required.
type Encoded struct {
	Algo    string `json:"algo"`
	Params  Params `json:"params"`
	SaltB64 string `json:"salt_b64"`
	HashB64 string `json:"hash_b64"`
}

// Hash returns an Encoded value derived from password using
// DefaultParams and a fresh random salt. Each call produces a
// different salt — hashing the same password twice yields two
// distinct Encoded values.
func Hash(password string) (Encoded, error) {
	p := DefaultParams
	salt := make([]byte, p.SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return Encoded{}, fmt.Errorf("auth: rand: %w", err)
	}
	h := argon2.IDKey([]byte(password), salt, p.T, p.M, p.P, p.HashLen)
	return Encoded{
		Algo:    "argon2id",
		Params:  p,
		SaltB64: base64.StdEncoding.EncodeToString(salt),
		HashB64: base64.StdEncoding.EncodeToString(h),
	}, nil
}

// Verify returns true iff password matches enc. Runs in constant
// time with respect to the hash comparison; salt/base64 decode
// errors are treated as a non-match (no panic). An empty password
// always returns false.
func Verify(enc Encoded, password string) bool {
	if password == "" {
		return false
	}
	if enc.Algo != "argon2id" || enc.Params.M == 0 {
		return false
	}
	salt, err := base64.StdEncoding.DecodeString(enc.SaltB64)
	if err != nil {
		return false
	}
	want, err := base64.StdEncoding.DecodeString(enc.HashB64)
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, enc.Params.T, enc.Params.M, enc.Params.P, enc.Params.HashLen)
	return subtle.ConstantTimeCompare(got, want) == 1
}
