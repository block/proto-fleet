package testutils

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// MinerAuthClaims represents JWT claims for miner authentication
type MinerAuthClaims struct {
	MinerSN string `json:"miner_sn"`
	jwt.RegisteredClaims
}

// Ed25519KeyPair represents an Ed25519 key pair for testing
type Ed25519KeyPair struct {
	PrivateKey ed25519.PrivateKey
	PublicKey  ed25519.PublicKey
}

// GenerateEd25519KeyPair generates a new Ed25519 key pair for testing
func GenerateEd25519KeyPair() (*Ed25519KeyPair, error) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate Ed25519 key pair: %w", err)
	}

	return &Ed25519KeyPair{
		PrivateKey: privateKey,
		PublicKey:  publicKey,
	}, nil
}

// PublicKeyBase64 returns the public key encoded as base64 SPKI DER format
// This is the format expected by the Proto miner pairing API
func (kp *Ed25519KeyPair) PublicKeyBase64() (string, error) {
	derBytes, err := x509.MarshalPKIXPublicKey(kp.PublicKey)
	if err != nil {
		return "", fmt.Errorf("failed to marshal public key to PKIX format: %w", err)
	}
	return base64.StdEncoding.EncodeToString(derBytes), nil
}

// GenerateJWT generates a JWT token signed with the Ed25519 private key
func (kp *Ed25519KeyPair) GenerateJWT(serialNumber string, expirationDuration time.Duration) (string, error) {
	now := time.Now()
	exp := now.Add(expirationDuration)

	claims := MinerAuthClaims{
		MinerSN: serialNumber,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	signedToken, err := token.SignedString(kp.PrivateKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign JWT token: %w", err)
	}
	return signedToken, nil
}
