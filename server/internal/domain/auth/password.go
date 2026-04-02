package auth

import (
	"crypto/rand"
	"encoding/base64"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

const (
	// Generate 24 bytes which encodes to 32 base64 characters, then trim to desired length
	temporaryPasswordBytes  = 24
	temporaryPasswordLength = 32
)

// generateTemporaryPassword creates a cryptographically secure random password
// using URL-safe base64 encoding which provides a good mix of uppercase, lowercase,
// numbers, and special characters (-_) without needing a hardcoded charset
func generateTemporaryPassword() (string, error) {
	randomBytes := make([]byte, temporaryPasswordBytes)

	if _, err := rand.Read(randomBytes); err != nil {
		return "", fleeterror.NewInternalErrorf("failed to generate random password: %v", err)
	}

	// URLEncoding uses A-Z, a-z, 0-9, -, _ (no padding with RawURLEncoding)
	password := base64.RawURLEncoding.EncodeToString(randomBytes)

	// Trim to desired length
	if len(password) > temporaryPasswordLength {
		password = password[:temporaryPasswordLength]
	}

	return password, nil
}
