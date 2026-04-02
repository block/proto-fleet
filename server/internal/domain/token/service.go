package token

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"time"

	"connectrpc.com/authn"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"

	"github.com/golang-jwt/jwt/v5"
)

var clientSigningMethod = jwt.SigningMethodHS256
var minerSigningMethod = jwt.SigningMethodEdDSA

const minClientSecretKeyLength = 32 // 32 bytes for HS256 security

// ClientAuthClaims struct for JWT payload
type ClientAuthClaims struct {
	UserID int64 `json:"user_id"`
	OrgID  int64 `json:"org_id"`
	jwt.RegisteredClaims
}

type MinerAuthClaims struct {
	MinerSN string `json:"miner_sn"`
	jwt.RegisteredClaims
}

type Service struct {
	cfg Config
}

// NewService validates and creates a Service instance
func NewService(cfg Config) (*Service, error) {
	if len(cfg.ClientToken.SecretKey) < minClientSecretKeyLength {
		return nil, fleeterror.NewInternalErrorf("secret key must be at least 32 bytes long: len=%d", len(cfg.ClientToken.SecretKey))
	}
	if cfg.ClientToken.ExpirationPeriod == 0 || cfg.MinerTokenExpirationPeriod == 0 {
		return nil, fleeterror.NewInternalError("expiration period value is required. e.g. '30m'")
	}

	return &Service{cfg: cfg}, nil
}

// GenerateClientAuthJWT creates a JWT for a client
func (ts *Service) GenerateClientAuthJWT(userID, orgID int64) (string, int64, error) {
	exp := jwt.NewNumericDate(time.Now().Add(ts.cfg.ClientToken.ExpirationPeriod))
	claims := ClientAuthClaims{
		UserID: userID,
		OrgID:  orgID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: exp,
		},
	}

	token := jwt.NewWithClaims(clientSigningMethod, claims)
	signedToken, err := token.SignedString([]byte(ts.cfg.ClientToken.SecretKey))
	if err != nil {
		return "", 0, fleeterror.NewInternalErrorf("error signing token: %v", err)
	}
	return signedToken, exp.Unix(), nil
}

// VerifyClientAuthJWT validates the token and extracts claims
func (ts *Service) VerifyClientAuthJWT(tokenString string) (*ClientAuthClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &ClientAuthClaims{}, func(_ *jwt.Token) (any, error) {
		return []byte(ts.cfg.ClientToken.SecretKey), nil
	}, jwt.WithValidMethods([]string{clientSigningMethod.Alg()}))

	if err != nil {
		return nil, fleeterror.NewUnauthenticatedErrorf("JWT is invalid: %v", err)
	}

	// Extract claims
	if claims, ok := token.Claims.(*ClientAuthClaims); ok && token.Valid {
		return claims, nil
	}

	return nil, fleeterror.NewUnauthenticatedError("JWT is invalid: cannot extract claims")
}

func GetClientAuthJWTClaims(ctx context.Context) (*ClientAuthClaims, error) {
	claims, ok := authn.GetInfo(ctx).(*ClientAuthClaims)
	if !ok {
		return nil, fleeterror.NewInternalError(
			"Context does not have JWT claims. Likely cause is usage of GetClientAuthJWTClaims from an Endpoint without authentication.",
		)
	}

	return claims, nil
}

func (ts *Service) CreateMinerAuthPrivateKeyForOrganization() ([]byte, error) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return []byte{}, fleeterror.NewInternalErrorf("error generating private key: %v", err)
	}

	return privateKey, nil
}

func (ts *Service) GenerateMinerAuthJWT(serialNumber string, privateKey []byte) (string, int64, error) {
	exp := jwt.NewNumericDate(time.Now().Add(ts.cfg.MinerTokenExpirationPeriod))

	claims := MinerAuthClaims{
		MinerSN: serialNumber,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: exp,
		},
	}

	token := jwt.NewWithClaims(minerSigningMethod, claims)
	signedToken, err := token.SignedString(ed25519.PrivateKey(privateKey))
	if err != nil {
		return "", 0, fleeterror.NewInternalErrorf("error signing token: %v", err)
	}
	return signedToken, exp.Unix(), nil
}

// ExtractPublicKeyFromPrivateKey to be used while pairing with the miner to distribute the public key
func (ts *Service) ExtractPublicKeyFromPrivateKey(privateKey []byte) (string, error) {
	privKey := ed25519.PrivateKey(privateKey)
	pubKey, ok := privKey.Public().(ed25519.PublicKey)
	if !ok {
		return "", fleeterror.NewInternalErrorf("not an Ed25519 public key")
	}

	derBytes, err := x509.MarshalPKIXPublicKey(pubKey)
	if err != nil {
		return "", fleeterror.NewInternalErrorf("failed to marshal SPKI DER: %v", err)
	}

	return base64.StdEncoding.EncodeToString(derBytes), nil
}
