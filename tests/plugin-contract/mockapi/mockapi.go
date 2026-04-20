// Package mockapi provides shared types for plugin contract test mock servers.
package mockapi

import (
	"net"
	"testing"
	"time"
)

// ListenWithRetry binds to the given address, retrying briefly if the port is
// still in TIME_WAIT from a previous test. Tests sharing port 4028 / 80 need
// this because Go runs top-level tests sequentially but socket cleanup is async.
func ListenWithRetry(t testing.TB, addr string) net.Listener {
	t.Helper()
	for i := 0; i < 20; i++ {
		l, err := net.Listen("tcp", addr)
		if err == nil {
			return l
		}
		if i == 19 {
			t.Fatalf("failed to bind %s after retries: %v", addr, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	panic("unreachable")
}

// ConnBehavior controls how a mock server handles a connection for a given command.
type ConnBehavior int

const (
	BehaviorNormal    ConnBehavior = iota
	BehaviorTimeout                // accept connection but never respond
	BehaviorCloseConn              // close connection immediately after reading
)

// MockServer is the interface implemented by all plugin mock servers.
type MockServer interface {
	Host() string
	SetResponse(cmd string, data []byte)
	SetDefaultConnBehavior(behavior ConnBehavior)
	ResetOverrides()
}
