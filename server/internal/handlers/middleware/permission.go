package middleware

import (
	"context"
	"encoding/json"

	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
)

// effectivePermissionsCtxKey is a private context key for stashing the
// per-request EffectivePermissions value produced by the auth
// interceptor. Using an unexported type avoids collisions with any
// other package that uses context.WithValue.
type effectivePermissionsCtxKey struct{}

// WithEffectivePermissions returns a derived context carrying the
// per-request EffectivePermissions. The auth interceptor calls this
// once after session.Info is populated; handlers (and the
// RequirePermission middleware below) read the value back via the
// private accessor.
//
// Passing nil is a programming error — the interceptor must always
// produce a non-nil value, even when the user has no live assignments
// (in that case the resolver returns an empty *EffectivePermissions
// that denies everything). RequirePermission treats a nil context
// value as a fail-closed Internal error so a misconfigured
// interceptor cannot accidentally grant access.
func WithEffectivePermissions(ctx context.Context, eff *authz.EffectivePermissions) context.Context {
	return context.WithValue(ctx, effectivePermissionsCtxKey{}, eff)
}

// effectivePermissionsFromContext returns the stashed value or nil.
func effectivePermissionsFromContext(ctx context.Context) *authz.EffectivePermissions {
	eff, _ := ctx.Value(effectivePermissionsCtxKey{}).(*authz.EffectivePermissions)
	return eff
}

// RequirePermission gates a handler on the named permission key
// against the caller-supplied resource context. It is the runtime
// counterpart of the existing RequireAdmin gate, which it replaces
// handler-by-handler as call sites migrate to the permission model.
//
// Returns the session.Info for handlers that need the caller's
// identity, or one of:
//   - Connect Unauthenticated  — no session.Info on context.
//   - Connect Internal         — fail-closed; EffectivePermissions is
//     missing on context, which means the interceptor wiring is broken
//     for this request path. ALLOW is never the fail-closed default.
//   - Connect PermissionDenied — the caller is authenticated but the
//     resolver says they cannot perform this action against this
//     resource scope. The error carries a structured payload echoing
//     the caller's request: {"required": key, "scope": {site_id: N}}.
//     The scope field is the caller's input only — it never includes
//     server-side assignment IDs, role names, or the caller's
//     effective permission list.
//
// Synthesized internal actors (session.Info.Actor != "") short-circuit
// to ALLOW without consulting EffectivePermissions. The scheduler and
// curtailment-reconciler synthesize a session.Info without a real
// UserID/OrganizationID; they're trusted by virtue of running
// in-process, and they have no rows in user_organization_role to
// resolve against.
//
// Revocation latency: the resolver runs once per request and caches
// the result on the context. An in-flight unary RPC acts under the
// permission set loaded at the start of the request — the window is
// sub-second. Long-running RPCs (firmware update, log download,
// streaming responses) should re-invoke RequirePermission between
// significant side-effects so revocation propagates within a single
// streaming session; this is the handler's responsibility, not the
// middleware's.
//
// TODO: every current caller passes authz.ResourceContext{} because
// the migrated handlers are all org-scoped. The first site-scoped
// migration (miner actions, rack ops, log download) should add a
// shared helper — e.g. siteResourceForMiner(ctx, minerID) — rather
// than inlining the miner_id → site_id lookup at each callsite. Drop
// this TODO once the helper exists.
func RequirePermission(ctx context.Context, key string, rc authz.ResourceContext) (*session.Info, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, fleeterror.NewUnauthenticatedError("authentication required")
	}

	// Synthesized actor short-circuit. Internal orchestrators trust
	// themselves by virtue of running in-process; LoadEffective is
	// never called for them and EffectivePermissions is absent from
	// their context.
	//
	// Allowlist explicitly rather than "any non-empty Actor" — a
	// future mistyped or user-influenced value must NOT be a bypass.
	// An unknown non-empty Actor fails closed with Internal so the
	// problem surfaces immediately rather than silently granting
	// access.
	if info.Actor != "" {
		switch info.Actor {
		case session.ActorScheduler, session.ActorCurtailment:
			return info, nil
		default:
			return nil, fleeterror.NewInternalErrorf(
				"authz: unknown internal actor %q; refusing to short-circuit RBAC",
				info.Actor,
			)
		}
	}

	eff := effectivePermissionsFromContext(ctx)
	if eff == nil {
		// Fail-closed: an authenticated request reached a permission
		// check without the resolver running. This is a wiring bug —
		// surface it loudly rather than silently allowing.
		return nil, fleeterror.NewInternalError(
			"authz: effective permissions missing from request context; auth interceptor wiring is broken",
		)
	}

	if !eff.Has(key, rc) {
		return nil, permissionDeniedError(key, rc)
	}
	return info, nil
}

// permissionDeniedError builds a Connect PermissionDenied error whose
// body is the structured payload the plan specifies:
//
//	{"required": "<permission_key>", "scope": {"site_id": <N>}}
//
// The scope sub-object reflects the caller's ResourceContext: a nil
// SiteID produces an empty object so the response shape is consistent
// for both org-scoped and site-scoped requests.
func permissionDeniedError(key string, rc authz.ResourceContext) error {
	payload := struct {
		Required string         `json:"required"`
		Scope    map[string]any `json:"scope"`
	}{
		Required: key,
		Scope:    scopeMap(rc),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		// json.Marshal on this concrete shape can't fail in practice
		// (no unsupported types). Fall back to a plain message rather
		// than panic so a future refactor doesn't crash the gate.
		return fleeterror.NewForbiddenError("permission denied")
	}
	return fleeterror.NewForbiddenError(string(body))
}

func scopeMap(rc authz.ResourceContext) map[string]any {
	out := map[string]any{}
	if rc.SiteID != nil {
		out["site_id"] = *rc.SiteID
	}
	return out
}
