package authz

import "context"

// AuthorizedSiteScope captures the site coverage authorized by a handler.
// When OrgWide is true, SiteIDs is a denylist created by narrower assignments.
// Otherwise, SiteIDs is the explicit allowlist. Nil sites represent resources
// that are not assigned to any site and require organization-scope authority.
type AuthorizedSiteScope struct {
	OrgWide bool
	SiteIDs []int64
}

// Allows reports whether a resource at siteID is covered by this scope.
func (s AuthorizedSiteScope) Allows(siteID *int64) bool {
	if siteID == nil {
		return s.OrgWide
	}
	listed := false
	for _, id := range s.SiteIDs {
		if id == *siteID {
			listed = true
			break
		}
	}
	if s.OrgWide {
		return !listed
	}
	return listed
}

type authorizedSiteScopeCtxKey struct{}

// WithAuthorizedSiteScope binds the handler-authorized scope to a domain call
// so locked resources can be revalidated transactionally.
func WithAuthorizedSiteScope(ctx context.Context, scope AuthorizedSiteScope) context.Context {
	return context.WithValue(ctx, authorizedSiteScopeCtxKey{}, scope)
}

// AuthorizedSiteScopeFromContext returns the bound scope. Internal/trusted
// callers omit it and retain the existing unguarded domain API.
func AuthorizedSiteScopeFromContext(ctx context.Context) (AuthorizedSiteScope, bool) {
	scope, ok := ctx.Value(authorizedSiteScopeCtxKey{}).(AuthorizedSiteScope)
	return scope, ok
}
