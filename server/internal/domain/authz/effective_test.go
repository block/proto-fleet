package authz_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/authz"
)

// scope helpers — concise builders to keep test tables readable.

func orgScope(keys ...string) authz.Assignment {
	return authz.Assignment{
		AssignmentID: 1,
		ScopeType:    authz.ScopeOrg,
		Permissions:  keys,
	}
}

func siteScope(siteID int64, keys ...string) authz.Assignment {
	return authz.Assignment{
		AssignmentID: 2,
		ScopeType:    authz.ScopeSite,
		SiteID:       &siteID,
		Permissions:  keys,
	}
}

// orgResource returns a ResourceContext with no site (org-scoped action like user:manage).
func orgResource() authz.ResourceContext { return authz.ResourceContext{} }

// site returns a ResourceContext at the given site.
func site(id int64) authz.ResourceContext { return authz.ResourceContext{SiteID: &id} }

func TestEffective_OrgScopeAllowsEverywhere(t *testing.T) {
	eff := authz.NewEffectivePermissions([]authz.Assignment{
		orgScope(authz.PermMinerReboot, authz.PermUserManage),
	})

	require.True(t, eff.Has(authz.PermMinerReboot, orgResource()), "org-scope grant satisfies org-scoped action")
	require.True(t, eff.Has(authz.PermMinerReboot, site(42)), "org-scope grant satisfies site-scoped action at any site")
	require.True(t, eff.Has(authz.PermUserManage, orgResource()))
}

func TestEffective_SiteScopeOnlyAllowsAtThatSite(t *testing.T) {
	eff := authz.NewEffectivePermissions([]authz.Assignment{
		siteScope(1,
			authz.PermFleetRead, authz.PermMinerBlinkLED),
	})

	require.True(t, eff.Has(authz.PermMinerBlinkLED, site(1)))
	require.False(t, eff.Has(authz.PermMinerBlinkLED, site(2)),
		"site-scope grant must NOT satisfy a request at a different site")
	require.False(t, eff.Has(authz.PermMinerBlinkLED, orgResource()),
		"site-scope grant must NOT satisfy an org-scoped action (no site context)")
}

func TestEffective_OrgScopedActionRequiresOrgScopeGrant(t *testing.T) {
	// FIELD_TECH at Site-A holds user:manage by some mistake. user:manage is
	// an org-scoped action — a site-scope grant cannot satisfy it because
	// there's no site context to match against.
	eff := authz.NewEffectivePermissions([]authz.Assignment{
		siteScope(1, authz.PermUserManage),
	})
	require.False(t, eff.Has(authz.PermUserManage, orgResource()),
		"org-scoped action is only satisfied by an org-scope assignment")
}

func TestEffective_NarrowingSiteScopeOverridesOrgScope(t *testing.T) {
	// ADMIN at org-scope holds miner:reboot. FIELD_TECH at Site-A does NOT
	// hold miner:reboot. The user has both assignments. Narrowing
	// semantics: at Site-A, the site-scope assignment wins, so
	// miner:reboot is denied at Site-A. At Site-B (no narrower
	// assignment), the org grant applies and miner:reboot is allowed.
	eff := authz.NewEffectivePermissions([]authz.Assignment{
		orgScope(authz.PermFleetRead, authz.PermMinerRead, authz.PermMinerReboot),
		siteScope(1,
			authz.PermFleetRead, authz.PermMinerRead, authz.PermMinerBlinkLED),
	})

	require.False(t, eff.Has(authz.PermMinerReboot, site(1)),
		"narrowing: site-scope FIELD_TECH at Site-1 overrides org-scope ADMIN at that site")
	require.True(t, eff.Has(authz.PermMinerReboot, site(2)),
		"narrowing: org-scope ADMIN still applies at sites where there is no narrower assignment")
	require.True(t, eff.Has(authz.PermMinerBlinkLED, site(1)),
		"blink_led is in FIELD_TECH; both grants in effect at site 1 (union within the narrower one)")
}

func TestEffective_NarrowingOrgScopeActionNotShadowed(t *testing.T) {
	// Org-scoped actions (user:manage, role:manage) are never shadowed by
	// site-scope assignments — there's no site context to "narrow" to.
	eff := authz.NewEffectivePermissions([]authz.Assignment{
		orgScope(authz.PermUserManage),
		siteScope(1, authz.PermFleetRead),
	})
	require.True(t, eff.Has(authz.PermUserManage, orgResource()),
		"org-scope action is satisfied by the org-scope assignment regardless of site-scope rows")
}

func TestEffective_MultipleSiteAssignmentsUnionAtTheirOwnSites(t *testing.T) {
	// User has ADMIN @ Site-A and FIELD_TECH @ Site-B (no org-scope row).
	// miner:reboot is in ADMIN's seed but not FIELD_TECH's.
	eff := authz.NewEffectivePermissions([]authz.Assignment{
		siteScope(1,
			authz.PermFleetRead, authz.PermMinerRead, authz.PermMinerReboot),
		siteScope(2,
			authz.PermFleetRead, authz.PermMinerRead, authz.PermMinerBlinkLED),
	})

	require.True(t, eff.Has(authz.PermMinerReboot, site(1)))
	require.False(t, eff.Has(authz.PermMinerReboot, site(2)))
	require.True(t, eff.Has(authz.PermMinerBlinkLED, site(2)))
	require.False(t, eff.Has(authz.PermMinerBlinkLED, site(1)),
		"ADMIN's seed does NOT include miner:blink_led")
}

func TestEffective_EmptyAssignmentsDenyEverything(t *testing.T) {
	eff := authz.NewEffectivePermissions(nil)
	require.False(t, eff.Has(authz.PermFleetRead, orgResource()))
	require.False(t, eff.Has(authz.PermMinerBlinkLED, site(1)))
}

func TestEffective_UnknownPermissionDenied(t *testing.T) {
	eff := authz.NewEffectivePermissions([]authz.Assignment{
		orgScope(authz.PermFleetRead),
	})
	require.False(t, eff.Has("synthetic:not_in_catalog", orgResource()))
}

func TestEffective_FlatPermissionsForUserInfo(t *testing.T) {
	// UserInfo.permissions is described in the plan as "the flat union of
	// permission keys across all assignments." Test that the projection is
	// deterministic and dedupes.
	eff := authz.NewEffectivePermissions([]authz.Assignment{
		orgScope(authz.PermFleetRead, authz.PermMinerRead),
		siteScope(1,
			authz.PermFleetRead, authz.PermMinerBlinkLED),
	})
	got := eff.FlatKeys()
	require.Equal(t, []string{authz.PermFleetRead, authz.PermMinerBlinkLED, authz.PermMinerRead}, got)
}

// FIELD_TECH on the AE (a tech can call BlinkLED but not Reboot).
func TestEffective_FieldTechCanBlinkButNotReboot(t *testing.T) {
	eff := authz.NewEffectivePermissions([]authz.Assignment{
		orgScope(
			authz.PermFleetRead, authz.PermMinerRead, authz.PermMinerBlinkLED,
			authz.PermMinerDownloadLogs, authz.PermRackRead, authz.PermRackManage),
	})

	require.True(t, eff.Has(authz.PermMinerBlinkLED, site(7)))
	require.False(t, eff.Has(authz.PermMinerReboot, site(7)))
	require.False(t, eff.Has(authz.PermUserManage, orgResource()))
}
