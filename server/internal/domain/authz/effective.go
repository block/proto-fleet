package authz

import "sort"

// ScopeType is the assignment's scope discriminator. The DB column is a
// VARCHAR with a CHECK constraint accepting only these two values;
// building-scope is deferred to a follow-up plan.
type ScopeType string

const (
	ScopeOrg  ScopeType = "org"
	ScopeSite ScopeType = "site"
)

// Assignment is one row from user_organization_role joined against
// role and role_permission, materialized as a flat in-memory value.
// Carries the assignment's identity, scope, and the set of permission
// keys its role grants. Role identity (id, name, builtin_key) is not
// needed at decision time and is deliberately omitted — the resolver
// only consults permission_key + scope.
type Assignment struct {
	AssignmentID int64
	ScopeType    ScopeType
	// SiteID is non-nil only when ScopeType is ScopeSite.
	SiteID      *int64
	Permissions []string
}

// ResourceContext is the per-request input to Has(). SiteID nil means
// the action is org-scoped (e.g. user:manage); a non-nil SiteID is the
// site the action targets. Building-level scoping is deferred; when it
// lands, this struct gains a BuildingID field and Has() gains a third
// containment level.
type ResourceContext struct {
	SiteID *int64
}

// EffectivePermissions is the per-request, immutable snapshot of one
// user's authorization state within one organization. The resolver
// builds it from a single DB query; the middleware queries it via
// Has() to gate handler entry.
//
// Narrowing semantics: when a user holds both an org-scope and a
// site-scope assignment in the same org, the site-scope assignment
// overrides the org grant *at that site*. The org grant continues to
// apply at every other site. This lets an admin grant broad org-level
// access and then narrow a user at a specific site by adding a smaller
// site-scoped role, without first removing the org-scoped assignment.
// Org-scoped actions (no site context in the request) are never
// shadowed by site-scope grants — there is no site key to narrow on.
type EffectivePermissions struct {
	// orgScope is the union of permission keys across every org-scope
	// assignment the user holds in this org.
	orgScope map[string]bool

	// bySite[siteID] is the union of permission keys across every
	// site-scope assignment the user holds at that site. The presence
	// of a key in bySite[X] (even an empty inner map) indicates the
	// user has at least one site-scope assignment at X, which is the
	// narrowing trigger.
	bySite map[int64]map[string]bool
}

// NewEffectivePermissions materializes an EffectivePermissions from a
// slice of Assignment rows. The slice can come from the
// ListEffectivePermissionsForUser sqlc query (grouped by assignment
// id by the resolver) or from a hand-built test fixture.
func NewEffectivePermissions(assignments []Assignment) *EffectivePermissions {
	out := &EffectivePermissions{
		orgScope: make(map[string]bool),
		bySite:   make(map[int64]map[string]bool),
	}
	for _, a := range assignments {
		switch a.ScopeType {
		case ScopeOrg:
			for _, k := range a.Permissions {
				out.orgScope[k] = true
			}
		case ScopeSite:
			if a.SiteID == nil {
				// Defensive: the DB CHECK forbids this combination,
				// but if a bad row somehow surfaces in-memory we skip
				// it rather than crash. The triple is non-actionable.
				continue
			}
			perms := out.bySite[*a.SiteID]
			if perms == nil {
				perms = make(map[string]bool)
				out.bySite[*a.SiteID] = perms
			}
			for _, k := range a.Permissions {
				perms[k] = true
			}
		}
	}
	return out
}

// Has reports whether the user is allowed to perform the named action
// against the given resource context. See the type doc for narrowing
// semantics.
//
// Empty EffectivePermissions deny everything; this is the fail-closed
// default the middleware relies on when the resolver returns no rows
// (deactivated user, soft-deleted assignments, no rows in the join).
func (e *EffectivePermissions) Has(key string, rc ResourceContext) bool {
	if e == nil {
		return false
	}

	if rc.SiteID == nil {
		// Org-scoped action: only org-scope grants can satisfy it.
		// Site-scope assignments have no site to "narrow on" for this
		// request, so they cannot grant the action.
		return e.orgScope[key]
	}

	// Site-scoped action. If the user has ANY site-scope assignment
	// at this site, narrowing kicks in: only the union of site-scope
	// permissions at this site is consulted; the org-scope grant is
	// shadowed at this site. Otherwise (no site-scope assignment at
	// this site), fall back to the org-scope grant.
	if siteKeys, ok := e.bySite[*rc.SiteID]; ok {
		return siteKeys[key]
	}
	return e.orgScope[key]
}

// FlatKeys returns every distinct permission key the user holds across
// every assignment, sorted lexicographically. UserInfo.permissions is
// projected from this for the client's coarse "has the permission
// anywhere" gating; the server still enforces scope via Has() on the
// actual call.
func (e *EffectivePermissions) FlatKeys() []string {
	if e == nil {
		return nil
	}
	seen := make(map[string]bool)
	for k := range e.orgScope {
		seen[k] = true
	}
	for _, siteKeys := range e.bySite {
		for k := range siteKeys {
			seen[k] = true
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
