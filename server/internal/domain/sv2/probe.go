package sv2

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

// Default cap for the dial portion of HandshakeProbe; the Noise
// handshake itself has its own deadline.
const DefaultTCPDialTimeout = 10 * time.Second

// X25519 public-key length used by the SRI v1.x Noise NX handshake.
const noisePoolKeyLen = 32

// SRI publishes authority pubkeys in a 38-byte base58check frame:
// 1 version byte || 1 secp256k1 compressed prefix || 32 X-coordinate
// bytes (used as the Noise X25519 key) || 4-byte BLAKE2b-256 checksum.
// We strip the framing without verifying the checksum — Noise itself
// authenticates the key over the wire.
const sriFramedPoolKeyLen = 1 + 1 + noisePoolKeyLen + 4

var ErrMissingNoiseKey = errors.New("stratum2+ URL is missing the /<authority_pubkey> path component")

// PoolNoiseKeyFromURL extracts the Noise authority pubkey from a
// Braiins-style SV2 URL (stratum2+tcp://HOST:PORT/<pubkey>). Accepts
// base58 in either raw 32-byte form or SRI's framed 37-byte form
// (1 version byte + 32 key bytes + 4 checksum bytes), and hex-encoded
// raw 32 bytes as a fallback.
func PoolNoiseKeyFromURL(stratumURL string) ([]byte, error) {
	u, err := url.Parse(stratumURL)
	if err != nil {
		return nil, fmt.Errorf("parse stratum URL: %w", err)
	}
	encoded := strings.TrimPrefix(u.Path, "/")
	if encoded == "" {
		return nil, ErrMissingNoiseKey
	}
	if decoded, err := decodeBase58(encoded); err == nil {
		switch len(decoded) {
		case noisePoolKeyLen:
			return decoded, nil
		case sriFramedPoolKeyLen:
			return decoded[2 : 2+noisePoolKeyLen], nil
		}
	}
	if key, err := decodeHex(encoded); err == nil && len(key) == noisePoolKeyLen {
		return key, nil
	}
	return nil, fmt.Errorf("authority pubkey %q must decode to %d bytes (raw) or %d bytes (SRI framed) via base58, or %d bytes via hex",
		encoded, noisePoolKeyLen, sriFramedPoolKeyLen, noisePoolKeyLen)
}

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

// IsSV2URL reports whether the URL is a Stratum V2 scheme. Case-insensitive.
func IsSV2URL(raw string) bool {
	return strings.HasPrefix(strings.ToLower(raw), "stratum2+tcp://")
}

func isSupportedScheme(raw string) bool {
	return IsSV2URL(raw)
}

// Bitcoin alphabet — used by Braiins V2 to encode the authority pubkey.
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
