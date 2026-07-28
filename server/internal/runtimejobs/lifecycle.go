// Package runtimejobs provides lifecycle management for Fleet background jobs.
package runtimejobs

import "context"

// Lifecycle is implemented by independently activatable background work.
//
// The context passed to Start defines the activation's admission lifetime, not
// only the startup operation. When it is canceled, implementations must stop
// admitting new work and begin shutdown; already-admitted work may drain until
// Stop's context expires. Callers must still invoke Stop, which requests the
// same cancellation when necessary, honors its own context while waiting, fully
// drains before returning nil, and allows a later Start. A failed Start must
// leave the lifecycle stopped and safe to start again.
type Lifecycle interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}
