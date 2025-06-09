package token

import (
	"connectrpc.com/authn"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var signingMethod = jwt.SigningMethodHS256

const minClientSecretKeyLength = 32 // 32 bytes for HS256 security

// ClientAuthClaims struct for JWT payload
type ClientAuthClaims struct {
	UserID int64 `json:"user_id"`
	OrgID  int64 `json:"org_id"`
	jwt.RegisteredClaims
}

type MinerAuthClaims struct {
	MinerID string `json:"miner_id"`
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

	token := jwt.NewWithClaims(signingMethod, claims)
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
	}, jwt.WithValidMethods([]string{signingMethod.Alg()}))

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
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return []byte{}, fleeterror.NewInternalErrorf("error generating private key: %v", err)
	}

	privateKeyBytes := x509.MarshalPKCS1PrivateKey(privateKey)
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privateKeyBytes,
	})

	if privateKeyPEM == nil {
		return []byte{}, fleeterror.NewInternalError("error creating private key: failed to encode private key")
	}

	return privateKeyPEM, nil
}

func (ts *Service) GenerateMinerAuthJWT(minerID string, organizationPrivateKey []byte) (string, int64, error) {
	block, _ := pem.Decode(organizationPrivateKey)
	if block == nil {
		return "", 0, fleeterror.NewInternalErrorf("failed to decode PEM block containing private key")
	}

	privateKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return "", 0, fleeterror.NewInternalErrorf("error parsing private key: %v", err)
	}

	exp := jwt.NewNumericDate(time.Now().Add(ts.cfg.MinerTokenExpirationPeriod))

	claims := MinerAuthClaims{
		MinerID: minerID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: exp,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signedToken, err := token.SignedString(privateKey)
	if err != nil {
		return "", 0, fleeterror.NewInternalErrorf("error signing token: %v", err)
	}
	return signedToken, exp.Unix(), nil
}

// ExtractPublicKeyFromPrivateKey to be used while pairing with the miner to distribute the public key
func (ts *Service) ExtractPublicKeyFromPrivateKey(privateKeyPEM string) (string, error) {
	block, _ := pem.Decode([]byte(privateKeyPEM))
	if block == nil {
		return "", fleeterror.NewInternalErrorf("failed to decode PEM block")
	}

	privateKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return "", fleeterror.NewInternalErrorf("error parsing private key: %v", err)
	}

	publicKeyBytes, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		return "", fleeterror.NewInternalErrorf("error marshalling public key: %v", err)
	}

	publicKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: publicKeyBytes,
	})

	return string(publicKeyPEM), nil
}
