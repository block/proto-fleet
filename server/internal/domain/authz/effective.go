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
// semantics. An empty / nil EffectivePermissions denies everything.
func (e *EffectivePermissions) Has(key string, rc ResourceContext) bool {
	if e == nil {
		return false
	}
	if rc.SiteID == nil {
		return e.orgScope[key]
	}
	if siteKeys, ok := e.bySite[*rc.SiteID]; ok {
		return siteKeys[key]
	}
	return e.orgScope[key]
}

// StrictlyDominates reports whether this EffectivePermissions
// subsumes other AND holds at least one (key, scope) pair other does
// not — i.e., a proper superset. Used as the no-role:manage branch of
// the user-management parity check, where equal permission sets must
// be rejected so peer-tier accounts can't manage each other.
func (e *EffectivePermissions) StrictlyDominates(other *EffectivePermissions) bool {
	return other.IsSubsumedBy(e) && !e.IsSubsumedBy(other)
}

// IsSubsumedBy reports whether every (permission key, scope) pair this
// EffectivePermissions holds is also held by other. Scope is part of
// the comparison: a permission held only at site 7 is *not* subsumed by
// the same key held only at org scope (and vice versa), because
// narrowing semantics make them functionally different grants. This is
// the predicate the auth domain layer uses to gate user-management
// mutations — a caller can mutate a target only when the caller's
// effective permissions subsume the target's at the same scope, so the
// caller could not gain capabilities by hijacking the target's session.
func (e *EffectivePermissions) IsSubsumedBy(other *EffectivePermissions) bool {
	if e == nil {
		return true
	}
	for key := range e.orgScope {
		if !other.Has(key, ResourceContext{}) {
			return false
		}
	}
	for siteID, perms := range e.bySite {
		sid := siteID
		rc := ResourceContext{SiteID: &sid}
		for key := range perms {
			if !other.Has(key, rc) {
				return false
			}
		}
	}
	return true
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
