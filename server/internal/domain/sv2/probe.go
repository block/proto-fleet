package sv2

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

// DefaultTCPDialTimeout caps the dial portion of HandshakeProbe when the
// caller passes a zero duration. The Noise handshake itself is bounded
// separately by noiseHandshakeTimeout so a half-open connection can't
// hang ValidatePool indefinitely.
const DefaultTCPDialTimeout = 10 * time.Second

// noisePoolKeyLen is the X25519 public-key length used by the SRI v1.x
// Noise NX handshake.
const noisePoolKeyLen = 32

// ErrMissingNoiseKey is returned by PoolNoiseKeyFromURL when the URL has
// no path component carrying the pool's authority pubkey. The CEL rule
// on ValidatePoolRequest.url enforces this format, so callers reaching
// here saw it slip past validation — likely a programmer error.
var ErrMissingNoiseKey = errors.New("stratum2+ URL is missing the /<authority_pubkey> path component")

// PoolNoiseKeyFromURL extracts the Noise authority pubkey from a Braiins-
// style SV2 URL: stratum2+tcp://HOST:PORT/<pubkey>. The pubkey is base58-
// encoded per Braiins V2 operator docs; we accept hex (64 chars) as a
// fallback so operators copy-pasting from raw Noise key dumps don't have
// to convert manually.
func PoolNoiseKeyFromURL(stratumURL string) ([]byte, error) {
	u, err := url.Parse(stratumURL)
	if err != nil {
		return nil, fmt.Errorf("parse stratum URL: %w", err)
	}
	encoded := strings.TrimPrefix(u.Path, "/")
	if encoded == "" {
		return nil, ErrMissingNoiseKey
	}
	if key, err := decodeBase58(encoded); err == nil && len(key) == noisePoolKeyLen {
		return key, nil
	}
	if key, err := decodeHex(encoded); err == nil && len(key) == noisePoolKeyLen {
		return key, nil
	}
	return nil, fmt.Errorf("authority pubkey %q must decode to %d bytes (base58 or hex)", encoded, noisePoolKeyLen)
}

// addressFromStratumURL extracts host:port from a stratum+(tcp|ssl|ws)
// or stratum2+tcp URL. Used by HandshakeProbe.
func addressFromStratumURL(raw string) (string, error) {
	if raw == "" {
		return "", fmt.Errorf("stratum URL is empty")
	}
	if !isSupportedScheme(raw) {
		return "", fmt.Errorf("unsupported stratum URL scheme: %q", raw)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parsing stratum URL %q: %w", raw, err)
	}
	host := u.Hostname()
	port := u.Port()
	if host == "" {
		return "", fmt.Errorf("stratum URL %q has no host", raw)
	}
	if port == "" {
		return "", fmt.Errorf("stratum URL %q requires an explicit port", raw)
	}
	return net.JoinHostPort(host, port), nil
}

func isSupportedScheme(raw string) bool {
	lower := strings.ToLower(raw)
	return strings.HasPrefix(lower, "stratum+tcp://") ||
		strings.HasPrefix(lower, "stratum2+tcp://")
}

// base58Alphabet is the Bitcoin alphabet (no 0, O, I, l) used by Braiins
// V2 to encode the Noise authority pubkey in its operator-facing URLs.
const base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

func decodeBase58(s string) ([]byte, error) {
	if s == "" {
		return nil, fmt.Errorf("empty input")
	}
	leadingZeros := 0
	for _, c := range s {
		if c != '1' {
			break
		}
		leadingZeros++
	}
	num := make([]byte, 0, len(s))
	for _, c := range s {
		idx := strings.IndexRune(base58Alphabet, c)
		if idx < 0 {
			return nil, fmt.Errorf("invalid base58 character %q", c)
		}
		carry := idx
		for i := len(num) - 1; i >= 0; i-- {
			carry += int(num[i]) * 58
			num[i] = byte(carry & 0xff)
			carry >>= 8
		}
		for carry > 0 {
			num = append([]byte{byte(carry & 0xff)}, num...)
			carry >>= 8
		}
	}
	out := make([]byte, leadingZeros+len(num))
	copy(out[leadingZeros:], num)
	return out, nil
}

func decodeHex(s string) ([]byte, error) {
	if len(s)%2 != 0 {
		return nil, fmt.Errorf("hex length must be even, got %d", len(s))
	}
	out := make([]byte, len(s)/2)
	for i := 0; i < len(out); i++ {
		hi, err := hexNibble(s[2*i])
		if err != nil {
			return nil, err
		}
		lo, err := hexNibble(s[2*i+1])
		if err != nil {
			return nil, err
		}
		out[i] = hi<<4 | lo
	}
	return out, nil
}

func hexNibble(c byte) (byte, error) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', nil
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, nil
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, nil
	}
	return 0, fmt.Errorf("invalid hex character %q", c)
}
