package token

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var TokenSigningMethod = jwt.SigningMethodHS256

const MinSecretKeyLength = 32 // 32 bytes for HS256 security

// Claims struct for JWT payload
type Claims struct {
	UserID string `json:"user_id"`
	jwt.RegisteredClaims
}

type Service struct {
	cfg Config
}

// NewService validates and creates a Service instance
func NewService(cfg Config) (*Service, error) {
	if len(cfg.SecretKey) < MinSecretKeyLength {
		return nil, fmt.Errorf("secret key must be at least 32 bytes long: len=%d", len(cfg.SecretKey))
	}

	// Ensure default expiration period
	if cfg.ExpirationPeriod == 0 {
		return nil, errors.New("expiration period value is required. e.g. '30m'")
	}

	return &Service{cfg: cfg}, nil
}

// GenerateJWT creates a JWT token
func (ts *Service) GenerateJWT(userID string) (string, int64, error) {
	exp := jwt.NewNumericDate(time.Now().Add(ts.cfg.ExpirationPeriod))
	claims := Claims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: exp,
		},
	}

	token := jwt.NewWithClaims(TokenSigningMethod, claims)
	signedToken, err := token.SignedString([]byte(ts.cfg.SecretKey))
	if err != nil {
		return "", 0, fmt.Errorf("error signing token: %w", err)
	}
	return signedToken, exp.Unix(), nil
}

// VerifyJWT validates the token and extracts claims
func (ts *Service) VerifyJWT(tokenString string) (*Claims, error) {
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
