package sv2

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/flynn/noise"
)

// noiseHandshakeTimeout caps the entire handshake at this duration,
// independent of the caller's context, so a half-open connection can't
// block a ValidatePool request forever. The dial itself is separately
// bounded by the timeout callers pass to HandshakeProbe.
const noiseHandshakeTimeout = 15 * time.Second

// noiseProtocolName is the SRI SV2 Noise handshake pattern. SRI v1.x
// uses Noise_NX_25519_ChaChaPoly_BLAKE2s — NX pattern (no client
// static key, server sends its static), X25519 DH, ChaChaPoly
// encryption, BLAKE2s hash. The exact string here matches the
// "Noise_<Pattern>_<DH>_<Cipher>_<Hash>" format the protocol spec
// requires for the hash prologue.
var noiseProtocolName = []byte("Noise_NX_25519_ChaChaPoly_BLAKE2s")

// ErrHandshakeUnsupported is returned when the caller asked for a
// handshake probe but didn't supply a pool Noise pubkey. Callers fall
// back to TCPDial in that case — it's a legitimate flow, not an error.
var ErrHandshakeUnsupported = errors.New("handshake probe requires the pool's Noise public key")

// HandshakeProbe attempts the Noise NX initiator side against an SV2
// pool and returns (true, nil) if the handshake completes: the pool
// presented a valid static key, we authenticated it against the
// provided pubkey, and both sides have a shared secret. The probe
// closes the connection immediately after; we don't send any SV2
// application-layer message, so this validates "the pool speaks SV2
// Noise with this authority key" rather than "credentials authorise
// mining".
//
// A nil or empty poolPubKey yields ErrHandshakeUnsupported so the
// caller can fall back to a pure TCP dial and mark the response mode
// accordingly. Length mismatches (anything other than 32 bytes) return
// the same — 32 bytes is the X25519 public-key length; other sizes
// indicate operator misconfiguration.
func HandshakeProbe(ctx context.Context, stratumURL string, poolPubKey []byte, timeout time.Duration) (bool, error) {
	if len(poolPubKey) != 32 {
		return false, ErrHandshakeUnsupported
	}
	addr, err := addressFromStratumURL(stratumURL)
	if err != nil {
		return false, err
	}
	if timeout <= 0 {
		timeout = DefaultTCPDialTimeout
	}

	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	dialer := net.Dialer{}
	conn, err := dialer.DialContext(dialCtx, "tcp", addr)
	if err != nil {
		return false, fmt.Errorf("tcp dial for handshake: %w", err)
	}
	defer conn.Close()

	overall := noiseHandshakeTimeout
	if deadline, ok := dialCtx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining > 0 && remaining < overall {
			overall = remaining
		}
	}
	if err := conn.SetDeadline(time.Now().Add(overall)); err != nil {
		return false, fmt.Errorf("set handshake deadline: %w", err)
	}

	hs, err := newInitiatorHandshake()
	if err != nil {
		return false, err
	}

	// Round 1: send ephemeral public (message 'e'). NX pattern's first
	// message has no payload.
	msg1, _, _, err := hs.WriteMessage(nil, nil)
	if err != nil {
		return false, fmt.Errorf("build handshake message 1: %w", err)
	}
	if err := writeNoiseFrame(conn, msg1); err != nil {
		return false, fmt.Errorf("send handshake message 1: %w", err)
	}

	// Round 2: read server's response ('e, ee, s, es'). The NX pattern
	// delivers the server's static key in this message; we then compare
	// it to the operator-provided authority key to authenticate the
	// pool. A mismatch means the pool on the wire isn't the pool the
	// operator thinks they're talking to — classic key-pinning failure.
	msg2, err := readNoiseFrame(conn)
	if err != nil {
		return false, fmt.Errorf("read handshake message 2: %w", err)
	}
	if _, _, _, err := hs.ReadMessage(nil, msg2); err != nil {
		return false, fmt.Errorf("verify handshake message 2: %w", err)
	}

	presented := hs.PeerStatic()
	if !bytes.Equal(presented, poolPubKey) {
		return false, fmt.Errorf("pool presented static key does not match operator-supplied authority key")
	}
	return true, nil
}

// newInitiatorHandshake builds a Noise NX initiator state pinned to
// SRI's cipher suite (Noise_NX_25519_ChaChaPoly_BLAKE2s). NX delivers
// the responder's static key over the wire, so we do NOT pre-load it
// here — the caller compares the received key to the operator-provided
// authority key after the handshake completes.
func newInitiatorHandshake() (*noise.HandshakeState, error) {
	cs := noise.NewCipherSuite(noise.DH25519, noise.CipherChaChaPoly, noise.HashBLAKE2s)
	hs, err := noise.NewHandshakeState(noise.Config{
		CipherSuite: cs,
		Random:      nil, // defaults to crypto/rand
		Pattern:     noise.HandshakeNX,
		Initiator:   true,
		Prologue:    noiseProtocolName,
	})
	if err != nil {
		return nil, fmt.Errorf("noise handshake init: %w", err)
	}
	return hs, nil
}

// writeNoiseFrame sends a length-prefixed Noise frame on the wire.
// SRI uses a 2-byte big-endian length header before the Noise payload;
// we use the same framing so any future extension to a full
// SetupConnection exchange can reuse these helpers.
func writeNoiseFrame(w io.Writer, payload []byte) error {
	if len(payload) > 0xFFFF {
		return fmt.Errorf("noise frame too large: %d bytes", len(payload))
	}
	var hdr [2]byte
	binary.BigEndian.PutUint16(hdr[:], uint16(len(payload)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

// readNoiseFrame reads a length-prefixed Noise frame. Matches writeNoiseFrame.
func readNoiseFrame(r io.Reader) ([]byte, error) {
	var hdr [2]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	n := int(binary.BigEndian.Uint16(hdr[:]))
	if n == 0 {
		return nil, fmt.Errorf("empty noise frame")
	}
	payload := make([]byte, n)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}
	return payload, nil
}
