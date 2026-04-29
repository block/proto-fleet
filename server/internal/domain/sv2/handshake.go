package sv2

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"time"

	btcec "github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/ellswift"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"golang.org/x/crypto/chacha20poly1305"
)

// SRI's Noise NX implementation uses secp256k1 + ElligatorSwift, NOT
// vanilla X25519. The protocol name is
// "Noise_NX_Secp256k1+EllSwift_ChaChaPoly_SHA256"; the constant below is
// the precomputed SHA-256 of that name. See
// https://github.com/stratum-mining/sv2-spec/blob/main/04-Protocol-Security.md
var noiseProtocolHash = [32]byte{
	46, 180, 120, 129, 32, 142, 158, 238, 31, 102, 159, 103, 198, 110, 231, 14,
	169, 234, 136, 9, 13, 80, 63, 232, 48, 220, 75, 200, 62, 41, 191, 16,
}

const (
	ellswiftEncodingSize             = 64
	encryptedEllswiftEncodingSize    = ellswiftEncodingSize + chacha20poly1305.Overhead // 80
	signatureNoiseMessageSize        = 74
	encryptedSignatureNoiseMsgSize   = signatureNoiseMessageSize + chacha20poly1305.Overhead // 90
	initiatorExpectedHandshakeMsgLen = ellswiftEncodingSize + encryptedEllswiftEncodingSize + encryptedSignatureNoiseMsgSize
)

// noiseHandshakeTimeout caps the entire handshake (post-dial). The dial
// itself is bounded by the timeout passed to HandshakeProbe.
const noiseHandshakeTimeout = 15 * time.Second

// ErrHandshakeUnsupported is returned when the caller didn't supply the
// pool's authority pubkey (32 bytes). Without it the signature in
// message 2 cannot be verified against an operator-pinned identity.
var ErrHandshakeUnsupported = errors.New("handshake probe requires the pool's Noise authority public key")

// HandshakeProbe runs the SRI Noise NX initiator side against an SV2
// pool. Returns (true, nil) if the responder presents a static key
// signed by the operator-supplied authority key (BIP340 Schnorr
// signature, valid_from/valid_to envelope from SRI's
// SignatureNoiseMessage). Closes the connection immediately after.
func HandshakeProbe(ctx context.Context, stratumURL string, authorityKey []byte, timeout time.Duration) (bool, error) {
	if len(authorityKey) != 32 {
		return false, ErrHandshakeUnsupported
	}
	authorityXOnly, err := schnorr.ParsePubKey(authorityKey)
	if err != nil {
		return false, fmt.Errorf("parse authority pubkey: %w", err)
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

	conn, err := (&net.Dialer{}).DialContext(dialCtx, "tcp", addr)
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

	priv, ourEllswift, err := ellswift.EllswiftCreate()
	if err != nil {
		return false, fmt.Errorf("generate ephemeral ellswift keypair: %w", err)
	}

	state := newHandshakeState()
	state.mixHash(ourEllswift[:])
	if err := state.encryptAndHash(nil); err != nil {
		return false, fmt.Errorf("step 0 encrypt empty payload: %w", err)
	}

	if _, err := conn.Write(ourEllswift[:]); err != nil {
		return false, fmt.Errorf("write handshake message 1: %w", err)
	}

	var reply [initiatorExpectedHandshakeMsgLen]byte
	if _, err := io.ReadFull(conn, reply[:]); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || os.IsTimeout(err) {
			return false, fmt.Errorf("connected to %s but the pool didn't reply with a Stratum V2 Noise handshake within the deadline — verify the URL points at a V2-capable endpoint: %w", addr, err)
		}
		return false, fmt.Errorf("read handshake message 2: %w", err)
	}

	// Step 2 — process the responder's reply.
	var theirEphemeralEllswift [ellswiftEncodingSize]byte
	copy(theirEphemeralEllswift[:], reply[0:ellswiftEncodingSize])
	state.mixHash(theirEphemeralEllswift[:])

	ecdhEphemeral, err := ellswift.V2Ecdh(priv, theirEphemeralEllswift, ourEllswift, true)
	if err != nil {
		return false, fmt.Errorf("ecdh(e, re): %w", err)
	}
	state.mixKey(ecdhEphemeral[:])

	encryptedTheirStaticEllswift := reply[ellswiftEncodingSize : ellswiftEncodingSize+encryptedEllswiftEncodingSize]
	theirStaticEllswiftBuf, err := state.decryptAndHash(encryptedTheirStaticEllswift)
	if err != nil {
		return false, fmt.Errorf("decrypt responder static key: %w", err)
	}
	if len(theirStaticEllswiftBuf) != ellswiftEncodingSize {
		return false, fmt.Errorf("decrypted static key has wrong length: got %d want %d", len(theirStaticEllswiftBuf), ellswiftEncodingSize)
	}
	var theirStaticEllswift [ellswiftEncodingSize]byte
	copy(theirStaticEllswift[:], theirStaticEllswiftBuf)

	ecdhStatic, err := ellswift.V2Ecdh(priv, theirStaticEllswift, ourEllswift, true)
	if err != nil {
		return false, fmt.Errorf("ecdh(e, rs): %w", err)
	}
	state.mixKey(ecdhStatic[:])

	encryptedSignatureMsg := reply[ellswiftEncodingSize+encryptedEllswiftEncodingSize : initiatorExpectedHandshakeMsgLen]
	signatureMsg, err := state.decryptAndHash(encryptedSignatureMsg)
	if err != nil {
		return false, fmt.Errorf("decrypt signature noise message: %w", err)
	}
	if len(signatureMsg) != signatureNoiseMessageSize {
		return false, fmt.Errorf("signature noise message length: got %d want %d", len(signatureMsg), signatureNoiseMessageSize)
	}

	respStaticXOnly, err := ellswiftXOnly(theirStaticEllswift)
	if err != nil {
		return false, fmt.Errorf("decode responder static pubkey: %w", err)
	}

	now, err := unixSecondsAsUint32(time.Now())
	if err != nil {
		return false, err
	}
	if err := verifySignatureNoiseMessage(signatureMsg, respStaticXOnly, authorityXOnly, now); err != nil {
		return false, fmt.Errorf("verify pool authority signature: %w", err)
	}
	return true, nil
}

