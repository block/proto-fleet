// Package runtimejobs provides lifecycle management for Fleet background jobs.
package runtimejobs

import "context"

// Lifecycle is implemented by independently activatable background work.
//
// The context passed to Start defines the activation lifetime, not only the
// startup operation. Implementations must stop activation-owned work when that
// context is canceled. Callers must still invoke Stop, which requests the same
// cancellation when necessary, honors its own context while waiting, fully
// drains before returning nil, and allows a later Start. A failed Start must
// leave the lifecycle stopped and safe to start again.
type Lifecycle interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}
