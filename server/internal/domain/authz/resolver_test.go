package authz_test

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/testutil"
)

// End-to-end resolver integration: seed an org, assign a user to its
// SUPER_ADMIN, then load the effective set and assert it contains the
// full catalog at org scope.
func TestResolver_OrgScopeSuperAdminGetsFullCatalog(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()

	orgID := insertTestOrganization(t, db)
	userID := insertTestUser(t, db)
	require.NoError(t, authz.Reconcile(ctx, db))
	superAdminID := getBuiltinRoleID(t, db, orgID, "SUPER_ADMIN")
	assignAssignment(t, db, userID, orgID, superAdminID, authz.ScopeOrg, sql.NullInt64{})

	resolver := authz.NewPermissionResolver(db)
	eff, err := resolver.LoadEffective(ctx, userID, orgID)
	require.NoError(t, err)

	// Spot-check a handful of catalog keys including org-scoped and
	// site-scoped actions. Comprehensive coverage lives in the
	// EffectivePermissions unit tests.
	for _, key := range []string{
		authz.PermFleetRead,
		authz.PermMinerReboot,
		authz.PermUserManage,
		authz.PermRoleManage,
	} {
		require.True(t, eff.Has(key, authz.ResourceContext{}), "SUPER_ADMIN must hold %q at org scope", key)
	}
	require.True(t, eff.Has(authz.PermMinerReboot, siteCtx(42)),
		"org-scope SUPER_ADMIN must allow miner:reboot at any site")
}

// FIELD_TECH at a single site: site-scoped grants stop at that site.
func TestResolver_SiteScopeFieldTechBoundsAtAssignedSite(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()

	orgID := insertTestOrganization(t, db)
	siteA := insertTestSite(t, db, orgID)
	siteB := insertTestSite(t, db, orgID)
	userID := insertTestUser(t, db)
	require.NoError(t, authz.Reconcile(ctx, db))
	fieldTechID := getBuiltinRoleID(t, db, orgID, "FIELD_TECH")
	assignAssignment(t, db, userID, orgID, fieldTechID, authz.ScopeSite,
		sql.NullInt64{Int64: siteA, Valid: true})

	resolver := authz.NewPermissionResolver(db)
	eff, err := resolver.LoadEffective(ctx, userID, orgID)
	require.NoError(t, err)

	require.True(t, eff.Has(authz.PermMinerBlinkLED, siteCtx(siteA)),
		"FIELD_TECH at site A should be able to blink LED at site A")
	require.False(t, eff.Has(authz.PermMinerBlinkLED, siteCtx(siteB)),
		"FIELD_TECH at site A must NOT have permissions at site B")
	require.False(t, eff.Has(authz.PermUserManage, authz.ResourceContext{}),
		"FIELD_TECH never gets user:manage; not in their seed")
}

// Narrowing end-to-end: org-scope ADMIN + site-scope FIELD_TECH at
// site A means miner:reboot is denied at site A (narrowing) but
// allowed at site B (org grant uncovered).
func TestResolver_NarrowingFromTwoAssignments(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()

	orgID := insertTestOrganization(t, db)
	siteA := insertTestSite(t, db, orgID)
	siteB := insertTestSite(t, db, orgID)
	userID := insertTestUser(t, db)
	require.NoError(t, authz.Reconcile(ctx, db))

	adminID := getBuiltinRoleID(t, db, orgID, "ADMIN")
	fieldTechID := getBuiltinRoleID(t, db, orgID, "FIELD_TECH")
	assignAssignment(t, db, userID, orgID, adminID, authz.ScopeOrg, sql.NullInt64{})
	assignAssignment(t, db, userID, orgID, fieldTechID, authz.ScopeSite,
		sql.NullInt64{Int64: siteA, Valid: true})

	resolver := authz.NewPermissionResolver(db)
	eff, err := resolver.LoadEffective(ctx, userID, orgID)
	require.NoError(t, err)

	require.False(t, eff.Has(authz.PermMinerReboot, siteCtx(siteA)),
		"narrowing: site-scope FIELD_TECH at site A overrides org-scope ADMIN there")
	require.True(t, eff.Has(authz.PermMinerReboot, siteCtx(siteB)),
		"narrowing: org-scope ADMIN still applies at site B (no narrower assignment)")
	require.True(t, eff.Has(authz.PermMinerReboot, authz.ResourceContext{}),
		"org-scope action satisfied by the org-scope ADMIN")
}

