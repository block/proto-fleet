package sv2

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTCPDial_ReachesListeningServer(t *testing.T) {
	// Start a listener on a random port and confirm TCPDial connects.
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = lis.Close() })

	go func() {
		for {
			conn, err := lis.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	url := "stratum2+tcp://" + lis.Addr().String()

	ok, err := TCPDial(context.Background(), url, time.Second)
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestTCPDial_UnreachableAddressReturnsError(t *testing.T) {
	// 127.0.0.1:1 is reserved; a stock machine rejects connections.
	ok, err := TCPDial(context.Background(), "stratum2+tcp://127.0.0.1:1", 250*time.Millisecond)
	require.Error(t, err)
	assert.False(t, ok)
}

func TestTCPDial_RejectsMissingPort(t *testing.T) {
	ok, err := TCPDial(context.Background(), "stratum2+tcp://pool.example.com", time.Second)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "explicit port")
	assert.False(t, ok)
}

func TestTCPDial_RejectsUnsupportedScheme(t *testing.T) {
	ok, err := TCPDial(context.Background(), "http://pool.example.com:80", time.Second)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported stratum URL scheme")
	assert.False(t, ok)
}

func TestTCPDial_AcceptsSV1Schemes(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = lis.Close() })
	go func() {
		for {
			conn, acceptErr := lis.Accept()
			if acceptErr != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	for _, scheme := range []string{"stratum+tcp://", "stratum+ssl://", "stratum+ws://"} {
		t.Run(scheme, func(t *testing.T) {
			ok, err := TCPDial(context.Background(), scheme+lis.Addr().String(), time.Second)
			require.NoError(t, err)
			assert.True(t, ok)
		})
	}
}

// TestTCPDial_AcceptsSV2URLWithAuthorityPubkey covers the canonical
// Braiins format — `stratum2+tcp://host:port/<pubkey>`. The path
// segment is informational at the dial layer; net/url parses the
// authority component cleanly and we ignore the path. The dial must
// succeed identically whether or not the pubkey suffix is present.
func TestTCPDial_AcceptsSV2URLWithAuthorityPubkey(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = lis.Close() })
	go func() {
		for {
			conn, acceptErr := lis.Accept()
			if acceptErr != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	url := "stratum2+tcp://" + lis.Addr().String() + "/u95GEReVMjK6k5YqiSFNqqTnKU4ypU2Wm8awa6tmbmDmk1bWt"
	ok, err := TCPDial(context.Background(), url, time.Second)
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestTCPDial_HonorsTimeout(t *testing.T) {
	// Pick an address that will hang (TEST-NET-1 RFC5737) and ensure
	// TCPDial respects its timeout rather than blocking indefinitely.
	start := time.Now()
	ok, err := TCPDial(context.Background(), "stratum2+tcp://192.0.2.1:34254", 150*time.Millisecond)
	elapsed := time.Since(start)
	require.Error(t, err)
	assert.False(t, ok)
	assert.Less(t, elapsed, 500*time.Millisecond, "timeout must cap dial duration")
}
