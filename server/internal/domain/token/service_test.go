package token_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/token"

	"github.com/alecthomas/assert/v2"
)

var testConfig = token.Config{
	ClientToken: token.AuthTokenConfig{
		SecretKey:        "test-secret-key-that-is-long-enough", // Ensure valid length for testing
		ExpirationPeriod: time.Minute * 5,                       // Short expiration for testing
	},
	MinerTokenExpirationPeriod: time.Minute * 5,
}

var userID int64 = 12345
var orgID int64 = 1

// Test: Generate JWT and verify it
func TestGenerateJWT(t *testing.T) {
	tokenService, err := token.NewService(testConfig)
	assert.NoError(t, err, "NewService should not return an error")

	token, exp, err := tokenService.GenerateClientAuthJWT(userID, orgID)
	assert.NoError(t, err, "GenerateClientAuthJWT should not return an error")
	assert.NotZero(t, token, "Generated token should not be empty")
	assert.NotZero(t, exp, "Token expiry should not be empty")

	// Verify token
	claims, err := tokenService.VerifyClientAuthJWT(token)
	assert.NoError(t, err, "VerifyClientAuthJWT should not return an error")
	assert.Equal(t, userID, claims.UserID, "UserID in token should match input")
	assert.Equal(t, claims.ExpiresAt.Unix(), exp, "Token expiry in claim should match returned exp")
}

// Test: Verify valid token
func TestVerifyJWT_ValidToken(t *testing.T) {
	tokenService, err := token.NewService(testConfig)
	assert.NoError(t, err, "NewService should not return an error")

	token, _, err := tokenService.GenerateClientAuthJWT(userID, orgID)
	assert.NoError(t, err, "GenerateClientAuthJWT should not return an error")

	claims, err := tokenService.VerifyClientAuthJWT(token)
	assert.NoError(t, err, "VerifyClientAuthJWT should not return an error for a valid token")
	assert.Equal(t, userID, claims.UserID, "Decoded UserID should match the original")
}

// Test: Verify invalid token
func TestVerifyJWT_InvalidToken(t *testing.T) {
	tokenService, err := token.NewService(testConfig)
	assert.NoError(t, err, "NewService should not return an error")

	invalidToken := "invalid.token.string"
	claims, err := tokenService.VerifyClientAuthJWT(invalidToken)
	assert.Error(t, err, "VerifyClientAuthJWT should return an error for an invalid token")
	assert.Zero(t, claims, "ClientAuthClaims should be nil for an invalid token")
}

// Test: Verify expired token
func TestVerifyJWT_ExpiredToken(t *testing.T) {
	expiredConfig := token.Config{
		ClientToken: token.AuthTokenConfig{
			SecretKey:        testConfig.ClientToken.SecretKey,
			ExpirationPeriod: -time.Minute, // Negative duration to force expiration
		},
		MinerTokenExpirationPeriod: testConfig.MinerTokenExpirationPeriod,
	}
	tokenService, err := token.NewService(expiredConfig)
	assert.NoError(t, err, "NewService should not return an error")

	token, _, err := tokenService.GenerateClientAuthJWT(userID, orgID)
	assert.NoError(t, err, "GenerateClientAuthJWT should not return an error")

	claims, err := tokenService.VerifyClientAuthJWT(token)
	assert.Error(t, err, "VerifyClientAuthJWT should return an error for an expired token")
	assert.Zero(t, claims, "ClientAuthClaims should be nil for an expired token")
}

// Test: Verify tampered token
func TestVerifyJWT_TamperedToken(t *testing.T) {
	tokenService, err := token.NewService(testConfig)
	assert.NoError(t, err, "NewService should not return an error")

	token, _, err := tokenService.GenerateClientAuthJWT(userID, orgID)
	assert.NoError(t, err, "GenerateClientAuthJWT should not return an error")

	// Modify token (tamper with it)
	tamperedToken := flipBits(token)
	claims, err := tokenService.VerifyClientAuthJWT(tamperedToken)
	assert.Error(t, err, "VerifyClientAuthJWT should return an error for a tampered token")
	assert.Zero(t, claims, "ClientAuthClaims should be nil for a tampered token")
}

// Test: NewService should reject short secret key
func TestNewTokenService_InvalidSecret(t *testing.T) {
	shortKey := "short-key"
	invalidConfig := token.Config{
		ClientToken: token.AuthTokenConfig{
			SecretKey:        shortKey,
			ExpirationPeriod: time.Minute * 5,
		},
		MinerTokenExpirationPeriod: time.Minute * 5,
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
		MinerTokenExpirationPeriod: time.Minute * 5,
	}

	tokenService, err := token.NewService(validConfig)
	assert.NoError(t, err, "NewService should not return an error for valid secret key")
	assert.NotZero(t, tokenService, "Service instance should be created successfully")
}

func flipBits(input string) string {
	bytes := []byte(input)
	for i := range bytes {
		bytes[i] = ^bytes[i]
	}
	return string(bytes)
}
