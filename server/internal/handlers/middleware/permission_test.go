package middleware_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

// ctxWithInfo and ctxWithEffective build the request context the auth
// interceptor would produce, so the middleware unit tests can exercise
// the gate without spinning up Connect or the resolver.

func ctxWithInfo(info *session.Info) context.Context {
	return authn.SetInfo(context.Background(), info)
}

func ctxWithEffective(t *testing.T, info *session.Info, assignments ...authz.Assignment) context.Context {
	t.Helper()
	ctx := ctxWithInfo(info)
	return middleware.WithEffectivePermissions(ctx, authz.NewEffectivePermissions(assignments))
}

func userInfo() *session.Info {
	return &session.Info{
		AuthMethod:     session.AuthMethodSession,
		SessionID:      "sess-1",
		UserID:         1,
		OrganizationID: 1,
		ExternalUserID: "user-1",
		Username:       "alice",
	}
}

func orgAssignment(perms ...string) authz.Assignment {
	return authz.Assignment{
		AssignmentID: 1,
		ScopeType:    authz.ScopeOrg,
		Permissions:  perms,
	}
}

func siteAssignment(siteID int64, perms ...string) authz.Assignment {
	return authz.Assignment{
		AssignmentID: 2,
		ScopeType:    authz.ScopeSite,
		SiteID:       &siteID,
		Permissions:  perms,
	}
}

func siteRC(id int64) authz.ResourceContext {
	return authz.ResourceContext{SiteID: &id}
}

func TestRequirePermission_AllowsWhenEffectiveHasKey(t *testing.T) {
	ctx := ctxWithEffective(t, userInfo(), orgAssignment(authz.PermMinerReboot))

	info, err := middleware.RequirePermission(ctx, authz.PermMinerReboot, authz.ResourceContext{})
	require.NoError(t, err)
	require.Equal(t, "alice", info.Username)
}

func TestRequirePermission_DeniesWithStructuredPayload(t *testing.T) {
	ctx := ctxWithEffective(t, userInfo(), orgAssignment(authz.PermFleetRead))

	info, err := middleware.RequirePermission(ctx, authz.PermMinerReboot, siteRC(42))
	require.Error(t, err)
	require.Nil(t, info)
	require.Equal(t, connect.CodePermissionDenied, connectCode(t, err))

	// Payload shape: exactly {"required": "...", "scope": {"site_id": N}}.
	// No assignment ids, role names, or effective-permission lists.
	var payload struct {
		Required string         `json:"required"`
		Scope    map[string]any `json:"scope"`
	}
	require.NoError(t, json.Unmarshal([]byte(connectMessage(t, err)), &payload))
	require.Equal(t, authz.PermMinerReboot, payload.Required)
	require.Equal(t, map[string]any{"site_id": float64(42)}, payload.Scope,
		"scope echoes the caller's ResourceContext; site_id stored as JSON number")
}

func TestRequirePermission_DenialPayloadForOrgScopedAction(t *testing.T) {
	ctx := ctxWithEffective(t, userInfo() /* no perms */)

	_, err := middleware.RequirePermission(ctx, authz.PermUserManage, authz.ResourceContext{})
	require.Error(t, err)

	var payload struct {
		Required string         `json:"required"`
		Scope    map[string]any `json:"scope"`
	}
	require.NoError(t, json.Unmarshal([]byte(connectMessage(t, err)), &payload))
	require.Equal(t, authz.PermUserManage, payload.Required)
	require.Equal(t, map[string]any{}, payload.Scope,
		"org-scoped request produces an empty scope object; no nil, no null, no site_id key")
}

func TestRequirePermission_UnauthenticatedWhenNoSessionInfo(t *testing.T) {
	// Bare context — no session.Info attached. Mimics an unauthenticated
	// request reaching the gate by mistake.
	_, err := middleware.RequirePermission(context.Background(), authz.PermFleetRead, authz.ResourceContext{})
	require.Error(t, err)
	require.Equal(t, connect.CodeUnauthenticated, connectCode(t, err))
}

func TestRequirePermission_FailClosedOnMissingEffective(t *testing.T) {
	// session.Info present, but no EffectivePermissions stashed. This is
	// a wiring bug — the interceptor failed to populate the value. The
	// gate must NOT default to ALLOW.
	ctx := ctxWithInfo(userInfo())
	_, err := middleware.RequirePermission(ctx, authz.PermFleetRead, authz.ResourceContext{})
	require.Error(t, err)
	require.Equal(t, connect.CodeInternal, connectCode(t, err),
		"missing EffectivePermissions must surface as Internal, not ALLOW and not PermissionDenied")
}