// Soft-deleted assignment is excluded by the SQL.
// Codex security regression (PR 2a HIGH): a site-scope role with
// zero permissions must still narrow the user's broader org-scope
// grant at that site. The earlier resolver used an INNER JOIN to
// role_permission and dropped rows for empty roles, which silently
// collapsed narrowing back to the org grant.
func TestResolver_ZeroPermissionSiteAssignmentStillNarrows(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()

	orgID := insertTestOrganization(t, db)
	siteA := insertTestSite(t, db, orgID)
	siteB := insertTestSite(t, db, orgID)
	userID := insertTestUser(t, db)
	require.NoError(t, authz.Reconcile(ctx, db))

	// Org-scope ADMIN grants miner:reboot everywhere by default.
	adminID := getBuiltinRoleID(t, db, orgID, "ADMIN")
	assignAssignment(t, db, userID, orgID, adminID, authz.ScopeOrg, sql.NullInt64{})

	// Create a custom role with zero permissions ("Site Lockdown")
	// and assign it at site A. The LEFT JOIN in the resolver SQL must
	// surface this assignment even though it grants nothing — without
	// it, narrowing at site A would silently fall back to ADMIN's
	// miner:reboot.
	lockdownID := createEmptyCustomRole(t, db, orgID, "Site Lockdown")
	assignAssignment(t, db, userID, orgID, lockdownID, authz.ScopeSite,
		sql.NullInt64{Int64: siteA, Valid: true})

	resolver := authz.NewPermissionResolver(db)
	eff, err := resolver.LoadEffective(ctx, userID, orgID)
	require.NoError(t, err)

	require.False(t, eff.Has(authz.PermMinerReboot, siteCtx(siteA)),
		"empty site-scope role at site A must narrow org-scope ADMIN there")
	require.False(t, eff.Has(authz.PermFleetRead, siteCtx(siteA)),
		"narrowing applies to every action key when the narrower role grants nothing")
	require.True(t, eff.Has(authz.PermMinerReboot, siteCtx(siteB)),
		"org grant still applies at site B (no narrower assignment)")
	// site:manage is in ADMIN's seed formula (AllPermissions() minus
	// user:* and role:manage); the org-scope grant must satisfy this
	// org-scoped action regardless of the empty site-scope assignment.
	require.True(t, eff.Has(authz.PermSiteManage, authz.ResourceContext{}),
		"org-scoped action satisfied by the org-scope ADMIN")
}

func TestResolver_SoftDeletedAssignmentIgnored(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()

	orgID := insertTestOrganization(t, db)
	userID := insertTestUser(t, db)
	require.NoError(t, authz.Reconcile(ctx, db))
	adminID := getBuiltinRoleID(t, db, orgID, "ADMIN")
	assignmentID := assignAssignment(t, db, userID, orgID, adminID, authz.ScopeOrg, sql.NullInt64{})

	resolver := authz.NewPermissionResolver(db)
	eff, err := resolver.LoadEffective(ctx, userID, orgID)
	require.NoError(t, err)
	require.True(t, eff.Has(authz.PermMinerReboot, authz.ResourceContext{}),
		"ADMIN holds miner:reboot before soft-delete")

	require.NoError(t, sqlc.New(db).UnassignRole(ctx, assignmentID))
	eff, err = resolver.LoadEffective(ctx, userID, orgID)
	require.NoError(t, err)
	require.False(t, eff.Has(authz.PermMinerReboot, authz.ResourceContext{}),
		"soft-deleted assignment must be ignored by the resolver")
}

// A user with no assignments in the org gets a non-nil empty value
// that denies everything — the fail-closed default for a deactivated
// user or a user who was never in this org.
func TestResolver_NoAssignmentsReturnsEmptyDenyAll(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()

	orgID := insertTestOrganization(t, db)
	userID := insertTestUser(t, db)
	require.NoError(t, authz.Reconcile(ctx, db))

	resolver := authz.NewPermissionResolver(db)
	eff, err := resolver.LoadEffective(ctx, userID, orgID)
	require.NoError(t, err)
	require.NotNil(t, eff)
	require.False(t, eff.Has(authz.PermFleetRead, authz.ResourceContext{}))
	require.False(t, eff.Has(authz.PermMinerBlinkLED, siteCtx(1)))
	require.Empty(t, eff.FlatKeys())
}

// ---------------------------------------------------------------
// helpers (test-only)
// ---------------------------------------------------------------

func siteCtx(id int64) authz.ResourceContext {
	return authz.ResourceContext{SiteID: &id}
}

func assignAssignment(t *testing.T, db *sql.DB, userID, orgID, roleID int64, scopeType authz.ScopeType, scopeID sql.NullInt64) int64 {
	t.Helper()
	row, err := sqlc.New(db).AssignRole(t.Context(), sqlc.AssignRoleParams{
		UserID:         userID,
		OrganizationID: orgID,
		RoleID:         roleID,
		ScopeType:      string(scopeType),
		ScopeID:        scopeID,
	})
	require.NoError(t, err)
	return row.ID
}

// createEmptyCustomRole creates a custom role with zero permissions
// attached. Used by the narrowing regression test to exercise the
// LEFT JOIN path that surfaces empty-role assignments.
func createEmptyCustomRole(t *testing.T, db *sql.DB, orgID int64, name string) int64 {
	t.Helper()
	id, err := sqlc.New(db).UpsertCustomRoleForOrg(t.Context(), sqlc.UpsertCustomRoleForOrgParams{
		Name:           name,
		Description:    sql.NullString{String: "no perms — narrowing lockdown", Valid: true},
		OrganizationID: sql.NullInt64{Int64: orgID, Valid: true},
	})
	require.NoError(t, err)
	return id
}
