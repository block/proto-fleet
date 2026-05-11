package agentbootstrap

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// Must mirror server agentenrollment.IdentityFingerprint exactly so the
// operator UI's visual fingerprint compare succeeds.
func IdentityFingerprint(pubkey []byte) string {
	h := sha256.Sum256(pubkey)
	return hex.EncodeToString(h[:8])
}

func GenerateKeypair() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate ed25519 keypair: %w", err)
	}
	return pub, priv, nil
}
