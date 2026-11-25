package sdk

import (
	"context"
	"net"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDeviceDialEvidenceDistinguishesConnectionFromReadAndCancellation(t *testing.T) {
	ctx := t.Context()
	dialer := &net.Dialer{Timeout: time.Second}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	address := listener.Addr().String()

	conn, err := dialer.DialContext(ctx, "tcp", address)
	require.NoError(t, err)
	peer, err := listener.Accept()
	require.NoError(t, err)
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Millisecond)))
	_, readErr := conn.Read(make([]byte, 1))
	require.Error(t, readErr)
	require.False(t, DeviceProbeFailed(WithDeviceDialFailure(ctx, readErr)))
	require.NoError(t, peer.Close())
	require.NoError(t, conn.Close())
	require.NoError(t, listener.Close())

	_, refused := dialer.DialContext(ctx, "tcp", address)
	require.Error(t, refused)
	require.True(t, DeviceProbeFailed(WithDeviceDialFailure(ctx, refused)))

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, canceledErr := dialer.DialContext(canceled, "tcp", address)
	require.ErrorIs(t, canceledErr, context.Canceled)
	require.False(t, DeviceProbeFailed(WithDeviceDialFailure(canceled, canceledErr)))
	require.False(t, DeviceProbeFailed(WithDeviceDialFailure(canceled, refused)))
}

func TestDeviceDialEvidenceAcceptsIndependentDialTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	dialer := net.Dialer{
		Timeout: 25 * time.Millisecond,
		ControlContext: func(dialCtx context.Context, _, _ string, _ syscall.RawConn) error {
			// Delay the local socket until the dialer's own deadline expires.
			<-dialCtx.Done()
			return nil
		},
	}
	_, err := dialer.DialContext(ctx, "tcp4", "127.0.0.1:1")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.NoError(t, ctx.Err())
	require.True(t, DeviceProbeFailed(WithDeviceDialFailure(ctx, err)))

	expired, stop := context.WithDeadline(ctx, time.Now().Add(-time.Second))
	defer stop()
	require.False(t, DeviceProbeFailed(WithDeviceDialFailure(expired, err)))
}

func TestDeviceDialEvidenceExcludesDNSTimeout(t *testing.T) {
	err := &net.OpError{Op: "dial", Net: "tcp", Err: &net.DNSError{IsTimeout: true}}
	require.True(t, err.Timeout())
	require.False(t, DeviceProbeFailed(WithDeviceDialFailure(t.Context(), err)))
}
