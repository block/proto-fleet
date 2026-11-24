package db

import (
	"context"
	"database/sql/driver"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type failingObservedConnector struct {
	driver.Connector
	err error
}

func (c failingObservedConnector) Connect(context.Context) (driver.Conn, error) { return nil, c.err }

func TestConnectionObserverPreservesFailure(t *testing.T) {
	want := errors.New("connection refused")
	calls := 0
	c := observedConnector{
		Connector: failingObservedConnector{err: want},
		observe:   func(_ context.Context, err error) { calls++; require.ErrorIs(t, err, want) },
	}
	for range 2 {
		conn, err := c.Connect(context.Background())
		require.Nil(t, conn)
		require.ErrorIs(t, err, want)
	}
	require.Equal(t, 2, calls, "replacement attempts must also be observed")
}

func TestConnectToDatabaseObservedReportsDialFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	address := listener.Addr().String()
	require.NoError(t, listener.Close())

	observed := make(chan error, 1)
	conn, err := connectToDatabaseObserved(&Config{
		Address: address, Name: "fleet", Username: "fleet", SSLMode: "disable",
	}, func(_ context.Context, err error) { observed <- err })
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	pingErr := conn.PingContext(ctx)
	require.Error(t, pingErr)
	select {
	case err := <-observed:
		require.Error(t, err)
		require.ErrorIs(t, pingErr, err)
	case <-ctx.Done():
		t.Fatal("connection observer did not receive the dial failure")
	}
}