// handshakeState carries the chaining key (ck), handshake hash (h), and
// optional cipher key (k) through the Noise NX progression. All
// operations match SRI's HandshakeOp trait byte-for-byte.
type handshakeState struct {
	ck    [32]byte
	h     [32]byte
	k     [32]byte
	haveK bool
	n     uint64
}

func newHandshakeState() *handshakeState {
	s := &handshakeState{ck: noiseProtocolHash}
	s.h = sha256.Sum256(s.ck[:])
	return s
}

func (s *handshakeState) mixHash(data []byte) {
	hasher := sha256.New()
	hasher.Write(s.h[:])
	hasher.Write(data)
	hasher.Sum(s.h[:0])
}

func (s *handshakeState) mixKey(material []byte) {
	ck, k := hkdf2(s.ck[:], material)
	s.ck = ck
	s.k = k
	s.haveK = true
	s.n = 0
}

// encryptAndHash encrypts plaintext with AD=h then mix_hashes the
// ciphertext. Before the cipher key is set, encryption is a no-op.
func (s *handshakeState) encryptAndHash(plaintext []byte) error {
	var ct []byte
	if s.haveK {
		var err error
		ct, err = aeadEncrypt(s.k, s.n, s.h[:], plaintext)
		if err != nil {
			return err
		}
		s.n++
	} else {
		ct = plaintext
	}
	s.mixHash(ct)
	return nil
}

// decryptAndHash decrypts ciphertext (when the cipher key is set) and
// mix_hashes the original ciphertext.
func (s *handshakeState) decryptAndHash(ciphertext []byte) ([]byte, error) {
	pt := ciphertext
	if s.haveK {
		var err error
		pt, err = aeadDecrypt(s.k, s.n, s.h[:], ciphertext)
		if err != nil {
			return nil, err
		}
		s.n++
	}
	s.mixHash(ciphertext)
	return pt, nil
}

func aeadEncrypt(key [32]byte, nonce uint64, ad, plaintext []byte) ([]byte, error) {
	aead, err := chacha20poly1305.New(key[:])
	if err != nil {
		return nil, fmt.Errorf("chacha20poly1305 init: %w", err)
	}
	return aead.Seal(nil, noiseNonce(nonce), plaintext, ad), nil
}

func aeadDecrypt(key [32]byte, nonce uint64, ad, ciphertext []byte) ([]byte, error) {
	aead, err := chacha20poly1305.New(key[:])
	if err != nil {
		return nil, fmt.Errorf("chacha20poly1305 init: %w", err)
	}
	pt, err := aead.Open(nil, noiseNonce(nonce), ciphertext, ad)
	if err != nil {
		return nil, fmt.Errorf("aead open: %w", err)
	}
	return pt, nil
}

