package auth

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"unicode/utf8"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

const (
	// Generate 24 bytes which encodes to 32 base64 characters, then trim to desired length
	temporaryPasswordBytes  = 24
	temporaryPasswordLength = 32
	minimumPasswordLength   = 8
	maximumPasswordBytes    = 72
)

// ValidatePassword applies the server-side policy shared by onboarding,
// authenticated password changes, and break-glass recovery. The byte limit is
// bcrypt's maximum input length.
func ValidatePassword(password string) error {
	if utf8.RuneCountInString(password) < minimumPasswordLength {
		return fmt.Errorf("password must be at least %d characters", minimumPasswordLength)
	}
	if len([]byte(password)) > maximumPasswordBytes {
		return fmt.Errorf("password must be at most %d bytes", maximumPasswordBytes)
	}
	return nil
}

// GenerateTemporaryPassword creates a cryptographically secure random password
// using URL-safe base64 encoding which provides a good mix of uppercase, lowercase,
// numbers, and special characters (-_) without needing a hardcoded charset
func GenerateTemporaryPassword() (string, error) {
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