func TestRequirePermission_SchedulerShortCircuitsToAllow(t *testing.T) {
	// Synthesized internal actor — no UserID, no EffectivePermissions
	// on context — must short-circuit to ALLOW without touching the
	// resolver state.
	info := &session.Info{
		AuthMethod:     session.AuthMethodSession,
		Actor:          session.ActorScheduler,
		OrganizationID: 1,
	}
	ctx := ctxWithInfo(info) // deliberately no WithEffectivePermissions

	got, err := middleware.RequirePermission(ctx, authz.PermMinerReboot, siteRC(99))
	require.NoError(t, err, "ActorScheduler must short-circuit before the EffectivePermissions check")
	require.Equal(t, session.ActorScheduler, got.Actor)
}

// Codex security regression (PR 2a MEDIUM): an unknown non-empty
// Actor must NOT bypass the gate. Allowlist only known constants
// (ActorScheduler, ActorCurtailment); fail closed for anything else.
func TestRequirePermission_UnknownActorDoesNotBypass(t *testing.T) {
	info := &session.Info{
		AuthMethod: session.AuthMethodSession,
		Actor:      session.Actor("future-orchestrator-typo"),
	}
	ctx := ctxWithInfo(info) // no EffectivePermissions stashed — bypass would mask this

	_, err := middleware.RequirePermission(ctx, authz.PermMinerReboot, siteRC(1))
	require.Error(t, err, "unknown Actor must not short-circuit to ALLOW")
	require.Equal(t, connect.CodeInternal, connectCode(t, err),
		"unknown Actor surfaces as Internal, not ALLOW and not PermissionDenied")
}

func TestRequirePermission_CurtailmentReconcilerActorAlsoAllowed(t *testing.T) {
	// Any non-empty Actor short-circuits — the gate trusts internal
	// orchestrators in general, not just the scheduler.
	info := &session.Info{
		AuthMethod: session.AuthMethodSession,
		Actor:      session.ActorCurtailment,
	}
	ctx := ctxWithInfo(info)

	_, err := middleware.RequirePermission(ctx, authz.PermCurtailmentManage, authz.ResourceContext{})
	require.NoError(t, err)
}

func TestFirmwareRolloutActorAllowedAcrossPermissionHelpers(t *testing.T) {
	info := &session.Info{
		AuthMethod:     session.AuthMethodSession,
		Actor:          session.ActorFirmwareRollout,
		OrganizationID: 1,
	}
	ctx := ctxWithInfo(info) // deliberately no WithEffectivePermissions

	got, err := middleware.RequirePermission(ctx, authz.PermMinerFirmwareUpdate, authz.ResourceContext{})
	require.NoError(t, err)
	require.Equal(t, session.ActorFirmwareRollout, got.Actor)

	got, err = middleware.RequireOrgWidePermission(ctx, authz.PermMinerFirmwareUpdate)
	require.NoError(t, err)
	require.Equal(t, session.ActorFirmwareRollout, got.Actor)

	allowed, err := middleware.HasPermission(ctx, authz.PermMinerFirmwareUpdate, authz.ResourceContext{})
	require.NoError(t, err)
	require.True(t, allowed)

	allowed, err = middleware.HasOrgWidePermission(ctx, authz.PermMinerFirmwareUpdate)
	require.NoError(t, err)
	require.True(t, allowed)

	orgWide, sites, err := middleware.SiteScopeForPermission(ctx, authz.PermMinerFirmwareUpdate)
	require.NoError(t, err)
	require.True(t, orgWide)
	require.Empty(t, sites)

	got, err = middleware.RequireAnyPermission(
		ctx,
		[]string{authz.PermMinerFirmwareUpdate, authz.PermFirmwareRolloutManage},
		authz.ResourceContext{},
	)
	require.NoError(t, err)
	require.Equal(t, session.ActorFirmwareRollout, got.Actor)
}

func TestRequirePermission_NarrowingAtSiteScope(t *testing.T) {
	// User has org-scope ADMIN (holds miner:reboot) plus a site-scope
	// FIELD_TECH at site 1 (no miner:reboot). At site 1, narrowing
	// applies and miner:reboot is denied. At site 2, the org grant
	// uncovered applies and miner:reboot is allowed.
	ctx := ctxWithEffective(t, userInfo(),
		orgAssignment(authz.PermFleetRead, authz.PermMinerReboot),
		siteAssignment(1, authz.PermFleetRead, authz.PermMinerBlinkLED),
	)

	_, err := middleware.RequirePermission(ctx, authz.PermMinerReboot, siteRC(1))
	require.Error(t, err, "narrowing: site 1 FIELD_TECH overrides org ADMIN at that site")
	require.Equal(t, connect.CodePermissionDenied, connectCode(t, err))

	_, err = middleware.RequirePermission(ctx, authz.PermMinerReboot, siteRC(2))
	require.NoError(t, err, "site 2 falls back to the org grant")
}

