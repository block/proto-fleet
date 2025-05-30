package token

import (
	"context"
	"time"

	"connectrpc.com/authn"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"

	"github.com/golang-jwt/jwt/v5"
)

var signingMethod = jwt.SigningMethodHS256

const minSecretKeyLength = 32 // 32 bytes for HS256 security

// Claims struct for JWT payload
type Claims struct {
	UserID int64 `json:"user_id"`
	OrgID  int64 `json:"org_id"`
	jwt.RegisteredClaims
}

type Service struct {
	cfg Config
}

// NewService validates and creates a Service instance
func NewService(cfg Config) (*Service, error) {
	if len(cfg.SecretKey) < minSecretKeyLength {
		return nil, fleeterror.NewInternalErrorf("secret key must be at least 32 bytes long: len=%d", len(cfg.SecretKey))
	}
	if cfg.ExpirationPeriod == 0 {
		return nil, fleeterror.NewInternalError("expiration period value is required. e.g. '30m'")
	}

	return &Service{cfg: cfg}, nil
}

// GenerateJWT creates a JWT token
func (ts *Service) GenerateJWT(userID, orgID int64) (string, int64, error) {
	exp := jwt.NewNumericDate(time.Now().Add(ts.cfg.ExpirationPeriod))
	claims := Claims{
		UserID: userID,
		OrgID:  orgID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: exp,
		},
	}

	token := jwt.NewWithClaims(signingMethod, claims)
	signedToken, err := token.SignedString([]byte(ts.cfg.SecretKey))
	if err != nil {
		return "", 0, fleeterror.NewInternalErrorf("error signing token: %v", err)
	}
	return signedToken, exp.Unix(), nil
}

// VerifyJWT validates the token and extracts claims
func (ts *Service) VerifyJWT(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(_ *jwt.Token) (any, error) {
		return []byte(ts.cfg.SecretKey), nil
	}, jwt.WithValidMethods([]string{signingMethod.Alg()}))

	if err != nil {
		return nil, fleeterror.NewUnauthenticatedErrorf("JWT is invalid: %v", err)
	}

	// Extract claims
	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}

	return nil, fleeterror.NewUnauthenticatedError("JWT is invalid: cannot extract claims")
}

func GetJWTClaims(ctx context.Context) (*Claims, error) {
	claims, ok := authn.GetInfo(ctx).(*Claims)
	if !ok {
		return nil, fleeterror.NewInternalError(
			"Context does not have JWT claims. Likely cause is usage of GetJWTClaims from an Endpoint without authentication.",
		)
	}

	return claims, nil
}
