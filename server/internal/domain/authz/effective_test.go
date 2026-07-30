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

func TestEffective_HasOrgWideHonorsSiteNarrowing(t *testing.T) {
	cases := []struct {
		name        string
		assignments []authz.Assignment
		want        bool
	}{
		{
			name:        "plain org grant is org-wide",
			assignments: []authz.Assignment{orgScope(authz.PermCurtailmentManage)},
			want:        true,
		},
		{
			name: "matching narrowed site grant stays org-wide",
			assignments: []authz.Assignment{
				orgScope(authz.PermCurtailmentManage),
				siteScope(1, authz.PermCurtailmentManage),
			},
			want: true,
		},
		{
			name: "narrowed site without permission is not org-wide",
			assignments: []authz.Assignment{
				orgScope(authz.PermCurtailmentManage),
				siteScope(1),
			},
			want: false,
		},
		{
			name:        "site-only grant is not org-wide",
			assignments: []authz.Assignment{siteScope(1, authz.PermCurtailmentManage)},
			want:        false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			eff := authz.NewEffectivePermissions(tc.assignments)
			require.Equal(t, tc.want, eff.HasOrgWide(authz.PermCurtailmentManage))
		})
	}
}

func TestEffective_SiteScopeFor(t *testing.T) {
	cases := []struct {
		name        string
		assignments []authz.Assignment
		wantOrgWide bool
		wantSites   []int64
	}{
		{
			name:        "org grant with no narrowing: org-wide, empty denylist",
			assignments: []authz.Assignment{orgScope(authz.PermSiteRead)},
			wantOrgWide: true,
			wantSites:   nil,
		},
		{
			name: "org grant narrowed away at two sites: sorted denylist",
			assignments: []authz.Assignment{
				orgScope(authz.PermSiteRead),
				siteScope(9), // zero-permission assignment still narrows
				siteScope(3, authz.PermFleetRead),
			},
			wantOrgWide: true,
			wantSites:   []int64{3, 9},
		},
		{
			name: "org grant with matching site grant: site not in denylist",
			assignments: []authz.Assignment{
				orgScope(authz.PermSiteRead),
				siteScope(3, authz.PermSiteRead),
			},
			wantOrgWide: true,
			wantSites:   nil,
		},
		{
			name: "site-only grants: sorted allowlist of granting sites",
			assignments: []authz.Assignment{
				siteScope(5, authz.PermSiteRead),
				siteScope(3, authz.PermSiteRead),
				siteScope(9, authz.PermFleetRead), // different key: not readable
			},
			wantOrgWide: false,
			wantSites:   []int64{3, 5},
		},
		{
			name:        "no assignments: deny everything",
			assignments: nil,
			wantOrgWide: false,
			wantSites:   nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			eff := authz.NewEffectivePermissions(tc.assignments)
			orgWide, sites := eff.SiteScopeFor(authz.PermSiteRead)
			require.Equal(t, tc.wantOrgWide, orgWide)
			require.Equal(t, tc.wantSites, sites)
		})
	}
}