func TestHasOrgWidePermissionHonorsSiteNarrowing(t *testing.T) {
	ctx := ctxWithEffective(t, userInfo(),
		orgAssignment(authz.PermMinerRename),
		siteAssignment(1, authz.PermFleetRead),
	)

	got, err := middleware.HasOrgWidePermission(ctx, authz.PermMinerRename)
	require.NoError(t, err)
	require.False(t, got, "site-scoped narrowing means the permission is not org-wide")
}

// ---------------------------------------------------------------
// RequireAnyPermission
// ---------------------------------------------------------------

func TestSiteScopeForPermission_ProjectsAllowlistAndDenylist(t *testing.T) {
	// Site-scoped-only caller: allowlist of granting sites.
	ctx := ctxWithEffective(t, userInfo(),
		siteAssignment(10, authz.PermSiteRead),
		siteAssignment(11, authz.PermFleetRead))
	orgWide, sites, err := middleware.SiteScopeForPermission(ctx, authz.PermSiteRead)
	require.NoError(t, err)
	require.False(t, orgWide)
	require.Equal(t, []int64{10}, sites)

	// Org-wide caller narrowed away at one site: denylist.
	ctx = ctxWithEffective(t, userInfo(),
		orgAssignment(authz.PermSiteRead),
		siteAssignment(11, authz.PermFleetRead))
	orgWide, sites, err = middleware.SiteScopeForPermission(ctx, authz.PermSiteRead)
	require.NoError(t, err)
	require.True(t, orgWide)
	require.Equal(t, []int64{11}, sites)
}

func TestSiteScopeForPermission_InternalActorIsOrgWide(t *testing.T) {
	info := &session.Info{
		AuthMethod:     session.AuthMethodSession,
		Actor:          session.ActorCurtailment,
		OrganizationID: 1,
	}
	ctx := ctxWithInfo(info) // deliberately no WithEffectivePermissions

	orgWide, sites, err := middleware.SiteScopeForPermission(ctx, authz.PermSiteRead)
	require.NoError(t, err, "allowlisted internal actor must short-circuit before the EffectivePermissions check")
	require.True(t, orgWide)
	require.Empty(t, sites)
}

func TestSiteScopeForPermission_UnknownActorDoesNotBypass(t *testing.T) {
	info := &session.Info{
		AuthMethod: session.AuthMethodSession,
		Actor:      session.Actor("future-orchestrator-typo"),
	}
	ctx := ctxWithInfo(info)

	_, _, err := middleware.SiteScopeForPermission(ctx, authz.PermSiteRead)
	require.Error(t, err, "unknown Actor must not short-circuit to org-wide")
	require.Equal(t, connect.CodeInternal, connectCode(t, err))
}

func TestSiteScopeForPermission_FailClosedOnMissingEffective(t *testing.T) {
	ctx := ctxWithInfo(userInfo())
	_, _, err := middleware.SiteScopeForPermission(ctx, authz.PermSiteRead)
	require.Error(t, err)
	require.Equal(t, connect.CodeInternal, connectCode(t, err),
		"missing EffectivePermissions must surface as Internal, not an org-wide grant")
}

func TestSiteScopeForPermission_UnauthenticatedWhenNoSessionInfo(t *testing.T) {
	_, _, err := middleware.SiteScopeForPermission(context.Background(), authz.PermSiteRead)
	require.Error(t, err)
	require.Equal(t, connect.CodeUnauthenticated, connectCode(t, err))
}

func TestRequireAnyPermission_AllowsWhenFirstKeyMatches(t *testing.T) {
	ctx := ctxWithEffective(t, userInfo(), orgAssignment(authz.PermRoleManage))

	info, err := middleware.RequireAnyPermission(ctx,
		[]string{authz.PermRoleManage, authz.PermUserManage}, authz.ResourceContext{})
	require.NoError(t, err)
	require.Equal(t, "alice", info.Username)
}

func TestRequireAnyPermission_AllowsWhenSecondKeyMatches(t *testing.T) {
	// The motivating case: built-in ADMIN holds user:manage but NOT
	// role:manage, and must still be able to load the assignable-role
	// list for the AddTeamMember modal.
	ctx := ctxWithEffective(t, userInfo(), orgAssignment(authz.PermUserManage))

	info, err := middleware.RequireAnyPermission(ctx,
		[]string{authz.PermRoleManage, authz.PermUserManage}, authz.ResourceContext{})
	require.NoError(t, err)
	require.Equal(t, "alice", info.Username)
}

