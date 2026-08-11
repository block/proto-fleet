package authz

import "context"

// AuthorizedPlacement records the source and destination sites the handler
// checked site:manage against, so the service can re-read them under the write
// lock and fail closed if a concurrent move changed them since authorization.
type AuthorizedPlacement struct {
	// CurrentSiteID: site moved OUT of (nil for a new/site-less rack).
	// TargetSiteID: site moved INTO (nil for an unassign or site-less building).
	CurrentSiteID *int64
	TargetSiteID  *int64
}

type authorizedPlacementCtxKey struct{}

// WithAuthorizedPlacement stashes the authorized sites on the context. The
// handler sets it after its site:manage checks pass; internal/trusted callers
// never set it.
func WithAuthorizedPlacement(ctx context.Context, ap AuthorizedPlacement) context.Context {
	return context.WithValue(ctx, authorizedPlacementCtxKey{}, ap)
}

// AuthorizedPlacementFromContext returns the stashed placement, or false when
// none is bound (an internal/trusted caller), leaving the write unguarded.
func AuthorizedPlacementFromContext(ctx context.Context) (AuthorizedPlacement, bool) {
	ap, ok := ctx.Value(authorizedPlacementCtxKey{}).(AuthorizedPlacement)
	return ap, ok
}
