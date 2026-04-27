package sv2

import (
	"context"
	"crypto/rand"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/flynn/noise"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHandshakeProbe_CompletesAgainstMatchingResponder exercises the
// full Noise NX initiator against a mock responder that uses the same
// cipher suite. The responder presents a static key; the initiator is
// pre-loaded with that static key and must complete the handshake
// cleanly — same contract HandshakeProbe offers against a real SV2
// pool.
func TestHandshakeProbe_CompletesAgainstMatchingResponder(t *testing.T) {
	respKey := generateStaticKey(t)

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = lis.Close() })

	done := make(chan struct{})
	go runMockResponder(t, lis, respKey, done)

	url := "stratum2+tcp://" + lis.Addr().String()
	ok, err := HandshakeProbe(context.Background(), url, respKey.Public, time.Second)
	require.NoError(t, err)
	assert.True(t, ok)

	<-done // let the responder finish before test teardown
}

// TestHandshakeProbe_FailsOnWrongPoolKey verifies the probe's
// authentication property: even if the pool speaks Noise NX, a
// mismatched static key must fail the handshake. This is the same
// failure mode operators should see when they paste the wrong key
// into the validate form.
func TestHandshakeProbe_FailsOnWrongPoolKey(t *testing.T) {
	respKey := generateStaticKey(t)
	otherKey := generateStaticKey(t)

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = lis.Close() })

	done := make(chan struct{})
	go runMockResponder(t, lis, respKey, done)

	url := "stratum2+tcp://" + lis.Addr().String()
	ok, err := HandshakeProbe(context.Background(), url, otherKey.Public, time.Second)
	require.Error(t, err)
	assert.False(t, ok)
	<-done
}

func TestHandshakeProbe_RejectsMissingKey(t *testing.T) {
	ok, err := HandshakeProbe(context.Background(), "stratum2+tcp://127.0.0.1:1", nil, time.Second)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrHandshakeUnsupported))
	assert.False(t, ok)
}

func TestHandshakeProbe_RejectsShortKey(t *testing.T) {
	ok, err := HandshakeProbe(context.Background(), "stratum2+tcp://127.0.0.1:1", []byte{1, 2, 3}, time.Second)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrHandshakeUnsupported))
	assert.False(t, ok)
}

func TestHandshakeProbe_FailsOnUnresponsiveServer(t *testing.T) {
	// TEST-NET-1 address — won't respond, probe must time out rather
	// than block forever.
	start := time.Now()
	_, err := HandshakeProbe(context.Background(), "stratum2+tcp://192.0.2.1:34254", generateStaticKey(t).Public, 200*time.Millisecond)
	elapsed := time.Since(start)
	require.Error(t, err)
	assert.Less(t, elapsed, 500*time.Millisecond)
}

// runMockResponder stands up a single-connection Noise NX responder
// with the provided static key. It speaks the same cipher suite and
// prologue as HandshakeProbe expects; any deviation would let the
// probe incorrectly pass against a non-SV2 server.
func runMockResponder(t *testing.T, lis net.Listener, staticKey noise.DHKey, done chan<- struct{}) {
	defer close(done)
	conn, err := lis.Accept()
	if err != nil {
		return
	}
	defer conn.Close()

	cs := noise.NewCipherSuite(noise.DH25519, noise.CipherChaChaPoly, noise.HashBLAKE2s)
	hs, err := noise.NewHandshakeState(noise.Config{
		CipherSuite:   cs,
		Pattern:       noise.HandshakeNX,
		Initiator:     false,
		Prologue:      noiseProtocolName,
		StaticKeypair: staticKey,
	})
	if err != nil {
		t.Logf("responder init: %v", err)
		return
	}

	msg1, err := readNoiseFrame(conn)
	if err != nil {
		t.Logf("responder read msg1: %v", err)
		return
	}
	if _, _, _, err := hs.ReadMessage(nil, msg1); err != nil {
		t.Logf("responder verify msg1: %v", err)
		return
	}

	msg2, _, _, err := hs.WriteMessage(nil, nil)
	if err != nil {
		t.Logf("responder build msg2: %v", err)
		return
	}
	if err := writeNoiseFrame(conn, msg2); err != nil {
		t.Logf("responder send msg2: %v", err)
		return
	}
}

func generateStaticKey(t *testing.T) noise.DHKey {
	t.Helper()
	cs := noise.NewCipherSuite(noise.DH25519, noise.CipherChaChaPoly, noise.HashBLAKE2s)
	key, err := cs.GenerateKeypair(rand.Reader)
	require.NoError(t, err)
	return key
}
