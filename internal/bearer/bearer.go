// Package bearer provides the shared Authorization header check used by the
// HTTP handlers.
package bearer

import (
	"crypto/sha256"
	"crypto/subtle"
	"strings"
)

// Matches reports whether the Authorization header value is
// "Bearer <token>" with the expected token. Both sides are SHA-256 hashed
// before a constant-time compare so neither content nor length of the
// expected token leaks through timing (ConstantTimeCompare returns early
// on a length mismatch).
func Matches(auth, token string) bool {
	if token == "" {
		return false // never fail open on an unset expected token
	}
	if !strings.HasPrefix(auth, "Bearer ") {
		return false
	}
	got := sha256.Sum256([]byte(strings.TrimPrefix(auth, "Bearer ")))
	want := sha256.Sum256([]byte(token))
	return subtle.ConstantTimeCompare(got[:], want[:]) == 1
}
