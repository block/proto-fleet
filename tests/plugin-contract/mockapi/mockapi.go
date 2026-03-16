// Package mockapi provides shared types for plugin contract test mock servers.
package mockapi

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
