package domain

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var TokenSigningMethod = jwt.SigningMethodHS256

const MinSecretKeyLength = 32 // 32 bytes for HS256 security

type AuthConfig struct {
	SecretKey        string        `help:"Secret key for signing the JWT" env:"SECRET_KEY"`
	ExpirationPeriod time.Duration `help:"Expiration period duration for the JWT" env:"EXPIRATION_PERIOD"`
}

// Claims struct for JWT payload
type Claims struct {
	UserID string `json:"user_id"`
	jwt.RegisteredClaims
}

type TokenService struct {
	cfg AuthConfig
}

// NewTokenService validates and creates a TokenService instance
func NewTokenService(cfg AuthConfig) (*TokenService, error) {
	if len(cfg.SecretKey) < MinSecretKeyLength {
		return nil, errors.New("secret key must be at least 32 bytes long")
	}

	// Ensure default expiration period
	if cfg.ExpirationPeriod == 0 {
		return nil, errors.New("expiration period value is required. e.g. '30m'")
	}

	return &TokenService{cfg: cfg}, nil
}

// GenerateJWT creates a JWT token
func (ts *TokenService) GenerateJWT(userID string) (string, error) {
	claims := Claims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(ts.cfg.ExpirationPeriod)),
		},
	}

	token := jwt.NewWithClaims(TokenSigningMethod, claims)
	signedToken, err := token.SignedString([]byte(ts.cfg.SecretKey))
	if err != nil {
		return "", fmt.Errorf("error signing token: %w", err)
	}
	return signedToken, nil
}

// VerifyJWT validates the token and extracts claims
func (ts *TokenService) VerifyJWT(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(_ *jwt.Token) (any, error) {
		return []byte(ts.cfg.SecretKey), nil
	}, jwt.WithValidMethods([]string{TokenSigningMethod.Alg()}))

	if err != nil {
		return nil, fmt.Errorf("error parsing claims: %w", err)
	}

	// Extract claims
	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}

	return nil, errors.New("invalid token")
}