// noiseNonce builds the 12-byte ChaCha20-Poly1305 nonce: 4 zero bytes
// followed by the 8-byte little-endian counter (Noise spec §5.1).
func noiseNonce(n uint64) []byte {
	nonce := make([]byte, 12)
	binary.LittleEndian.PutUint64(nonce[4:], n)
	return nonce
}

// hkdf2 implements the Noise HKDF (HMAC-SHA256) two-output variant that
// SRI's hkdf_2 helper computes.
func hkdf2(chainingKey, ikm []byte) ([32]byte, [32]byte) {
	temp := hmacSHA256(chainingKey, ikm)
	out1 := hmacSHA256(temp[:], []byte{0x01})
	out2 := hmacSHA256(temp[:], append(out1[:], 0x02))
	return out1, out2
}

func hmacSHA256(key, data []byte) [32]byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	var out [32]byte
	copy(out[:], mac.Sum(nil))
	return out
}

// verifySignatureNoiseMessage validates SRI's SignatureNoiseMessage
// against the responder's static pubkey (recovered from the handshake)
// and the operator-pinned authority pubkey. Layout:
//
//	bytes  0.. 1: u16 little-endian version (must be 0)
//	bytes  2.. 5: u32 little-endian valid_from
//	bytes  6.. 9: u32 little-endian not_valid_after
//	bytes 10..73: 64-byte BIP340 Schnorr signature over the certificate body
//
// The signature is over sha256(version || valid_from || not_valid_after ||
// responder_static_xonly).
func verifySignatureNoiseMessage(msg []byte, responderStaticXOnly, authority *btcec.PublicKey, now uint32) error {
	if len(msg) != signatureNoiseMessageSize {
		return fmt.Errorf("unexpected length: %d", len(msg))
	}
	version := binary.LittleEndian.Uint16(msg[0:2])
	if version != 0 {
		return fmt.Errorf("unsupported SignatureNoiseMessage version %d", version)
	}
	validFrom := binary.LittleEndian.Uint32(msg[2:6])
	notAfter := binary.LittleEndian.Uint32(msg[6:10])
	if now < validFrom {
		return fmt.Errorf("certificate not yet valid (valid_from=%d, now=%d)", validFrom, now)
	}
	if now > notAfter {
		return fmt.Errorf("certificate expired (not_valid_after=%d, now=%d)", notAfter, now)
	}

	digest := signatureCertDigest(msg[0:10], responderStaticXOnly)

	sig, err := schnorr.ParseSignature(msg[10:74])
	if err != nil {
		return fmt.Errorf("parse schnorr signature: %w", err)
	}
	if !sig.Verify(digest[:], authority) {
		return errors.New("signature does not verify against operator-supplied authority key")
	}
	return nil
}

func signatureCertDigest(envelope []byte, responderStaticXOnly *btcec.PublicKey) [32]byte {
	var staticXOnly [32]byte
	copy(staticXOnly[:], schnorr.SerializePubKey(responderStaticXOnly))
	hasher := sha256.New()
	hasher.Write(envelope)
	hasher.Write(staticXOnly[:])
	var out [32]byte
	hasher.Sum(out[:0])
	return out
}

// ellswiftXOnly decodes a 64-byte ElligatorSwift encoding into the
// X-only secp256k1 public key the schnorr verifier wants. SRI fixes the
// parity at Even, so we pass the raw 32-byte X-coordinate to
// schnorr.ParsePubKey which assumes even Y.
func ellswiftXOnly(enc [ellswiftEncodingSize]byte) (*btcec.PublicKey, error) {
	var u, t btcec.FieldVal
	if u.SetByteSlice(enc[0:32]) {
		u.Normalize()
	}
	if t.SetByteSlice(enc[32:64]) {
		t.Normalize()
	}
	x, err := ellswift.XSwiftEC(&u, &t)
	if err != nil {
		return nil, fmt.Errorf("ellswift decode: %w", err)
	}
	xBytes := x.Bytes()
	pk, err := schnorr.ParsePubKey(xBytes[:])
	if err != nil {
		return nil, fmt.Errorf("schnorr parse pubkey: %w", err)
	}
	return pk, nil
}

// unixSecondsAsUint32 narrows a time.Time to the SV2 SignatureNoiseMessage's
// 4-byte epoch-seconds slot. Year-2106 problem aside, no real clock should
// reach this branch.
func unixSecondsAsUint32(t time.Time) (uint32, error) {
	secs := t.Unix()
	if secs < 0 || secs > math.MaxUint32 {
		return 0, fmt.Errorf("clock %v out of uint32 epoch-seconds range", t)
	}
	return uint32(secs), nil
}
