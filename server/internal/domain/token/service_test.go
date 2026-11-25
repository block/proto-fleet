package token_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/token"

	"github.com/alecthomas/assert/v2"
)

// Test: NewService should reject short secret key
func TestNewTokenService_InvalidSecret(t *testing.T) {
	shortKey := "short-key"
	invalidConfig := token.Config{
		ClientToken: token.AuthTokenConfig{
			SecretKey:        shortKey,
			ExpirationPeriod: time.Minute * 5,
		},
	}

	_, err := token.NewService(invalidConfig)
	assert.Error(t, err, "Expected error for short secret key")
	assert.Equal(t, fmt.Sprintf("FleetError: internal (Common: 0) secret key must be at least 32 bytes long: len=%d", len(shortKey)), err.Error(), "Error message should match expected")
}

// Test: NewService with valid secret key
func TestNewTokenService_ValidSecret(t *testing.T) {
	validConfig := token.Config{
		ClientToken: token.AuthTokenConfig{
			SecretKey:        "valid-secret-key-that-is-long-enough",
			ExpirationPeriod: time.Minute * 5,
		},
	}

	tokenService, err := token.NewService(validConfig)
	assert.NoError(t, err, "NewService should not return an error for valid secret key")
	assert.NotZero(t, tokenService, "Service instance should be created successfully")
}
