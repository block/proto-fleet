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

// SiteScopeFor projects the user's site-level authority for one
// permission key into a shape a store-layer query can consume, so list
// endpoints can filter rows in SQL instead of fetching everything and
// dropping unreadable rows per-item.
//
// Two shapes, following the narrowing semantics on the type doc:
//
//   - orgWide true: the user holds key at org scope. sites is the
//     DENYLIST — the sites where a site-scope assignment narrows key
//     away. Every other site is allowed.
//   - orgWide false: no org-scope grant. sites is the ALLOWLIST — the
//     sites whose site-scope assignments grant key. Empty means the
//     user can act nowhere; callers should short-circuit to an empty
//     result without querying.
//
// sites is sorted for deterministic query parameters and test
// assertions. A nil receiver denies everything: (false, nil).
func (e *EffectivePermissions) SiteScopeFor(key string) (orgWide bool, sites []int64) {
	if e == nil {
		return false, nil
	}
	orgWide = e.orgScope[key]
	for siteID, perms := range e.bySite {
		if orgWide && !perms[key] {
			// Site-scope assignment narrows the org grant away here.
			sites = append(sites, siteID)
		}
		if !orgWide && perms[key] {
			// Site-scope assignment grants key here.
			sites = append(sites, siteID)
		}
	}
	sort.Slice(sites, func(i, j int) bool { return sites[i] < sites[j] })
	return orgWide, sites
}

// HasOrgWide reports whether key is held at org scope and remains effective at
// every site where this user has a narrower site-scope assignment.
func (e *EffectivePermissions) HasOrgWide(key string) bool {
	if e == nil || !e.orgScope[key] {
		return false
	}
	for siteID := range e.bySite {
		if !e.Has(key, ResourceContext{SiteID: &siteID}) {
			return false
		}
	}
	return true
}

// StrictlyDominates reports whether this EffectivePermissions
// subsumes other AND holds at least one (key, scope) pair other does
// not — i.e., a proper superset. Used as the no-role:manage branch of
// the user-management parity check, where equal permission sets must
// be rejected so peer-tier accounts can't manage each other.
func (e *EffectivePermissions) StrictlyDominates(other *EffectivePermissions) bool {
	return other.IsSubsumedBy(e) && !e.IsSubsumedBy(other)
}

// IsSubsumedBy reports whether every (permission key, resource scope)
// the receiver can act under is also actionable by other. Scope-aware:
// a permission held only at site 7 is *not* subsumed by the same key
// held only at org scope (and vice versa), and — crucially —
// other-side narrowing is treated as reducing coverage for the
// receiver's org-scope keys at every site where other narrows. Without
// that, a caller with org-scope SUPER_ADMIN plus a restrictive
// FIELD_TECH assignment at site 7 would falsely subsume an org ADMIN
// target whose ADMIN authority still applies at site 7 (the target
// has no narrowing there, so its org-scope grants are live).
//
// The check covers two domains:
//
//  1. The org-scope action space. Every org-scope key the receiver
//     holds must be held by other at org scope.
//
//  2. The site-scope action space at every site where either party
//     narrows. For each such site s, the receiver's effective key set
//     at s is its site-scope set if it has one (narrowing applies),
//     otherwise its org-scope set; every such key must be actionable
//     by other at s via the same narrowing-aware Has().
//
// Sites where neither party narrows are covered by the org-scope pass.
func (e *EffectivePermissions) IsSubsumedBy(other *EffectivePermissions) bool {
	if e == nil {
		return true
	}
	for key := range e.orgScope {
		if !other.Has(key, ResourceContext{}) {
			return false
		}
	}

	relevantSites := make(map[int64]struct{}, len(e.bySite)+len(other.bySite))
	for s := range e.bySite {
		relevantSites[s] = struct{}{}
	}
	for s := range other.bySite {
		relevantSites[s] = struct{}{}
	}
	for s := range relevantSites {
		sid := s
		rc := ResourceContext{SiteID: &sid}
		keys := e.bySite[s]
		if keys == nil {
			// e has no narrowing at this site, so its org-scope grants
			// apply here and become the action set that other must
			// cover at this site.
			keys = e.orgScope
		}
		for key := range keys {
			if !other.Has(key, rc) {
				return false
			}
		}
	}
	return true
}

// Keys returns every distinct default-scope permission key the user holds,
// sorted lexicographically. UserInfo.permissions is projected from this for
// client UI gates that map to org-scoped RPCs. Narrower resource-scoped grants
// should be exposed separately so site-only authority is not confused for
// org-wide authority.
func (e *EffectivePermissions) Keys() []string {
	if e == nil {
		return nil
	}
	out := make([]string, 0, len(e.orgScope))
	for k := range e.orgScope {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
