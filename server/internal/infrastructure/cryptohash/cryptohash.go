// Package cryptohash provides shared hashing helpers used to fingerprint
// secrets at rest (api keys, enrollment codes, session tokens). Plaintext is
// returned to the caller exactly once; only the hex-encoded SHA-256 hash is
// persisted.
package cryptohash

import (
	"crypto/sha256"
	"encoding/hex"
)

// Sha256Hex returns the hex-encoded SHA-256 of s.
func Sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
