package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// identityFingerprint must stay byte-for-byte equal to the server's
// agentenrollment.IdentityFingerprint so visual comparison works.
func identityFingerprint(pubkey []byte) string {
	h := sha256.Sum256(pubkey)
	return hex.EncodeToString(h[:8])
}

func generateKeypair() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate ed25519 keypair: %w", err)
	}
	return pub, priv, nil
}