func TestEffective_SiteScopeForNilReceiver(t *testing.T) {
	var eff *authz.EffectivePermissions
	orgWide, sites := eff.SiteScopeFor(authz.PermSiteRead)
	require.False(t, orgWide, "nil EffectivePermissions denies everything")
	require.Nil(t, sites)
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

// Codex security regression: an empty site-scope assignment (a role
// with zero permissions) MUST narrow against the user's broader
// org-scope grant at that site. Without recording bySite[siteID],
// narrowing would collapse and the org grant would silently apply at
// the site the empty role was meant to lock down.
func TestEffective_ZeroPermissionSiteAssignmentStillNarrows(t *testing.T) {
	siteOne := int64(1)
	emptySite1 := authz.Assignment{
		AssignmentID: 99,
		ScopeType:    authz.ScopeSite,
		SiteID:       &siteOne,
		Permissions:  nil,
	}

	eff := authz.NewEffectivePermissions([]authz.Assignment{
		orgScope(authz.PermFleetRead, authz.PermMinerRead, authz.PermMinerReboot),
		emptySite1,
	})

	require.False(t, eff.Has(authz.PermMinerReboot, site(1)),
		"empty site-scope assignment at site 1 must narrow the user's org-scope grant there")
	require.False(t, eff.Has(authz.PermFleetRead, site(1)),
		"narrowing applies to every action key when the narrower assignment grants nothing")
	require.True(t, eff.Has(authz.PermMinerReboot, site(2)),
		"org grant still applies at site 2 — no narrower assignment there")
	require.True(t, eff.Has(authz.PermMinerReboot, orgResource()),
		"org-scope action satisfied by the org-scope ADMIN; site narrowing only applies to site-targeted requests")
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

func TestEffective_KeysForUserInfo(t *testing.T) {
	// UserInfo.permissions is the default/org projection the client uses for
	// org-scoped RPC gates. Site-only permissions must not leak into it.
	eff := authz.NewEffectivePermissions([]authz.Assignment{
		orgScope(authz.PermFleetRead, authz.PermMinerRead),
		siteScope(1,
			authz.PermFleetRead, authz.PermMinerBlinkLED),
	})
	got := eff.Keys()
	require.Equal(t, []string{authz.PermFleetRead, authz.PermMinerRead}, got)
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

func TestEffective_StrictlyDominates(t *testing.T) {
	t.Parallel()

	larger := authz.NewEffectivePermissions([]authz.Assignment{
		orgScope(authz.PermUserRead, authz.PermUserManage, authz.PermRoleManage),
	})
	smaller := authz.NewEffectivePermissions([]authz.Assignment{
		orgScope(authz.PermUserRead, authz.PermUserManage),
	})
	equalToSmaller := authz.NewEffectivePermissions([]authz.Assignment{
		orgScope(authz.PermUserRead, authz.PermUserManage),
	})

	require.True(t, larger.StrictlyDominates(smaller),
		"a proper superset strictly dominates")
	require.False(t, smaller.StrictlyDominates(larger),
		"a proper subset does not dominate its superset")
	require.False(t, smaller.StrictlyDominates(equalToSmaller),
		"equality is not strict domination (this is what blocks peer-tier user management)")
}

func TestEffective_IsSubsumedBy(t *testing.T) {
	t.Parallel()

	superAdminOrg := authz.NewEffectivePermissions([]authz.Assignment{
		orgScope(authz.PermUserRead, authz.PermUserManage, authz.PermRoleManage, authz.PermMinerReboot),
	})
	adminOrg := authz.NewEffectivePermissions([]authz.Assignment{
		orgScope(authz.PermUserRead, authz.PermUserManage, authz.PermMinerReboot),
	})
	adminOrgPlusSiteRoleManage := authz.NewEffectivePermissions([]authz.Assignment{
		orgScope(authz.PermUserRead, authz.PermUserManage, authz.PermMinerReboot),
		siteScope(7, authz.PermRoleManage),
	})
	siteOnlyMinerReboot := authz.NewEffectivePermissions([]authz.Assignment{
		siteScope(7, authz.PermMinerReboot),
	})

	require.True(t, adminOrg.IsSubsumedBy(superAdminOrg),
		"super_admin's catalog covers admin's org-scope keys")
	require.False(t, superAdminOrg.IsSubsumedBy(adminOrg),
		"admin lacks role:manage that super_admin holds at org scope")
	require.False(t, superAdminOrg.IsSubsumedBy(adminOrgPlusSiteRoleManage),
		"a caller's site-scoped role:manage must NOT subsume a target's org-scoped role:manage")
	require.True(t, siteOnlyMinerReboot.IsSubsumedBy(superAdminOrg),
		"org-scope grants narrow into the site scope when no site-scope assignment exists, so the org-only super_admin covers site-only miner:reboot")
	require.False(t, siteOnlyMinerReboot.IsSubsumedBy(authz.NewEffectivePermissions([]authz.Assignment{
		siteScope(7, authz.PermMinerRead), // narrowing at site 7 excludes miner:reboot
	})),
		"a site-scope assignment that narrows away the target's key must NOT subsume the target")
	require.True(t, authz.NewEffectivePermissions(nil).IsSubsumedBy(adminOrg),
		"empty target is trivially subsumed by any caller")

	// Caller-side narrowing must reduce coverage of the caller's
	// org-scope grants at the narrowed site. Otherwise a caller with
	// broad org-scope authority + a restrictive site assignment would
	// falsely subsume a target whose org-scope authority is still live
	// at that site.
	callerOrgFullPlusNarrowSite7 := authz.NewEffectivePermissions([]authz.Assignment{
		orgScope(authz.PermUserRead, authz.PermUserManage, authz.PermRoleManage, authz.PermMinerReboot),
		siteScope(7, authz.PermUserRead), // narrows site 7 to read-only
	})
	require.False(t, adminOrg.IsSubsumedBy(callerOrgFullPlusNarrowSite7),
		"caller's site-7 narrowing strips miner:reboot, but the org-scope ADMIN target's miner:reboot authority remains live at site 7 — caller must not subsume target")
}
