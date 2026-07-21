// Package runtimejobs provides lifecycle management for Fleet background jobs.
package runtimejobs

import "context"

// Lifecycle is implemented by independently activatable background work.
//
// Start must return only after startup has succeeded or failed. A failed Start
// must leave the lifecycle stopped and safe to start again. Stop must honor its
// context, fully drain the activation before returning nil, and allow a later
// Start.
type Lifecycle interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}
