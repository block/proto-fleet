package authz

import "context"

// AuthorizedPlacement records the source and destination sites a rack-write
// caller was authorized against in the handler, so the service can bind the
// authorization to the locked write and fail closed if the sites changed
// between the handler's unlocked resolution and the transaction.
//
// The handler resolves the rack's current site and the destination site with
// unlocked reads, checks site:manage against both, then stashes the observed
// pair via WithAuthorizedPlacement. Under the write lock the service re-reads
// the authoritative current/target sites and rejects the write unless they
// still match — closing the window where a concurrent building- or rack-move
// would otherwise let a placement commit under authorization granted for a
// different site.
type AuthorizedPlacement struct {
	// CurrentSiteID is the rack's site the caller was authorized to move OUT
	// of (nil for a new or site-less rack). TargetSiteID is the site the
	// caller was authorized to move INTO (nil for an unassign or a site-less
	// building).
	CurrentSiteID *int64
	TargetSiteID  *int64
}

type authorizedPlacementCtxKey struct{}

// WithAuthorizedPlacement returns a derived context carrying the sites the
// caller was authorized against. The handler sets this after its site:manage
// checks pass; internal/trusted callers that bypass handler authorization
// simply never set it.
func WithAuthorizedPlacement(ctx context.Context, ap AuthorizedPlacement) context.Context {
	return context.WithValue(ctx, authorizedPlacementCtxKey{}, ap)
}

// AuthorizedPlacementFromContext returns the authorized placement and true
// when one was stashed. A false second return means no handler authorization
// is bound to this request (an internal/trusted caller), and the service
// leaves its placement write unguarded by this check.
func AuthorizedPlacementFromContext(ctx context.Context) (AuthorizedPlacement, bool) {
	ap, ok := ctx.Value(authorizedPlacementCtxKey{}).(AuthorizedPlacement)
	return ap, ok
}
