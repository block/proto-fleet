package token_test

import (
	"fmt"
	"github.com/btc-mining/proto-fleet/server/internal/domain/token"
	"testing"
	"time"

	"github.com/alecthomas/assert/v2"
)

var testConfig = token.Config{
	SecretKey:        "test-secret-key-that-is-long-enough", // Ensure valid length for testing
	ExpirationPeriod: time.Minute * 5,                       // Short expiration for testing
}

// Test: Generate JWT and verify it
func TestGenerateJWT(t *testing.T) {
	tokenService, err := token.NewService(testConfig)
	assert.NoError(t, err, "NewService should not return an error")

	userID := "12345"
	token, exp, err := tokenService.GenerateJWT(userID)
	assert.NoError(t, err, "GenerateJWT should not return an error")
	assert.NotZero(t, token, "Generated token should not be empty")
	assert.NotZero(t, exp, "Token expiry should not be empty")

	// Verify token
	claims, err := tokenService.VerifyJWT(token)
	assert.NoError(t, err, "VerifyJWT should not return an error")
	assert.Equal(t, userID, claims.UserID, "UserID in token should match input")
	assert.Equal(t, claims.ExpiresAt.Unix(), exp, "Token expiry in claim should match returned exp")
}

// Test: Verify valid token
func TestVerifyJWT_ValidToken(t *testing.T) {
	tokenService, err := token.NewService(testConfig)
	assert.NoError(t, err, "NewService should not return an error")

	userID := "67890"
	token, _, err := tokenService.GenerateJWT(userID)
	assert.NoError(t, err, "GenerateJWT should not return an error")

	claims, err := tokenService.VerifyJWT(token)
	assert.NoError(t, err, "VerifyJWT should not return an error for a valid token")
	assert.Equal(t, userID, claims.UserID, "Decoded UserID should match the original")
}

// Test: Verify invalid token
func TestVerifyJWT_InvalidToken(t *testing.T) {
	tokenService, err := token.NewService(testConfig)
	assert.NoError(t, err, "NewService should not return an error")

	invalidToken := "invalid.token.string"
	claims, err := tokenService.VerifyJWT(invalidToken)
	assert.Error(t, err, "VerifyJWT should return an error for an invalid token")
	assert.Zero(t, claims, "Claims should be nil for an invalid token")
}

// Test: Verify expired token
func TestVerifyJWT_ExpiredToken(t *testing.T) {
	expiredConfig := token.Config{
		SecretKey:        testConfig.SecretKey,
		ExpirationPeriod: -time.Minute, // Negative duration to force expiration
	}
	tokenService, err := token.NewService(expiredConfig)
	assert.NoError(t, err, "NewService should not return an error")

	userID := "expiredUser"
	token, _, err := tokenService.GenerateJWT(userID)
	assert.NoError(t, err, "GenerateJWT should not return an error")

	claims, err := tokenService.VerifyJWT(token)
	assert.Error(t, err, "VerifyJWT should return an error for an expired token")
	assert.Zero(t, claims, "Claims should be nil for an expired token")
}

// Test: Verify tampered token
func TestVerifyJWT_TamperedToken(t *testing.T) {
	tokenService, err := token.NewService(testConfig)
	assert.NoError(t, err, "NewService should not return an error")

	userID := "tamperedUser"
	token, _, err := tokenService.GenerateJWT(userID)
	assert.NoError(t, err, "GenerateJWT should not return an error")

	// Modify token (tamper with it)
	tamperedToken := flipBits(token)
	claims, err := tokenService.VerifyJWT(tamperedToken)
	assert.Error(t, err, "VerifyJWT should return an error for a tampered token")
	assert.Zero(t, claims, "Claims should be nil for a tampered token")
}

// Test: NewService should reject short secret key
func TestNewTokenService_InvalidSecret(t *testing.T) {
	shortKey := "short-key"
	invalidConfig := token.Config{
		SecretKey:        shortKey,
		ExpirationPeriod: time.Minute * 5,
	}

	_, err := token.NewService(invalidConfig)
	assert.Error(t, err, "Expected error for short secret key")
	assert.Equal(t, fmt.Sprintf("secret key must be at least 32 bytes long: len=%d", len(shortKey)), err.Error(), "Error message should match expected")
}

// Test: NewService with valid secret key
func TestNewTokenService_ValidSecret(t *testing.T) {
	validConfig := token.Config{
		SecretKey:        "valid-secret-key-that-is-long-enough",
		ExpirationPeriod: time.Minute * 5,
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