func TestRequireAnyPermission_DeniesWhenNoKeyMatches(t *testing.T) {
	ctx := ctxWithEffective(t, userInfo(), orgAssignment(authz.PermFleetRead))

	info, err := middleware.RequireAnyPermission(ctx,
		[]string{authz.PermRoleManage, authz.PermUserManage}, authz.ResourceContext{})
	require.Error(t, err)
	require.Nil(t, info)
	require.Equal(t, connect.CodePermissionDenied, connectCode(t, err))

	// Denial payload reports the primary key (the first entry in the
	// slice) so the existing structured-payload contract is stable.
	var payload struct {
		Required string         `json:"required"`
		Scope    map[string]any `json:"scope"`
	}
	require.NoError(t, json.Unmarshal([]byte(connectMessage(t, err)), &payload))
	require.Equal(t, authz.PermRoleManage, payload.Required,
		"denial echoes the primary key (first in slice)")
	require.Equal(t, map[string]any{}, payload.Scope)
}

func TestRequireAnyPermission_UnauthenticatedWhenNoSessionInfo(t *testing.T) {
	_, err := middleware.RequireAnyPermission(context.Background(),
		[]string{authz.PermRoleManage, authz.PermUserManage}, authz.ResourceContext{})
	require.Error(t, err)
	require.Equal(t, connect.CodeUnauthenticated, connectCode(t, err))
}

func TestRequireAnyPermission_FailClosedOnMissingEffective(t *testing.T) {
	ctx := ctxWithInfo(userInfo())
	_, err := middleware.RequireAnyPermission(ctx,
		[]string{authz.PermRoleManage, authz.PermUserManage}, authz.ResourceContext{})
	require.Error(t, err)
	require.Equal(t, connect.CodeInternal, connectCode(t, err))
}

func TestRequireAnyPermission_FailClosedOnEmptyKeys(t *testing.T) {
	// Programming error — must fail closed rather than silently allow.
	ctx := ctxWithEffective(t, userInfo(), orgAssignment(authz.PermRoleManage))
	_, err := middleware.RequireAnyPermission(ctx, nil, authz.ResourceContext{})
	require.Error(t, err)
	require.Equal(t, connect.CodeInternal, connectCode(t, err),
		"empty keys must surface as Internal, not ALLOW and not PermissionDenied")
}

func TestRequireAnyPermission_SchedulerShortCircuitsToAllow(t *testing.T) {
	info := &session.Info{
		AuthMethod:     session.AuthMethodSession,
		Actor:          session.ActorScheduler,
		OrganizationID: 1,
	}
	ctx := ctxWithInfo(info)

	got, err := middleware.RequireAnyPermission(ctx,
		[]string{authz.PermRoleManage, authz.PermUserManage}, authz.ResourceContext{})
	require.NoError(t, err)
	require.Equal(t, session.ActorScheduler, got.Actor)
}

func TestRequireAnyPermission_UnknownActorDoesNotBypass(t *testing.T) {
	info := &session.Info{
		AuthMethod: session.AuthMethodSession,
		Actor:      session.Actor("future-orchestrator-typo"),
	}
	ctx := ctxWithInfo(info)

	_, err := middleware.RequireAnyPermission(ctx,
		[]string{authz.PermRoleManage, authz.PermUserManage}, authz.ResourceContext{})
	require.Error(t, err)
	require.Equal(t, connect.CodeInternal, connectCode(t, err))
}

// ---------------------------------------------------------------
// helpers
// ---------------------------------------------------------------

// connectCode extracts the Connect status code from a FleetError. The
// fleeterror.FleetError type is the one all middleware errors wrap, so
// the code is reachable without unwrapping into Connect's error type.
func connectCode(t *testing.T, err error) connect.Code {
	t.Helper()
	var fe fleeterror.FleetError
	require.True(t, errors.As(err, &fe), "expected FleetError, got %T", err)
	return fe.GRPCCode
}

// connectMessage returns the FleetError's debug message, which is what
// the middleware stuffs the JSON payload into. The plan specifies the
// payload shape directly in the message body so the client can pick
// it up via Connect's standard error.Message().
func connectMessage(t *testing.T, err error) string {
	t.Helper()
	var fe fleeterror.FleetError
	require.True(t, errors.As(err, &fe), "expected FleetError, got %T", err)
	return fe.DebugMessage
}
