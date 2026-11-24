package frigaterelease

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var (
	// ErrNoTrustedKeys is returned when no release key is configured.
	ErrNoTrustedKeys = errors.New("no trusted controller software release keys are configured")
	// ErrSignatureInvalid is returned when no trusted key verifies the signature.
	ErrSignatureInvalid = errors.New("release signature does not verify against any trusted key")
	// ErrKeyIDMismatch is returned when the caller named a key ID that is not the signing key.
	ErrKeyIDMismatch = errors.New("release signature was not made by the requested key")

	keyIDRe = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

// TrustedKey is one trusted release-signing public key.
type TrustedKey struct {
	ID  string
	Key *ecdsa.PublicKey
}

// KeyID returns "sha256:" plus the lowercase hex SHA-256 of the key's DER
// SubjectPublicKeyInfo.
func KeyID(pub *ecdsa.PublicKey) (string, error) {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return "", fmt.Errorf("marshal public key: %w", err)
	}
	sum := sha256.Sum256(der)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// NewTrustedKey wraps an ECDSA P-256 public key.
func NewTrustedKey(pub *ecdsa.PublicKey) (TrustedKey, error) {
	if pub == nil || pub.Curve != elliptic.P256() {
		return TrustedKey{}, errors.New("release keys must be ECDSA P-256")
	}
	id, err := KeyID(pub)
	if err != nil {
		return TrustedKey{}, err
	}
	return TrustedKey{ID: id, Key: pub}, nil
}

// ParsePublicKeys decodes every PEM "PUBLIC KEY" block in data.
func ParsePublicKeys(data []byte) ([]TrustedKey, error) {
	var keys []TrustedKey
	rest := data
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "PUBLIC KEY" {
			return nil, fmt.Errorf("unexpected PEM block type %q: release key files hold only PUBLIC KEY blocks", block.Type)
		}
		parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse public key: %w", err)
		}
		pub, ok := parsed.(*ecdsa.PublicKey)
		if !ok {
			return nil, errors.New("release keys must be ECDSA P-256")
		}
		key, err := NewTrustedKey(pub)
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, nil
}

// LoadKeysDir reads every *.pem file in dir. Symbolic links are followed, which
// is how Kubernetes secret mounts expose their files. Entries that do not
// resolve to a regular file are ignored; a .pem file that does not parse, holds
// a block that is not a public key, or holds no key at all is an error so a bad
// rotation does not silently shrink the trusted set.
func LoadKeysDir(dir string) ([]TrustedKey, error) {
	if dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read release keys directory: %w", err)
	}
	var keys []TrustedKey
	seen := map[string]bool{}
	for _, entry := range entries {
		if !strings.EqualFold(filepath.Ext(entry.Name()), ".pem") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		info, err := os.Stat(path) // Follows symlinks, unlike entry.Type().
		if err != nil {
			return nil, fmt.Errorf("release key %s: %w", entry.Name(), err)
		}
		if !info.Mode().IsRegular() {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read release key %s: %w", entry.Name(), err)
		}
		parsed, err := ParsePublicKeys(data)
		if err != nil {
			return nil, fmt.Errorf("release key %s: %w", entry.Name(), err)
		}
		if len(parsed) == 0 {
			return nil, fmt.Errorf("release key %s: no PEM public key found", entry.Name())
		}
		for _, key := range parsed {
			if !seen[key.ID] {
				seen[key.ID] = true
				keys = append(keys, key)
			}
		}
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].ID < keys[j].ID })
	return keys, nil
}

// Verifier checks release statements against a trusted key set.
type Verifier struct {
	load func() ([]TrustedKey, error)
}

// NewDirVerifier returns a verifier that reads dir on every call, so a key
// rotation takes effect without a restart. An empty dir has no trusted keys.
func NewDirVerifier(dir string) *Verifier {
	return &Verifier{load: func() ([]TrustedKey, error) { return LoadKeysDir(dir) }}
}

// NewStaticVerifier returns a verifier with a fixed key set.
func NewStaticVerifier(keys ...TrustedKey) *Verifier {
	return &Verifier{load: func() ([]TrustedKey, error) { return keys, nil }}
}

// Verified is a statement whose signature verified.
type Verified struct {
	Statement Statement
	// KeyID identifies the key that verified the signature.
	KeyID string
}

// Verify checks the signature over the raw statement bytes first and only then
// parses the statement. When wantKeyID is not empty it must be the signing key.
func (v *Verifier) Verify(statement, signature []byte, wantKeyID string) (Verified, error) {
	if len(statement) == 0 || len(statement) > MaxStatementBytes {
		return Verified{}, fmt.Errorf("%w: statement must be between 1 and %d bytes", ErrStatementInvalid, MaxStatementBytes)
	}
	if len(signature) == 0 || len(signature) > MaxSignatureBytes {
		return Verified{}, fmt.Errorf("%w: signature must be between 1 and %d bytes", ErrSignatureInvalid, MaxSignatureBytes)
	}
	if wantKeyID != "" && !keyIDRe.MatchString(wantKeyID) {
		return Verified{}, fmt.Errorf("invalid release key ID %q", wantKeyID)
	}
	keys, err := v.load()
	if err != nil {
		return Verified{}, err
	}
	if len(keys) == 0 {
		return Verified{}, ErrNoTrustedKeys
	}
	digest := sha256.Sum256(statement)
	signedBy := ""
	for _, key := range keys {
		if ecdsa.VerifyASN1(key.Key, digest[:], signature) {
			signedBy = key.ID
			break
		}
	}
	if signedBy == "" {
		return Verified{}, ErrSignatureInvalid
	}
	if wantKeyID != "" && wantKeyID != signedBy {
		return Verified{}, fmt.Errorf("%w: signed by %s, requested %s", ErrKeyIDMismatch, signedBy, wantKeyID)
	}
	parsed, err := ParseStatement(statement)
	if err != nil {
		return Verified{}, err
	}
	return Verified{Statement: parsed, KeyID: signedBy}, nil
}
